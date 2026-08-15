package db

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yuanshuai1122/vodoge/internal/upstreamproxy"
)

func openTestDB(t *testing.T) {
	t.Helper()
	OpenTestDB(t)
	loadCountryTableFixture(t)
}

func loadCountryTableFixture(t *testing.T) {
	t.Helper()
	cachePath := filepath.Join(t.TempDir(), "mcc-mnc-table.json")
	rows := `[{"mcc":"310","mnc":"260","iso":"us","country":"United States","country_code":"US","network":"T-Mobile"}]`
	if err := os.WriteFile(cachePath, []byte(rows), 0o644); err != nil {
		t.Fatalf("WriteFile() error=%v", err)
	}
	result := upstreamproxy.InitCountryTable(context.Background(), upstreamproxy.CountryTableOptions{
		CachePath: cachePath,
		SourceURL: "http://127.0.0.1:1/missing",
	})
	if result.Err != nil {
		t.Fatalf("InitCountryTable() error=%v", result.Err)
	}
}

func TestUpstreamProxyCountryRuleSelectsEnabledProxyByHomeMCC(t *testing.T) {
	openTestDB(t)
	now := time.Now()
	if err := UpsertUpstreamProxy(UpstreamProxy{ID: "proxy-us", Name: "US", Addr: "127.0.0.1:1080", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("UpsertUpstreamProxy() error=%v", err)
	}
	if err := UpsertUpstreamProxyCountryRule(UpstreamProxyCountryRule{CountryCode: " us ", UpstreamProxyID: "proxy-us", Enabled: true}); err != nil {
		t.Fatalf("UpsertUpstreamProxyCountryRule() error=%v", err)
	}
	proxy, country, err := GetHomeMCCUpstreamProxy("310")
	if err != nil {
		t.Fatalf("GetHomeMCCUpstreamProxy() error=%v", err)
	}
	if country != "US" || proxy == nil || proxy.ID != "proxy-us" {
		t.Fatalf("proxy=%+v country=%q, want proxy-us/US", proxy, country)
	}
}

func TestUpstreamProxyCountryRuleDirectWhenNoRuleOrDisabled(t *testing.T) {
	openTestDB(t)
	if err := UpsertUpstreamProxy(UpstreamProxy{ID: "proxy-us", Addr: "127.0.0.1:1080", Enabled: true}); err != nil {
		t.Fatalf("UpsertUpstreamProxy() error=%v", err)
	}
	proxy, country, err := GetHomeMCCUpstreamProxy("310")
	if err != nil || proxy != nil || country != "US" {
		t.Fatalf("no rule proxy=%+v country=%q err=%v, want nil/US/nil", proxy, country, err)
	}
	if err := UpsertUpstreamProxyCountryRule(UpstreamProxyCountryRule{CountryCode: "US", UpstreamProxyID: "proxy-us", Enabled: false}); err != nil {
		t.Fatalf("UpsertUpstreamProxyCountryRule() error=%v", err)
	}
	proxy, country, err = GetHomeMCCUpstreamProxy("310")
	if err != nil || proxy != nil || country != "US" {
		t.Fatalf("disabled rule proxy=%+v country=%q err=%v, want nil/US/nil", proxy, country, err)
	}
}

func TestUpstreamProxyCountryRuleDirectWhenUnknownMCCOrMissingProxy(t *testing.T) {
	openTestDB(t)
	proxy, country, err := GetHomeMCCUpstreamProxy("404")
	if err != nil || proxy != nil || country != "" {
		t.Fatalf("unknown mcc proxy=%+v country=%q err=%v, want nil/empty/nil", proxy, country, err)
	}
	if err := UpsertUpstreamProxyCountryRule(UpstreamProxyCountryRule{CountryCode: "US", UpstreamProxyID: "missing", Enabled: true}); err != nil {
		t.Fatalf("UpsertUpstreamProxyCountryRule() error=%v", err)
	}
	proxy, country, err = GetHomeMCCUpstreamProxy("310")
	if err != nil || proxy != nil || country != "US" {
		t.Fatalf("missing proxy proxy=%+v country=%q err=%v, want nil/US/nil", proxy, country, err)
	}
}

func TestProfileBindingCRUDAndCascade(t *testing.T) {
	openTestDB(t)
	now := time.Now()
	if err := UpsertUpstreamProxy(UpstreamProxy{ID: "route-1", Addr: "127.0.0.1:1080", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("UpsertUpstreamProxy() error=%v", err)
	}
	iccid := "89441000400128014257"
	if err := UpsertProfileBinding(UpstreamProxyProfileBinding{
		ICCID: iccid, DeviceID: "ec20", ProfileName: "Vodafone", UpstreamProxyID: "route-1",
	}); err != nil {
		t.Fatalf("UpsertProfileBinding() error=%v", err)
	}
	got, err := GetProfileUpstreamProxy(iccid)
	if err != nil || got == nil || got.ID != "route-1" {
		t.Fatalf("GetProfileUpstreamProxy()=%+v err=%v", got, err)
	}
	if err := DeleteUpstreamProxy("route-1"); err != nil {
		t.Fatalf("DeleteUpstreamProxy() error=%v", err)
	}
	left, err := ListProfileBindings()
	if err != nil {
		t.Fatalf("ListProfileBindings() error=%v", err)
	}
	if len(left) != 0 {
		t.Fatalf("bindings after proxy delete=%+v", left)
	}
}

func TestValidProfileBindingICCID(t *testing.T) {
	if !ValidProfileBindingICCID("89441000400128014257") {
		t.Fatal("valid 20-digit rejected")
	}
	if ValidProfileBindingICCID("123") || ValidProfileBindingICCID("8944100040012801425a") {
		t.Fatal("invalid ICCID accepted")
	}
}
