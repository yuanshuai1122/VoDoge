package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodog/internal/config"
	"github.com/yuanshuai1122/vodog/pkg/logger"
)

func (s *Server) deviceLimit() int {
	return config.ResolveDeviceLimit(s.fullCfg)
}

func (s *Server) configuredDeviceCount() int {
	if s != nil && s.fullCfg != nil {
		n := 0
		for _, d := range s.fullCfg.Devices {
			if strings.TrimSpace(d.ID) != "" {
				n++
			}
		}
		return n
	}
	n := 0
	for _, d := range config.ListDevices() {
		if strings.TrimSpace(d.ID) != "" {
			n++
		}
	}
	return n
}

type deviceQuotaResponse struct {
	Limit        int `json:"limit"`
	Used         int `json:"used"`
	DefaultLimit int `json:"default_limit"`
	MaxLimit     int `json:"max_limit"`
}

func (s *Server) deviceQuotaPayload() deviceQuotaResponse {
	limit := s.deviceLimit()
	used := s.configuredDeviceCount()
	return deviceQuotaResponse{
		Limit:        limit,
		Used:         used,
		DefaultLimit: config.DefaultDeviceLimit,
		MaxLimit:     config.MaxDeviceLimit,
	}
}

func (s *Server) handleGetDeviceQuota(c *gin.Context) {
	respondOK(c, s.deviceQuotaPayload())
}

func (s *Server) handleUpdateDeviceQuota(c *gin.Context) {
	var req struct {
		Limit *int `json:"limit"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Limit == nil {
		fail(c, http.StatusBadRequest, "", "参数错误: 需要 limit")
		return
	}
	limit := *req.Limit
	if err := config.ValidateDeviceLimit(limit); err != nil {
		fail(c, http.StatusBadRequest, "", fmt.Sprintf("limit 允许 1–%d", config.MaxDeviceLimit))
		return
	}
	if strings.TrimSpace(s.configPath) != "" {
		if err := config.UpdateDeviceLimitInFile(s.configPath, limit); err != nil {
			logger.Error("写入设备配额失败", "err", err)
			fail(c, http.StatusInternalServerError, "", "写入配置文件失败: "+err.Error())
			return
		}
	}
	if s.fullCfg != nil {
		s.fullCfg.Server.MaxDevices = limit
	}
	respondOKWith(c, s.deviceQuotaPayload(), gin.H{"applied": true})
}
