package device

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yuanshuai1122/vodog/internal/db"
	"github.com/yuanshuai1122/vodog/internal/upstreamproxy"
)

func loadDeviceCountryTableFixture(t *testing.T) {
	t.Helper()
	cachePath := filepath.Join(t.TempDir(), "mcc-mnc-table.json")
	rows := `[{"mcc":"310","mnc":"260","iso":"us","country":"United States","country_code":"US","network":"T-Mobile"}]`
	if err := os.WriteFile(cachePath, []byte(rows), 0o644); err != nil {
		t.Fatalf("WriteFile() error=%v", err)
	}
	result := upstreamproxy.InitCountryTable(context.Background(), upstreamproxy.CountryTableOptions{CachePath: cachePath})
	if result.Err != nil {
		t.Fatalf("InitCountryTable() error=%v", result.Err)
	}
}

func openDeviceTestDB(t *testing.T) {
	t.Helper()
	db.OpenTestDB(t)
}

func TestResolveVoWiFiCountryProxySelectsUSProxy(t *testing.T) {
	openDeviceTestDB(t)
	loadDeviceCountryTableFixture(t)
	if err := db.UpsertUpstreamProxy(db.UpstreamProxy{ID: "proxy-us", Addr: "127.0.0.1:1080", Enabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("UpsertUpstreamProxy() error=%v", err)
	}
	if err := db.UpsertUpstreamProxyCountryRule(db.UpstreamProxyCountryRule{CountryCode: "US", UpstreamProxyID: "proxy-us", Enabled: true}); err != nil {
		t.Fatalf("UpsertUpstreamProxyCountryRule() error=%v", err)
	}
	got := resolveVoWiFiCountryProxy("intl", "310", "", "trace-1", "dev-1")
	if got == nil || got.ID != "proxy-us" || got.Addr != "127.0.0.1:1080" || !got.Enabled {
		t.Fatalf("resolveVoWiFiCountryProxy()=%+v, want proxy-us", got)
	}
}

func TestResolveVoWiFiCountryProxyDirectWhenNoCountryRule(t *testing.T) {
	openDeviceTestDB(t)
	loadDeviceCountryTableFixture(t)
	if got := resolveVoWiFiCountryProxy("intl", "404", "", "trace-1", "dev-1"); got != nil {
		t.Fatalf("resolveVoWiFiCountryProxy(404)=%+v, want nil direct", got)
	}
}

func TestResolveVoWiFiCountryProxyCNLaneStaysDirect(t *testing.T) {
	openDeviceTestDB(t)
	loadDeviceCountryTableFixture(t)
	if err := db.UpsertUpstreamProxy(db.UpstreamProxy{ID: "proxy-us", Addr: "127.0.0.1:1080", Enabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("UpsertUpstreamProxy() error=%v", err)
	}
	if err := db.UpsertUpstreamProxyCountryRule(db.UpstreamProxyCountryRule{CountryCode: "US", UpstreamProxyID: "proxy-us", Enabled: true}); err != nil {
		t.Fatalf("UpsertUpstreamProxyCountryRule() error=%v", err)
	}
	if got := resolveVoWiFiCountryProxy("cn", "310", "", "trace-1", "dev-cn"); got != nil {
		t.Fatalf("cn lane must ignore country upstream, got %+v", got)
	}
}

func TestResolveVoWiFiCountryProxyProfileBindingBeatsCountry(t *testing.T) {
	openDeviceTestDB(t)
	loadDeviceCountryTableFixture(t)
	now := time.Now()
	if err := db.UpsertUpstreamProxy(db.UpstreamProxy{ID: "proxy-us", Addr: "127.0.0.1:1080", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("UpsertUpstreamProxy(us) error=%v", err)
	}
	if err := db.UpsertUpstreamProxy(db.UpstreamProxy{ID: "proxy-uk", Addr: "127.0.0.1:1081", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("UpsertUpstreamProxy(uk) error=%v", err)
	}
	if err := db.UpsertUpstreamProxyCountryRule(db.UpstreamProxyCountryRule{CountryCode: "US", UpstreamProxyID: "proxy-us", Enabled: true}); err != nil {
		t.Fatalf("UpsertUpstreamProxyCountryRule() error=%v", err)
	}
	iccid := "89441000400128014257"
	if err := db.UpsertProfileBinding(db.UpstreamProxyProfileBinding{
		ICCID: iccid, DeviceID: "dev-1", ProfileName: "Vodafone", UpstreamProxyID: "proxy-uk",
	}); err != nil {
		t.Fatalf("UpsertProfileBinding() error=%v", err)
	}
	got := resolveVoWiFiCountryProxy("intl", "310", iccid, "trace-1", "dev-1")
	if got == nil || got.ID != "proxy-uk" {
		t.Fatalf("profile binding should win, got %+v", got)
	}
}

func TestResolveVoWiFiCountryProxyCNLaneIgnoresProfileBinding(t *testing.T) {
	openDeviceTestDB(t)
	now := time.Now()
	if err := db.UpsertUpstreamProxy(db.UpstreamProxy{ID: "proxy-uk", Addr: "127.0.0.1:1081", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("UpsertUpstreamProxy() error=%v", err)
	}
	iccid := "89441000400128014257"
	if err := db.UpsertProfileBinding(db.UpstreamProxyProfileBinding{
		ICCID: iccid, DeviceID: "dev-cn", ProfileName: "CN", UpstreamProxyID: "proxy-uk",
	}); err != nil {
		t.Fatalf("UpsertProfileBinding() error=%v", err)
	}
	if got := resolveVoWiFiCountryProxy("cn", "460", iccid, "trace-1", "dev-cn"); got != nil {
		t.Fatalf("cn lane must ignore profile binding, got %+v", got)
	}
}
