package device

import (
	"fmt"
	"strings"
	"sync"

	"github.com/damonto/euicc-go/driver"
	"github.com/damonto/euicc-go/lpa"
	"github.com/yuanshuai1122/vodog/internal/config"
	"github.com/yuanshuai1122/vodog/internal/esim"
	"github.com/yuanshuai1122/vodog/internal/pcsc"
	"github.com/yuanshuai1122/vodog/pkg/logger"
)

func (p *Pool) occupancy() *pcsc.Occupancy {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.readerOccupancy == nil {
		p.readerOccupancy = pcsc.NewOccupancy()
	}
	return p.readerOccupancy
}

func (p *Pool) startPCSCReaderWorker(devCfg config.DeviceConfig) (*Worker, error) {
	name := strings.TrimSpace(devCfg.ReaderName)
	if name == "" {
		return nil, fmt.Errorf("pcsc 设备必须填写 reader_name")
	}
	devCfg.DeviceBackend = config.DeviceBackendPCSC
	devCfg.NetworkEnabled = false
	devCfg.VoWiFiEnabled = false
	devCfg.SMSEnabled = true

	w := &Worker{
		ID:     devCfg.ID,
		Config: devCfg,
		Pool:   p,
		stop:   make(chan struct{}),
	}
	w.state.Runtime.Ready = true
	w.state.Meta.Healthy = true

	occ := p.occupancy()
	holder := pcsc.Holder{DeviceID: devCfg.ID, Kind: pcsc.KindReader, ReaderName: name}
	w.EsimMgr = esim.NewManagerWithChannelFactory(devCfg.ID, func(aid []byte) (*lpa.Client, error) {
		if err := occ.Acquire(holder); err != nil {
			return nil, err
		}
		ch := &occupiedChannel{
			SmartCardChannel: pcsc.NewChannel(name),
			release:          func() { occ.Release(devCfg.ID) },
		}
		client, err := esim.NewClientWithChannel(ch, aid)
		if err != nil {
			return nil, err
		}
		return client, nil
	}, nil, nil, nil)

	p.mu.Lock()
	p.workers[devCfg.ID] = w
	p.mu.Unlock()

	logger.Info("已添加 PC/SC 读卡器设备", "device", devCfg.ID, "reader", name)
	return w, nil
}

// occupiedChannel 在 LPA 关掉通道时放下读卡器占用。
type occupiedChannel struct {
	driver.SmartCardChannel
	release func()
	once    sync.Once
}

func (c *occupiedChannel) Disconnect() error {
	var err error
	if c.SmartCardChannel != nil {
		err = c.SmartCardChannel.Disconnect()
	}
	c.once.Do(func() {
		if c.release != nil {
			c.release()
		}
	})
	return err
}

// HoldModemESIM 在模组走 eSIM APDU 前占用当前 ICCID，避免和读卡器同时摸同一张卡。
func (p *Pool) HoldModemESIM(deviceID, iccid string) error {
	if p == nil {
		return nil
	}
	iccid = strings.TrimSpace(iccid)
	if iccid == "" {
		return nil
	}
	return p.occupancy().Acquire(pcsc.Holder{DeviceID: deviceID, Kind: pcsc.KindModem, ICCID: iccid})
}

func (p *Pool) ReleaseESIMHold(deviceID string) {
	if p == nil {
		return
	}
	p.occupancy().Release(deviceID)
}
