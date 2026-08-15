package notify

import (
	"strings"
	"testing"

	"github.com/yuanshuai1122/vodog/internal/db"
)

func TestSMSSendBlockedByRate(t *testing.T) {
	db.OpenTestDB(t)
	if msg := smsSendBlockedByRate(1, "dev", "+100"); msg != "" {
		t.Fatalf("first send should pass, got %q", msg)
	}
	msg := smsSendBlockedByRate(1, "dev", "+101")
	if msg == "" || !strings.Contains(msg, "每小时上限") {
		t.Fatalf("second send should be blocked, got %q", msg)
	}
}
