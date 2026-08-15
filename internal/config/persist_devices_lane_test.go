package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddAndUpdateDeviceInFilePersistsLane(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("devices: []\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	dev := DeviceConfig{
		ID:            "dev-cn",
		ModemIMEI:     "861234567890123",
		DeviceBackend: "qmi",
		Lane:          " CN ",
	}
	if err := AddDeviceInFile(path, dev); err != nil {
		t.Fatalf("AddDeviceInFile() error = %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() after add: %v", err)
	}
	if len(got.Devices) != 1 || got.Devices[0].Lane != DeviceLaneCN {
		t.Fatalf("after add lane=%q want cn, devices=%+v", got.Devices[0].Lane, got.Devices)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "lane: cn") {
		t.Fatalf("expected persisted lane: cn, got:\n%s", raw)
	}

	dev.Lane = "intl"
	if err := UpdateDeviceInFile(path, "dev-cn", dev); err != nil {
		t.Fatalf("UpdateDeviceInFile() error = %v", err)
	}
	got, err = Load(path)
	if err != nil {
		t.Fatalf("Load() after update: %v", err)
	}
	if got.Devices[0].Lane != DeviceLaneIntl {
		t.Fatalf("after update lane=%q want intl", got.Devices[0].Lane)
	}

	dev.Lane = ""
	if err := UpdateDeviceInFile(path, "dev-cn", dev); err != nil {
		t.Fatalf("UpdateDeviceInFile() clear lane: %v", err)
	}
	got, err = Load(path)
	if err != nil {
		t.Fatalf("Load() after clear: %v", err)
	}
	if got.Devices[0].Lane != "" {
		t.Fatalf("after clear lane=%q want empty", got.Devices[0].Lane)
	}
	raw, _ = os.ReadFile(path)
	if strings.Contains(string(raw), "lane:") {
		t.Fatalf("empty lane must be omitted from yaml, got:\n%s", raw)
	}
}

func TestAddDeviceInFilePersistsReaderName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("devices: []\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := AddDeviceInFile(path, DeviceConfig{
		ID:            "reader-alcor",
		DeviceBackend: DeviceBackendPCSC,
		ReaderName:    "Alcor Micro AU9540",
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Devices[0].ReaderName != "Alcor Micro AU9540" || got.Devices[0].DeviceBackend != DeviceBackendPCSC {
		t.Fatalf("got %+v", got.Devices[0])
	}
}
