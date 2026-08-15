package device

import (
	"context"
	"testing"

	"github.com/yuanshuai1122/vodog/internal/apduarbiter"
	"github.com/yuanshuai1122/vodog/internal/config"
	"github.com/yuanshuai1122/vodog/internal/modem"
)

func TestShouldDeferSMSPollWhenModemBusy(t *testing.T) {
	m, err := modem.New(config.DeviceConfig{ID: "d1", ATPort: "/dev/null"})
	if err != nil {
		t.Fatal(err)
	}
	m.SetBusy(true)
	w := &Worker{ID: "d1", Modem: m}
	if !w.shouldDeferSMSPoll() {
		t.Fatal("busy send must defer CMGL")
	}
	m.SetBusy(false)
	if w.shouldDeferSMSPoll() {
		t.Fatal("idle modem should not defer")
	}
}

func TestShouldDeferSMSPollWhenAPDUHeld(t *testing.T) {
	w := &Worker{
		ID:          "d1",
		APDUArbiter: apduarbiter.New("d1", apduarbiter.Options{}),
	}
	lease, err := w.APDUArbiter.AcquireTransport(context.Background(), apduarbiter.Request{
		Owner: "aka",
		Class: apduarbiter.APDUClassUSIMAKA,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lease.Release() })
	if !w.shouldDeferSMSPoll() {
		t.Fatal("AKA lease must defer CMGL")
	}
}
