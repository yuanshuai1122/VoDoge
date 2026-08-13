// 数据网络启停与换 IP。
package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleRotate(c *gin.Context) {
	var req struct {
		DeviceID       string `json:"device_id" form:"device_id"`
		Username       string `json:"username" form:"username"`
		Password       string `json:"password" form:"password"`
		LegacyDeviceID string `json:"device" form:"device"`
	}
	_ = c.ShouldBind(&req)

	if !s.authorizeRotate(c, req.Username, req.Password, time.Now()) {
		return
	}

	deviceID := c.Query("device_id")
	if deviceID == "" {
		if req.DeviceID != "" {
			deviceID = req.DeviceID
		} else if req.LegacyDeviceID != "" {
			deviceID = req.LegacyDeviceID
		}
	}

	// 如果未指定设备 ID 且只有一个设备，默认使用该设备
	if deviceID == "" {
		workers := s.pool.GetAllWorkers()
		if len(workers) == 1 {
			deviceID = workers[0].ID
		} else {
			fail(c, http.StatusBadRequest, "", "存在多个设备时必须指定 device_id")
			return
		}
	}

	worker := s.pool.GetWorker(deviceID)
	if worker == nil {
		fail(c, http.StatusNotFound, "", "设备未找到")
		return
	}
	nc := worker.NetworkController()
	if nc == nil {
		fail(c, http.StatusBadRequest, "", "当前设备不支持网络控制")
		return
	}
	if !worker.NetworkConnected() {
		fail(c, http.StatusBadRequest, "", "设备网络未连接，请先启动网络")
		return
	}

	// 执行切换 (同步操作，带重试和通知)
	startTime := time.Now()
	oldIP, newIP, err := worker.Rotate()
	duration := time.Since(startTime)

	if err != nil {
		failWith(c, http.StatusInternalServerError, "", err.Error(), gin.H{
			"old_ip":   oldIP,
			"new_ip":   newIP,
			"duration": duration.String(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":   "ok",
		"message":  "IP 切换成功",
		"device":   deviceID,
		"old_ip":   oldIP,
		"new_ip":   newIP,
		"duration": duration.String(),
	})
}

func (s *Server) handleDeviceMgmtStartNetwork(c *gin.Context) {
	deviceID := deviceIDParam(c)
	worker := s.pool.GetWorker(deviceID)
	if worker == nil {
		fail(c, http.StatusNotFound, "", "设备未找到")
		return
	}
	nc := worker.NetworkController()
	if nc == nil {
		fail(c, http.StatusBadRequest, "", "当前设备不支持网络控制")
		return
	}
	if s.pool.IsVoWiFiActive(deviceID) {
		fail(c, http.StatusConflict, "", "VoWiFi 运行中，无法启动数据网络")
		return
	}
	if err := worker.StartNetwork(); err != nil {
		fail(c, http.StatusInternalServerError, "", "启动数据网络失败: "+err.Error())
		return
	}
	go func() { _ = worker.RefreshRuntime(nil, "start_network") }()
	c.JSON(http.StatusOK, gin.H{
		"status":            "ok",
		"message":           "数据网络已启动",
		"device":            deviceID,
		"network_connected": worker.NetworkConnected(),
		"private_ip":        nc.GetPrivateIP(),
		"private_ipv6":      nc.GetPrivateIPv6(),
		"public_ip":         worker.GetCachedIP(),
		"public_ipv6":       worker.GetCachedIPv6(),
	})
}

func (s *Server) handleDeviceMgmtStopNetwork(c *gin.Context) {
	deviceID := deviceIDParam(c)
	worker := s.pool.GetWorker(deviceID)
	if worker == nil {
		fail(c, http.StatusNotFound, "", "设备未找到")
		return
	}
	nc := worker.NetworkController()
	if nc == nil {
		fail(c, http.StatusBadRequest, "", "当前设备不支持网络控制")
		return
	}
	if err := worker.StopNetwork(); err != nil {
		fail(c, http.StatusInternalServerError, "", "停止数据网络失败: "+err.Error())
		return
	}
	go func() { _ = worker.RefreshRuntime(nil, "stop_network") }()
	c.JSON(http.StatusOK, gin.H{
		"status":            "ok",
		"message":           "数据网络已停止",
		"device":            deviceID,
		"network_connected": worker.NetworkConnected(),
		"private_ip":        "",
		"private_ipv6":      "",
		"public_ip":         "",
		"public_ipv6":       "",
	})
}
