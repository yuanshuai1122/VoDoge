package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodoge/internal/config"
	"github.com/yuanshuai1122/vodoge/internal/netaccess"
)

func TestAuthMiddlewareRejectsLegacySessionCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{auth: config.WebConfig{Password: "secret"}}
	token, _, err := s.issueSessionToken()
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.GET("/api/protected", s.authMiddleware(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	cookieOnly := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	cookieOnly.AddCookie(&http.Cookie{Name: "vodoge_session", Value: token, Path: "/"})
	cookieRec := httptest.NewRecorder()
	router.ServeHTTP(cookieRec, cookieOnly)
	if cookieRec.Code != http.StatusUnauthorized {
		t.Fatalf("cookie-only status=%d want %d", cookieRec.Code, http.StatusUnauthorized)
	}

	bearer := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	bearer.Header.Set("Authorization", "Bearer "+token)
	bearerRec := httptest.NewRecorder()
	router.ServeHTTP(bearerRec, bearer)
	if bearerRec.Code != http.StatusNoContent {
		t.Fatalf("bearer status=%d want %d", bearerRec.Code, http.StatusNoContent)
	}
}

func TestLogoutExpiresCurrentAndHistoricalSessionCookies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)

	s.handleLogout(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	got := map[string]*http.Cookie{}
	for _, cookie := range rec.Result().Cookies() {
		got[cookie.Name] = cookie
	}
	for _, name := range legacySessionCookieNames {
		cookie := got[name]
		if cookie == nil {
			t.Fatalf("missing deletion cookie %q", name)
		}
		if cookie.Path != "/" || cookie.MaxAge >= 0 || !cookie.HttpOnly {
			t.Fatalf("cookie %q=%+v, want Path=/ MaxAge<0 HttpOnly", name, cookie)
		}
	}
}

func TestLoginReturnsBearerAndOnlyDeletesLegacyCookies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{
		auth:          config.WebConfig{Username: "admin", Password: "secret"},
		loginAttempts: make(map[string]loginAttempt),
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"secret"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	s.handleLogin(c)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"token"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.MaxAge >= 0 || cookie.Value != "" {
			t.Fatalf("login issued ambient cookie: %+v", cookie)
		}
	}
}

func TestAuthCredentialsConcurrentSnapshotAndUpdate(t *testing.T) {
	s := &Server{auth: config.WebConfig{Username: "admin", Password: "secret-a"}}
	start := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 2000; i++ {
			password := "secret-a"
			if i%2 != 0 {
				password = "secret-b"
			}
			s.setAuthPassword(password)
		}
	}()

	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 500 {
				token, _, err := s.issueSessionToken()
				if err != nil {
					t.Errorf("issueSessionToken: %v", err)
					return
				}
				_ = s.isSessionTokenValid(token, time.Now())
				_ = s.authSnapshot()
			}
		}()
	}

	close(start)
	wg.Wait()
}

func TestLoginRateLimitUsesAccessPolicyClientIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{
		auth:          config.WebConfig{Username: "admin", Password: "secret"},
		loginAttempts: make(map[string]loginAttempt),
	}
	s.setAccessPolicy(netaccess.Parsed{Mode: netaccess.ModePublic, TrustProxy: false})

	for i := 0; i < 11; i++ {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Request.Header.Set("X-Forwarded-For", "198.51.100."+strconv.Itoa(i+1))
		c.Request.RemoteAddr = "203.0.113.10:4000"

		s.handleLogin(c)
		want := http.StatusUnauthorized
		if i == 10 {
			want = http.StatusTooManyRequests
		}
		if rec.Code != want {
			t.Fatalf("attempt %d status=%d want=%d body=%s", i+1, rec.Code, want, rec.Body.String())
		}
	}
}
