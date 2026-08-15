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
	"github.com/yuanshuai1122/vodog/internal/httpsmode"
)

func TestHTTPSRedirectWhenEnabled(t *testing.T) {
	m, err := httpsmode.New(t.TempDir(), ":7575", true)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{https: m}
	h := s.wrapHTTPSRedirect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://vodog.lab.lan:7575/sms", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPermanentRedirect {
		t.Fatalf("status=%d want 308 body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "https://vodog.lab.lan:7575/sms" {
		t.Fatalf("Location=%q", loc)
	}
}

func TestHTTPSSettingsExemptFromRedirect(t *testing.T) {
	m, err := httpsmode.New(t.TempDir(), ":7575", true)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{https: m}
	h := s.wrapHTTPSRedirect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("settings"))
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7575/api/settings/https", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "settings" {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetAndUpdateHTTPSSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	m, err := httpsmode.New(dir, ":7575", false)
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("server:\n  port: 7575\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Server: config.ServerConfig{Port: ":7575"}}
	s := &Server{https: m, fullCfg: cfg, configPath: cfgPath}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/settings/https", nil)
	s.handleGetHTTPSSettings(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", rec.Code, rec.Body.String())
	}
	var st httpsmode.State
	decodeData(t, rec, &st)
	if st.Enabled || st.Fingerprint == "" {
		t.Fatalf("get state=%+v", st)
	}

	body, _ := json.Marshal(map[string]bool{"enabled": true})
	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/settings/https", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	s.handleUpdateHTTPSSettings(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !cfg.Server.SelfSignedHTTPS || !m.Enabled() {
		t.Fatal("enable not applied")
	}
	raw, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(raw), "self_signed_https: true") {
		t.Fatalf("file:\n%s", raw)
	}

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/settings/https/certificate", nil)
	s.handleDownloadHTTPSCertificate(c)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "BEGIN CERTIFICATE") {
		t.Fatalf("cert status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateSelfSignedHTTPSInFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 7575\n  max_devices: 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.UpdateSelfSignedHTTPSInFile(path, true); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	text := string(raw)
	if !strings.Contains(text, "self_signed_https: true") || !strings.Contains(text, "max_devices: 5") {
		t.Fatalf("file:\n%s", text)
	}
}
