package device

import (
	"strings"

	"github.com/yuanshuai1122/vodoge/internal/config"
)

func (p *Pool) ResolveQMIRecoveryAttachment(cfg config.DeviceConfig) qmiRecoveryScanDecision {
	if !requiresQMICore(cfg) {
		return qmiRecoveryScanDecision{Ready: true, Reason: "non_qmi"}
	}

	live, discoveryAvailable := p.qmiRecoveryLiveCandidates(cfg)
	configuredIMEI := strings.TrimSpace(cfg.ModemIMEI)
	if configuredIMEI != "" {
		if !discoveryAvailable {
			return qmiRecoveryScanGate(cfg, live, discoveryAvailable)
		}
		for _, candidate := range live {
			if config.IMEIMatches(candidate.IMEI, configuredIMEI) {
				return qmiRecoveryScanDecision{
					Ready:      true,
					Reason:     "live_imei_match",
					Attachment: candidate.Device,
				}
			}
		}
		// IMEI 仍不可读时才允许路径兜底；已读到不同 IMEI 由扫描门控拒绝，
		// 防止 cdc-wdm/wwan 重编号把另一张卡接管进来。
		return qmiRecoveryScanGate(cfg, live, discoveryAvailable)
	}

	return qmiRecoveryScanGate(cfg, live, discoveryAvailable)
}
