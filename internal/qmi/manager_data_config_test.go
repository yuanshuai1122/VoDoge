package qmicore

import (
	"context"
	"errors"
	"testing"

	qmimanager "github.com/boa-z/quectel-qmi-go/pkg/manager"
	"github.com/boa-z/quectel-qmi-go/pkg/qmi"
	"github.com/yuanshuai1122/vodoge/internal/config"
)

func TestSetDataConfigRebuildsIdleManagerWithPolicy(t *testing.T) {
	m := New(config.DeviceConfig{ID: "qmi-policy", APN: "internet", IPVersion: "v4"}, &qmimanager.ModemDevice{})
	old := m.currentQMIManager()
	var rebuilt []qmimanager.Config
	m.qmiManagerFactory = func(cfg qmimanager.Config, logger qmimanager.Logger) *qmimanager.Manager {
		rebuilt = append(rebuilt, cfg)
		return qmimanager.New(cfg, logger)
	}

	if err := m.SetDataConfig(context.Background(), DataConfig{APN: "ims", IPVersion: "v4v6"}); err != nil {
		t.Fatalf("SetDataConfig() error = %v", err)
	}
	if got := m.DataConfigSnapshot(); got != (DataConfig{APN: "ims", IPVersion: "v4v6"}) {
		t.Fatalf("requested data config = %+v", got)
	}
	if got := m.AppliedDataConfigSnapshot(); got != (DataConfig{APN: "ims", IPVersion: "v4v6"}) {
		t.Fatalf("applied data config = %+v", got)
	}
	if m.currentQMIManager() == old {
		t.Fatal("policy change should replace the idle underlying manager")
	}
	if len(rebuilt) != 1 {
		t.Fatalf("rebuild count=%d, want 1", len(rebuilt))
	}
	if got := rebuilt[0]; got.APN != "ims" || !got.EnableIPv4 || !got.EnableIPv6 {
		t.Fatalf("rebuilt QMI config = APN %q v4=%v v6=%v", got.APN, got.EnableIPv4, got.EnableIPv6)
	}
}

func TestSetDataConfigCanonicalizesIPFamilyAndAvoidsNoopRebuild(t *testing.T) {
	m := New(config.DeviceConfig{ID: "qmi-policy", APN: "internet", IPVersion: "v4"}, &qmimanager.ModemDevice{})
	old := m.currentQMIManager()
	if err := m.SetDataConfig(context.Background(), DataConfig{APN: " internet ", IPVersion: "ipv4"}); err != nil {
		t.Fatalf("SetDataConfig() error = %v", err)
	}
	if m.currentQMIManager() != old {
		t.Fatal("equivalent canonical policy should not rebuild manager")
	}
	if got := m.DataConfigSnapshot(); got != (DataConfig{APN: "internet", IPVersion: "v4"}) {
		t.Fatalf("canonical data config = %+v", got)
	}
}

func TestSetDataConfigInvalidatesAPDUSessions(t *testing.T) {
	m := New(config.DeviceConfig{ID: "qmi-policy", APN: "internet", IPVersion: "v4"}, &qmimanager.ModemDevice{})
	m.bindAPDUSession(7, "vowifi_aka")

	if err := m.SetDataConfig(context.Background(), DataConfig{APN: "ims", IPVersion: "v4v6"}); err != nil {
		t.Fatalf("SetDataConfig() error = %v", err)
	}
	if m.hasAPDUSession(7) {
		t.Fatal("data-policy rebuild retained a logical APDU session from the old QMI manager")
	}
	if _, err := m.TransmitEUICCAPDU(context.Background(), 1, 7, []byte{0x00, 0xA4}); err == nil || err.Error() != "qmi_apdu_session_invalidated" {
		t.Fatalf("TransmitEUICCAPDU() error = %v, want invalidated session", err)
	}
	if err := m.CloseSIMAuthLogicalChannel(context.Background(), 1, 7); err == nil || err.Error() != "qmi_apdu_session_invalidated" {
		t.Fatalf("CloseSIMAuthLogicalChannel() error = %v, want invalidated session", err)
	}
}

func TestSetDataConfigRetriesPendingCoreStart(t *testing.T) {
	m := New(config.DeviceConfig{ID: "qmi-policy", APN: "internet", IPVersion: "v4"}, &qmimanager.ModemDevice{})
	// Model a previous live-policy replacement that stopped the old core and
	// could not start its replacement on the first attempt.
	m.qmiCoreStartPending.Store(true)
	starts := 0
	m.qmiStartCoreContext = func(*qmimanager.Manager, context.Context) error {
		starts++
		if starts == 1 {
			return errors.New("transient qmi start failure")
		}
		return nil
	}

	policy := DataConfig{APN: "ims", IPVersion: "v4v6"}
	if err := m.SetDataConfig(context.Background(), policy); err == nil {
		t.Fatal("first SetDataConfig() error = nil, want replacement core start failure")
	}
	if got := m.AppliedDataConfigSnapshot(); got != policy {
		t.Fatalf("applied policy after replacement = %+v, want %+v", got, policy)
	}
	if !m.qmiCoreStartPending.Load() || m.qmiCoreStarted.Load() {
		t.Fatalf("unexpected core flags: pending=%v started=%v", m.qmiCoreStartPending.Load(), m.qmiCoreStarted.Load())
	}

	if err := m.SetDataConfig(context.Background(), policy); err != nil {
		t.Fatalf("retry SetDataConfig() error = %v", err)
	}
	if starts != 2 {
		t.Fatalf("core start attempts=%d, want 2", starts)
	}
	if m.qmiCoreStartPending.Load() || !m.qmiCoreStarted.Load() {
		t.Fatalf("unexpected recovered core flags: pending=%v started=%v", m.qmiCoreStartPending.Load(), m.qmiCoreStarted.Load())
	}
}

func TestSetDataConfigRetainsPersistentQMICallbacks(t *testing.T) {
	m := New(config.DeviceConfig{ID: "qmi-policy", APN: "internet", IPVersion: "v4"}, &qmimanager.ModemDevice{})
	old := m.currentQMIManager()
	simEvents := 0
	voiceEvents := 0
	m.OnSimStatusChanged(func() { simEvents++ })
	if err := m.OnVoiceCallStatus(func(*qmi.VoiceAllCallInfo) { voiceEvents++ }); err != nil {
		t.Fatalf("OnVoiceCallStatus() error = %v", err)
	}

	if err := m.SetDataConfig(context.Background(), DataConfig{APN: "ims", IPVersion: "v4v6"}); err != nil {
		t.Fatalf("SetDataConfig() error = %v", err)
	}
	if m.currentQMIManager() == old {
		t.Fatal("data-policy rebuild did not replace the underlying manager")
	}
	m.dispatchPersistentQMICallbacks(qmimanager.Event{Type: qmimanager.EventSimStatusChanged})
	m.dispatchPersistentQMICallbacks(qmimanager.Event{Type: qmimanager.EventVoiceCallStatus})
	if simEvents != 1 || voiceEvents != 1 {
		t.Fatalf("persistent callbacks after replacement: sim=%d voice=%d", simEvents, voiceEvents)
	}
}
