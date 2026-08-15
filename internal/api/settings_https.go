package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodoge/internal/config"
	"github.com/yuanshuai1122/vodoge/pkg/logger"
)

func (s *Server) handleGetHTTPSSettings(c *gin.Context) {
	if s.https == nil {
		fail(c, http.StatusServiceUnavailable, "https_unavailable", "本机自签 HTTPS 不可用")
		return
	}
	if _, err := s.https.CertificatePEM(); err != nil {
		fail(c, http.StatusInternalServerError, "", "读取证书失败: "+err.Error())
		return
	}
	respondOK(c, s.https.State(c.Request.Host))
}

func (s *Server) handleUpdateHTTPSSettings(c *gin.Context) {
	if s.https == nil {
		fail(c, http.StatusServiceUnavailable, "https_unavailable", "本机自签 HTTPS 不可用")
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		fail(c, http.StatusBadRequest, "", "参数错误: 需要 enabled")
		return
	}
	enabled := *req.Enabled
	if _, err := s.https.SetEnabled(enabled); err != nil {
		fail(c, http.StatusInternalServerError, "", "更新 HTTPS 失败: "+err.Error())
		return
	}
	if strings.TrimSpace(s.configPath) != "" {
		if err := config.UpdateSelfSignedHTTPSInFile(s.configPath, enabled); err != nil {
			logger.Error("写入 HTTPS 开关失败", "err", err)
			fail(c, http.StatusInternalServerError, "", "写入配置文件失败: "+err.Error())
			return
		}
	}
	if s.fullCfg != nil {
		s.fullCfg.Server.SelfSignedHTTPS = enabled
	}
	respondOKWith(c, s.https.State(c.Request.Host), gin.H{"applied": true})
}

func (s *Server) handleDownloadHTTPSCertificate(c *gin.Context) {
	if s.https == nil {
		fail(c, http.StatusServiceUnavailable, "https_unavailable", "本机自签 HTTPS 不可用")
		return
	}
	pem, err := s.https.CertificatePEM()
	if err != nil {
		fail(c, http.StatusInternalServerError, "", "读取证书失败: "+err.Error())
		return
	}
	c.Header("Content-Type", "application/x-pem-file")
	c.Header("Content-Disposition", `attachment; filename="vodoge-selfsigned.crt"`)
	c.Header("Content-Length", strconv.Itoa(len(pem)))
	c.Data(http.StatusOK, "application/x-pem-file", pem)
}
