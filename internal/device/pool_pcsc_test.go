package device

import (
	"errors"
	"strings"
	"testing"

	"github.com/yuanshuai1122/vodoge/internal/config"
	"github.com/yuanshuai1122/vodoge/internal/pcsc"
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

func TestAddPCSCReaderWorkerRejectsTruncatedName(t *testing.T) {
	p := NewPool(&config.Config{})
	_, err := p.AddWorkerFromConfig(config.DeviceConfig{
		ID:            "reader-long",
		DeviceBackend: config.DeviceBackendPCSC,
		ReaderName:    strings.Repeat("x", 128),
	})
	if !errors.Is(err, pcsc.ErrReaderNameTooLong) {
		t.Fatalf("err=%v, want ErrReaderNameTooLong", err)
	}
}

func TestPCSCReaderReleasesOccupancyWhenLPAInitializationFails(t *testing.T) {
	t.Setenv("PCSCLITE_CSOCK_NAME", "/tmp/vodoge-missing-pcscd-"+t.Name())
	p := NewPool(&config.Config{})
	w, err := p.AddWorkerFromConfig(config.DeviceConfig{
		ID:            "reader-failing-lpa",
		DeviceBackend: config.DeviceBackendPCSC,
		ReaderName:    "Unavailable Reader",
	})
	if err != nil {
		t.Fatalf("AddWorkerFromConfig: %v", err)
	}

	if _, err := w.EsimMgr.GetEIDs(); err == nil {
		t.Fatal("GetEIDs() error=nil, want pcscd connection failure")
	}
	if holder, held := p.occupancy().HolderOfReader("Unavailable Reader"); held {
		t.Fatalf("reader occupancy leaked after LPA failure: %+v", holder)
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
