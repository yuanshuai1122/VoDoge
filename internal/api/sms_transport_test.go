package api

import "testing"

func TestPlanSMSSendIntlFallsBackToIMSWhenNotRegistered(t *testing.T) {
	got := planSMSSend("intl", true, false)
	if got.Primary != smsTransportIMS || got.Fallback != smsTransportCellular {
		t.Fatalf("intl+ims+no radio: %+v", got)
	}
}

func TestPlanSMSSendIntlPrefersCellularWhenRegistered(t *testing.T) {
	got := planSMSSend("INTL", true, true)
	if got.Primary != smsTransportCellular || got.Fallback != smsTransportIMS {
		t.Fatalf("intl+registered: %+v", got)
	}
}

func TestPlanSMSSendCNKeepsVoWiFiOnly(t *testing.T) {
	got := planSMSSend("cn", true, false)
	if got.Primary != smsTransportIMS || got.Fallback != "" {
		t.Fatalf("cn+vowifi must not fall back to cellular: %+v", got)
	}
}

func TestPlanSMSSendDefaultIsCellular(t *testing.T) {
	got := planSMSSend("", false, true)
	if got.Primary != smsTransportCellular || got.Fallback != "" {
		t.Fatalf("empty lane: %+v", got)
	}
}

func TestRadioRegistered(t *testing.T) {
	if !radioRegistered(1) || !radioRegistered(5) {
		t.Fatal("1/5 should be registered")
	}
	if radioRegistered(0) || radioRegistered(2) || radioRegistered(3) {
		t.Fatal("searching/denied/unknown should not count")
	}
}
