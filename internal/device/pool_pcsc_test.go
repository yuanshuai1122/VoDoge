package device

import (
	"errors"
	"testing"

	"github.com/yuanshuai1122/vodog/internal/config"
	"github.com/yuanshuai1122/vodog/internal/pcsc"
)

func TestAddPCSCReaderWorkerSkipsRadio(t *testing.T) {
	p := NewPool(&config.Config{})
	w, err := p.AddWorkerFromConfig(config.DeviceConfig{
		ID:            "reader-1",
		DeviceBackend: config.DeviceBackendPCSC,
		ReaderName:    "Alcor Micro AU9540",
	})
	if err != nil {
		t.Fatalf("AddWorkerFromConfig: %v", err)
	}
	if w.EsimMgr == nil {
		t.Fatal("reader worker must have EsimMgr")
	}
	if w.QMICore != nil || w.Modem != nil {
		t.Fatal("reader worker must not start radio stack")
	}
	if requiresQMICore(w.Config) {
		t.Fatal("pcsc must not require QMI")
	}
}

func TestAddPCSCReaderWorkerRequiresName(t *testing.T) {
	p := NewPool(&config.Config{})
	_, err := p.AddWorkerFromConfig(config.DeviceConfig{
		ID:            "reader-2",
		DeviceBackend: config.DeviceBackendPCSC,
	})
	if err == nil {
		t.Fatal("want error for empty reader_name")
	}
}

func TestModemAndReaderCannotHoldSameICCID(t *testing.T) {
	p := NewPool(&config.Config{})
	if err := p.HoldModemESIM("modem-1", "8986001"); err != nil {
		t.Fatal(err)
	}
	err := p.occupancy().Acquire(pcsc.Holder{
		DeviceID: "reader-1", Kind: pcsc.KindReader, ReaderName: "Alcor", ICCID: "8986001",
	})
	if !errors.Is(err, pcsc.ErrInUse) {
		t.Fatalf("err=%v want ErrInUse", err)
	}
	p.ReleaseESIMHold("modem-1")
	if err := p.occupancy().Acquire(pcsc.Holder{
		DeviceID: "reader-1", Kind: pcsc.KindReader, ReaderName: "Alcor", ICCID: "8986001",
	}); err != nil {
		t.Fatal(err)
	}
}
