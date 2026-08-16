package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const pluginSessionTTL = 30 * time.Minute

type pluginCapability struct {
	PluginID string `json:"plugin_id"`
	Expires  int64  `json:"expires"`
	Nonce    string `json:"nonce"`
}

func (s *Server) handleCreatePluginSession(c *gin.Context) {
	var req struct {
		ContributionID string `json:"contribution_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.ContributionID) == "" {
		fail(c, http.StatusBadRequest, "plugin_contribution_required", "需要 contribution_id")
		return
	}

	pluginID := strings.TrimSpace(c.Param("id"))
	entry, ok := s.pluginContributionEntry(pluginID, strings.TrimSpace(req.ContributionID))
	if !ok {
		fail(c, http.StatusNotFound, "plugin_contribution_not_found", "插件页面不存在或未启用")
		return
	}

	now := time.Now()
	token, expiresAt, err := s.issuePluginCapability(pluginID, now)
	if err != nil {
		fail(c, http.StatusInternalServerError, "plugin_session_failed", "创建插件会话失败")
		return
	}
	launchURL, err := s.pluginLaunchURL(c.Request, pluginID, entry, token)
	if err != nil {
		fail(c, http.StatusInternalServerError, "plugin_origin_invalid", err.Error())
		return
	}
	respondOK(c, gin.H{
		"launch_url": launchURL,
		"expires_at": expiresAt.Format(time.RFC3339),
	})
}

func (s *Server) pluginContributionEntry(pluginID, contributionID string) (string, bool) {
	mgr := s.plugins()
	if mgr == nil {
		return "", false
	}
	for _, plugin := range mgr.List() {
		if plugin.ID != pluginID || !plugin.Enabled {
			continue
		}
		for _, contribution := range plugin.Contributions {
			if contribution.ID == contributionID && contribution.Location == "sidebar" {
				return contribution.Entry, true
			}
		}
	}
	return "", false
}

func (s *Server) pluginLaunchURL(r *http.Request, pluginID, entry, token string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("request is required")
	}
	host := (&url.URL{Host: r.Host}).Hostname()
	if host == "" {
		return "", fmt.Errorf("request host is required")
	}
	port := configuredPort(s.cfg.PluginPort, "7576")
	if port == "" {
		return "", fmt.Errorf("plugin port is invalid")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	entry = strings.ReplaceAll(strings.TrimSpace(entry), `\`, "/")
	u := url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(host, port),
		Path:   "/plugin-assets/" + pluginID + "/" + entry,
	}
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func configuredPort(raw, fallback string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	if _, port, err := net.SplitHostPort(raw); err == nil {
		return port
	}
	return strings.TrimPrefix(raw, ":")
}

func (s *Server) issuePluginCapability(pluginID string, now time.Time) (string, time.Time, error) {
	key, err := s.pluginCapabilityKey()
	if err != nil {
		return "", time.Time{}, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", time.Time{}, err
	}
	expiresAt := now.Add(pluginSessionTTL)
	payload, err := json.Marshal(pluginCapability{
		PluginID: pluginID,
		Expires:  expiresAt.Unix(),
		Nonce:    hex.EncodeToString(nonce[:]),
	})
	if err != nil {
		return "", time.Time{}, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(encoded))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + signature, expiresAt, nil
}

func (s *Server) pluginCapabilityKey() ([]byte, error) {
	s.pluginAuthMu.Lock()
	defer s.pluginAuthMu.Unlock()
	if len(s.pluginAuthKey) == sha256.Size {
		return append([]byte(nil), s.pluginAuthKey...), nil
	}
	key := make([]byte, sha256.Size)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	s.pluginAuthKey = key
	return append([]byte(nil), key...), nil
}

func (s *Server) validatePluginCapability(token, pluginID string, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	key, err := s.pluginCapabilityKey()
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(parts[0]))
	wantSignature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(wantSignature, mac.Sum(nil)) {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	var capability pluginCapability
	if err := json.Unmarshal(payload, &capability); err != nil {
		return false
	}
	return capability.PluginID == pluginID && now.Before(time.Unix(capability.Expires, 0))
}

func pluginCookieName(pluginID string) string {
	return "vodoge_plugin_" + pluginID
}

func pluginAssetCookiePath(pluginID string) string {
	return "/plugin-assets/" + pluginID + "/"
}

func pluginBackendCookiePath(pluginID string) string {
	return "/api/extensions/" + pluginID + "/backend"
}

func (s *Server) setPluginCapabilityCookies(c *gin.Context, pluginID, token string) {
	maxAge := int(pluginSessionTTL.Seconds())
	for _, path := range []string{pluginAssetCookiePath(pluginID), pluginBackendCookiePath(pluginID)} {
		http.SetCookie(c.Writer, &http.Cookie{
			Name:     pluginCookieName(pluginID),
			Value:    token,
			Path:     path,
			MaxAge:   maxAge,
			Secure:   c.Request.TLS != nil,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
	}
}

func (s *Server) pluginCapabilityMiddleware(allowLaunchToken bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		pluginID := strings.TrimSpace(c.Param("id"))
		token := ""
		fromLaunch := false
		if allowLaunchToken {
			token = strings.TrimSpace(c.Query("token"))
			fromLaunch = token != ""
		}
		if token == "" {
			if cookie, err := c.Request.Cookie(pluginCookieName(pluginID)); err == nil {
				token = strings.TrimSpace(cookie.Value)
			}
		}
		if token == "" || !s.validatePluginCapability(token, pluginID, time.Now()) {
			fail(c, http.StatusUnauthorized, "plugin_session_unauthorized", "插件会话无效或已过期")
			c.Abort()
			return
		}
		if fromLaunch {
			s.setPluginCapabilityCookies(c, pluginID, token)
			clean := *c.Request.URL
			query := clean.Query()
			query.Del("token")
			clean.RawQuery = query.Encode()
			c.Redirect(http.StatusFound, clean.String())
			c.Abort()
			return
		}
		c.Next()
	}
}

func (s *Server) newPluginRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.LoggerWithFormatter(accessLogFormatter))
	r.Use(gin.Recovery())
	r.Use(s.requestIDMiddleware())
	r.Use(s.accessControlMiddleware())
	r.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Next()
	})

	assets := s.pluginCapabilityMiddleware(true)
	r.GET("/plugin-assets/:id/*filepath", assets, s.handlePluginAsset)
	r.HEAD("/plugin-assets/:id/*filepath", assets, s.handlePluginAsset)

	backend := s.pluginCapabilityMiddleware(false)
	for _, path := range []string{"/api/extensions/:id/backend", "/api/extensions/:id/backend/*filepath"} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			r.Handle(method, path, backend, s.handleExtensionBackend)
		}
	}
	r.NoRoute(func(c *gin.Context) {
		fail(c, http.StatusNotFound, "plugin_route_not_found", "插件运行时路由不存在")
	})
	return r
}
