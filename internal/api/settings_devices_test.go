package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodoge/internal/config"
)

func TestGetDeviceQuotaDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{fullCfg: &config.Config{
		Devices: []config.DeviceConfig{{ID: "a"}, {ID: "b"}},
	}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/settings/devices", nil)
	s.handleGetDeviceQuota(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got deviceQuotaResponse
	decodeData(t, rec, &got)
	if got.Limit != config.DefaultDeviceLimit || got.Used != 2 || got.MaxLimit != config.MaxDeviceLimit {
		t.Fatalf("quota=%+v", got)
	}
}

func TestUpdateDeviceQuotaPersists(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 7575\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Server: config.ServerConfig{Port: ":7575", MaxDevices: 5}}
	s := &Server{fullCfg: cfg, configPath: path}

	body, _ := json.Marshal(map[string]int{"limit": 8})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/settings/devices", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	s.handleUpdateDeviceQuota(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if cfg.Server.MaxDevices != 8 {
		t.Fatalf("in-memory=%d", cfg.Server.MaxDevices)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "max_devices: 8") {
		t.Fatalf("file:\n%s", raw)
	}
}

func TestUpdateDeviceQuotaRejectsOutOfRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{fullCfg: &config.Config{}}
	for _, n := range []int{0, 11} {
		body, _ := json.Marshal(map[string]int{"limit": n})
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/settings/devices", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		s.handleUpdateDeviceQuota(c)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("n=%d status=%d body=%s", n, rec.Code, rec.Body.String())
		}
	}
}

func TestValidateFreeDeviceConfigLimitUsesConfiguredCap(t *testing.T) {
	devs := []config.DeviceConfig{{ID: "a"}, {ID: "b"}}
	if err := validateFreeDeviceConfigLimit(devs, 2); err == nil {
		t.Fatal("want error at cap")
	}
	if err := validateFreeDeviceConfigLimit(devs[:1], 2); err != nil {
		t.Fatalf("under cap: %v", err)
	}
}
