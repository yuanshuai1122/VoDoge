package device

import (
	"strings"
	"testing"

	"github.com/yuanshuai1122/vodog/internal/config"
	"github.com/yuanshuai1122/vodog/internal/pcsc"
	innersim "github.com/yuanshuai1122/vodog/internal/sim"
)

func TestAddPCSCReaderWorkerEnablesVoWiFi(t *testing.T) {
	p := NewPool(&config.Config{})
	w, err := p.AddWorkerFromConfig(config.DeviceConfig{
		ID:            "reader-vowifi",
		DeviceBackend: config.DeviceBackendPCSC,
		ReaderName:    "Alcor Micro AU9540",
	})
	if err != nil {
		t.Fatalf("AddWorkerFromConfig: %v", err)
	}
	if !w.Config.VoWiFiEnabled {
		t.Fatal("reader worker must enable VoWiFi")
	}
	if w.Config.NetworkEnabled {
		t.Fatal("reader worker must not enable cellular data")
	}
	modem, err := newVoWiFiModemInterface(w, w.ID)
	if err != nil {
		t.Fatalf("newVoWiFiModemInterface: %v", err)
	}
	if _, ok := modem.(*pcscModemAdapter); !ok {
		t.Fatalf("modem type=%T want *pcscModemAdapter", modem)
	}
	if _, ok := modem.(innersim.ATModem); !ok {
		t.Fatal("pcsc adapter must implement sim.ATModem for AKA")
	}
	if got := BuildAKAProvider(w, w.ID); got == nil {
		t.Fatal("BuildAKAProvider() = nil, want APDU provider")
	}
}

func TestPCSCIdentityAndVoWiFiProfileFromCard(t *testing.T) {
	startPCSCTestDaemon(t, "Alcor Micro AU9540 00 00")
	p := NewPool(&config.Config{})
	w, err := p.AddWorkerFromConfig(config.DeviceConfig{
		ID:            "reader-aka",
		DeviceBackend: config.DeviceBackendPCSC,
		ReaderName:    "Alcor Micro AU9540 00 00",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.RefreshIdentityLive(nil, "test"); err != nil {
		t.Fatalf("RefreshIdentityLive: %v", err)
	}
	if w.state.Identity.IMSI != "460001234567890" {
		t.Fatalf("imsi=%q", w.state.Identity.IMSI)
	}
	if w.state.Identity.ICCID != "8986001234567890123" {
		t.Fatalf("iccid=%q", w.state.Identity.ICCID)
	}
	if w.state.Identity.NativeMCC != "460" || w.state.Identity.NativeMNC != "00" {
		t.Fatalf("plmn=%s/%s", w.state.Identity.NativeMCC, w.state.Identity.NativeMNC)
	}
	profile, err := p.buildVoWiFiStartProfile(w, "test")
	if err != nil {
		t.Fatalf("buildVoWiFiStartProfile: %v", err)
	}
	if profile.IMSI != "460001234567890" || profile.MCC != "460" || profile.MNC != "00" {
		t.Fatalf("profile=%+v", profile)
	}
	if strings.TrimSpace(profile.IMEI) == "" || len(profile.IMEI) != 15 {
		t.Fatalf("imei=%q", profile.IMEI)
	}
}

func startPCSCTestDaemon(t *testing.T, name string) {
	t.Helper()
	pcsc.StartFakeDaemonForTest(t, name)
}
