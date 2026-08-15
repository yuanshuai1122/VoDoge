package pcsc

import (
	"testing"
)

func TestDecodeICCIDAndIMSI(t *testing.T) {
	iccid := decodeICCID([]byte{0x98, 0x68, 0x00, 0x21, 0x43, 0x65, 0x87, 0x09, 0x21, 0xF3})
	if iccid != "8986001234567890123" {
		t.Fatalf("iccid=%q", iccid)
	}
	imsi, err := decodeIMSI([]byte{0x08, 0x49, 0x06, 0x00, 0x21, 0x43, 0x65, 0x87, 0x09})
	if err != nil {
		t.Fatal(err)
	}
	if imsi != "460001234567890" {
		t.Fatalf("imsi=%q", imsi)
	}
}

func TestDerivedIMEIIsLuhn15(t *testing.T) {
	imei := DerivedIMEI("Alcor Micro AU9540")
	if len(imei) != 15 {
		t.Fatalf("len=%d imei=%s", len(imei), imei)
	}
	for _, c := range imei {
		if c < '0' || c > '9' {
			t.Fatalf("non-digit in %s", imei)
		}
	}
	sum := 0
	for i, c := range imei[:14] {
		n := int(c - '0')
		if (14-i)%2 == 0 {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
	}
	want := (10 - sum%10) % 10
	if int(imei[14]-'0') != want {
		t.Fatalf("luhn check=%c want %d (%s)", imei[14], want, imei)
	}
	if DerivedIMEI("Alcor Micro AU9540") != imei {
		t.Fatal("DerivedIMEI must be stable")
	}
}

func TestReadICCIDAndIMSIViaFakeDaemon(t *testing.T) {
	startFakeDaemon(t, []fakeReader{{name: "Alcor Micro AU9540 00 00", cardPresent: true}})
	ch := NewChannel("Alcor Micro AU9540 00 00")
	if err := ch.Connect(); err != nil {
		t.Fatal(err)
	}
	defer ch.Disconnect()
	iccid, err := ch.ReadICCID()
	if err != nil {
		t.Fatal(err)
	}
	if iccid != "8986001234567890123" {
		t.Fatalf("iccid=%q", iccid)
	}
	imsi, err := ch.ReadIMSI()
	if err != nil {
		t.Fatal(err)
	}
	if imsi != "460001234567890" {
		t.Fatalf("imsi=%q", imsi)
	}
}

func TestGuardReaderSerializes(t *testing.T) {
	o := NewOccupancy()
	unlock := o.GuardReader("Alcor")
	done := make(chan struct{})
	go func() {
		unlock2 := o.GuardReader("Alcor")
		close(done)
		unlock2()
	}()
	select {
	case <-done:
		t.Fatal("second guard must wait")
	default:
	}
	unlock()
	<-done
}
