// 单设备概览的 SSE 实时流。
//
// 10s 定时快照 + VoWiFi 状态变更 + 实时流量三路合并。
// 相邻两帧无实质变化时跳过推送，见 shouldSkipOverviewStatePush。
package api

import (
	"context"
	"strings"
	"time"

	"github.com/yuanshuai1122/vodoge/internal/config"
	"github.com/yuanshuai1122/vodoge/internal/db"
	"github.com/yuanshuai1122/vodoge/internal/modem"
	proxytraffic "github.com/yuanshuai1122/vodoge/internal/proxy/traffic"
	"github.com/yuanshuai1122/vodoge/pkg/logger"

	"github.com/gin-gonic/gin"
)

type overviewStreamEmitVersion struct {
	VoWiFiActive    bool
	LifecyclePhase  string
	LifecycleReason string
	HasRuntime      bool
	Phase           string
	TunnelReady     bool
	IMSReady        bool
	SMSReady        bool
	LastErrorClass  string
}

func newOverviewStreamEmitVersion(item deviceMgmtOverviewLiteItem) overviewStreamEmitVersion {
	v := overviewStreamEmitVersion{
		VoWiFiActive:    item.VoWiFiActive,
		LifecyclePhase:  item.LifecyclePhase,
		LifecycleReason: item.LifecycleReason,
	}
	if item.VoWiFiRuntime != nil {
		v.HasRuntime = true
		v.Phase = item.VoWiFiRuntime.Phase
		v.TunnelReady = item.VoWiFiRuntime.TunnelReady
		v.IMSReady = item.VoWiFiRuntime.IMSReady
		v.SMSReady = item.VoWiFiRuntime.SMSReady
		v.LastErrorClass = item.VoWiFiRuntime.LastErrorClass
	}
	return v
}

func shouldSkipOverviewStatePush(last *overviewStreamEmitVersion, curr overviewStreamEmitVersion) bool {
	if last == nil {
		return false
	}
	return *last == curr
}

// handleDeviceMgmtOverviewStreamSingle 给前端管理的概览信息提供带有动态刷新的 SSE 推流（仅针对选中的单个设备）
func (s *Server) handleDeviceMgmtOverviewStreamSingle(c *gin.Context) {
	s.prepareSSE(c)
	// 开发期前端需直连本端口订阅：Next dev 的 rewrite 代理会缓冲 SSE，
	// 经代理时一个字节都收不到。仅在 Debug 模式下放行 localhost，见 isAllowedSSEOrigin。

	deviceID := deviceIDParam(c)
	if deviceID == "" {
		return
	}

	worker := s.pool.GetWorker(deviceID)
	if worker != nil {
		worker.IncStreamSub()
		defer worker.DecStreamSub()
	}

	notify := c.Writer.CloseNotify()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// 订阅 VoWiFi 运行态变更——状态一变（如 IMS 注册成功）立即推送，无需等待 Ticker。
	// 若 VoWiFi 未启动则 stateCh 为 nil，nil channel 在 select 中永远阻塞，行为安全。
	stateCh, unsubState := s.pool.SubscribeVoWiFiState(deviceID)
	defer unsubState()
	ussdCh, unsubUSSD := s.pool.SubscribeVoWiFiUSSD(deviceID)
	defer unsubUSSD()
	trafficStream := overviewTrafficStreamState{
		subscriber: s.trafficRT,
		deviceID:   deviceID,
		ctx:        c.Request.Context(),
	}
	defer trafficStream.stop()
	var trafficCh <-chan proxytraffic.RealtimeSnapshot

	var (
		cachedCfg *config.DeviceConfig
		lastSent  *overviewStreamEmitVersion
	)
	getConfig := func(refresh bool) *config.DeviceConfig {
		if !refresh && cachedCfg != nil {
			return cachedCfg
		}
		md, _ := config.GetDeviceByID(deviceID)
		cachedCfg = md
		return md
	}

	sendData := func(refreshConfig bool, fromStateEvent bool) {
		md := getConfig(refreshConfig)
		if md == nil {
			return
		}
		var item deviceMgmtOverviewLiteItem

		w := s.pool.GetWorker(deviceID)
		if w != nil {
			status := w.GetCachedDeviceStatus()
			trueVal := true
			// 用运行时投影(w.Config)合并展示，使策略字段反映跟卡走的有效值
			item = s.buildOverviewLiteDetailItemFromWorker(w, overviewDisplayConfig(w.Config, *md, true), status, &trueVal)
			if overviewRealtimeTrafficEnabled(item) {
				tag := w.ID + "@" + md.Interface
				ps, rx, tx, _ := s.data().Traffic.LatestMinuteDeltas("iface", tag)
				item.Traffic, item.TrafficRaw, item.TrafficMeta = buildTrafficOverviewFields(md.Interface, db.LatestMinuteDeltas{
					PeriodStart: ps,
					RxBytes:     rx,
					TxBytes:     tx,
				}, time.Now())
			}
		} else {
			trueVal := true
			pol := s.resolveOfflineDevicePolicy(deviceID)
			item = deviceMgmtOverviewLiteItem{
				ID:                     md.ID,
				Name:                   md.Name,
				Running:                false,
				Healthy:                false,
				ControlOnline:          false,
				PublicIP:               "",
				Interface:              md.Interface,
				ControlDevice:          md.ControlDevice,
				ESIMTransport:          config.NormalizeESIMTransport(md.ESIMTransport),
				ATPort:                 md.ATPort,
				AudioDevice:            md.AudioDevice,
				SMSEnabled:             pol.SMSEnabled,
				NetworkEnabled:         pol.NetworkEnabled,
				VoWiFiEnabled:          pol.VoWiFiEnabled,
				VoWiFiActive:           false,
				NetworkConnected:       false,
				RegistrationStateLabel: registrationStateLabel(0),
				RadioLiveOK:            &trueVal,
				Modem:                  modem.DeviceStatus{},
				Traffic:                nil,
				BackendMode:            resolveOfflineBackendMode(*md),
			}
			s.applyLifecycleToOverviewLiteItem(&item, nil, *md)
		}

		trafficCh = trafficStream.sync(item)
		curr := newOverviewStreamEmitVersion(item)
		if fromStateEvent && shouldSkipOverviewStatePush(lastSent, curr) {
			return
		}
		lastSent = &curr
		if fromStateEvent {
			phase := ""
			if item.VoWiFiRuntime != nil {
				phase = item.VoWiFiRuntime.Phase
			}
			logger.Debug("overview SSE 推送 VoWiFi 状态变更", "device", deviceID, "phase", phase)
		}

		// 仍然使用 devices 结构体包裹返回单项从而无缝对接前台旧结构
		c.SSEvent("overview", gin.H{"devices": []deviceMgmtOverviewLiteItem{item}})
		c.Writer.Flush()
	}

	sendData(true, false)

	for {
		select {
		case <-notify:
			return
		case <-c.Request.Context().Done():
			return
		case <-s.shutdownCh:
			return
		case <-ticker.C:
			sendData(true, false)
		case <-stateCh: // VoWiFi 状态变化（隧道建立/IMS 注册/SMS 就绪等），立即推送
			sendData(false, true)
		case ev := <-ussdCh:
			c.SSEvent("ussd", ev)
			c.Writer.Flush()
		case snap, ok := <-trafficCh:
			if !ok {
				trafficStream.stop()
				trafficCh = nil
				continue
			}
			c.SSEvent("traffic", snap)
			c.Writer.Flush()
		}
	}
}

type overviewTrafficStreamState struct {
	subscriber  realtimeTrafficSubscriber
	deviceID    string
	ctx         context.Context
	ch          <-chan proxytraffic.RealtimeSnapshot
	unsubscribe func()
}

func overviewRealtimeTrafficEnabled(item deviceMgmtOverviewLiteItem) bool {
	return item.NetworkEnabled && item.NetworkConnected
}

func resolveOfflineBackendMode(cfg config.DeviceConfig) string {
	m := strings.ToLower(strings.TrimSpace(cfg.DeviceBackend))
	if m == "" && strings.TrimSpace(cfg.ControlDevice) != "" {
		return "qmi"
	}
	if m == "qmi" {
		return "qmi"
	}
	return "at"
}
