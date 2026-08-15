package api

import (
	"testing"

	"github.com/yuanshuai1122/vodog/internal/config"
	"github.com/yuanshuai1122/vodog/internal/db"
)

func TestFilterSMSContactsByLane(t *testing.T) {
	cfgByID := map[string]config.DeviceConfig{
		"dev-cn":   {ID: "dev-cn", Lane: "cn"},
		"dev-intl": {ID: "dev-intl", Lane: "intl"},
		"dev-none": {ID: "dev-none"},
	}
	contacts := []SMSContactWithDevice{
		{SMSContact: db.SMSContact{Peer: "10086", ICCID: "8986cn"}, DeviceID: "dev-cn", Lane: "cn"},
		{SMSContact: db.SMSContact{Peer: "+1202", ICCID: "8986us"}, DeviceID: "dev-intl", Lane: "intl"},
		{SMSContact: db.SMSContact{Peer: "10010", ICCID: "8986xx"}, DeviceID: "dev-none"},
		{SMSContact: db.SMSContact{Peer: "orphan", ICCID: "no-dev"}},
	}

	got := filterSMSContactsByLane(contacts, "cn", cfgByID)
	if len(got) != 1 || got[0].Peer != "10086" {
		t.Fatalf("lane=cn got %+v", got)
	}

	got = filterSMSContactsByLane(contacts, "INTL", cfgByID)
	if len(got) != 1 || got[0].Peer != "+1202" {
		t.Fatalf("lane=intl got %+v", got)
	}

	got = filterSMSContactsByLane(contacts, "", cfgByID)
	if len(got) != 4 {
		t.Fatalf("empty lane should keep all, got %d", len(got))
	}

	// DeviceID 命中、Lane 字段空：仍应按配置归线
	unlabeled := []SMSContactWithDevice{
		{SMSContact: db.SMSContact{Peer: "1069"}, DeviceID: "dev-cn"},
	}
	got = filterSMSContactsByLane(unlabeled, "cn", cfgByID)
	if len(got) != 1 || got[0].Peer != "1069" {
		t.Fatalf("device_id fallback failed: %+v", got)
	}
	got = filterSMSContactsByLane(unlabeled, "intl", cfgByID)
	if len(got) != 0 {
		t.Fatalf("unlabeled cn device must not appear on intl, got %+v", got)
	}
}

func TestValidateLaneQueryRejected(t *testing.T) {
	if err := config.ValidateLane("eu"); err == nil {
		t.Fatal("expected invalid lane")
	}
}
