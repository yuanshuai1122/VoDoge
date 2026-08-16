package api

import (
	"errors"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodoge/internal/websheet"
)

const websheetSandboxPolicy = "sandbox allow-scripts allow-forms allow-popups allow-modals allow-downloads"

var websheetProxyMethods = []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}

// registerWebsheetRoutes 只在测试里单独用；正常启动时路由由 newRouter 统一注册。
// 两者共用 s.websheetRoutes()，不会出现测试挂的路由与线上不一致。
func (s *Server) registerWebsheetRoutes(api *gin.RouterGroup) {
	registerRoutes(api, s.websheetRoutes())
}

func (s *Server) websheetSession(c *gin.Context) (*websheet.Session, error) {
	if s.websheets == nil {
		return nil, websheet.ErrNotFound
	}
	return s.websheets.Get(c.Param("id"))
}

func (s *Server) authorizedWebsheetCapabilitySession(c *gin.Context) (*websheet.Session, error) {
	session, err := s.websheetSession(c)
	if err != nil {
		return nil, err
	}
	if err := session.Authorize(c.Request); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *Server) authorizedWebsheetStatusSession(c *gin.Context) (*websheet.Session, error) {
	session, err := s.websheetSession(c)
	if err != nil {
		return nil, err
	}
	if err := session.Authorize(c.Request); err != nil {
		if errors.Is(err, websheet.ErrUnauthorized) && s.isAuthenticatedRequest(c, time.Now()) {
			return session, nil
		}
		return nil, err
	}
	return session, nil
}

func setWebsheetResponseHeaders(c *gin.Context) {
	c.Header("Content-Security-Policy", websheetSandboxPolicy)
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Cache-Control", "no-store")
	if strings.TrimSpace(c.GetHeader("Origin")) == "null" {
		c.Header("Access-Control-Allow-Origin", "null")
		appendVary(c.Writer.Header(), "Origin")
	}
}

func appendVary(header http.Header, value string) {
	for _, item := range header.Values("Vary") {
		for _, existing := range strings.Split(item, ",") {
			if strings.EqualFold(strings.TrimSpace(existing), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}

func (s *Server) handleWebsheetBootstrap(c *gin.Context) {
	setWebsheetResponseHeaders(c)
	session, err := s.authorizedWebsheetCapabilitySession(c)
	if err != nil {
		respondWebsheetError(c, err)
		return
	}
	if err := session.ServeBootstrap(c.Writer, c.Request); err != nil {
		respondWebsheetError(c, err)
	}
}

// handleWebsheetStatus 供前端轮询流程是否结束。
//
// 承载表单跑在运营商页面里、又是在新窗口打开的，跨源既读不到内容也收不到关闭
// 事件，只能由服务端告诉前端"桥接脚本回调过了没有"。会话过期后返回 410。
func (s *Server) handleWebsheetStatus(c *gin.Context) {
	setWebsheetResponseHeaders(c)
	session, err := s.authorizedWebsheetStatusSession(c)
	if err != nil {
		respondWebsheetError(c, err)
		return
	}
	respondOK(c, session.Status())
}

func (s *Server) handleWebsheetProxy(c *gin.Context) {
	setWebsheetResponseHeaders(c)
	session, err := s.authorizedWebsheetCapabilitySession(c)
	if err != nil {
		respondWebsheetError(c, err)
		return
	}
	if err := session.Proxy(c.Writer, c.Request); err != nil {
		respondWebsheetError(c, err)
	}
}

func (s *Server) handleWebsheetOptions(c *gin.Context) {
	setWebsheetResponseHeaders(c)
	if _, err := s.authorizedWebsheetCapabilitySession(c); err != nil {
		respondWebsheetError(c, err)
		return
	}
	if strings.TrimSpace(c.GetHeader("Origin")) != "null" {
		fail(c, http.StatusForbidden, "websheet_origin_forbidden", "仅允许 WebSheet opaque origin")
		return
	}
	requestedMethod := strings.ToUpper(strings.TrimSpace(c.GetHeader("Access-Control-Request-Method")))
	if !containsString(websheetProxyMethods, requestedMethod) {
		fail(c, http.StatusMethodNotAllowed, "websheet_method_not_allowed", "不支持该 WebSheet 请求方法")
		return
	}
	requestedHeaders, ok := canonicalRequestedHeaders(c.GetHeader("Access-Control-Request-Headers"))
	if !ok {
		fail(c, http.StatusBadRequest, "websheet_headers_invalid", "WebSheet 预检请求头无效")
		return
	}
	c.Header("Access-Control-Allow-Methods", strings.Join(websheetProxyMethods, ", "))
	if requestedHeaders != "" {
		c.Header("Access-Control-Allow-Headers", requestedHeaders)
	}
	c.Header("Access-Control-Max-Age", "600")
	appendVary(c.Writer.Header(), "Access-Control-Request-Method")
	appendVary(c.Writer.Header(), "Access-Control-Request-Headers")
	c.Status(http.StatusNoContent)
}

func canonicalRequestedHeaders(raw string) (string, bool) {
	if strings.TrimSpace(raw) == "" {
		return "", true
	}
	parts := strings.Split(raw, ",")
	canonical := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" || name == "*" || !validHTTPToken(name) {
			return "", false
		}
		canonical = append(canonical, textproto.CanonicalMIMEHeaderKey(name))
	}
	return strings.Join(canonical, ", "), true
}

func validHTTPToken(value string) bool {
	const separators = "()<>@,;:\\\"/[]?={} \t"
	for i := 0; i < len(value); i++ {
		if value[i] <= 32 || value[i] >= 127 || strings.ContainsRune(separators, rune(value[i])) {
			return false
		}
	}
	return value != ""
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (s *Server) handleWebsheetCallback(c *gin.Context) {
	setWebsheetResponseHeaders(c)
	session, err := s.authorizedWebsheetCapabilitySession(c)
	if err != nil {
		respondWebsheetError(c, err)
		return
	}
	var callback websheet.Callback
	if err := c.ShouldBindJSON(&callback); err != nil {
		fail(c, http.StatusBadRequest, "websheet_callback_invalid", err.Error())
		return
	}
	session.Callback(callback)
	if isTerminalWebsheetCallback(callback) {
		session.Done()
	}
	respondOK(c, nil)
}

func isTerminalWebsheetCallback(callback websheet.Callback) bool {
	value := strings.ToLower(strings.TrimSpace(firstNonEmpty(callback.Event, callback.Method, callback.ResultCode)))
	if value == "" {
		return true
	}
	return !strings.Contains(value, "phoneservicesaccountstatuschanged")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (s *Server) handleWebsheetDone(c *gin.Context) {
	setWebsheetResponseHeaders(c)
	session, err := s.authorizedWebsheetCapabilitySession(c)
	if err != nil {
		respondWebsheetError(c, err)
		return
	}
	// 这里刻意不销毁会话：前端要靠 /status 观察终态，而承载页在运营商域下，
	// 跨源拿不到任何完成信号。会话按 TTL 统一回收（websheet.Broker.gcLocked）。
	session.Done()
	respondOK(c, nil)
}

func respondWebsheetError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, websheet.ErrNotFound):
		fail(c, http.StatusNotFound, "websheet_not_found", err.Error())
	case errors.Is(err, websheet.ErrExpired):
		fail(c, http.StatusGone, "websheet_expired", err.Error())
	case errors.Is(err, websheet.ErrUnsafeURL):
		fail(c, http.StatusBadRequest, "websheet_unsafe_url", err.Error())
	case errors.Is(err, websheet.ErrUnauthorized):
		fail(c, http.StatusUnauthorized, "websheet_unauthorized", err.Error())
	default:
		fail(c, http.StatusInternalServerError, "websheet_proxy_failed", err.Error())
	}
}
