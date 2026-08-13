// 设备概览的 DTO 与构建逻辑。
//
// 概览要把三处状态合成一份：配置（用户意图）、模组状态（硬件事实）、
// 生命周期快照（进程在哪一步）。三者不一致是常态，取舍规则都在这里。
package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/boa-z/vowifi-go/runtimehost"
	"github.com/yuanshuai1122/vohive/internal/backend"
	"github.com/yuanshuai1122/vohive/internal/config"
	"github.com/yuanshuai1122/vohive/internal/db"
	"github.com/yuanshuai1122/vohive/internal/device"
	"github.com/yuanshuai1122/vohive/internal/e911"
	"github.com/yuanshuai1122/vohive/internal/modem"

	"github.com/gin-gonic/gin"
)

type deviceMgmtOverviewItem struct {
	ID                     string             `json:"id"`
	Name                   string             `json:"name"`
	Running                bool               `json:"running"`
	Healthy                bool               `json:"healthy"`
	ControlOnline          bool               `json:"control_online"`
	PhysicalPresent        bool               `json:"physical_present"`
	WorkerRunning          bool               `json:"worker_running"`
	DataConnected          bool               `json:"data_connected"`
	RadioRegistered        bool               `json:"radio_registered"`
	LifecyclePhase         string             `json:"lifecycle_phase"`
	LifecycleReason        string             `json:"lifecycle_reason,omitempty"`
	PrivateIP              string             `json:"private_ip,omitempty"`
	PrivateIPv6            string             `json:"private_ipv6,omitempty"`
	PublicIP               string             `json:"public_ip"`
	PublicIPv6             string             `json:"public_ipv6,omitempty"`
	Config                 *deviceConfigDTO   `json:"config,omitempty"`
	Modem                  modem.DeviceStatus `json:"modem"`
	Traffic                map[string]string  `json:"traffic,omitempty"`
	TrafficRaw             map[string]int64   `json:"traffic_raw,omitempty"`
	TrafficMeta            *deviceTrafficMeta `json:"traffic_meta,omitempty"`
	BackendMode            string             `json:"backend_mode,omitempty"`
	NetworkConnected       bool               `json:"network_connected"`
	RegistrationStateLabel string             `json:"registration_state_label"`
	// Interface / ControlDevice / ATPort / USBPath 是 worker 运行时解析出的当前路径
	// (零路径持久化后不入库),前端据此显示与判定 QMI 后端可用性、流量接口,
	// 不再依赖持久化 config 的路径字段。
	Interface     string `json:"interface,omitempty"`
	ControlDevice string `json:"control_device,omitempty"`
	ATPort        string `json:"at_port,omitempty"`
	USBPath       string `json:"usb_path,omitempty"`
}

func modemSummaryStatus(status modem.DeviceStatus) modem.DeviceStatus {
	status.GID1 = ""
	status.GID2 = ""
	status.PNN = nil
	status.OPL = nil
	status.SIMServiceTable = nil
	return status
}

func registrationStateLabel(regStatus int) string {
	switch regStatus {
	case 1, 5:
		return "registered"
	case 2:
		return "searching"
	case 3:
		return "denied"
	default:
		return "unknown"
	}
}

// overviewDisplayConfig 合并设备的展示配置:身份与用户意图取自持久化 config,
// 而 interface / control_device / at_port 等运行时路径取自 worker 内存 config。
// 零路径持久化后持久化侧已不含这些路径,必须用运行时真实值展示,否则在线设备会被
// 误判为“未探测到数据控制端”、流量按空接口名也统计不到。
// cardPolicyVoWiFiEnabled 从卡策略读取用户意图，ICCID 为空或查不到时降级用 fallback。
func (s *Server) cardPolicyVoWiFiEnabled(iccid string, fallback bool) bool {
	if iccid == "" {
		return fallback
	}
	pol, err := s.data().CardPolicy.Get(iccid)
	if err != nil {
		return fallback
	}
	return pol.VoWiFiEnabled
}

func overviewDisplayConfig(runtime, persisted config.DeviceConfig, hasPersisted bool) config.DeviceConfig {
	if !hasPersisted {
		return runtime
	}
	cfg := persisted
	cfg.Interface = runtime.Interface
	cfg.ControlDevice = runtime.ControlDevice
	cfg.ATPort = runtime.ATPort
	cfg.ManagePort = runtime.ManagePort
	cfg.USBPath = runtime.USBPath
	cfg.QMIDevice = runtime.QMIDevice
	cfg.AudioDevice = runtime.AudioDevice
	// 策略字段（network/vowifi/airplane/ip/apn）已改为跟卡走、只存在于运行时投影，
	// 不再来自 persisted(config.yaml)。必须取 runtime，否则概览显示恒为 off。
	cfg.NetworkEnabled = runtime.NetworkEnabled
	cfg.VoWiFiEnabled = runtime.VoWiFiEnabled
	cfg.AirplaneEnabled = runtime.AirplaneEnabled
	cfg.IPVersion = runtime.IPVersion
	cfg.APN = runtime.APN
	// SMS 是系统不变量（恒开），不随卡策略/投影时序变化，直接置真，
	// 否则 worker 投影完成前或离线时 sms_enabled 为 false，会被短信中心设备过滤掉。
	cfg.SMSEnabled = true
	return cfg
}

func (s *Server) handleDeviceMgmtOverview(c *gin.Context) {
	includeConfig := strings.TrimSpace(c.DefaultQuery("include_config", "1")) != "0"
	workers := s.pool.GetAllWorkers()
	managed := config.ListDevices()
	cfgByID := map[string]config.DeviceConfig{}
	for _, d := range managed {
		cfgByID[d.ID] = d
	}
	tagByID := map[string]string{}
	tags := make([]string, 0, len(workers))
	for _, w := range workers {
		cfg := w.Config
		if v, ok := cfgByID[w.ID]; ok {
			cfg = overviewDisplayConfig(w.Config, v, true)
		}
		if cfg.Interface == "" {
			continue
		}
		tag := w.ID + "@" + cfg.Interface
		tagByID[w.ID] = tag
		tags = append(tags, tag)
	}
	byTag, _ := s.data().Traffic.LatestMinuteDeltasBatch("iface", tags)
	now := time.Now()

	workerByID := map[string]bool{}
	items := make([]deviceMgmtOverviewItem, 0, len(workers))
	for _, w := range workers {
		workerByID[w.ID] = true
		cfg := w.Config
		if v, ok := cfgByID[w.ID]; ok {
			cfg = overviewDisplayConfig(w.Config, v, true)
		}
		status := w.GetCachedDeviceStatus() // 设备管理总览列表读缓存，0 IPC
		controlOnline := w.GetCachedHealthy()
		item := deviceMgmtOverviewItem{
			ID:                     w.ID,
			Name:                   cfg.Name,
			Running:                true,
			Healthy:                controlOnline, // 兼容旧客户端：healthy 表示控制面在线
			ControlOnline:          controlOnline,
			PublicIP:               w.GetCachedIP(),
			PublicIPv6:             w.GetCachedIPv6(),
			Modem:                  modemSummaryStatus(status),
			NetworkConnected:       w.NetworkConnected(),
			RegistrationStateLabel: registrationStateLabel(status.RegStatus),
			BackendMode: func() string {
				if w.Backend != nil {
					return w.Backend.Mode()
				}
				return "at"
			}(),
		}
		item.Interface = cfg.Interface
		item.ControlDevice = cfg.ControlDevice
		item.ATPort = w.ResolvedATPort()
		item.USBPath = cfg.USBPath
		if includeConfig {
			dto := deviceConfigToDTO(cfg)
			item.Config = &dto
		}
		if nc := w.NetworkController(); nc != nil {
			item.PrivateIP = nc.GetPrivateIP()
			item.PrivateIPv6 = nc.GetPrivateIPv6()
		}
		item.Traffic, item.TrafficRaw, item.TrafficMeta = buildTrafficOverviewFields(cfg.Interface, byTag[tagByID[w.ID]], now)
		s.applyLifecycleToOverviewItem(&item, true, cfg)
		items = append(items, item)
	}
	for _, dc := range managed {
		if workerByID[dc.ID] {
			continue
		}
		var cfgDTO *deviceConfigDTO
		if includeConfig {
			dto := deviceConfigToDTO(dc)
			cfgDTO = &dto
		}
		item := deviceMgmtOverviewItem{
			ID:                     dc.ID,
			Name:                   dc.Name,
			Running:                false,
			Healthy:                false,
			ControlOnline:          false,
			PublicIP:               "",
			Config:                 cfgDTO,
			Modem:                  modem.DeviceStatus{},
			Traffic:                nil,
			BackendMode:            resolveOfflineBackendMode(dc),
			NetworkConnected:       false,
			RegistrationStateLabel: registrationStateLabel(0),
		}
		s.applyLifecycleToOverviewItem(&item, false, dc)
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"devices": items})
}

type deviceMgmtOverviewLiteItem struct {
	ID                     string             `json:"id"`
	Name                   string             `json:"name"`
	Running                bool               `json:"running"`
	Healthy                bool               `json:"healthy"`
	ControlOnline          bool               `json:"control_online"`
	PhysicalPresent        bool               `json:"physical_present"`
	WorkerRunning          bool               `json:"worker_running"`
	DataConnected          bool               `json:"data_connected"`
	RadioRegistered        bool               `json:"radio_registered"`
	LifecyclePhase         string             `json:"lifecycle_phase"`
	LifecycleReason        string             `json:"lifecycle_reason,omitempty"`
	PrivateIP              string             `json:"private_ip,omitempty"`
	PrivateIPv6            string             `json:"private_ipv6,omitempty"`
	PublicIP               string             `json:"public_ip"`
	PublicIPv6             string             `json:"public_ipv6,omitempty"`
	Interface              string             `json:"interface,omitempty"`
	ControlDevice          string             `json:"control_device,omitempty"`
	ESIMTransport          string             `json:"esim_transport,omitempty"`
	ATPort                 string             `json:"at_port,omitempty"`
	USBPath                string             `json:"usb_path,omitempty"`
	AudioDevice            string             `json:"audio_device,omitempty"`
	LocalPhone             string             `json:"local_phone,omitempty"`
	E911SetupAvailable     bool               `json:"e911_setup_available,omitempty"`
	ActiveESIMProfileName  string             `json:"active_esim_profile_name,omitempty"`
	SMSEnabled             bool               `json:"sms_enabled"`
	NetworkEnabled         bool               `json:"network_enabled"`
	VoWiFiEnabled          bool               `json:"vowifi_enabled"`
	VoWiFiActive           bool               `json:"vowifi_active"`
	VoWiFiRuntime          *voWiFiRuntimeDTO  `json:"vowifi_runtime,omitempty"`
	RadioLiveOK            *bool              `json:"radio_live_ok,omitempty"`
	Modem                  modem.DeviceStatus `json:"modem"`
	Traffic                map[string]string  `json:"traffic,omitempty"`
	TrafficRaw             map[string]int64   `json:"traffic_raw,omitempty"`
	TrafficMeta            *deviceTrafficMeta `json:"traffic_meta,omitempty"`
	BackendMode            string             `json:"backend_mode"`
	NetworkConnected       bool               `json:"network_connected"`
	RegistrationStateLabel string             `json:"registration_state_label"`
}

type deviceMgmtListModem struct {
	Operator      string `json:"operator"`
	NativeSPN     string `json:"native_spn,omitempty"`
	NativeMCC     string `json:"native_mcc,omitempty"`
	NativeMNC     string `json:"native_mnc,omitempty"`
	NetworkMode   string `json:"network_mode"`
	NetworkDuplex string `json:"network_duplex"`
	RadioBand     string `json:"radio_band,omitempty"`
	RadioChannel  uint32 `json:"radio_channel,omitempty"`
	SignalDBM     int    `json:"signal_dbm"`
	SignalSINR    int    `json:"signal_sinr,omitempty"`
	IMEI          string `json:"imei,omitempty"`
	ICCID         string `json:"iccid,omitempty"`
	RegStatus     int    `json:"reg_status"`
	PSAttached    bool   `json:"ps_attached"`
}

type deviceMgmtListItem struct {
	ID                     string              `json:"id"`
	Name                   string              `json:"name"`
	Running                bool                `json:"running"`
	Healthy                bool                `json:"healthy"`
	ControlOnline          bool                `json:"control_online"`
	PhysicalPresent        bool                `json:"physical_present"`
	WorkerRunning          bool                `json:"worker_running"`
	DataConnected          bool                `json:"data_connected"`
	RadioRegistered        bool                `json:"radio_registered"`
	LifecyclePhase         string              `json:"lifecycle_phase"`
	LifecycleReason        string              `json:"lifecycle_reason,omitempty"`
	PublicIP               string              `json:"public_ip"`
	PublicIPv6             string              `json:"public_ipv6,omitempty"`
	Interface              string              `json:"interface,omitempty"`
	ESIMTransport          string              `json:"esim_transport,omitempty"`
	SMSEnabled             bool                `json:"sms_enabled"`
	NetworkEnabled         bool                `json:"network_enabled"`
	VoWiFiEnabled          bool                `json:"vowifi_enabled"`
	VoWiFiRuntime          *voWiFiRuntimeDTO   `json:"vowifi_runtime,omitempty"`
	Modem                  deviceMgmtListModem `json:"modem"`
	NetworkConnected       bool                `json:"network_connected"`
	RegistrationStateLabel string              `json:"registration_state_label"`
}

type voWiFiRuntimeDTO struct {
	DeviceID       string    `json:"device_id"`
	Phase          string    `json:"phase"`
	DataplaneMode  string    `json:"dataplane_mode"`
	ICCID          string    `json:"iccid,omitempty"`
	IMSI           string    `json:"imsi,omitempty"`
	SIMReady       bool      `json:"sim_ready"`
	AccessReady    bool      `json:"access_ready"`
	TunnelReady    bool      `json:"tunnel_ready"`
	IMSReady       bool      `json:"ims_ready"`
	SMSReady       bool      `json:"sms_ready"`
	RegStatus      int       `json:"reg_status"`
	RegStatusText  string    `json:"reg_status_text"`
	NetworkMode    string    `json:"network_mode"`
	LastErrorClass string    `json:"last_error_class"`
	LastError      string    `json:"last_error"`
	LastReason     string    `json:"last_reason"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func runtimeStateToDTO(st runtimehost.State, status modem.DeviceStatus) *voWiFiRuntimeDTO {
	return &voWiFiRuntimeDTO{
		DeviceID:       st.DeviceID,
		Phase:          string(st.Phase),
		DataplaneMode:  st.DataplaneMode,
		ICCID:          strings.TrimSpace(status.ICCID),
		IMSI:           strings.TrimSpace(status.IMSI),
		SIMReady:       st.SIMReady,
		AccessReady:    st.AccessReady,
		TunnelReady:    st.TunnelReady,
		IMSReady:       st.IMSReady,
		SMSReady:       st.SMSReady,
		RegStatus:      st.RegStatus,
		RegStatusText:  st.RegStatusText,
		NetworkMode:    st.NetworkMode,
		LastErrorClass: st.LastErrorClass,
		LastError:      st.LastError,
		LastReason:     st.LastReason,
		UpdatedAt:      st.UpdatedAt,
	}
}

func (s *Server) getVoWiFiRuntimeDTO(deviceID string) *voWiFiRuntimeDTO {
	st, ok := s.pool.GetVoWiFiRuntimeState(deviceID)
	if !ok {
		return nil
	}
	status := modem.DeviceStatus{}
	if w := s.pool.GetWorker(deviceID); w != nil {
		status = w.ProjectDeviceStatus()
	}
	return runtimeStateToDTO(st, status)
}

func isLifecycleActiveForAPI(phase string) bool {
	switch phase {
	case string(device.LifecyclePhaseRebooting),
		string(device.LifecyclePhaseUSBWait),
		string(device.LifecyclePhaseWorkerStarting),
		string(device.LifecyclePhaseQMIStarting),
		string(device.LifecyclePhaseRecovering):
		return true
	default:
		return false
	}
}

func lifecyclePhaseString(snap device.LifecycleSnapshot) string {
	if snap.Phase == "" {
		return string(device.LifecyclePhaseOffline)
	}
	return string(snap.Phase)
}

func lifecycleSnapshotForAPI(pool *device.Pool, deviceID string) device.LifecycleSnapshot {
	if pool == nil {
		return device.LifecycleSnapshot{Phase: device.LifecyclePhaseOffline}
	}
	return pool.LifecycleSnapshot(deviceID)
}

func lifecyclePhaseForAPI(snap device.LifecycleSnapshot, workerRunning bool, controlOnline bool) string {
	phase := lifecyclePhaseString(snap)
	if workerRunning && controlOnline {
		switch phase {
		case string(device.LifecyclePhaseOffline),
			string(device.LifecyclePhaseUSBWait),
			string(device.LifecyclePhaseWorkerStarting),
			string(device.LifecyclePhaseQMIStarting),
			string(device.LifecyclePhaseRecovering):
			return string(device.LifecyclePhaseOnline)
		}
	}
	return phase
}

func (s *Server) applyLifecycleToOverviewItem(item *deviceMgmtOverviewItem, workerRunning bool, cfg config.DeviceConfig) {
	if item == nil {
		return
	}
	snap := lifecycleSnapshotForAPI(s.pool, cfg.ID)
	phase := lifecyclePhaseForAPI(snap, workerRunning, item.ControlOnline)
	item.WorkerRunning = workerRunning
	item.DataConnected = item.NetworkConnected
	item.RadioRegistered = item.Modem.RegStatus == 1 || item.Modem.RegStatus == 5
	item.LifecyclePhase = phase
	item.LifecycleReason = snap.Reason
	item.PhysicalPresent = workerRunning || isLifecycleActiveForAPI(phase)
}

func (s *Server) applyLifecycleToListItem(item *deviceMgmtListItem, workerRunning bool, cfg config.DeviceConfig) {
	if item == nil {
		return
	}
	snap := lifecycleSnapshotForAPI(s.pool, cfg.ID)
	phase := lifecyclePhaseForAPI(snap, workerRunning, item.ControlOnline)
	item.WorkerRunning = workerRunning
	item.DataConnected = item.NetworkConnected
	item.RadioRegistered = item.Modem.RegStatus == 1 || item.Modem.RegStatus == 5
	item.LifecyclePhase = phase
	item.LifecycleReason = snap.Reason
	item.PhysicalPresent = workerRunning || isLifecycleActiveForAPI(phase)
}

func (s *Server) applyLifecycleToOverviewLiteItem(item *deviceMgmtOverviewLiteItem, worker *device.Worker, cfg config.DeviceConfig) {
	if item == nil {
		return
	}
	workerRunning := worker != nil
	snap := lifecycleSnapshotForAPI(s.pool, cfg.ID)
	phase := lifecyclePhaseForAPI(snap, workerRunning, item.ControlOnline)
	item.WorkerRunning = workerRunning
	item.DataConnected = item.NetworkConnected
	item.RadioRegistered = item.Modem.RegStatus == 1 || item.Modem.RegStatus == 5
	item.LifecyclePhase = phase
	item.LifecycleReason = snap.Reason
	item.PhysicalPresent = workerRunning || isLifecycleActiveForAPI(phase)
}

func (s *Server) buildOverviewLiteItemFromWorker(w *device.Worker, cfg config.DeviceConfig, status modem.DeviceStatus, radioLiveOK *bool) deviceMgmtOverviewLiteItem {
	item := s.buildOverviewLiteItemFromWorkerWithModem(w, cfg, status, radioLiveOK, status)
	item.Modem = modemSummaryStatus(item.Modem)
	return item
}

func (s *Server) buildOverviewLiteDetailItemFromWorker(w *device.Worker, cfg config.DeviceConfig, status modem.DeviceStatus, radioLiveOK *bool) deviceMgmtOverviewLiteItem {
	return s.buildOverviewLiteItemFromWorkerWithModem(w, cfg, status, radioLiveOK, status)
}

func (s *Server) buildOverviewLiteItemFromWorkerWithModem(w *device.Worker, cfg config.DeviceConfig, status modem.DeviceStatus, radioLiveOK *bool, modemStatus modem.DeviceStatus) deviceMgmtOverviewLiteItem {
	controlOnline := w.GetCachedHealthy()
	item := deviceMgmtOverviewLiteItem{
		ID:                     w.ID,
		Name:                   cfg.Name,
		Running:                true,
		Healthy:                controlOnline,
		ControlOnline:          controlOnline,
		PublicIP:               w.GetCachedIP(),
		PublicIPv6:             w.GetCachedIPv6(),
		Interface:              cfg.Interface,
		ControlDevice:          cfg.ControlDevice,
		ESIMTransport:          config.NormalizeESIMTransport(cfg.ESIMTransport),
		ATPort:                 w.ResolvedATPort(),
		USBPath:                cfg.USBPath,
		AudioDevice:            cfg.AudioDevice,
		LocalPhone:             s.overviewLocalPhone(effectiveOverviewIMSI(w, status), strings.TrimSpace(status.ICCID)),
		E911SetupAvailable:     e911.SetupAvailable(modemStatus),
		SMSEnabled:             cfg.SMSEnabled,
		NetworkEnabled:         cfg.NetworkEnabled,
		VoWiFiEnabled:          s.cardPolicyVoWiFiEnabled(strings.TrimSpace(status.ICCID), cfg.VoWiFiEnabled),
		VoWiFiActive:           s.pool.IsVoWiFiActive(w.ID),
		VoWiFiRuntime:          s.getVoWiFiRuntimeDTO(w.ID),
		RadioLiveOK:            radioLiveOK,
		Modem:                  modemStatus,
		NetworkConnected:       w.NetworkConnected(),
		RegistrationStateLabel: registrationStateLabel(status.RegStatus),
		BackendMode: func() string {
			if w.Backend != nil {
				return w.Backend.Mode()
			}
			return "at"
		}(),
	}
	if nc := w.NetworkController(); nc != nil {
		item.PrivateIP = nc.GetPrivateIP()
		item.PrivateIPv6 = nc.GetPrivateIPv6()
	}
	if w.EsimMgr != nil {
		if name, err := w.EsimMgr.ActiveProfileName(); err == nil {
			item.ActiveESIMProfileName = name
		}
	}
	s.applyLifecycleToOverviewLiteItem(&item, w, cfg)
	return item
}

func effectiveOverviewIMSI(w *device.Worker, status modem.DeviceStatus) string {
	if w != nil && w.SIMIdentitySuppressesOverviewIMSI() {
		return ""
	}
	imsi := strings.TrimSpace(status.IMSI)
	if imsi != "" {
		return imsi
	}
	if w == nil {
		return ""
	}
	if !w.SIMIdentityAllowsOverviewFallback() {
		return ""
	}
	return strings.TrimSpace(w.GetCachedIMSI())
}

func (s *Server) overviewLocalPhoneByIMSI(imsi string) string {
	imsi = strings.TrimSpace(imsi)
	if imsi == "" {
		return ""
	}
	phone, err := s.data().SIM.PhoneByIMSI(imsi)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(phone)
}

func (s *Server) overviewLocalPhone(imsi, iccid string) string {
	imsi = strings.TrimSpace(imsi)
	iccid = strings.TrimSpace(iccid)
	if imsi == "" && iccid == "" {
		return ""
	}
	phone, err := s.data().SIM.PhoneByIMSIOrICCID(imsi, iccid)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(phone)
}

func (s *Server) handleDeviceMgmtOverviewLite(c *gin.Context) {
	id := deviceIDParam(c)
	if id == "" {
		id = strings.TrimSpace(c.Query("id"))
	}
	liveRefresh := overviewDetailLiveRefreshRequested(c)
	workers := s.pool.GetAllWorkers()
	managed := config.ListDevices()
	cfgByID := map[string]config.DeviceConfig{}
	for _, d := range managed {
		cfgByID[d.ID] = d
	}

	if id != "" {
		for _, w := range workers {
			if w.ID != id {
				continue
			}
			cfg := w.Config
			if v, ok := cfgByID[w.ID]; ok {
				cfg = overviewDisplayConfig(w.Config, v, true)
			}
			if liveRefresh {
				_ = w.RefreshRuntime(c.Request.Context(), "overview_detail")
				_ = w.RefreshIdentityLive(c.Request.Context(), "overview_detail")
			}
			status := w.ProjectDeviceStatus()
			var radioLiveOK *bool
			if liveRefresh && (w.Backend == nil || w.Backend.Mode() == backend.BackendAT) {
				radioLiveOK = new(bool)
				*radioLiveOK = true
			}
			item := s.buildOverviewLiteDetailItemFromWorker(w, cfg, status, radioLiveOK)
			controlOnline := w.GetCachedHealthy()
			item.Healthy = controlOnline
			item.ControlOnline = controlOnline
			s.applyLifecycleToOverviewLiteItem(&item, w, cfg)
			tag := w.ID + "@" + cfg.Interface
			ps, rx, tx, _ := s.data().Traffic.LatestMinuteDeltas("iface", tag)
			item.Traffic, item.TrafficRaw, item.TrafficMeta = buildTrafficOverviewFields(cfg.Interface, db.LatestMinuteDeltas{
				PeriodStart: ps,
				RxBytes:     rx,
				TxBytes:     tx,
			}, time.Now())
			c.JSON(http.StatusOK, gin.H{"devices": []deviceMgmtOverviewLiteItem{item}})
			return
		}

		if dc, err := config.GetDeviceByID(id); err == nil && dc != nil {
			pol := s.resolveOfflineDevicePolicy(id)
			item := deviceMgmtOverviewLiteItem{
				ID:                     dc.ID,
				Name:                   dc.Name,
				Running:                false,
				Healthy:                false,
				ControlOnline:          false,
				PublicIP:               "",
				Interface:              dc.Interface,
				ControlDevice:          dc.ControlDevice,
				ESIMTransport:          config.NormalizeESIMTransport(dc.ESIMTransport),
				ATPort:                 dc.ATPort,
				USBPath:                dc.USBPath,
				SMSEnabled:             pol.SMSEnabled,
				NetworkEnabled:         pol.NetworkEnabled,
				VoWiFiEnabled:          pol.VoWiFiEnabled,
				VoWiFiActive:           false,
				NetworkConnected:       false,
				RegistrationStateLabel: registrationStateLabel(0),
				Modem:                  modem.DeviceStatus{},
				Traffic:                nil,
			}
			s.applyLifecycleToOverviewLiteItem(&item, nil, *dc)
			c.JSON(http.StatusOK, gin.H{"devices": []deviceMgmtOverviewLiteItem{item}})
			return
		}

		c.JSON(http.StatusOK, gin.H{"devices": []deviceMgmtOverviewLiteItem{}})
		return
	}

	tagByID := map[string]string{}
	tags := make([]string, 0, len(workers))
	for _, w := range workers {
		cfg := w.Config
		if v, ok := cfgByID[w.ID]; ok {
			cfg = overviewDisplayConfig(w.Config, v, true)
		}
		if cfg.Interface == "" {
			continue
		}
		tag := w.ID + "@" + cfg.Interface
		tagByID[w.ID] = tag
		tags = append(tags, tag)
	}
	byTag, _ := s.data().Traffic.LatestMinuteDeltasBatch("iface", tags)
	now := time.Now()

	workerByID := map[string]bool{}
	items := make([]deviceMgmtOverviewLiteItem, 0, len(workers))
	for _, w := range workers {
		workerByID[w.ID] = true
		cfg := w.Config
		if v, ok := cfgByID[w.ID]; ok {
			cfg = overviewDisplayConfig(w.Config, v, true)
		}
		status := w.GetCachedDeviceStatus() // 批量列表读缓存，0 IPC
		item := s.buildOverviewLiteItemFromWorker(w, cfg, status, nil)
		item.Traffic, item.TrafficRaw, item.TrafficMeta = buildTrafficOverviewFields(cfg.Interface, byTag[tagByID[w.ID]], now)
		items = append(items, item)
	}
	for _, dc := range managed {
		if workerByID[dc.ID] {
			continue
		}
		item := deviceMgmtOverviewLiteItem{
			ID:                     dc.ID,
			Name:                   dc.Name,
			Running:                false,
			Healthy:                false,
			ControlOnline:          false,
			PublicIP:               "",
			Interface:              dc.Interface,
			ControlDevice:          dc.ControlDevice,
			ESIMTransport:          config.NormalizeESIMTransport(dc.ESIMTransport),
			ATPort:                 dc.ATPort,
			SMSEnabled:             true, // SMS 恒开（系统不变量）
			NetworkEnabled:         dc.NetworkEnabled,
			VoWiFiActive:           false, // 非运行设备无活跃 VoWiFi
			NetworkConnected:       false,
			RegistrationStateLabel: registrationStateLabel(0),
			Modem:                  modem.DeviceStatus{},
			Traffic:                nil,
		}
		s.applyLifecycleToOverviewLiteItem(&item, nil, dc)
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"devices": items})
}

func overviewDetailLiveRefreshRequested(c *gin.Context) bool {
	if c == nil {
		return false
	}
	for _, key := range []string{"refresh", "live"} {
		switch strings.ToLower(strings.TrimSpace(c.Query(key))) {
		case "1", "true", "yes", "on":
			return true
		}
	}
	return false
}
