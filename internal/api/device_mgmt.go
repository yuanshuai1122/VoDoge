// 设备的增删改查与配置。
//
// 概览与实时流在 device_overview*.go，动作类在 device_actions.go，
// eSIM 在 device_esim.go，硬件发现在 device_discovery.go。
package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/yuanshuai1122/vohive/internal/config"
	"github.com/yuanshuai1122/vohive/internal/device"
	proxytraffic "github.com/yuanshuai1122/vohive/internal/proxy/traffic"
	"github.com/yuanshuai1122/vohive/pkg/logger"

	"github.com/gin-gonic/gin"
)

type deviceConfigDTO struct {
	ID                    string  `json:"id"`
	Name                  string  `json:"name"`
	ModemIMEI             string  `json:"modem_imei"`
	USBPath               string  `json:"usb_path"`
	ATPort                string  `json:"at_port"`
	ProxyPort             int     `json:"proxy_port"`
	Interface             string  `json:"interface"`
	ControlDevice         string  `json:"control_device,omitempty"`
	QMIUseProxy           *bool   `json:"qmi_use_proxy,omitempty"`
	QMIProxyPath          *string `json:"qmi_proxy_path,omitempty"`
	QMIProxyExecutable    *string `json:"qmi_proxy_executable,omitempty"`
	ESIMTransport         string  `json:"esim_transport,omitempty"`
	BaudRate              int     `json:"baud_rate,omitempty"`
	DataBits              int     `json:"data_bits,omitempty"`
	StopBits              int     `json:"stop_bits,omitempty"`
	Parity                string  `json:"parity,omitempty"`
	OperatorSelectionMode string  `json:"operator_selection_mode,omitempty"`
	OperatorSelectionPLMN string  `json:"operator_selection_plmn,omitempty"`
	OperatorSelectionRAT  string  `json:"operator_selection_rat,omitempty"`
	SMSEnabled            bool    `json:"sms_enabled"`
	APN                   string  `json:"apn,omitempty"`
	IPVersion             string  `json:"ip_version,omitempty"`
	NetworkEnabled        bool    `json:"network_enabled"`
	VoWiFiEnabled         bool    `json:"vowifi_enabled"`
	DeviceBackend         string  `json:"device_backend,omitempty"`
	ModuleVendor          string  `json:"module_vendor,omitempty"`
}

func deviceConfigToDTO(c config.DeviceConfig) deviceConfigDTO {
	return deviceConfigDTO{
		ID:                    c.ID,
		Name:                  c.Name,
		ModemIMEI:             c.ModemIMEI,
		USBPath:               c.USBPath,
		ATPort:                c.ATPort,
		ProxyPort:             c.ProxyPort,
		Interface:             c.Interface,
		ControlDevice:         c.ControlDevice,
		QMIUseProxy:           boolPtr(c.QMIUseProxy),
		QMIProxyPath:          stringPtr(c.QMIProxyPath),
		QMIProxyExecutable:    stringPtr(c.QMIProxyExecutable),
		ESIMTransport:         config.NormalizeESIMTransport(c.ESIMTransport),
		BaudRate:              c.BaudRate,
		DataBits:              c.DataBits,
		StopBits:              c.StopBits,
		Parity:                c.Parity,
		OperatorSelectionMode: c.OperatorSelectionMode,
		OperatorSelectionPLMN: c.OperatorSelectionPLMN,
		OperatorSelectionRAT:  c.OperatorSelectionRAT,
		SMSEnabled:            c.SMSEnabled,
		APN:                   c.APN,
		IPVersion:             c.IPVersion,
		NetworkEnabled:        c.NetworkEnabled,
		VoWiFiEnabled:         c.VoWiFiEnabled,
		DeviceBackend:         c.DeviceBackend,
		ModuleVendor:          config.NormalizeModuleVendor(c.ModuleVendor),
	}
}

func deviceConfigFromDTO(d deviceConfigDTO) config.DeviceConfig {
	return deviceConfigFromDTOWithBase(d, nil)
}

func deviceConfigFromDTOWithBase(d deviceConfigDTO, base *config.DeviceConfig) config.DeviceConfig {
	id := strings.TrimSpace(d.ID)
	if id == "" {
		id = strings.TrimSpace(d.Interface)
	}
	qmiUseProxy := false
	qmiProxyPath := ""
	qmiProxyExecutable := ""
	if base != nil {
		qmiUseProxy = base.QMIUseProxy
		qmiProxyPath = base.QMIProxyPath
		qmiProxyExecutable = base.QMIProxyExecutable
	}
	if d.QMIUseProxy != nil {
		qmiUseProxy = *d.QMIUseProxy
	}
	if d.QMIProxyPath != nil {
		qmiProxyPath = strings.TrimSpace(*d.QMIProxyPath)
	}
	if d.QMIProxyExecutable != nil {
		qmiProxyExecutable = strings.TrimSpace(*d.QMIProxyExecutable)
	}
	return config.DeviceConfig{
		ID:                    id,
		Name:                  strings.TrimSpace(d.Name),
		ModemIMEI:             strings.TrimSpace(d.ModemIMEI),
		USBPath:               strings.TrimSpace(d.USBPath),
		ATPort:                strings.TrimSpace(d.ATPort),
		ProxyPort:             d.ProxyPort,
		Interface:             strings.TrimSpace(d.Interface),
		ControlDevice:         strings.TrimSpace(d.ControlDevice),
		QMIUseProxy:           qmiUseProxy,
		QMIProxyPath:          qmiProxyPath,
		QMIProxyExecutable:    qmiProxyExecutable,
		ESIMTransport:         config.NormalizeESIMTransport(d.ESIMTransport),
		BaudRate:              d.BaudRate,
		DataBits:              d.DataBits,
		StopBits:              d.StopBits,
		Parity:                strings.TrimSpace(d.Parity),
		OperatorSelectionMode: strings.TrimSpace(d.OperatorSelectionMode),
		OperatorSelectionPLMN: strings.TrimSpace(d.OperatorSelectionPLMN),
		OperatorSelectionRAT:  strings.TrimSpace(d.OperatorSelectionRAT),
		SMSEnabled:            true, // 短信功能始终启用
		APN:                   strings.TrimSpace(d.APN),
		IPVersion:             strings.TrimSpace(d.IPVersion),
		NetworkEnabled:        d.NetworkEnabled,
		VoWiFiEnabled:         d.VoWiFiEnabled,
		DeviceBackend:         d.DeviceBackend,
		ModuleVendor:          config.NormalizeModuleVendor(d.ModuleVendor),
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func stringPtr(v string) *string {
	return &v
}

func (s *Server) handleDeviceMgmtList(c *gin.Context) {
	workers := s.pool.GetAllWorkers()
	managed := config.ListDevices()
	cfgByID := map[string]config.DeviceConfig{}
	for _, d := range managed {
		cfgByID[d.ID] = d
	}

	workerByID := map[string]bool{}
	items := make([]deviceMgmtListItem, 0, len(workers))
	for _, w := range workers {
		workerByID[w.ID] = true
		cfg := w.Config
		if v, ok := cfgByID[w.ID]; ok {
			cfg = overviewDisplayConfig(w.Config, v, true)
		}
		status := w.GetCachedDeviceStatus()
		controlOnline := w.GetCachedHealthy()
		item := deviceMgmtListItem{
			ID:                     w.ID,
			Name:                   cfg.Name,
			Running:                true,
			Healthy:                controlOnline,
			ControlOnline:          controlOnline,
			PublicIP:               w.GetCachedIP(),
			PublicIPv6:             w.GetCachedIPv6(),
			Interface:              cfg.Interface,
			ESIMTransport:          config.NormalizeESIMTransport(cfg.ESIMTransport),
			SMSEnabled:             cfg.SMSEnabled,
			NetworkEnabled:         cfg.NetworkEnabled,
			VoWiFiEnabled:          s.pool.IsVoWiFiActive(w.ID), // 使用多设备状态查询
			VoWiFiRuntime:          s.getVoWiFiRuntimeDTO(w.ID),
			NetworkConnected:       w.NetworkConnected(),
			RegistrationStateLabel: registrationStateLabel(status.RegStatus),
			Modem: deviceMgmtListModem{
				Operator:      status.Operator,
				NativeSPN:     status.NativeSPN,
				NativeMCC:     status.NativeMCC,
				NativeMNC:     status.NativeMNC,
				NetworkMode:   status.NetworkMode,
				NetworkDuplex: status.NetworkDuplex,
				RadioBand:     status.RadioBand,
				RadioChannel:  status.RadioChannel,
				SignalDBM:     status.SignalDBM,
				SignalSINR:    status.SignalSINR,
				IMEI:          status.IMEI,
				ICCID:         status.ICCID,
				RegStatus:     status.RegStatus,
				PSAttached:    status.PSAttached,
			},
		}
		s.applyLifecycleToListItem(&item, true, cfg)
		items = append(items, item)
	}

	for _, dc := range managed {
		if workerByID[dc.ID] {
			continue
		}
		item := deviceMgmtListItem{
			ID:                     dc.ID,
			Name:                   dc.Name,
			Running:                false,
			Healthy:                false,
			ControlOnline:          false,
			PublicIP:               "",
			Interface:              dc.Interface,
			ESIMTransport:          config.NormalizeESIMTransport(dc.ESIMTransport),
			SMSEnabled:             true, // SMS 恒开（系统不变量）
			NetworkEnabled:         dc.NetworkEnabled,
			VoWiFiEnabled:          false, // 非运行设备无活跃 VoWiFi
			NetworkConnected:       false,
			RegistrationStateLabel: registrationStateLabel(0),
			Modem:                  deviceMgmtListModem{},
		}
		s.applyLifecycleToListItem(&item, false, dc)
		items = append(items, item)
	}

	c.JSON(http.StatusOK, gin.H{"devices": items, "device_limit": device.DefaultFreeDeviceLimit})
}

func (s *Server) handleDeviceMgmtGetDeviceConfig(c *gin.Context) {
	id := deviceIDParam(c)
	if id == "" {
		fail(c, http.StatusBadRequest, "", "参数错误")
		return
	}
	md, err := config.GetDeviceByID(id)
	if err != nil {
		fail(c, http.StatusInternalServerError, "", "读取设备配置失败: "+err.Error())
		return
	}
	if md == nil {
		fail(c, http.StatusNotFound, "", "设备未找到")
		return
	}
	cfgDTO := deviceConfigToDTO(*md)
	if worker := s.pool.GetWorker(id); worker != nil {
		cfgDTO.Interface = worker.Config.Interface
		cfgDTO.ControlDevice = worker.Config.ControlDevice
		cfgDTO.ATPort = worker.ResolvedATPort()
		cfgDTO.USBPath = worker.Config.USBPath
	}
	c.JSON(http.StatusOK, gin.H{"config": cfgDTO})
}

type updateDeviceRequest struct {
	Config deviceConfigDTO `json:"config"`
}

func hasManagedNetworkCapability(cfg config.DeviceConfig) bool {
	return strings.TrimSpace(cfg.ControlDevice) != "" && strings.TrimSpace(cfg.Interface) != ""
}

func validateManagedNetworkConfig(cfg config.DeviceConfig) error {
	if err := validateDeviceBackendConfig(cfg); err != nil {
		return err
	}
	if _, _, err := config.ResolveIPFamily(cfg.IPVersion); err != nil {
		return err
	}
	// 零路径持久化后 control_device/interface 由运行时从 IMEI 发现，不再作为保存前置条件。
	return nil
}

func normalizeManagedDeviceConfig(cfg config.DeviceConfig) (config.DeviceConfig, string) {
	if cfg.VoWiFiEnabled && cfg.NetworkEnabled {
		cfg.NetworkEnabled = false
		return cfg, "VoWiFi 已启用"
	}
	return cfg, ""
}

func deviceConfigForAdd(cfg config.DeviceConfig) config.DeviceConfig {
	cfg.APN = ""
	cfg.IPVersion = ""
	cfg.NetworkEnabled = false
	cfg.VoWiFiEnabled = false
	cfg.AirplaneEnabled = false
	cfg.SMSEnabled = true
	return cfg
}

func joinWarningMessages(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	return strings.Join(filtered, "；")
}

type deviceBindingConflict struct {
	Field   string
	Value   string
	OtherID string
}

func detectDeviceBindingConflict(cfg config.DeviceConfig, excludeID string) *deviceBindingConflict {
	return detectDeviceBindingConflictInList(cfg, excludeID, config.ListDevices())
}

func detectDeviceBindingConflictInList(cfg config.DeviceConfig, excludeID string, devices []config.DeviceConfig) *deviceBindingConflict {
	type key struct {
		field string
		value string
	}
	keys := make([]key, 0, 5)
	if v := strings.TrimSpace(cfg.ModemIMEI); v != "" {
		keys = append(keys, key{field: "modem_imei", value: v})
	}
	if v := strings.TrimSpace(cfg.ControlDevice); v != "" {
		keys = append(keys, key{field: "control_device", value: v})
	}
	if v := strings.TrimSpace(cfg.USBPath); v != "" {
		keys = append(keys, key{field: "usb_path", value: v})
	}
	if v := strings.TrimSpace(cfg.Interface); v != "" {
		keys = append(keys, key{field: "interface", value: v})
	}
	if v := strings.TrimSpace(cfg.ATPort); v != "" {
		keys = append(keys, key{field: "at_port", value: v})
	}
	if len(keys) == 0 {
		return nil
	}

	for _, existing := range devices {
		existingID := strings.TrimSpace(existing.ID)
		if existingID == "" {
			continue
		}
		if existingID == strings.TrimSpace(excludeID) {
			continue
		}
		for _, k := range keys {
			switch k.field {
			case "modem_imei":
				if strings.TrimSpace(existing.ModemIMEI) == k.value {
					return &deviceBindingConflict{Field: k.field, Value: k.value, OtherID: existingID}
				}
			case "control_device":
				if strings.TrimSpace(existing.ControlDevice) == k.value {
					return &deviceBindingConflict{Field: k.field, Value: k.value, OtherID: existingID}
				}
			case "usb_path":
				if strings.TrimSpace(existing.USBPath) == k.value {
					return &deviceBindingConflict{Field: k.field, Value: k.value, OtherID: existingID}
				}
			case "interface":
				if strings.TrimSpace(existing.Interface) == k.value {
					return &deviceBindingConflict{Field: k.field, Value: k.value, OtherID: existingID}
				}
			case "at_port":
				if strings.TrimSpace(existing.ATPort) == k.value {
					return &deviceBindingConflict{Field: k.field, Value: k.value, OtherID: existingID}
				}
			}
		}
	}
	return nil
}

func (s *Server) handleDeviceMgmtUpdateDevice(c *gin.Context) {
	id := deviceIDParam(c)
	var req updateDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", "参数错误")
		return
	}
	if strings.TrimSpace(req.Config.ID) != "" && strings.TrimSpace(req.Config.ID) != id {
		fail(c, http.StatusBadRequest, "", "不支持修改设备 ID")
		return
	}

	oldMD, err := config.GetDeviceByID(id)
	if err != nil {
		fail(c, http.StatusInternalServerError, "", "读取设备配置失败: "+err.Error())
		return
	}
	if oldMD == nil {
		fail(c, http.StatusNotFound, "", "设备未找到")
		return
	}

	req.Config.ID = id
	newCfg := deviceConfigFromDTOWithBase(req.Config, oldMD)
	newCfg, forcedWarning := normalizeManagedDeviceConfig(newCfg)
	if err := validateManagedNetworkConfig(newCfg); err != nil {
		fail(c, http.StatusBadRequest, "", err.Error())
		return
	}
	if conflict := detectDeviceBindingConflict(newCfg, id); conflict != nil {
		fail(c, http.StatusConflict, "",
			fmt.Sprintf("设备资源冲突：%s=%s 已被设备 %s 使用", conflict.Field, conflict.Value, conflict.OtherID))
		return
	}

	oldCfg := *oldMD
	// 策略跟卡走：设备保存只负责硬件/身份字段，不再触碰策略（策略经 PUT /cards/:iccid/policy 独立编辑）。
	// DTO 仍会回传 network/vowifi/ip/apn，但 GET config 不投影这些字段（恒零），直接采信会把卡策略清空。
	// 故把当前有效策略同时写回 oldCfg 与 newCfg，使其在开关转换判断中互相抵消（中性化），
	// 不写 card_policies、不误触发 VoWiFi 关闭重建/恢复射频/热拉起。
	_, effNetwork, effVoWiFi, effIP, effAPN := s.currentEffectiveDevicePolicy(id)
	oldCfg.NetworkEnabled = effNetwork
	oldCfg.VoWiFiEnabled = effVoWiFi
	oldCfg.IPVersion = effIP
	oldCfg.APN = effAPN
	newCfg.NetworkEnabled = effNetwork
	newCfg.VoWiFiEnabled = effVoWiFi
	newCfg.IPVersion = effIP
	newCfg.APN = effAPN

	requiresRestart := deviceConfigRequiresRestart(oldCfg, newCfg)
	if err := config.UpdateDeviceInFile(s.configPath, newCfg.ID, newCfg); err != nil {
		logger.Error("写入设备配置失败", "err", err)
		fail(c, http.StatusInternalServerError, "", "写入配置失败: "+err.Error())
		return
	}

	worker := s.pool.GetWorker(id)
	s.pool.UpdateWorkerConfig(id, newCfg, !requiresRestart)

	// 检测 DeviceBackend 状态变化，或 VoWiFi 从开启变为关闭都需要彻底重建 Worker 释放残余句柄
	needsRebuild := oldCfg.DeviceBackend != newCfg.DeviceBackend ||
		config.NormalizeModuleVendor(oldCfg.ModuleVendor) != config.NormalizeModuleVendor(newCfg.ModuleVendor) ||
		qmiProxyConfigChanged(oldCfg, newCfg) ||
		(!newCfg.VoWiFiEnabled && oldCfg.VoWiFiEnabled) ||
		(worker != nil && managedNetworkConfigChanged(oldCfg, newCfg))
	shouldApplyNetworkNow := worker != nil || needsRebuild

	warningMessage := forcedWarning
	if needsRebuild {
		logger.Info("配置保存触发底盘或 VoWiFi 停止变更，将彻底重建 Worker", "device", id)
		if err := s.pool.RebuildWorker(id); err != nil {
			logger.Error("重建 Worker 失败", "device", id, "err", err)
			warningMessage = joinWarningMessages(warningMessage, "配置已保存，但运行时重建失败: "+err.Error())
		}
	}

	shouldRestoreRadio := oldCfg.VoWiFiEnabled && !newCfg.VoWiFiEnabled
	if shouldRestoreRadio {
		if s.pool.IsVoWiFiActive(id) {
			warningMessage = joinWarningMessages(warningMessage, "VoWiFi 尚未完全退出，暂未恢复射频")
		} else if err := s.pool.RestoreRadioAfterVoWiFi(id); err != nil {
			logger.Warn("配置保存后恢复射频失败", "device", id, "err", err)
			warningMessage = joinWarningMessages(warningMessage, "配置已保存，但恢复射频失败: "+err.Error())
		}
	}

	// 检测 VoWiFi 状态变化，仅对于由关到开执行热拉起
	if newCfg.VoWiFiEnabled && !oldCfg.VoWiFiEnabled {
		logger.Info("配置保存触发 VoWiFi 启动", "device", id)
		if err := s.pool.EnableVoWiFi(id); err != nil {
			logger.Error("VoWiFi 启动失败", "device", id, "err", err)
			c.JSON(http.StatusOK, gin.H{
				"status":           "ok",
				"requires_restart": requiresRestart,
				"warning":          joinWarningMessages(warningMessage, "VoWiFi 启动失败: "+err.Error()),
				"vowifi_error":     "VoWiFi 启动失败: " + err.Error(),
			})
			return
		}
	}

	if shouldApplyNetworkNow && !newCfg.VoWiFiEnabled && !s.pool.IsVoWiFiActive(id) {
		if err := s.pool.ApplyConfiguredNetwork(id); err != nil {
			logger.Warn("配置保存后自动应用网络偏好失败", "device", id, "err", err)
			warningMessage = joinWarningMessages(warningMessage, "配置已保存，但自动应用网络失败: "+err.Error())
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":           "ok",
		"requires_restart": requiresRestart,
		"warning":          warningMessage,
	})
}

func (s *Server) handleDeviceMgmtDeleteDevice(c *gin.Context) {
	id := deviceIDParam(c)

	existing, err := config.GetDeviceByID(id)
	if err != nil {
		fail(c, http.StatusInternalServerError, "", "读取设备配置失败: "+err.Error())
		return
	}
	if existing == nil {
		fail(c, http.StatusNotFound, "", "设备未找到: "+id)
		return
	}

	if err := s.pool.RemoveWorker(id); err != nil {
		logger.Warn("删除设备配置前停止运行时设备失败", "device_id", id, "err", err)
		if !strings.Contains(err.Error(), "设备未找到") {
			fail(c, http.StatusConflict, "", "设备正在停止，请稍后重试: "+err.Error())
			return
		}
	}

	if err := config.DeleteDeviceInFile(s.configPath, id); err != nil {
		fail(c, http.StatusNotFound, "", err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

type addDeviceRequest struct {
	Config deviceConfigDTO `json:"config"`
}

// validateDeviceBackendConfig 校验 device_backend 配置合法值。
// 零路径持久化后 control_device 由运行时从 IMEI 发现，不再作为保存前置条件。
func validateDeviceBackendConfig(cfg config.DeviceConfig) error {
	backend := strings.ToLower(strings.TrimSpace(cfg.DeviceBackend))
	switch backend {
	case "", "at", "qmi", "mbim":
		// 合法值
	default:
		return fmt.Errorf("不支持的 device_backend: %q，可选值: at, qmi, mbim", backend)
	}
	if err := config.ValidateModuleVendor(cfg.ModuleVendor); err != nil {
		return fmt.Errorf("不支持的 module_vendor: %q，可选值: quectel, simcom", strings.TrimSpace(cfg.ModuleVendor))
	}
	return nil
}

func validateFreeDeviceConfigLimit(devices []config.DeviceConfig) error {
	if device.FreeDeviceLimitReached(len(devices)) {
		return fmt.Errorf("%s", device.FreeDeviceAddLimitMessage())
	}
	return nil
}

func (s *Server) handleDeviceMgmtAddDevice(c *gin.Context) {
	var req addDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", "参数错误")
		return
	}
	newCfg := deviceConfigFromDTO(req.Config)
	newCfg = deviceConfigForAdd(newCfg)
	newCfg, forcedWarning := normalizeManagedDeviceConfig(newCfg)
	if strings.TrimSpace(newCfg.ID) == "" {
		fail(c, http.StatusBadRequest, "", "必须填写 id")
		return
	}
	if err := validateManagedNetworkConfig(newCfg); err != nil {
		fail(c, http.StatusBadRequest, "", err.Error())
		return
	}

	if existing, err := config.GetDeviceByID(newCfg.ID); err == nil && existing != nil {
		fail(c, http.StatusConflict, "", "设备 ID 已存在")
		return
	}
	if conflict := detectDeviceBindingConflict(newCfg, ""); conflict != nil {
		fail(c, http.StatusConflict, "",
			fmt.Sprintf("设备资源冲突：%s=%s 已被设备 %s 使用", conflict.Field, conflict.Value, conflict.OtherID))
		return
	}
	if err := validateFreeDeviceConfigLimit(config.ListDevices()); err != nil {
		fail(c, http.StatusConflict, "", err.Error())
		return
	}
	// MBIM 设备使用 MBIM DeviceCaps 探测 IMEI，非 MBIM 设备使用 QMI 探测
	if strings.ToLower(strings.TrimSpace(newCfg.DeviceBackend)) == "mbim" {
		if config.NormalizeIMEI(newCfg.ModemIMEI) == "" && strings.TrimSpace(newCfg.ControlDevice) != "" {
			if mbimIMEI, err := device.ProbeIMEIViaMBIM(newCfg.ControlDevice); err == nil && mbimIMEI != "" {
				newCfg.ModemIMEI = mbimIMEI
			}
		}
	} else {
		enrichedCfg, imeiErr := ensureAddDeviceIMEI(newCfg, probeIMEIForAddFn)
		if imeiErr != nil {
			fail(c, http.StatusBadRequest, "", imeiErr.Error())
			return
		}
		newCfg = enrichedCfg
	}

	if err := config.AddDeviceInFile(s.configPath, newCfg); err != nil {
		logger.Error("写入新设备配置失败", "err", err)
		fail(c, http.StatusInternalServerError, "", "写入配置失败: "+err.Error())
		return
	}

	if _, err := s.pool.AddWorkerFromConfig(newCfg); err != nil {
		logger.Warn("设备配置已添加，但启动运行时设备失败", "device_id", newCfg.ID, "err", err)
		c.JSON(http.StatusOK, gin.H{
			"status":           "ok",
			"started":          false,
			"requires_restart": true,
			"warning":          "设备配置已添加，但运行时启动失败（可尝试重启服务或检查端口/权限）: " + err.Error(),
		})
		return
	}

	warningMessage := forcedWarning

	c.JSON(http.StatusOK, gin.H{
		"status":           "ok",
		"started":          true,
		"requires_restart": false,
		"warning":          warningMessage,
	})
}

// eSIM 下载的事件构造与 SSE 写出已迁往 esim_download.go——那里下载是一个
// 后台任务，事件要同时发给多个订阅者，不能再直接往某个 gin.Context 上写。

func (s *overviewTrafficStreamState) sync(item deviceMgmtOverviewLiteItem) <-chan proxytraffic.RealtimeSnapshot {
	if s == nil || s.subscriber == nil || s.deviceID == "" {
		return nil
	}
	if !overviewRealtimeTrafficEnabled(item) {
		s.stop()
		return nil
	}
	if s.ch != nil {
		return s.ch
	}
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	s.ch, s.unsubscribe = s.subscriber.Subscribe(ctx, s.deviceID)
	return s.ch
}

func (s *overviewTrafficStreamState) stop() {
	if s == nil {
		return
	}
	if s.unsubscribe != nil {
		s.unsubscribe()
	}
	s.ch = nil
	s.unsubscribe = nil
}
