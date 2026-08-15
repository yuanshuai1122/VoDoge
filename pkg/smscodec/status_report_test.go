package smscodec

import "testing"

func TestParseCMGSReference(t *testing.T) {
	mr, ok := ParseCMGSReference("\r\n+CMGS: 23\r\n\r\nOK\r\n")
	if !ok || mr != 23 {
		t.Fatalf("mr=%d ok=%v", mr, ok)
	}
	if _, ok := ParseCMGSReference("OK"); ok {
		t.Fatal("expected no ref")
	}
}

func TestDecodeStatusReportTPDU(t *testing.T) {
	// 公开 TPDU 样例：MTI=SMS-STATUS-REPORT，MR=0x42，ST=0xab
	raw := []byte{
		0x02, 0x42, 0x04, 0x91, 0x36, 0x19, 0x51, 0x50, 0x71, 0x32, 0x20,
		0x05, 0x23, 0x51, 0x40, 0x81, 0x32, 0x20, 0x05, 0x42, 0xab,
	}
	got, ok := DecodeStatusReportTPDU(raw)
	if !ok || got.MR != 0x42 || got.Status != 0xab {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
	if StatusReportDelivered(0xab) {
		t.Fatal("0xab is not a delivered status")
	}
	if !StatusReportDelivered(0x00) {
		t.Fatal("0x00 should be delivered")
	}
	if _, ok := DecodeStatusReportTPDU(nil); ok {
		t.Fatal("empty must not decode")
	}
}
