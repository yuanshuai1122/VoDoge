package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodoge/internal/config"
)

type sseDeadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline      time.Time
	deadlineCalls int
}

func (r *sseDeadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	r.deadline = deadline
	r.deadlineCalls++
	return nil
}

func TestPrepareSSESetsStreamingHeadersAndClearsWriteDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &sseDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "http://api.test/api/logs/stream", nil)
	c.Request.Header.Set("Origin", "http://localhost:3000")

	s := &Server{cfg: config.ServerConfig{Debug: true}}
	s.prepareSSE(c)

	wantHeaders := map[string]string{
		"Content-Type":                 "text/event-stream",
		"Cache-Control":                "no-cache, no-store",
		"Connection":                   "keep-alive",
		"X-Accel-Buffering":            "no",
		"X-Content-Type-Options":       "nosniff",
		"Access-Control-Allow-Origin":  "http://localhost:3000",
		"Access-Control-Allow-Methods": "GET, OPTIONS",
		"Vary":                         "Origin",
	}
	for name, want := range wantHeaders {
		if got := recorder.Header().Get(name); got != want {
			t.Errorf("header %s = %q, want %q", name, got, want)
		}
	}
	if recorder.deadlineCalls != 1 {
		t.Fatalf("SetWriteDeadline calls = %d, want 1", recorder.deadlineCalls)
	}
	if !recorder.deadline.IsZero() {
		t.Fatalf("write deadline = %v, want zero", recorder.deadline)
	}
}
