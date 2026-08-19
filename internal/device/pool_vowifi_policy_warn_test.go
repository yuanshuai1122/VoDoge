package device

import "testing"

// MCC 禁令是一条永远不会改变的结论，而目标态协调每 30 秒扫一遍全部 worker。
// 不去重的话真机上就是每半分钟一条 WARN，把真正的问题埋掉。
func TestVoWiFiPolicyWarnOncePerDeviceMCC(t *testing.T) {
	p := &Pool{}

	if !p.shouldWarnVoWiFiPolicyBlocked("dev-a", "460") {
		t.Fatal("首次必须提醒")
	}
	for i := 0; i < 10; i++ {
		if p.shouldWarnVoWiFiPolicyBlocked("dev-a", "460") {
			t.Fatalf("第 %d 次重复提醒：同一设备同一 MCC 只应提醒一次", i+2)
		}
	}

	// 另一台设备各算各的
	if !p.shouldWarnVoWiFiPolicyBlocked("dev-b", "460") {
		t.Fatal("不同设备应各自提醒一次")
	}

	// 换卡导致 MCC 变化时要重新提醒，否则换卡后真出问题会毫无提示
	if !p.shouldWarnVoWiFiPolicyBlocked("dev-a", "466") {
		t.Fatal("MCC 变化后应重新提醒")
	}
	if p.shouldWarnVoWiFiPolicyBlocked("dev-a", "466") {
		t.Fatal("新 MCC 同样只提醒一次")
	}
	// 换回原 MCC 也算变化，应再提醒一次
	if !p.shouldWarnVoWiFiPolicyBlocked("dev-a", "460") {
		t.Fatal("MCC 变回原值也应重新提醒")
	}
}

// nil 接收者不得 panic，并且要保守地选择“提醒”而不是静默吞掉。
func TestVoWiFiPolicyWarnNilPoolWarns(t *testing.T) {
	var p *Pool
	if !p.shouldWarnVoWiFiPolicyBlocked("dev-a", "460") {
		t.Fatal("nil Pool 应保守地返回 true")
	}
}
