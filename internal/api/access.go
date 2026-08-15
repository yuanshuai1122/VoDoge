package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodoge/internal/config"
	"github.com/yuanshuai1122/vodoge/internal/netaccess"
	"github.com/yuanshuai1122/vodoge/pkg/logger"
)

func (s *Server) setAccessPolicy(p netaccess.Parsed) {
	if s == nil {
		return
	}
	s.accessMu.Lock()
	s.access = p
	s.accessMu.Unlock()
}

func (s *Server) currentAccessPolicy() netaccess.Parsed {
	if s == nil {
		return netaccess.Default()
	}
	s.accessMu.RLock()
	defer s.accessMu.RUnlock()
	if s.access.Mode == "" {
		// 未加载策略时不拦截（单测直接构造 Server 走这条）。
		// 生产路径 New() 会 loadAccessPolicyFromConfig，默认 internal。
		return netaccess.Parsed{Mode: netaccess.ModePublic}
	}
	return s.access
}

func (s *Server) loadAccessPolicyFromConfig() {
	src := config.AccessPolicy{}
	if s != nil && s.fullCfg != nil {
		src = s.fullCfg.Server.Access
	}
	parsed, err := netaccess.Parse(netaccess.Policy{
		Mode:              src.Mode,
		AllowedCIDRs:      src.AllowedCIDRs,
		TrustProxyHeaders: src.TrustProxyHeaders,
	})
	if err != nil {
		logger.Warn("访问策略无效，回落到仅内网", "err", err)
		parsed = netaccess.Default()
	}
	s.setAccessPolicy(parsed)
}

func (s *Server) accessControlMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		policy := s.currentAccessPolicy()
		addr := policy.ClientIP(c.Request)
		if policy.Allowed(addr) {
			c.Next()
			return
		}
		logger.Warn("访问被网段策略拒绝",
			"remote_addr", c.Request.RemoteAddr,
			"client_ip", addr.String(),
			"path", c.Request.URL.Path)
		fail(c, http.StatusForbidden, "network_access_denied", "仅允许内网地址访问")
		c.Abort()
	}
}

// accessSnapshot 给设置页回显。
type accessSnapshot struct {
	Mode              string   `json:"mode"`
	AllowedCIDRs      []string `json:"allowed_cidrs"`
	TrustProxyHeaders bool     `json:"trust_proxy_headers"`
	ClientIP          string   `json:"client_ip"`
	ClientAllowed     bool     `json:"client_allowed"`
}

func (s *Server) accessSnapshot(c *gin.Context) accessSnapshot {
	policy := s.currentAccessPolicy()
	addr := policy.ClientIP(c.Request)
	return accessSnapshot{
		Mode:              policy.Mode,
		AllowedCIDRs:      policy.CIDRStrings(),
		TrustProxyHeaders: policy.TrustProxy,
		ClientIP:          addr.String(),
		ClientAllowed:     policy.Allowed(addr),
	}
}
