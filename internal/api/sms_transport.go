package api

import "github.com/yuanshuai1122/vodog/internal/config"

const (
	smsTransportCellular = "cellular"
	smsTransportIMS      = "ims"
)

// smsSendPlan 决定这一条短信先走哪条通道、失败后能不能换一条。
//
// 对照 VoCat：国外线（EG25）在蜂窝登不上时用 IMS 补通路。
// 国内线（EC25-CN）保持原行为：VoWiFi 开着就只走 IMS，否则只走蜂窝。
// 软件路径已齐；EC25 蜂窝短信、EG25 IMS 出口仍要真机再跑一遍。
type smsSendPlan struct {
	Primary  string
	Fallback string
	Reason   string
}

func radioRegistered(regStatus int) bool {
	return regStatus == 1 || regStatus == 5
}

func planSMSSend(lane string, vowifiActive, radioOK bool) smsSendPlan {
	switch config.NormalizeLane(lane) {
	case config.DeviceLaneIntl:
		if radioOK {
			plan := smsSendPlan{
				Primary: smsTransportCellular,
				Reason:  "国外线已驻网，先走蜂窝",
			}
			if vowifiActive {
				plan.Fallback = smsTransportIMS
				plan.Reason = "国外线已驻网，先走蜂窝；失败再走 IMS"
			}
			return plan
		}
		if vowifiActive {
			return smsSendPlan{
				Primary:  smsTransportIMS,
				Fallback: smsTransportCellular,
				Reason:   "国外线未驻网，先走 IMS；失败再试蜂窝",
			}
		}
		return smsSendPlan{
			Primary: smsTransportCellular,
			Reason:  "国外线未驻网且 IMS 未就绪，只能试蜂窝",
		}
	default:
		if vowifiActive {
			return smsSendPlan{
				Primary: smsTransportIMS,
				Reason:  "VoWiFi 已启用，走 IMS",
			}
		}
		return smsSendPlan{
			Primary: smsTransportCellular,
			Reason:  "走蜂窝短信",
		}
	}
}
