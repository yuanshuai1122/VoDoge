package api

import (
	"testing"

	"github.com/yuanshuai1122/vodog/internal/data/repo"
	"github.com/yuanshuai1122/vodog/internal/db"
)

// 这两个测试原本要 db.OpenTestDB(t)：起容器、清空全库、往 devices 和 card_policies
// 插两行，只为了验证一段纯粹的取值优先级逻辑。抽出 repository 层之后不需要了。

func TestResolveOfflineDevicePolicyFromCard(t *testing.T) {
	store, cp, sim, _, _, _ := repo.NewFakeStore()
	iccid := "8986007777777777777"
	sim.CurrentICCIDForDeviceFn = func(deviceID string) string {
		if deviceID == "wwan0" {
			return iccid
		}
		return ""
	}
	cp.GetFn = func(string) (db.CardPolicy, error) {
		return db.CardPolicy{
			ICCID: iccid, NetworkEnabled: true, VoWiFiEnabled: true,
			IPVersion: "v4v6", Source: "user",
		}, nil
	}
	s := &Server{store: store}

	got := s.resolveOfflineDevicePolicy("wwan0")
	if !got.NetworkEnabled || !got.VoWiFiEnabled {
		t.Fatalf("应取卡策略: %+v", got)
	}
	if got.IPVersion != "v4v6" || !got.SMSEnabled {
		t.Fatalf("ip/sms 错: %+v", got)
	}
}

// 查不到卡时必须全关：离线设备的展示宁可保守，也不要显示成"已联网"。
func TestResolveOfflineDevicePolicyNoCardSafeDefault(t *testing.T) {
	store, _, _, _, _, _ := repo.NewFakeStore()
	s := &Server{store: store}

	got := s.resolveOfflineDevicePolicy("unknown-device")
	if got.NetworkEnabled || got.VoWiFiEnabled {
		t.Fatalf("无卡应全关: %+v", got)
	}
	if !got.SMSEnabled || got.IPVersion != "v4" {
		t.Fatalf("默认应 sms=on/ip=v4: %+v", got)
	}
}

// 有 ICCID 但没有对应策略行，与完全没有卡一样走安全默认，而不是把零值当成策略。
func TestResolveOfflineDevicePolicyMissingPolicyRow(t *testing.T) {
	store, cp, sim, _, _, _ := repo.NewFakeStore()
	sim.CurrentICCIDForDeviceFn = func(string) string { return "8986001" }
	cp.GetFn = func(string) (db.CardPolicy, error) {
		return db.CardPolicy{}, db.ErrCardPolicyNotFound
	}
	s := &Server{store: store}

	got := s.resolveOfflineDevicePolicy("dev-1")
	if got.NetworkEnabled || got.VoWiFiEnabled {
		t.Fatalf("无策略行应全关: %+v", got)
	}
	if !got.SMSEnabled || got.IPVersion != "v4" {
		t.Fatalf("默认应 sms=on/ip=v4: %+v", got)
	}
}
