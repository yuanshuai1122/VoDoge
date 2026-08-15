package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yuanshuai1122/vodoge/internal/db"
	"github.com/yuanshuai1122/vodoge/pkg/logger"
)

type profileProxyBindingPayload struct {
	DeviceID    string `json:"device_id"`
	ICCID       string `json:"iccid"`
	ProfileName string `json:"profile_name"`
}

func (s *Server) handleListProfileProxyBindings(c *gin.Context) {
	values, err := s.data().UpstreamProxy.ListProfileBindings()
	if err != nil {
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}
	if values == nil {
		values = []db.UpstreamProxyProfileBinding{}
	}
	respondOK(c, values)
}

func (s *Server) handleCreateProfileProxyBindings(c *gin.Context) {
	var req struct {
		UpstreamProxyID string                       `json:"upstream_proxy_id"`
		Bindings        []profileProxyBindingPayload `json:"bindings"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", "参数解析失败: "+err.Error())
		return
	}
	proxy, err := s.data().UpstreamProxy.Get(strings.TrimSpace(req.UpstreamProxyID))
	if err != nil {
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}
	if proxy == nil {
		fail(c, http.StatusNotFound, "", "前置代理不存在")
		return
	}
	if !proxy.Enabled {
		fail(c, http.StatusConflict, "upstream_proxy_disabled", "请先启用该前置代理再绑定 Profile")
		return
	}
	if len(req.Bindings) == 0 || len(req.Bindings) > 200 {
		fail(c, http.StatusBadRequest, "invalid_bindings", "一次请选择 1–200 个 Profile")
		return
	}

	seen := make(map[string]struct{}, len(req.Bindings))
	out := make([]db.UpstreamProxyProfileBinding, 0, len(req.Bindings))
	for _, item := range req.Bindings {
		iccid := db.NormalizeProfileBindingICCID(item.ICCID)
		deviceID := strings.TrimSpace(item.DeviceID)
		if deviceID == "" {
			fail(c, http.StatusBadRequest, "invalid_bindings", "device_id 不能为空")
			return
		}
		if !db.ValidProfileBindingICCID(iccid) {
			fail(c, http.StatusBadRequest, "invalid_iccid", "ICCID 必须是 18–22 位数字")
			return
		}
		if _, dup := seen[iccid]; dup {
			fail(c, http.StatusBadRequest, "invalid_bindings", "同一请求里 ICCID 重复: "+iccid)
			return
		}
		seen[iccid] = struct{}{}
		existing, err := s.data().UpstreamProxy.GetProfileBinding(iccid)
		if err != nil {
			fail(c, http.StatusInternalServerError, "", err.Error())
			return
		}
		if existing != nil && existing.UpstreamProxyID != proxy.ID {
			fail(c, http.StatusConflict, "profile_already_bound", "该 ICCID 已绑定其它前置代理，请先解除")
			return
		}
		name := strings.TrimSpace(item.ProfileName)
		out = append(out, db.UpstreamProxyProfileBinding{
			ICCID:           iccid,
			DeviceID:        deviceID,
			ProfileName:     name,
			UpstreamProxyID: proxy.ID,
		})
	}

	reconnected := make([]string, 0)
	for _, value := range out {
		if err := s.data().UpstreamProxy.UpsertProfileBinding(value); err != nil {
			fail(c, http.StatusInternalServerError, "", err.Error())
			return
		}
		if s.requestProfileProxyRouteReconnect(value.DeviceID, value.ICCID) {
			reconnected = append(reconnected, value.DeviceID)
		}
	}
	respondOKWith(c, out, gin.H{"reconnected": reconnected})
}

func (s *Server) handleDeleteProfileProxyBindings(c *gin.Context) {
	var req struct {
		UpstreamProxyID string   `json:"upstream_proxy_id"`
		ICCIDs          []string `json:"iccids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", "参数解析失败: "+err.Error())
		return
	}
	if len(req.ICCIDs) == 0 || len(req.ICCIDs) > 200 {
		fail(c, http.StatusBadRequest, "invalid_bindings", "一次请选择 1–200 个 ICCID")
		return
	}
	wantProxy := strings.TrimSpace(req.UpstreamProxyID)
	reconnected := make([]string, 0)
	deleted := make([]string, 0, len(req.ICCIDs))
	for _, raw := range req.ICCIDs {
		iccid := db.NormalizeProfileBindingICCID(raw)
		if !db.ValidProfileBindingICCID(iccid) {
			fail(c, http.StatusBadRequest, "invalid_iccid", "ICCID 必须是 18–22 位数字")
			return
		}
		binding, err := s.data().UpstreamProxy.GetProfileBinding(iccid)
		if err != nil {
			fail(c, http.StatusInternalServerError, "", err.Error())
			return
		}
		if binding == nil {
			fail(c, http.StatusNotFound, "binding_not_found", "未找到 ICCID 的绑定: "+iccid)
			return
		}
		if wantProxy != "" && binding.UpstreamProxyID != wantProxy {
			fail(c, http.StatusConflict, "binding_proxy_mismatch", "该 ICCID 未绑定到指定前置代理")
			return
		}
		if err := s.data().UpstreamProxy.DeleteProfileBinding(iccid); err != nil {
			if errors.Is(err, db.ErrProfileBindingNotFound) {
				fail(c, http.StatusNotFound, "binding_not_found", "未找到 ICCID 的绑定: "+iccid)
				return
			}
			fail(c, http.StatusInternalServerError, "", err.Error())
			return
		}
		deleted = append(deleted, iccid)
		if s.requestProfileProxyRouteReconnect(binding.DeviceID, binding.ICCID) {
			reconnected = append(reconnected, binding.DeviceID)
		}
	}
	respondOKWith(c, gin.H{"iccids": deleted}, gin.H{"reconnected": reconnected})
}

// requestProfileProxyRouteReconnect 仅当该 ICCID 正是设备当前卡时触发重连。
// 绑定已落盘；重连失败只记日志，不回滚。
func (s *Server) requestProfileProxyRouteReconnect(deviceID, iccid string) bool {
	if s == nil || s.pool == nil {
		return false
	}
	deviceID = strings.TrimSpace(deviceID)
	iccid = db.NormalizeProfileBindingICCID(iccid)
	if deviceID == "" || iccid == "" {
		return false
	}
	w := s.pool.GetWorker(deviceID)
	if w == nil {
		return false
	}
	current := strings.TrimSpace(w.CurrentICCID())
	if current == "" {
		current = strings.TrimSpace(w.GetCachedDeviceStatus().ICCID)
	}
	if current == "" || current != iccid {
		return false
	}
	if err := s.pool.RestartVoWiFi(deviceID); err != nil {
		logger.Warn("Profile 绑定后重连 VoWiFi 失败",
			"device", deviceID,
			"iccid", iccid,
			"err", err)
		return false
	}
	return true
}
