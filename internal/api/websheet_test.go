package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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

	req := httptest.NewRequest(http.MethodPost, "/api/websheets/"+session.Info().ID+"/callback?token="+websheetToken(session), strings.NewReader(`{
		"source":"vowifi",
		"event":"entitlementChanged",
		"method":"e911AddressValidated",
		"resultCode":"success"
	}`))
	req.Header.Set("Content-Type", "application/json")
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
	parts := strings.SplitN(session.Info().EmbedURL, "token=", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
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
	router.ServeHTTP(done, httptest.NewRequest(http.MethodPost, "/api/websheets/"+id+"/done?token="+token, nil))
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
}
