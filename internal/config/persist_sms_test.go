package config

import (
	"os"
	"strings"
	"testing"
)

func TestUpdateSMSHourlyLimitInFileCreatesNode(t *testing.T) {
	path := writeTempConfig(t, `
server:
  port: 7575
web:
  username: admin
`)
	if err := UpdateSMSHourlyLimitInFile(path, 8); err != nil {
		t.Fatalf("UpdateSMSHourlyLimitInFile() error=%v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "sms:") || !strings.Contains(text, "hourly_limit: 8") {
		t.Fatalf("missing sms.hourly_limit in:\n%s", text)
	}
	if !strings.Contains(text, "username: admin") {
		t.Fatalf("unrelated keys were dropped:\n%s", text)
	}
}

func TestUpdateSMSHourlyLimitInFileRejectsOutOfRange(t *testing.T) {
	path := writeTempConfig(t, "server:\n  port: 7575\n")
	if err := UpdateSMSHourlyLimitInFile(path, 201); err == nil {
		t.Fatal("expected error for 201")
	}
	if err := UpdateSMSHourlyLimitInFile(path, -1); err == nil {
		t.Fatal("expected error for -1")
	}
}

func TestLoadSMSHourlyLimitDefaultAndZero(t *testing.T) {
	missing := writeTempConfig(t, `
server:
  port: 7575
`)
	cfg, err := Load(missing)
	if err != nil {
		t.Fatalf("Load() error=%v", err)
	}
	if cfg.SMS.HourlyLimit != DefaultSMSHourlyLimit {
		t.Fatalf("default HourlyLimit=%d want %d", cfg.SMS.HourlyLimit, DefaultSMSHourlyLimit)
	}

	unlimited := writeTempConfig(t, `
server:
  port: 7575
sms:
  hourly_limit: 0
`)
	cfg, err = Load(unlimited)
	if err != nil {
		t.Fatalf("Load(0) error=%v", err)
	}
	if cfg.SMS.HourlyLimit != 0 {
		t.Fatalf("zero should stay unlimited, got %d", cfg.SMS.HourlyLimit)
	}

	clamped := writeTempConfig(t, `
server:
  port: 7575
sms:
  hourly_limit: 999
`)
	cfg, err = Load(clamped)
	if err != nil {
		t.Fatalf("Load(999) error=%v", err)
	}
	if cfg.SMS.HourlyLimit != MaxSMSHourlyLimit {
		t.Fatalf("clamped HourlyLimit=%d want %d", cfg.SMS.HourlyLimit, MaxSMSHourlyLimit)
	}
}

func TestUpdateDeviceLimitInFileAndLoad(t *testing.T) {
	path := writeTempConfig(t, `
server:
  port: 7575
  debug: false
`)
	if err := UpdateDeviceLimitInFile(path, 7); err != nil {
		t.Fatalf("UpdateDeviceLimitInFile() error=%v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "max_devices: 7") || !strings.Contains(text, "port:") {
		t.Fatalf("unexpected file:\n%s", text)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error=%v", err)
	}
	if cfg.Server.MaxDevices != 7 {
		t.Fatalf("MaxDevices=%d", cfg.Server.MaxDevices)
	}
}

func TestUpdateAccessPolicyInFile(t *testing.T) {
	path := writeTempConfig(t, `
server:
  port: 7575
  max_devices: 5
`)
	if err := UpdateAccessPolicyInFile(path, AccessPolicy{
		Mode:              "internal",
		AllowedCIDRs:      []string{"203.0.113.0/24"},
		TrustProxyHeaders: true,
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "203.0.113.0/24") || !strings.Contains(text, "max_devices: 5") {
		t.Fatalf("file:\n%s", text)
	}
}

func TestNormalizeDeviceLimit(t *testing.T) {
	if got := NormalizeDeviceLimit(0); got != DefaultDeviceLimit {
		t.Fatalf("0=%d", got)
	}
	if got := NormalizeDeviceLimit(3); got != 3 {
		t.Fatalf("3=%d", got)
	}
	if got := NormalizeDeviceLimit(99); got != MaxDeviceLimit {
		t.Fatalf("99=%d", got)
	}
}

func TestNormalizeSMSHourlyLimit(t *testing.T) {
	if got := NormalizeSMSHourlyLimit(-3); got != 0 {
		t.Fatalf("neg=%d", got)
	}
	if got := NormalizeSMSHourlyLimit(0); got != 0 {
		t.Fatalf("zero=%d", got)
	}
	if got := NormalizeSMSHourlyLimit(20); got != 20 {
		t.Fatalf("20=%d", got)
	}
	if got := NormalizeSMSHourlyLimit(500); got != MaxSMSHourlyLimit {
		t.Fatalf("500=%d", got)
	}
}
