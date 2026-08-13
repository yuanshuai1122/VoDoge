// VoWiFi 启停与状态。落库的策略副作用在 route_adapters.go 里，
// 这里只负责运行时动作。
package api

import (
	"net/http"

	"github.com/boa-z/vowifi-go/runtimehost/voicehost"

	"github.com/gin-gonic/gin"
)

// handleVoWiFiEnable 为指定设备启用 VoWiFi
func (s *Server) handleVoWiFiEnable(c *gin.Context) {
	deviceID := deviceIDParam(c)
	if deviceID == "" {
		fail(c, http.StatusBadRequest, "", "请指定设备 ID")
		return
	}

	if s.pool == nil {
		fail(c, http.StatusServiceUnavailable, "", "服务未就绪")
		return
	}

	if err := s.pool.EnableVoWiFi(deviceID); err != nil {
		failWith(c, http.StatusInternalServerError, "", "VoWiFi 启用失败: "+err.Error(), gin.H{
			"device": deviceID,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": "VoWiFi 已启用，设备已进入飞行模式",
		"device":  deviceID,
	})
}

// handleVoWiFiDisable 禁用 VoWiFi，保留当前射频/网络状态
func (s *Server) handleVoWiFiDisable(c *gin.Context) {
	deviceID := deviceIDParam(c)

	if s.pool == nil {
		fail(c, http.StatusServiceUnavailable, "", "服务未就绪")
		return
	}

	if err := s.pool.DisableVoWiFi(deviceID); err != nil {
		failWith(c, http.StatusInternalServerError, "", "VoWiFi 禁用失败: "+err.Error(), gin.H{
			"device": deviceID,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": "VoWiFi 已禁用",
		"device":  deviceID,
	})
}

// handleSimulateCall 处理无头模拟呼叫请求
func (s *Server) handleSimulateCall(c *gin.Context) {
	deviceID := deviceIDParam(c)
	if s.voiceGW == nil {
		fail(c, http.StatusServiceUnavailable, "", "语音网关未启用")
		return
	}

	var req voicehost.SimulateCallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", "无效的请求参数："+err.Error())
		return
	}

	result, err := s.voiceGW.SimulateCall(c.Request.Context(), deviceID, req)
	if err != nil {
		failWith(c, http.StatusInternalServerError, "", err.Error(), gin.H{
			"success": false,
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

// handleVoWiFiStatus 返回 VoWiFi 当前状态
func (s *Server) handleVoWiFiStatus(c *gin.Context) {
	if s.pool == nil {
		c.JSON(http.StatusOK, gin.H{
			"enabled":   false,
			"device_id": "",
			"status":    "服务未就绪",
		})
		return
	}

	enabled, deviceID, status := s.pool.GetVoWiFiStatus()
	c.JSON(http.StatusOK, gin.H{
		"enabled":   enabled,
		"device_id": deviceID,
		"status":    status,
	})
}
