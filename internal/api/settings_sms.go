package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodoge/internal/config"
	"github.com/yuanshuai1122/vodoge/internal/db"
	"github.com/yuanshuai1122/vodoge/pkg/logger"
)

func (s *Server) smsHourlyLimit() int {
	if s == nil || s.fullCfg == nil {
		return config.DefaultSMSHourlyLimit
	}
	return config.NormalizeSMSHourlyLimit(s.fullCfg.SMS.HourlyLimit)
}

// reserveSMSSend 在真正发出前占 1 条全局额度。已写响应时返回 false。
func (s *Server) reserveSMSSend(c *gin.Context, deviceID, phone string) bool {
	_, err := s.data().SMS.ReserveSend(s.smsHourlyLimit(), deviceID, phone)
	if err == nil {
		return true
	}
	var limited *db.SMSRateLimitedError
	if errors.As(err, &limited) {
		if limited.RetryAfterSeconds > 0 {
			c.Header("Retry-After", strconv.Itoa(limited.RetryAfterSeconds))
		}
		failWith(c, http.StatusTooManyRequests, "sms_rate_limited", limited.Error(), gin.H{
			"hourly_limit":        limited.HourlyLimit,
			"used":                limited.Used,
			"remaining":           limited.Remaining,
			"retry_after_seconds": limited.RetryAfterSeconds,
			"window_seconds":      limited.WindowSeconds,
			"unlimited":           limited.Unlimited,
		})
		return false
	}
	fail(c, http.StatusInternalServerError, "", "检查发送限额失败: "+err.Error())
	return false
}

func (s *Server) handleGetSMSSettings(c *gin.Context) {
	st, err := s.data().SMS.RateStatus(s.smsHourlyLimit())
	if err != nil {
		fail(c, http.StatusInternalServerError, "", "读取发送限额失败: "+err.Error())
		return
	}
	respondOK(c, st)
}

func (s *Server) handleUpdateSMSSettings(c *gin.Context) {
	var req struct {
		HourlyLimit *int `json:"hourly_limit"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.HourlyLimit == nil {
		fail(c, http.StatusBadRequest, "", "参数错误: 需要 hourly_limit")
		return
	}
	limit := *req.HourlyLimit
	if err := config.ValidateSMSHourlyLimit(limit); err != nil {
		fail(c, http.StatusBadRequest, "", fmt.Sprintf("hourly_limit 允许 0–%d（0 表示不限制）", config.MaxSMSHourlyLimit))
		return
	}
	if strings.TrimSpace(s.configPath) != "" {
		if err := config.UpdateSMSHourlyLimitInFile(s.configPath, limit); err != nil {
			logger.Error("写入短信限额失败", "err", err)
			fail(c, http.StatusInternalServerError, "", "写入配置文件失败: "+err.Error())
			return
		}
	}
	if s.fullCfg != nil {
		s.fullCfg.SMS.HourlyLimit = limit
	}
	st, err := s.data().SMS.RateStatus(limit)
	if err != nil {
		fail(c, http.StatusInternalServerError, "", "读取发送限额失败: "+err.Error())
		return
	}
	respondOKWith(c, st, gin.H{"applied": true})
}
