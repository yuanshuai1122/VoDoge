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
	"github.com/yuanshuai1122/vodog/internal/config"
	"github.com/yuanshuai1122/vodog/internal/data/repo"
	"github.com/yuanshuai1122/vodog/internal/db"
)

func TestGetSMSSettingsReadsStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, _, _, sms, _, _ := repo.NewFakeStore()
	sms.RateStatusFn = func(limit int) (db.SMSRateStatus, error) {
		if limit != 20 {
			t.Fatalf("limit=%d want 20", limit)
		}
		return db.NewSMSRateStatus(20, 5), nil
	}
	s := &Server{
		store:   store,
		fullCfg: &config.Config{SMS: config.SMSConfig{HourlyLimit: 20}},
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/settings/sms", nil)
	s.handleGetSMSSettings(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var st db.SMSRateStatus
	decodeData(t, rec, &st)
	if st.HourlyLimit != 20 || st.Used != 5 || st.Remaining != 15 {
		t.Fatalf("status=%+v", st)
	}
}

func TestUpdateSMSSettingsPersistsAndApplies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 7575\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, _, _, sms, _, _ := repo.NewFakeStore()
	sms.RateStatusFn = func(limit int) (db.SMSRateStatus, error) {
		return db.NewSMSRateStatus(limit, 1), nil
	}
	cfg := &config.Config{SMS: config.SMSConfig{HourlyLimit: 20}}
	s := &Server{store: store, fullCfg: cfg, configPath: path}

	body, _ := json.Marshal(map[string]int{"hourly_limit": 7})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/settings/sms", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	s.handleUpdateSMSSettings(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if cfg.SMS.HourlyLimit != 7 {
		t.Fatalf("in-memory limit=%d", cfg.SMS.HourlyLimit)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "hourly_limit: 7") {
		t.Fatalf("file not updated:\n%s", raw)
	}
	var st db.SMSRateStatus
	decodeData(t, rec, &st)
	if st.HourlyLimit != 7 || st.Used != 1 {
		t.Fatalf("resp=%+v", st)
	}
}

func TestUpdateSMSSettingsRejectsOutOfRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{fullCfg: &config.Config{}}
	for _, n := range []int{-1, 201} {
		body, _ := json.Marshal(map[string]int{"hourly_limit": n})
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/settings/sms", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		s.handleUpdateSMSSettings(c)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("n=%d status=%d body=%s", n, rec.Code, rec.Body.String())
		}
	}
}

func TestReserveSMSSendWrites429(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, _, _, sms, _, _ := repo.NewFakeStore()
	sms.ReserveSendFn = func(limit int, deviceID, recipient string) (db.SMSRateStatus, error) {
		if limit != 3 || deviceID != "ec25" || recipient != "+86138" {
			t.Fatalf("reserve args limit=%d device=%s phone=%s", limit, deviceID, recipient)
		}
		return db.SMSRateStatus{}, &db.SMSRateLimitedError{
			SMSRateStatus:     db.NewSMSRateStatus(3, 3),
			RetryAfterSeconds: 42,
		}
	}
	s := &Server{
		store:   store,
		fullCfg: &config.Config{SMS: config.SMSConfig{HourlyLimit: 3}},
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/sms/send", nil)
	if s.reserveSMSSend(c, "ec25", "+86138") {
		t.Fatal("expected false when limited")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") != "42" {
		t.Fatalf("Retry-After=%q", rec.Header().Get("Retry-After"))
	}
	env := decodeEnvelope(t, rec)
	if env.Error == nil || env.Error.Code != "sms_rate_limited" {
		t.Fatalf("error=%+v", env.Error)
	}
	if env.Error.Details["retry_after_seconds"] != float64(42) {
		t.Fatalf("details=%v", env.Error.Details)
	}
}
