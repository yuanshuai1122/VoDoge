package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodoge/internal/config"
	"github.com/yuanshuai1122/vodoge/internal/websheet"
)

func TestRespondWebsheetErrorMapsStatuses(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{err: websheet.ErrNotFound, want: http.StatusNotFound},
		{err: websheet.ErrExpired, want: http.StatusGone},
		{err: websheet.ErrUnsafeURL, want: http.StatusBadRequest},
		{err: websheet.ErrUnauthorized, want: http.StatusUnauthorized},
		{err: errors.New("boom"), want: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		respondWebsheetError(c, tt.err)
		if rec.Code != tt.want {
			t.Fatalf("respondWebsheetError(%v)=%d want %d", tt.err, rec.Code, tt.want)
		}
	}
}

func TestWebsheetBootstrapUsesSessionTokenOutsideGlobalAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	broker := websheet.New(websheet.Config{AllowPrivateHosts: true})
	session, err := broker.Create(context.Background(), websheet.Request{URL: "https://203.0.113.10/start"})
	if err != nil {
		t.Fatal(err)
	}

	server := &Server{
		auth:      config.WebConfig{Password: "secret"},
		websheets: broker,
	}
	router := gin.New()
	api := router.Group("/api")
	server.registerWebsheetRoutes(api)
	api.Use(server.authMiddleware())
	api.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	valid := httptest.NewRecorder()
	validReq := httptest.NewRequest(http.MethodGet, session.Info().EmbedURL, nil)
	router.ServeHTTP(valid, validReq)
	if valid.Code == http.StatusUnauthorized {
		t.Fatalf("bootstrap with websheet token returned auth 401: %s", valid.Body.String())
	}
	if valid.Code != http.StatusFound {
		t.Fatalf("bootstrap with websheet token status=%d want %d body=%s", valid.Code, http.StatusFound, valid.Body.String())
	}

	missing := httptest.NewRecorder()
	missingReq := httptest.NewRequest(http.MethodGet, "/api/websheets/"+session.Info().ID, nil)
	router.ServeHTTP(missing, missingReq)
	if missing.Code != http.StatusUnauthorized {
		t.Fatalf("bootstrap without websheet token status=%d want %d body=%s", missing.Code, http.StatusUnauthorized, missing.Body.String())
	}

	protected := httptest.NewRecorder()
	protectedReq := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	router.ServeHTTP(protected, protectedReq)
	if protected.Code != http.StatusUnauthorized {
		t.Fatalf("protected route status=%d want %d", protected.Code, http.StatusUnauthorized)
	}
}

func TestWebsheetCallbackMarksTerminalSessionDone(t *testing.T) {
	gin.SetMode(gin.TestMode)

	broker := websheet.New(websheet.Config{AllowPrivateHosts: true})
	session, err := broker.Create(context.Background(), websheet.Request{URL: "https://203.0.113.10/start"})
	if err != nil {
		t.Fatal(err)
	}

	server := &Server{websheets: broker}
	router := gin.New()
	api := router.Group("/api")
	server.registerWebsheetRoutes(api)

	req := httptest.NewRequest(http.MethodPost, websheetSessionPath(session, "/callback"), strings.NewReader(`{
		"source":"vowifi",
		"event":"entitlementChanged",
		"method":"e911AddressValidated",
		"resultCode":"success"
	}`))
	// Sandboxed opaque-origin documents use a CORS-simple content type so the
	// callback can be delivered with mode=no-cors and no ambient credentials.
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("callback status=%d body=%s", rec.Code, rec.Body.String())
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := session.WaitDone(waitCtx); err != nil {
		t.Fatalf("session was not marked done after terminal callback: %v", err)
	}
}

func websheetToken(session *websheet.Session) string {
	parsed, err := url.Parse(session.Info().EmbedURL)
	if err != nil {
		return ""
	}
	return parsed.Query().Get("token")
}

func websheetSessionPath(session *websheet.Session, suffix string) string {
	return "/api/websheets/" + url.PathEscape(session.Info().ID) + "/session/" + url.PathEscape(websheetToken(session)) + suffix
}

// 前端靠轮询 /status 判断流程结束——终态回调之后会话必须还在，否则前端永远
// 等不到"完成"，只会等到 404。
func TestWebsheetStatusSurvivesDoneAndReportsTerminalState(t *testing.T) {
	gin.SetMode(gin.TestMode)

	broker := websheet.New(websheet.Config{AllowPrivateHosts: true})
	session, err := broker.Create(context.Background(), websheet.Request{URL: "https://203.0.113.10/start"})
	if err != nil {
		t.Fatal(err)
	}

	server := &Server{websheets: broker}
	router := gin.New()
	api := router.Group("/api")
	server.registerWebsheetRoutes(api)

	id := session.Info().ID
	token := websheetToken(session)
	statusPath := "/api/websheets/" + id + "/status?token=" + token

	before := httptest.NewRecorder()
	router.ServeHTTP(before, httptest.NewRequest(http.MethodGet, statusPath, nil))
	if before.Code != http.StatusOK {
		t.Fatalf("status before done=%d body=%s", before.Code, before.Body.String())
	}
	if strings.Contains(before.Body.String(), `"finished":true`) {
		t.Fatalf("status=%s want finished=false before any callback", before.Body.String())
	}

	done := httptest.NewRecorder()
	router.ServeHTTP(done, httptest.NewRequest(http.MethodPost, websheetSessionPath(session, "/done"), nil))
	if done.Code != http.StatusOK {
		t.Fatalf("done status=%d body=%s", done.Code, done.Body.String())
	}

	after := httptest.NewRecorder()
	router.ServeHTTP(after, httptest.NewRequest(http.MethodGet, statusPath, nil))
	if after.Code != http.StatusOK {
		t.Fatalf("status after done=%d body=%s — the session must outlive the flow", after.Code, after.Body.String())
	}
	if !strings.Contains(after.Body.String(), `"finished":true`) {
		t.Fatalf("status=%s want finished=true", after.Body.String())
	}
}

// 状态查询也接受已登录用户的凭证：前端手里只有自己的 bearer token，
// 会话 token 藏在 embedUrl 里，没必要再解析出来。
func TestWebsheetStatusAcceptsLoggedInUserWithoutSessionToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	broker := websheet.New(websheet.Config{AllowPrivateHosts: true})
	session, err := broker.Create(context.Background(), websheet.Request{URL: "https://203.0.113.10/start"})
	if err != nil {
		t.Fatal(err)
	}

	server := &Server{auth: config.WebConfig{Password: "secret"}, websheets: broker}
	token, _, err := server.issueSessionToken()
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	api := router.Group("/api")
	server.registerWebsheetRoutes(api)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/websheets/"+session.Info().ID+"/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status with user bearer token=%d body=%s", rec.Code, rec.Body.String())
	}

	anon := httptest.NewRecorder()
	router.ServeHTTP(anon, httptest.NewRequest(http.MethodGet, "/api/websheets/"+session.Info().ID+"/status", nil))
	if anon.Code != http.StatusUnauthorized {
		t.Fatalf("status without any credential=%d want 401", anon.Code)
	}

	cookieOnly := httptest.NewRecorder()
	cookieReq := httptest.NewRequest(http.MethodGet, "/api/websheets/"+session.Info().ID+"/status", nil)
	cookieReq.AddCookie(&http.Cookie{Name: "vodoge_session", Value: token})
	router.ServeHTTP(cookieOnly, cookieReq)
	if cookieOnly.Code != http.StatusUnauthorized {
		t.Fatalf("status with legacy management cookie=%d want 401", cookieOnly.Code)
	}
}

func TestWebsheetRuntimeRoutesRejectManagementBearerWithoutCapability(t *testing.T) {
	gin.SetMode(gin.TestMode)

	broker := websheet.New(websheet.Config{AllowPrivateHosts: true})
	session, err := broker.Create(context.Background(), websheet.Request{URL: "https://203.0.113.10/start"})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{auth: config.WebConfig{Password: "secret"}, websheets: broker}
	managementToken, _, err := server.issueSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	server.registerWebsheetRoutes(router.Group("/api"))

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/websheets/" + session.Info().ID},
		{method: http.MethodGet, path: "/api/websheets/" + session.Info().ID + "/session/wrong/proxy/https/203.0.113.10/start"},
		{method: http.MethodPost, path: "/api/websheets/" + session.Info().ID + "/session/wrong/callback", body: `{}`},
		{method: http.MethodPost, path: "/api/websheets/" + session.Info().ID + "/session/wrong/done"},
	}
	for _, tc := range tests {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+managementToken)
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with management bearer=%d want 401 body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

func TestWebsheetSandboxAndOpaqueOriginCORS(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "sandbox allow-same-origin")
		w.Header().Set("Referrer-Policy", "unsafe-url")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		_, _ = w.Write([]byte(`<!doctype html><html><body>carrier</body></html>`))
	}))
	defer upstream.Close()

	broker := websheet.New(websheet.Config{AllowPrivateHosts: true})
	session, err := broker.Create(context.Background(), websheet.Request{URL: upstream.URL + "/start"})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{websheets: broker}
	router := gin.New()
	server.registerWebsheetRoutes(router.Group("/api"))

	bootstrap := httptest.NewRecorder()
	router.ServeHTTP(bootstrap, httptest.NewRequest(http.MethodGet, session.Info().EmbedURL, nil))
	if bootstrap.Code != http.StatusFound {
		t.Fatalf("bootstrap=%d body=%s", bootstrap.Code, bootstrap.Body.String())
	}
	assertWebsheetSandboxHeaders(t, bootstrap.Header())
	location := bootstrap.Header().Get("Location")
	tokenSegment := "/session/" + url.PathEscape(websheetToken(session)) + "/proxy/"
	if !strings.Contains(location, tokenSegment) || strings.Contains(location, "token=") {
		t.Fatalf("bootstrap Location=%q want path-scoped capability without token query", location)
	}

	proxied := httptest.NewRecorder()
	proxyReq := httptest.NewRequest(http.MethodGet, location, nil)
	proxyReq.Header.Set("Origin", "null")
	router.ServeHTTP(proxied, proxyReq)
	if proxied.Code != http.StatusOK {
		t.Fatalf("proxy=%d body=%s", proxied.Code, proxied.Body.String())
	}
	assertWebsheetSandboxHeaders(t, proxied.Header())
	if got := proxied.Header().Values("Access-Control-Allow-Origin"); len(got) != 1 || got[0] != "null" {
		t.Fatalf("Access-Control-Allow-Origin=%q want only null", got)
	}
	if got := proxied.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("Access-Control-Allow-Credentials=%q want omitted", got)
	}
	if !headerListContains(proxied.Header(), "Vary", "Origin") {
		t.Fatalf("Vary=%q want Origin", proxied.Header().Values("Vary"))
	}

	preflight := httptest.NewRecorder()
	preflightReq := httptest.NewRequest(http.MethodOptions, location, nil)
	preflightReq.Header.Set("Origin", "null")
	preflightReq.Header.Set("Access-Control-Request-Method", http.MethodPost)
	preflightReq.Header.Set("Access-Control-Request-Headers", "content-type, x-requested-with")
	router.ServeHTTP(preflight, preflightReq)
	if preflight.Code != http.StatusNoContent {
		t.Fatalf("preflight=%d body=%s", preflight.Code, preflight.Body.String())
	}
	if got := preflight.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type, X-Requested-With" {
		t.Fatalf("Access-Control-Allow-Headers=%q", got)
	}
	if got := preflight.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodPost) || strings.Contains(got, "*") {
		t.Fatalf("Access-Control-Allow-Methods=%q", got)
	}
	if got := preflight.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("preflight credentials=%q want omitted", got)
	}

	badPreflight := httptest.NewRecorder()
	badPath := strings.Replace(location, tokenSegment, "/session/wrong/proxy/", 1)
	badReq := httptest.NewRequest(http.MethodOptions, badPath, nil)
	badReq.Header.Set("Origin", "null")
	badReq.Header.Set("Access-Control-Request-Method", http.MethodPost)
	router.ServeHTTP(badPreflight, badReq)
	if badPreflight.Code != http.StatusUnauthorized {
		t.Fatalf("preflight with wrong path capability=%d want 401", badPreflight.Code)
	}
}

func assertWebsheetSandboxHeaders(t *testing.T, header http.Header) {
	t.Helper()
	if got := header.Get("Content-Security-Policy"); got != websheetSandboxPolicy || strings.Contains(got, "allow-same-origin") {
		t.Fatalf("Content-Security-Policy=%q", got)
	}
	if got := header.Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy=%q", got)
	}
	if got := header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control=%q", got)
	}
}

func headerListContains(header http.Header, key, want string) bool {
	for _, value := range header.Values(key) {
		for _, item := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(item), want) {
				return true
			}
		}
	}
	return false
}

func TestAccessLogRedactsWebsheetPathCapability(t *testing.T) {
	const capability = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	out := accessLogFormatter(gin.LogFormatterParams{
		Method:     http.MethodGet,
		Path:       "/api/websheets/session-id/session/" + capability + "/proxy/https/carrier.example/app.js?target_query=a%3Db",
		StatusCode: http.StatusOK,
	})
	if strings.Contains(out, capability) {
		t.Fatalf("access log leaked WebSheet capability: %s", out)
	}
	if !strings.Contains(out, "/api/websheets/session-id/session/***/proxy/") {
		t.Fatalf("access log did not preserve a useful redacted path: %s", out)
	}
}
