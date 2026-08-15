package device

import (
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/boa-z/vowifi-go/runtimehost"
	"github.com/boa-z/vowifi-go/runtimehost/identity"
	"github.com/yuanshuai1122/vodoge/internal/config"
	"github.com/yuanshuai1122/vodoge/internal/pcsc"
	innersim "github.com/yuanshuai1122/vodoge/internal/sim"
	"github.com/yuanshuai1122/vodoge/pkg/logger"
)

// pcscModemAdapter 把读卡器当成 VoWiFi 的 SIM 入口：无射频，AKA / 身份走 PC/SC。
type pcscModemAdapter struct {
	deviceID string
	reader   string
	imei     string
	ch       *pcsc.Channel
	occ      *pcsc.Occupancy
	mu       sync.Mutex
}

func newPCSCModemAdapter(w *Worker) (*pcscModemAdapter, error) {
	if w == nil {
		return nil, fmt.Errorf("worker 为空")
	}
	name := strings.TrimSpace(w.Config.ReaderName)
	if name == "" {
		return nil, fmt.Errorf("pcsc 设备必须填写 reader_name")
	}
	imei := strings.TrimSpace(w.Config.ModemIMEI)
	if config.NormalizeIMEI(imei) == "" {
		imei = pcsc.DerivedIMEI(name)
	}
	var occ *pcsc.Occupancy
	if w.Pool != nil {
		occ = w.Pool.occupancy()
	}
	ch := pcsc.NewChannel(name)
	if occ != nil {
		reader := name
		ch.SetGate(func() func() { return occ.GuardReader(reader) })
	}
	return &pcscModemAdapter{
		deviceID: w.ID,
		reader:   name,
		imei:     imei,
		ch:       ch,
		occ:      occ,
	}, nil
}

func (a *pcscModemAdapter) ensureConnected() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ch == nil {
		a.ch = pcsc.NewChannel(a.reader)
	}
	return a.ch.Connect()
}

func (a *pcscModemAdapter) DeviceID() string { return a.deviceID }
func (a *pcscModemAdapter) IsHealthy() bool  { return true }
func (a *pcscModemAdapter) IsSimInserted() bool {
	ok, err := a.QuerySIMInserted()
	return err == nil && ok
}
func (a *pcscModemAdapter) QuerySIMInserted() (bool, error) {
	if err := a.ensureConnected(); err != nil {
		if err == pcsc.ErrNoCard {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
func (a *pcscModemAdapter) GetRegStatus() (int, string) {
	return 0, "读卡器无蜂窝射频，仅 IMS"
}
func (a *pcscModemAdapter) GetNetworkMode() string { return "wifi" }
func (a *pcscModemAdapter) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ch != nil {
		_ = a.ch.Disconnect()
	}
}

func (a *pcscModemAdapter) ExecuteATSilent(string, time.Duration) (string, error) {
	return "", fmt.Errorf("读卡器没有 AT 口")
}

func (a *pcscModemAdapter) OpenLogicalChannel(aid string) (int, error) {
	if err := a.ensureConnected(); err != nil {
		return 0, err
	}
	raw, err := pcsc.DecodeHexAID(aid)
	if err != nil {
		return 0, err
	}
	n, err := a.ch.OpenLogicalChannel(raw)
	return int(n), err
}

func (a *pcscModemAdapter) CloseLogicalChannel(channel int) error {
	if a.ch == nil {
		return nil
	}
	return a.ch.CloseLogicalChannel(byte(channel))
}

func (a *pcscModemAdapter) TransmitAPDU(channel int, hexAPDU string) (string, error) {
	if err := a.ensureConnected(); err != nil {
		return "", err
	}
	cmd, err := hex.DecodeString(strings.TrimSpace(hexAPDU))
	if err != nil {
		return "", err
	}
	if channel > 0 && len(cmd) > 0 && cmd[0]&0x03 == 0 && channel < 4 {
		cmd = append([]byte(nil), cmd...)
		cmd[0] = (cmd[0] & 0xFC) | byte(channel)
	}
	resp, err := a.ch.Transmit(cmd)
	if err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(resp)), nil
}

func (a *pcscModemAdapter) GetISIMIdentity() (identity.Identity, error) {
	return identity.Identity{}, fmt.Errorf("读卡器先走 USIM 身份")
}

func (a *pcscModemAdapter) ReadICCID() (string, error) {
	if err := a.ensureConnected(); err != nil {
		return "", err
	}
	return a.ch.ReadICCID()
}

func (a *pcscModemAdapter) ReadIMSI() (string, error) {
	if err := a.ensureConnected(); err != nil {
		return "", err
	}
	return a.ch.ReadIMSI()
}

func (a *pcscModemAdapter) IMEI() string { return a.imei }

// ResolveLogicalChannelAID 给 ATAKAProvider 一个完整 AID。读卡器没有 QMI/MBIM 应用列表。
func (a *pcscModemAdapter) ResolveLogicalChannelAID(app string, fallbackAID string) (string, string, error) {
	app = strings.ToLower(strings.TrimSpace(app))
	fallback := strings.ToUpper(strings.TrimSpace(fallbackAID))
	switch app {
	case "usim":
		return "A0000000871002FF86FFFF89FFFFFFFF", "pcsc_static", nil
	case "isim":
		return "A0000000871004FF86FFFF89FFFFFFFF", "pcsc_static", nil
	}
	if fallback != "" {
		return fallback, "fallback", nil
	}
	return "", "", fmt.Errorf("读卡器无法解析 %s AID", app)
}

var (
	_ runtimehost.Modem = (*pcscModemAdapter)(nil)
	_ innersim.ATModem  = (*pcscModemAdapter)(nil)
)

func seedPCSCIdentity(w *Worker) {
	if w == nil {
		return
	}
	adapter, err := newPCSCModemAdapter(w)
	if err != nil {
		return
	}
	defer adapter.Stop()
	iccid, err1 := adapter.ReadICCID()
	imsi, err2 := adapter.ReadIMSI()
	w.cacheMu.Lock()
	if err1 == nil && iccid != "" {
		w.state.Identity.ICCID = iccid
	}
	if err2 == nil && imsi != "" {
		w.state.Identity.IMSI = imsi
		if len(imsi) >= 5 {
			w.state.Identity.NativeMCC = imsi[:3]
			if len(imsi) >= 6 {
				w.state.Identity.NativeMNC = imsi[3:5]
			}
		}
	}
	if w.state.Identity.IMEI == "" {
		w.state.Identity.IMEI = adapter.IMEI()
	}
	w.state.Identity.Ready = w.state.Identity.IMSI != ""
	w.cacheMu.Unlock()
	if err1 != nil || err2 != nil {
		logger.Debug("读卡器身份预读未完成", "device", w.ID, "iccid_err", err1, "imsi_err", err2)
	}
}

func (w *Worker) refreshPCSCIdentityLive(reason string) (liveSIMIdentityRefreshResult, error) {
	if w == nil {
		return liveSIMIdentityRefreshResult{}, fmt.Errorf("worker_nil")
	}
	adapter, err := newPCSCModemAdapter(w)
	if err != nil {
		return liveSIMIdentityRefreshResult{}, err
	}
	defer adapter.Stop()

	iccid, err1 := adapter.ReadICCID()
	imsi, err2 := adapter.ReadIMSI()
	iccid = strings.TrimSpace(iccid)
	imsi = strings.TrimSpace(imsi)
	if iccid == "" && imsi == "" {
		if err1 != nil {
			return liveSIMIdentityRefreshResult{}, fmt.Errorf("live_identity_empty: %w", err1)
		}
		if err2 != nil {
			return liveSIMIdentityRefreshResult{}, fmt.Errorf("live_identity_empty: %w", err2)
		}
		return liveSIMIdentityRefreshResult{}, fmt.Errorf("live_identity_empty")
	}

	now := time.Now()
	w.cacheMu.Lock()
	if iccid != "" {
		w.state.Identity.ICCID = iccid
	}
	if imsi != "" {
		w.state.Identity.IMSI = imsi
		if len(imsi) >= 5 {
			w.state.Identity.NativeMCC = imsi[:3]
			if len(imsi) >= 6 {
				w.state.Identity.NativeMNC = imsi[3:5]
			}
		}
	}
	if w.state.Identity.IMEI == "" {
		w.state.Identity.IMEI = adapter.IMEI()
	}
	w.state.Identity.Ready = true
	w.state.Identity.Phase = simIdentityPhaseReady
	w.state.Identity.TargetICCID = ""
	w.state.Identity.LastReason = strings.TrimSpace(reason)
	w.state.Identity.LastError = ""
	w.state.Meta.IdentityUpdatedAt = now
	w.state.Meta.UpdatedAt = now
	w.cacheMu.Unlock()
	return liveSIMIdentityRefreshResult{ICCID: iccid, IMSI: imsi}, nil
}
