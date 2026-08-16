package api

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodoge/internal/config"
	"github.com/yuanshuai1122/vodoge/internal/extensions"
)

func newPluginRuntimeTestServer(t *testing.T) *Server {
	t.Helper()
	mgr, err := extensions.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)
	if _, err := mgr.InstallZip(testPluginZip(t, "hello-lab"), ""); err != nil {
		t.Fatal(err)
	}
	return &Server{
		cfg:        config.ServerConfig{Port: "7575", PluginPort: "7576"},
		extensions: mgr,
	}
}

func TestPluginSessionLaunchesOnIndependentOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := newPluginRuntimeTestServer(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "id", Value: "hello-lab"}}
	c.Request = httptest.NewRequest(http.MethodPost, "http://device.local:7575/api/extensions/hello-lab/session", strings.NewReader(`{"contribution_id":"demo-page"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	s.handleCreatePluginSession(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		LaunchURL string `json:"launch_url"`
	}
	decodeData(t, rec, &got)
	launch, err := url.Parse(got.LaunchURL)
	if err != nil {
		t.Fatal(err)
	}
	if launch.Scheme != "http" || launch.Host != "device.local:7576" {
		t.Fatalf("launch origin=%s://%s want http://device.local:7576", launch.Scheme, launch.Host)
	}
	if launch.Query().Get("token") == "" {
		t.Fatalf("launch URL missing capability: %s", got.LaunchURL)
	}
}

func TestPluginLaunchURLEscapesContributionEntryOnce(t *testing.T) {
	s := &Server{cfg: config.ServerConfig{PluginPort: "7576"}}
	req := httptest.NewRequest(http.MethodPost, "http://device.local:7575/api/extensions/hello-lab/session", nil)
	got, err := s.pluginLaunchURL(req, "hello-lab", "pages/setup 100% ready.html", "capability")
	if err != nil {
		t.Fatal(err)
	}
	launch, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if launch.Path != "/plugin-assets/hello-lab/pages/setup 100% ready.html" {
		t.Fatalf("decoded launch path=%q want contribution entry unchanged", launch.Path)
	}
	if launch.EscapedPath() != "/plugin-assets/hello-lab/pages/setup%20100%25%20ready.html" {
		t.Fatalf("escaped launch path=%q want exactly one URL-escaping pass", launch.EscapedPath())
	}
}

func TestPluginLaunchURLPrefersConfiguredPublicOrigin(t *testing.T) {
	s := &Server{cfg: config.ServerConfig{
		PluginPort:      "7576",
		PluginPublicURL: "https://plugins.vodoge.com",
		Access:          config.AccessPolicy{TrustProxyHeaders: true},
	}}
	req := httptest.NewRequest(http.MethodPost, "http://internal.invalid:7575/session", nil)
	req.Header.Set("Forwarded", "proto=http;host=attacker.invalid")
	req.Header.Set("X-Forwarded-Proto", "http")
	req.Header.Set("X-Forwarded-Host", "attacker.invalid")

	got, err := s.pluginLaunchURL(req, "hello-lab", "index.html", "capability")
	if err != nil {
		t.Fatal(err)
	}
	launch, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if launch.Scheme != "https" || launch.Host != "plugins.vodoge.com" {
		t.Fatalf("launch origin = %s://%s, want https://plugins.vodoge.com", launch.Scheme, launch.Host)
	}
}

func TestPluginLaunchURLProxyHeaderFallback(t *testing.T) {
	tests := []struct {
		name       string
		trustProxy bool
		forwarded  string
		xProto     string
		xHost      string
		wantOrigin string
	}{
		{
			name:       "untrusted headers are ignored",
			forwarded:  "for=192.0.2.1;proto=https;host=public.example",
			xProto:     "https",
			xHost:      "public.example",
			wantOrigin: "http://device.local:7576",
		},
		{
			name:       "RFC Forwarded is preferred",
			trustProxy: true,
			forwarded:  "for=192.0.2.1;proto=https;host=\"public.example:443\"",
			xProto:     "http",
			xHost:      "ignored.example",
			wantOrigin: "https://public.example:7576",
		},
		{
			name:       "X-Forwarded fallback",
			trustProxy: true,
			xProto:     "https, http",
			xHost:      "public.example:443, internal.example",
			wantOrigin: "https://public.example:7576",
		},
		{
			name:       "malformed trusted headers are ignored",
			trustProxy: true,
			forwarded:  "proto=javascript;host=\"attacker.invalid/path\"",
			xProto:     "javascript",
			xHost:      "attacker.invalid/path",
			wantOrigin: "http://device.local:7576",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{cfg: config.ServerConfig{
				PluginPort: "7576",
				Access:     config.AccessPolicy{TrustProxyHeaders: tt.trustProxy},
			}}
			req := httptest.NewRequest(http.MethodPost, "http://device.local:7575/session", nil)
			if tt.forwarded != "" {
				req.Header.Set("Forwarded", tt.forwarded)
			}
			if tt.xProto != "" {
				req.Header.Set("X-Forwarded-Proto", tt.xProto)
			}
			if tt.xHost != "" {
				req.Header.Set("X-Forwarded-Host", tt.xHost)
			}
			got, err := s.pluginLaunchURL(req, "hello-lab", "index.html", "capability")
			if err != nil {
				t.Fatal(err)
			}
			launch, err := url.Parse(got)
			if err != nil {
				t.Fatal(err)
			}
			origin := launch.Scheme + "://" + launch.Host
			if origin != tt.wantOrigin {
				t.Fatalf("launch origin = %q, want %q", origin, tt.wantOrigin)
			}
		})
	}
}

func TestPluginCapabilityCookieSecureUsesPublicOrigin(t *testing.T) {
	tests := []struct {
		name            string
		requestURL      string
		pluginPublicURL string
		trustProxy      bool
		forwardedProto  string
		want            bool
	}{
		{name: "configured HTTPS over HTTP upstream", requestURL: "http://internal:7576", pluginPublicURL: "https://plugins.vodoge.com", want: true},
		{name: "configured HTTP over HTTPS upstream", requestURL: "https://internal:7576", pluginPublicURL: "http://plugins.vodoge.com", forwardedProto: "https", trustProxy: true, want: false},
		{name: "trusted proxy HTTPS", requestURL: "http://internal:7576", forwardedProto: "https", trustProxy: true, want: true},
		{name: "untrusted proxy HTTPS", requestURL: "http://internal:7576", forwardedProto: "https", want: false},
		{name: "direct HTTPS", requestURL: "https://internal:7576", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{cfg: config.ServerConfig{
				PluginPort:      "7576",
				PluginPublicURL: tt.pluginPublicURL,
				Access:          config.AccessPolicy{TrustProxyHeaders: tt.trustProxy},
			}}
			req := httptest.NewRequest(http.MethodGet, tt.requestURL, nil)
			if tt.forwardedProto != "" {
				req.Header.Set("X-Forwarded-Proto", tt.forwardedProto)
			}
			got, err := s.pluginCapabilityCookieSecure(req)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("secure = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestPluginLaunchSetsOnlyPathScopedCapabilityCookies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := newPluginRuntimeTestServer(t)
	token, _, err := s.issuePluginCapability("hello-lab", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	router := s.newPluginRouter()
	path := "/plugin-assets/hello-lab/index.html?token=" + url.QueryEscape(token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("launch status=%d body=%s", rec.Code, rec.Body.String())
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("cookies=%d want 2: %+v", len(cookies), cookies)
	}
	wantPaths := map[string]bool{
		pluginAssetCookiePath("hello-lab"):   false,
		pluginBackendCookiePath("hello-lab"): false,
	}
	for _, cookie := range cookies {
		if cookie.Name != pluginCookieName("hello-lab") || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
			t.Fatalf("unexpected capability cookie: %+v", cookie)
		}
		if _, ok := wantPaths[cookie.Path]; !ok {
			t.Fatalf("cookie has broad path %q", cookie.Path)
		}
		wantPaths[cookie.Path] = true
	}
	for path, found := range wantPaths {
		if !found {
			t.Fatalf("missing cookie path %q", path)
		}
	}

	cleanReq := httptest.NewRequest(http.MethodGet, rec.Header().Get("Location"), nil)
	cleanReq.AddCookie(cookies[0])
	assetRec := httptest.NewRecorder()
	router.ServeHTTP(assetRec, cleanReq)
	if assetRec.Code != http.StatusOK || !strings.Contains(assetRec.Body.String(), "demo") {
		t.Fatalf("asset status=%d body=%s", assetRec.Code, assetRec.Body.String())
	}
}

func TestPluginCapabilityIsBoundToPluginAndExpiry(t *testing.T) {
	s := &Server{}
	now := time.Now()
	token, _, err := s.issuePluginCapability("hello-lab", now)
	if err != nil {
		t.Fatal(err)
	}
	if !s.validatePluginCapability(token, "hello-lab", now) {
		t.Fatal("fresh capability was rejected")
	}
	if s.validatePluginCapability(token, "other-plugin", now) {
		t.Fatal("capability authorized another plugin")
	}
	if s.validatePluginCapability(token, "hello-lab", now.Add(pluginSessionTTL+time.Second)) {
		t.Fatal("expired capability was accepted")
	}
}

func TestPluginCookiePortIsNotSecurityBoundary(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	pluginOrigin, _ := url.Parse("http://device.local:7576/plugin-assets/hello-lab/index.html")
	jar.SetCookies(pluginOrigin, []*http.Cookie{{
		Name:  pluginCookieName("hello-lab"),
		Value: "capability",
		Path:  pluginBackendCookiePath("hello-lab"),
	}})
	managementURL, _ := url.Parse("http://device.local:7575/api/extensions/hello-lab/backend")
	if len(jar.Cookies(managementURL)) != 1 {
		t.Fatal("test invariant failed: cookies are host/path scoped, not port scoped")
	}

	main := newPluginRuntimeTestServer(t).newRouter()
	req := httptest.NewRequest(http.MethodGet, managementURL.Path, nil)
	for _, cookie := range jar.Cookies(managementURL) {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	main.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("management origin accepted plugin runtime route: status=%d body=%s", rec.Code, rec.Body.String())
	}
}
