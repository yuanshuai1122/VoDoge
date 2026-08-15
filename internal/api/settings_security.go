package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodoge/internal/config"
	"github.com/yuanshuai1122/vodoge/internal/netaccess"
	"github.com/yuanshuai1122/vodoge/pkg/logger"
)

func (s *Server) handleGetSecuritySettings(c *gin.Context) {
	respondOK(c, s.accessSnapshot(c))
}

func (s *Server) handleUpdateSecuritySettings(c *gin.Context) {
	var req netaccess.Policy
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", "参数解析失败: "+err.Error())
		return
	}
	parsed, err := netaccess.Parse(req)
	if err != nil {
		fail(c, http.StatusBadRequest, "invalid_access_policy", err.Error())
		return
	}
	policy := config.AccessPolicy{
		Mode:              parsed.Mode,
		AllowedCIDRs:      parsed.CIDRStrings(),
		TrustProxyHeaders: parsed.TrustProxy,
	}
	if strings.TrimSpace(s.configPath) != "" {
		if err := config.UpdateAccessPolicyInFile(s.configPath, policy); err != nil {
			logger.Error("写入访问策略失败", "err", err)
			fail(c, http.StatusInternalServerError, "", "写入配置文件失败: "+err.Error())
			return
		}
	}
	if s.fullCfg != nil {
		s.fullCfg.Server.Access = policy
	}
	s.setAccessPolicy(parsed)
	respondOKWith(c, s.accessSnapshot(c), gin.H{"applied": true})
}
