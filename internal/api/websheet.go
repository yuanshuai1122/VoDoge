package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vohive/internal/websheet"
)

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

func (s *Server) authorizedWebsheetSession(c *gin.Context) (*websheet.Session, error) {
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

func (s *Server) handleWebsheetBootstrap(c *gin.Context) {
	session, err := s.authorizedWebsheetSession(c)
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
	session, err := s.authorizedWebsheetSession(c)
	if err != nil {
		respondWebsheetError(c, err)
		return
	}
	c.JSON(http.StatusOK, session.Status())
}

func (s *Server) handleWebsheetProxy(c *gin.Context) {
	session, err := s.authorizedWebsheetSession(c)
	if err != nil {
		respondWebsheetError(c, err)
		return
	}
	if err := session.Proxy(c.Writer, c.Request); err != nil {
		respondWebsheetError(c, err)
	}
}

func (s *Server) handleWebsheetCallback(c *gin.Context) {
	session, err := s.authorizedWebsheetSession(c)
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
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
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
	session, err := s.authorizedWebsheetSession(c)
	if err != nil {
		respondWebsheetError(c, err)
		return
	}
	// 这里刻意不销毁会话：前端要靠 /status 观察终态，而承载页在运营商域下，
	// 跨源拿不到任何完成信号。会话按 TTL 统一回收（websheet.Broker.gcLocked）。
	session.Done()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
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
