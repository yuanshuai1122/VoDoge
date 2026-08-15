package pcsc

import (
	"errors"
	"testing"
)

func TestDeviceIDFromReaderIsStable(t *testing.T) {
	a := DeviceIDFromReader(" Alcor Micro AU9540 ")
	b := DeviceIDFromReader("Alcor Micro AU9540")
	if a == "" || a != b {
		t.Fatalf("id=%q / %q", a, b)
	}
	if DeviceIDFromReader("") != "" {
		t.Fatal("empty name must yield empty id")
	}
}

func TestOccupancyReaderExclusive(t *testing.T) {
	o := NewOccupancy()
	if err := o.Acquire(Holder{DeviceID: "r1", Kind: KindReader, ReaderName: "Alcor"}); err != nil {
		t.Fatal(err)
	}
	err := o.Acquire(Holder{DeviceID: "r2", Kind: KindReader, ReaderName: "Alcor"})
	if !errors.Is(err, ErrInUse) {
		t.Fatalf("err=%v want ErrInUse", err)
	}
	o.Release("r1")
	if err := o.Acquire(Holder{DeviceID: "r2", Kind: KindReader, ReaderName: "Alcor"}); err != nil {
		t.Fatalf("after release: %v", err)
	}
}

func TestOccupancySameCardCannotBeOnModemAndReader(t *testing.T) {
	o := NewOccupancy()
	if err := o.Acquire(Holder{DeviceID: "modem-cn", Kind: KindModem, ICCID: "8986001"}); err != nil {
		t.Fatal(err)
	}
	err := o.Acquire(Holder{DeviceID: "reader-1", Kind: KindReader, ReaderName: "Alcor", ICCID: "8986001"})
	if !errors.Is(err, ErrInUse) {
		t.Fatalf("err=%v want ErrInUse", err)
	}
}
