// 仪表盘与状态查询：设备概览、健康、统计。
package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/yuanshuai1122/vohive/internal/config"
	"github.com/yuanshuai1122/vohive/internal/global"
	"github.com/yuanshuai1122/vohive/internal/proxy/server"

	"github.com/spf13/viper"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleListDevices(c *gin.Context) {
	workers := s.pool.GetAllWorkers()
	cfgByID := map[string]config.DeviceConfig{}
	{
		managed := config.ListDevices()
		for _, d := range managed {
			cfgByID[d.ID] = d
		}
	}

	type DeviceStatus struct {
		ID               string            `json:"id"`
		Name             string            `json:"name"`
		Interface        string            `json:"interface"`
		ProxyPort        int               `json:"proxy_port"`
		PublicIP         string            `json:"public_ip"`
		PublicIPv6       string            `json:"public_ipv6,omitempty"`
		Healthy          bool              `json:"healthy"`
		Operator         string            `json:"operator"`
		SignalDBM        int               `json:"signal_dbm"`
		NetworkMode      string            `json:"network_mode"`
		NetworkDuplex    string            `json:"network_duplex"`
		VoWiFiActive     bool              `json:"vowifi_active"`
		VoWiFiRuntime    *voWiFiRuntimeDTO `json:"vowifi_runtime,omitempty"`
		Traffic          map[string]string `json:"traffic,omitempty"`
		NetworkConnected bool              `json:"network_connected"`
	}

	list := make([]DeviceStatus, 0, len(workers))
	for _, w := range workers {
		status := w.GetCachedDeviceStatus() // 仓表盘列表读缓存，0 IPC
		cfg := w.Config
		if v, ok := cfgByID[w.ID]; ok {
			cfg = v
		}
		item := DeviceStatus{
			ID:               cfg.ID,
			Name:             cfg.Name,
			Interface:        cfg.Interface,
			ProxyPort:        cfg.ProxyPort,
			PublicIP:         w.GetCachedIP(),
			PublicIPv6:       w.GetCachedIPv6(),
			Healthy:          w.GetCachedHealthy(), // 健康状态读缓存
			Operator:         status.Operator,
			SignalDBM:        status.SignalDBM,
			NetworkMode:      status.NetworkMode,
			NetworkDuplex:    status.NetworkDuplex,
			VoWiFiActive:     s.pool.IsVoWiFiActive(w.ID), // 逐个设备判断 VoWiFi 状态，支持多设备
			VoWiFiRuntime:    s.getVoWiFiRuntimeDTO(w.ID),
			NetworkConnected: w.NetworkConnected(),
		}
		// 添加格式化流量
		if w.Proxy != nil {
			item.Traffic = w.Proxy.GetFormattedStats()
		}
		list = append(list, item)
	}
	respondOK(c, list)
}

// handleDeviceRescan 手动触发设备重新扫描
func (s *Server) handleDeviceRescan(c *gin.Context) {
	if s.pool == nil {
		fail(c, http.StatusServiceUnavailable, "", "服务未就绪")
		return
	}

	if err := s.pool.RescanAndReconnect(); err != nil {
		fail(c, http.StatusInternalServerError, "", "重新扫描失败: "+err.Error())
		return
	}

	respondOKWith(c, nil, gin.H{"message": "设备重新扫描完成"})
}

func (s *Server) handleDeviceDetail(c *gin.Context) {
	deviceID := deviceIDParam(c)
	worker := s.pool.GetWorker(deviceID)
	if worker == nil {
		fail(c, http.StatusNotFound, "", "设备未找到")
		return
	}

	respondOK(c, worker.GetStats())
}

func (s *Server) handleDeviceTraffic(c *gin.Context) {
	deviceID := deviceIDParam(c)
	worker := s.pool.GetWorker(deviceID)
	if worker == nil {
		fail(c, http.StatusNotFound, "", "设备未找到")
		return
	}
	iface := worker.Config.Interface
	tag := deviceID + "@" + iface
	ps, rx, tx, _ := s.data().Traffic.LatestMinuteDeltas("iface", tag)
	var ifaceObj any = nil
	if iface != "" {
		ifaceObj = gin.H{
			"interface":    iface,
			"period_start": ps,
			"rx_bytes":     rx,
			"tx_bytes":     tx,
			"rx":           server.FormatBytes(rx),
			"tx":           server.FormatBytes(tx),
			"rate":         server.FormatBytes(int64(float64(rx+tx)/60.0)) + "/s",
		}
	}

	type instTraffic struct {
		ID          string    `json:"id"`
		Name        string    `json:"name"`
		Mode        string    `json:"mode"`
		PeriodStart time.Time `json:"period_start"`
		RxBytes     int64     `json:"rx_bytes"`
		TxBytes     int64     `json:"tx_bytes"`
		Rx          string    `json:"rx"`
		Tx          string    `json:"tx"`
		Rate        string    `json:"rate"`
	}
	ctx := c.Request.Context()
	instances, err := s.proxyRepo.List(ctx)
	if err != nil {
		fail(c, http.StatusInternalServerError, "", "加载代理实例失败: "+err.Error())
		return
	}
	var insts []instTraffic
	for _, inst := range instances {
		if inst.DeviceID != deviceID {
			continue
		}
		mode := strings.ToLower(strings.TrimSpace(inst.Mode))
		if mode == "" {
			mode = "socks5"
		}
		ips, irx, itx, _ := s.data().Traffic.LatestMinuteDeltas("proxy_instance", inst.ID)
		if irx == 0 && itx == 0 {
			continue
		}
		insts = append(insts, instTraffic{
			ID:          inst.ID,
			Name:        inst.Name,
			Mode:        mode,
			PeriodStart: ips,
			RxBytes:     irx,
			TxBytes:     itx,
			Rx:          server.FormatBytes(irx),
			Tx:          server.FormatBytes(itx),
			Rate:        server.FormatBytes(int64(float64(irx+itx)/60.0)) + "/s",
		})
	}

	respondOK(c, gin.H{
		"device_id":       deviceID,
		"iface":           ifaceObj,
		"proxy_instances": insts,
	})
}

func (s *Server) handleHealth(c *gin.Context) {
	workers := s.pool.GetAllWorkers()
	allHealthy := true

	type DeviceHealth struct {
		Healthy          bool `json:"healthy"`
		ModemOK          bool `json:"modem_ok"`
		IfaceUp          bool `json:"iface_up"`
		NetworkConnected bool `json:"network_connected"`
		Signal           int  `json:"signal,omitempty"`
	}

	status := make(map[string]DeviceHealth)
	for _, w := range workers {
		modemOK := w.IsDeviceHealthy()
		ifaceUp := false
		healthy := modemOK

		if w.QMICore != nil {
			ifaceUp = w.QMICore.IsInterfaceUp()
		}

		// 获取信号 (非阻塞)
		signal := 0
		stats := w.GetStats()
		if s, ok := stats["signal"].(int); ok {
			signal = s
		}

		status[w.ID] = DeviceHealth{
			Healthy:          healthy,
			ModemOK:          modemOK,
			IfaceUp:          ifaceUp,
			NetworkConnected: ifaceUp,
			Signal:           signal,
		}
		if !healthy {
			allHealthy = false
		}
	}

	// 恒返回 200，健康与否放在 data.healthy 里。
	//
	// 原先整体不健康时返回 503。那与信封的不变式冲突——非 2xx 应当带 error，
	// 而"有设备不健康"并不是这次请求失败。且本端点需鉴权、返回逐设备明细，
	// 面向的是人和面板，不是按状态码判活的探针；那个角色由免鉴权的 /ping 承担。
	//
	// 载荷里也不再出现 "status" 字段：它与信封的成功/失败语义重名，
	// 表达的却是另一件事（集群健康度），放在一起只会让人读错。
	respondOK(c, gin.H{"healthy": allHealthy, "devices": status})
}

func (s *Server) handleStats(c *gin.Context) {
	workers := s.pool.GetAllWorkers()

	var totalSent, totalReceived, totalConns int64

	tagByID := map[string]string{}
	tags := make([]string, 0, len(workers))
	for _, w := range workers {
		if w == nil {
			continue
		}
		iface := w.Config.Interface
		if iface == "" {
			continue
		}
		tag := w.ID + "@" + iface
		tagByID[w.ID] = tag
		tags = append(tags, tag)
	}

	byTag, _ := s.data().Traffic.LatestMinuteDeltasBatch("iface", tags)

	deviceStats := make(map[string]map[string]int64)
	for _, w := range workers {
		if w == nil {
			continue
		}
		tag := tagByID[w.ID]
		if tag == "" {
			continue
		}
		d := byTag[tag]
		stats := map[string]int64{
			"bytes_sent":     d.TxBytes,
			"bytes_received": d.RxBytes,
			"connections":    0,
		}
		deviceStats[w.ID] = stats
		totalSent += d.TxBytes
		totalReceived += d.RxBytes
	}

	respondOK(c, gin.H{
		"total": gin.H{
			"bytes_sent":         totalSent,
			"bytes_received":     totalReceived,
			"connections":        totalConns,
			"sent_formatted":     server.FormatBytes(totalSent),
			"received_formatted": server.FormatBytes(totalReceived),
		},
		"devices": deviceStats,
	})
}

// handleStatus 返回所有设备的状态概览
func (s *Server) handleStatus(c *gin.Context) {
	workers := s.pool.GetAllWorkers()

	type DeviceStatusSummary struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		IMEI       string `json:"imei"`
		ICCID      string `json:"iccid"`
		Operator   string `json:"operator"`
		SignalDBM  int    `json:"signal_dbm"`
		RegStatus  string `json:"reg_status"`
		PublicIP   string `json:"public_ip"`
		PublicIPv6 string `json:"public_ipv6,omitempty"`
		ProxyPort  int    `json:"proxy_port"`
		Healthy    bool   `json:"healthy"`
	}

	list := make([]DeviceStatusSummary, 0, len(workers))
	for _, w := range workers {
		status := w.GetCachedDeviceStatus() // 设备摘要列表读缓存，0 IPC
		list = append(list, DeviceStatusSummary{
			ID:         w.ID,
			Name:       w.Config.Name,
			IMEI:       status.IMEI,
			ICCID:      status.ICCID,
			Operator:   status.Operator,
			SignalDBM:  status.SignalDBM,
			RegStatus:  status.RegStatusText,
			PublicIP:   w.GetCachedIP(),
			PublicIPv6: w.GetCachedIPv6(),
			ProxyPort:  w.Config.ProxyPort,
			Healthy:    w.GetCachedHealthy(), // 健康状态读缓存
		})
	}

	respondOK(c, list)
}

// handleStatusDetail 返回单个设备的详细状态
func (s *Server) handleStatusDetail(c *gin.Context) {
	deviceID := deviceIDParam(c)
	worker := s.pool.GetWorker(deviceID)
	if worker == nil {
		fail(c, http.StatusNotFound, "", "设备未找到")
		return
	}

	_ = worker.RefreshRuntime(c.Request.Context(), "status_detail")
	_ = worker.RefreshIdentityLive(c.Request.Context(), "status_detail")
	status := worker.ProjectDeviceStatus()

	response := gin.H{
		"id":                worker.ID,
		"name":              worker.Config.Name,
		"imei":              status.IMEI,
		"firmware":          status.Firmware,
		"iccid":             status.ICCID,
		"imsi":              status.IMSI,
		"native_spn":        status.NativeSPN,
		"native_mcc":        status.NativeMCC,
		"native_mnc":        status.NativeMNC,
		"gid1":              status.GID1,
		"gid2":              status.GID2,
		"pnn":               status.PNN,
		"opl":               status.OPL,
		"sim_service_table": status.SIMServiceTable,
		"operator":          status.Operator,
		"sim_inserted":      status.SimInserted,
		"signal_dbm":        status.SignalDBM,
		"signal_rsrp":       status.SignalRSRP,
		"signal_rsrq":       status.SignalRSRQ,
		"signal_sinr":       status.SignalSINR,
		"nr5g_signal_sinr":  status.NR5GSignalSINR,
		"radio_band":        status.RadioBand,
		"radio_channel":     status.RadioChannel,
		"reg_status":        status.RegStatus,
		"reg_status_text":   status.RegStatusText,
		"lac":               status.LAC,
		"cell_id":           status.CellID,
		"apn":               status.APN,
		"ims_status":        status.IMSStatus,
		"public_ip":         worker.GetCachedIP(),
		"public_ipv6":       worker.GetCachedIPv6(),
		"interface":         worker.Config.Interface,
		"proxy_port":        worker.Config.ProxyPort,
		"healthy":           worker.IsDeviceHealthy(),
		"network_connected": worker.NetworkConnected(),
	}

	if worker.Proxy != nil {
		response["traffic"] = worker.Proxy.GetFormattedStats()
	}

	vowifi := gin.H{
		"active": s.pool.IsVoWiFiActive(worker.ID),
	}
	if obs := s.pool.GetVoWiFiObs(worker.ID); obs != nil {
		for k, v := range obs {
			vowifi[k] = v
		}
	} else {
		if app := s.pool.GetVoWiFiAppForDevice(worker.ID); app != nil {
			status := app.Status()
			vowifi["imscore"] = status
			vowifi["smsip"] = status
		}
	}
	if s.voiceGW != nil {
		vowifi["voice"] = s.voiceGW.DeviceStatus(worker.ID)
	}
	response["vowifi"] = vowifi

	respondOK(c, response)
}

func (s *Server) handleSystemInfo(c *gin.Context) {
	respondOK(c, gin.H{
		"version":    global.Version,
		"build_time": global.BuildTime,
		"config":     viper.ConfigFileUsed(),
		"docs":       currentAPIDocsLinks(),
	})
}
