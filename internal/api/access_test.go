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
	"github.com/yuanshuai1122/vodoge/internal/netaccess"
)

func TestAccessControlMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{}
	s.setAccessPolicy(netaccess.Default())
	r := gin.New()
	r.Use(s.accessControlMiddleware())
	r.GET("/", func(c *gin.Context) { c.String(200, "ok") })

	check := func(remote, forwarded string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = remote
		if forwarded != "" {
			req.Header.Set("X-Forwarded-For", forwarded)
		}
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := check("192.168.2.10:5000", ""); got != http.StatusOK {
		t.Fatalf("private denied: %d", got)
	}
	if got := check("8.8.8.8:5000", ""); got != http.StatusForbidden {
		t.Fatalf("public allowed: %d", got)
	}
	parsed, _ := netaccess.Parse(netaccess.Policy{Mode: "internal", AllowedCIDRs: []string{"8.8.8.0/24"}})
	s.setAccessPolicy(parsed)
	if got := check("8.8.8.8:5000", ""); got != http.StatusOK {
		t.Fatalf("extra cidr not honored: %d", got)
	}
	s.setAccessPolicy(netaccess.Parsed{Mode: netaccess.ModePublic})
	if got := check("1.1.1.1:9", ""); got != http.StatusOK {
		t.Fatalf("public mode denied: %d", got)
	}

	s.setAccessPolicy(netaccess.Parsed{Mode: netaccess.ModeInternal, TrustProxy: true})
	if got := check("8.8.8.8:5000", "192.168.1.20"); got != http.StatusOK {
		t.Fatalf("trusted XFF not honored: %d", got)
	}
	s.setAccessPolicy(netaccess.Default())
	if got := check("8.8.8.8:5000", "192.168.1.20"); got != http.StatusForbidden {
		t.Fatalf("untrusted XFF honored: %d", got)
	}
}

func TestSecuritySettingsRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 7575\n  max_devices: 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Server: config.ServerConfig{Port: ":7575"}}
	s := &Server{fullCfg: cfg, configPath: path}
	s.setAccessPolicy(netaccess.Default())

	body, _ := json.Marshal(map[string]any{
		"mode":                "internal",
		"allowed_cidrs":       []string{"203.0.113.0/24"},
		"trust_proxy_headers": true,
	})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/security", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.168.2.20:5000"
	c.Request = req
	s.handleUpdateSecuritySettings(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !s.currentAccessPolicy().TrustProxy {
		t.Fatal("runtime not updated")
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "203.0.113.0/24") || !strings.Contains(string(raw), "max_devices: 5") {
		t.Fatalf("file:\n%s", raw)
	}

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	get := httptest.NewRequest(http.MethodGet, "/api/settings/security", nil)
	get.RemoteAddr = "192.168.2.20:5000"
	c.Request = get
	s.handleGetSecuritySettings(c)
	var snap accessSnapshot
	decodeData(t, rec, &snap)
	if snap.Mode != "internal" || !snap.TrustProxyHeaders || !snap.ClientAllowed {
		t.Fatalf("GET %+v", snap)
	}
}

func TestSecuritySettingsRejectsBadPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/settings/security", strings.NewReader(`{"mode":"nowhere"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	s.handleUpdateSecuritySettings(c)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
