package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodog/internal/pcsc"
)

type fakePCSC struct {
	st pcsc.Status
}

func (f fakePCSC) Discover(context.Context) pcsc.Status { return f.st }

func TestHandleListReadersMissingDaemon(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{pcsc: fakePCSC{st: pcsc.Status{
		Daemon:  pcsc.DaemonMissing,
		Message: "未检测到 pcscd（PC/SC 智能卡服务未运行）",
	}}}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/readers", nil)
	s.handleListReaders(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200, body=%s", rec.Code, rec.Body.String())
	}
	body := rawBody(t, rec)
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("data shape: %v", body)
	}
	if data["daemon"] != pcsc.DaemonMissing {
		t.Fatalf("daemon=%v want missing", data["daemon"])
	}
	if data["message"] == "" {
		t.Fatal("missing pcscd must include an explicit message")
	}
	readers, _ := data["readers"].([]any)
	if readers == nil {
		t.Fatal("readers must be [] not omitted")
	}
	if len(readers) != 0 {
		t.Fatalf("readers=%v want []", readers)
	}
}

func TestHandleListReadersReturnsNames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{pcsc: fakePCSC{st: pcsc.Status{
		Daemon:  pcsc.DaemonRunning,
		Message: "pcscd 在运行",
		Socket:  "/run/pcscd/pcscd.comm",
		Readers: []pcsc.Reader{{Name: "Alcor Micro AU9540"}},
	}}}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/readers", nil)
	s.handleListReaders(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rawBody(t, rec)
	raw, err := json.Marshal(body["data"])
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	if !jsonHas(string(raw), "Alcor Micro AU9540") {
		t.Fatalf("expected reader name in data: %s", raw)
	}
}

func jsonHas(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
