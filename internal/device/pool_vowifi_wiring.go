package device

import (
	"fmt"

	"github.com/boa-z/vowifi-go/runtimehost/voicehost"
	"github.com/emiago/sipgo/sip"
	"github.com/yuanshuai1122/vodoge/internal/sipgw"

	"github.com/yuanshuai1122/vodoge/pkg/logger"
)

// SetVoiceGateway 注入 VoWiFi 语音网关，用于优先走 IMS 外呼/挂断路径。
func (p *Pool) SetVoiceGateway(g *voicehost.Gateway) {
	p.mu.Lock()
	p.voiceGateway = g
	p.mu.Unlock()
	p.voWiFiHost().ConfigureRuntimeDependencies(g, vowifiDeliveryStore{}, poolVoWiFiRuntimeDispatcher{pool: p})
}

// GetVoiceGateway 返回绑定的 VoiceGateway 实例
func (p *Pool) GetVoiceGateway() *voicehost.Gateway {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.voiceGateway
}

// SetSIPRegistrar 注入 SIP 注册器
// 由于此方法通常在 Worker 初始化之后才被调用，
// 需要回扫已有 Worker，给有 AudioDevice 但还没有 CSCallMgr 的 Worker 补创建
func (p *Pool) SetSIPRegistrar(r *sipgw.Registrar) {
	p.mu.Lock()
	p.sipRegistrar = r
	for _, w := range p.workers {
		logger.Debug(fmt.Sprintf("[%s] SetSIPRegistrar 回扫: AudioDevice=%q, CSCallMgr=%v, Modem=%v", w.ID, w.Config.AudioDevice, w.CSCallMgr != nil, w.Modem != nil))
		if w.Config.AudioDevice != "" && w.CSCallMgr == nil {
			w.CSCallMgr = newCSCallManagerForWorker(w, r)
			if w.CSCallMgr != nil {
				logger.Info(fmt.Sprintf("[%s] 已启用 CS 域语音桥接 (AudioDev: %s)", w.ID, w.Config.AudioDevice))
			}
		}
	}
	p.mu.Unlock()

	r.SetOnInvite(func(deviceID string, req *sip.Request, tx sip.ServerTransaction) {
		p.mu.RLock()
		w, ok := p.workers[deviceID]
		voiceGW := p.voiceGateway
		p.mu.RUnlock()

		if p.hasActiveIMSVoiceRoute(deviceID, voiceGW) {
			logger.Info(fmt.Sprintf("[%s] 外呼 INVITE: 优先走 VoWiFi IMS VoiceGateway", deviceID))
			voiceGW.HandleClientInvite(deviceID, req, tx)
			return
		}

		if !ok || w.CSCallMgr == nil {
			logger.Warn(fmt.Sprintf("[%s] 外呼 INVITE: 设备或 CSCall 管理器不存在", deviceID))
			tx.Respond(sip.NewResponseFromRequest(req, 404, "Not Found", nil))
			return
		}
		logger.Info(fmt.Sprintf("[%s] 外呼 INVITE: 回退到 CS 域语音桥接", deviceID))
		w.CSCallMgr.HandleOutboundInvite(deviceID, req, tx)
	})

	r.SetOnBye(func(deviceID string, req *sip.Request, tx sip.ServerTransaction) {
		p.routeClientBye(deviceID, req, tx)
	})

	r.SetOnCancel(func(deviceID string, req *sip.Request, tx sip.ServerTransaction) {
		p.routeClientCancel(deviceID, req, tx)
	})

	r.SetOnInfo(func(deviceID string, req *sip.Request, tx sip.ServerTransaction) {
		p.mu.RLock()
		voiceGW := p.voiceGateway
		p.mu.RUnlock()
		if p.hasActiveIMSVoiceRoute(deviceID, voiceGW) {
			voiceGW.HandleClientInfo(deviceID, req, tx)
			return
		}
		tx.Respond(sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil))
	})

	r.SetOnUpdate(func(deviceID string, req *sip.Request, tx sip.ServerTransaction) {
		p.mu.RLock()
		voiceGW := p.voiceGateway
		p.mu.RUnlock()
		if p.hasActiveIMSVoiceRoute(deviceID, voiceGW) {
			voiceGW.HandleClientUpdate(deviceID, req, tx)
			return
		}
		tx.Respond(sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil))
	})
}

// routeClientBye chooses the active dialog owner. A registered IMS agent is
// not sufficient proof: it can remain registered after VoWiFi stops while a
// CS call is in progress on the same worker.
func (p *Pool) routeClientBye(deviceID string, req *sip.Request, tx sip.ServerTransaction) {
	p.mu.RLock()
	w, ok := p.workers[deviceID]
	voiceGW := p.voiceGateway
	p.mu.RUnlock()
	callID := sipDialogCallID(req)
	if ok && w.CSCallMgr != nil && callID != "" && w.CSCallMgr.HasCall(callID) {
		w.CSCallMgr.HandleClientBye(callID)
		return
	}
	if p.hasActiveIMSVoiceRoute(deviceID, voiceGW) {
		voiceGW.HandleClientBye(deviceID, req, tx)
		return
	}
	if ok && w.CSCallMgr != nil && callID != "" {
		w.CSCallMgr.HandleClientBye(callID)
	}
}

// routeClientCancel mirrors routeClientBye for early-dialog cancellation.
func (p *Pool) routeClientCancel(deviceID string, req *sip.Request, tx sip.ServerTransaction) {
	p.mu.RLock()
	w, ok := p.workers[deviceID]
	voiceGW := p.voiceGateway
	p.mu.RUnlock()
	callID := sipDialogCallID(req)
	if ok && w.CSCallMgr != nil && callID != "" && w.CSCallMgr.HasCall(callID) {
		w.CSCallMgr.HandleClientCancel(callID)
		return
	}
	if p.hasActiveIMSVoiceRoute(deviceID, voiceGW) {
		voiceGW.HandleClientCancel(deviceID, req, tx)
		return
	}
	if ok && w.CSCallMgr != nil && callID != "" {
		w.CSCallMgr.HandleClientCancel(callID)
	}
}

func sipDialogCallID(req *sip.Request) string {
	if req == nil || req.CallID() == nil {
		return ""
	}
	return req.CallID().Value()
}

func (p *Pool) hasActiveIMSVoiceRoute(deviceID string, voiceGW *voicehost.Gateway) bool {
	return p != nil && voiceGW != nil && p.IsVoWiFiActive(deviceID) && voiceGW.GetAgent(deviceID) != nil
}
