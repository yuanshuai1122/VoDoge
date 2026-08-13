// eSIM：Profile 增删改查、通知、芯片信息。
//
// 所有操作经 APDU 仲裁器串行化，任一调用都可能撞上 ESIM_BUSY（409）。
// Profile 下载不在这里——它是后台任务模型，见 esim_download.go。
package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yuanshuai1122/vohive/internal/apduarbiter"
	"github.com/yuanshuai1122/vohive/internal/device"
	"github.com/yuanshuai1122/vohive/internal/esim"

	"github.com/gin-gonic/gin"
)

// handleEsimListProfiles 获取 eSIM Profile 列表
func (s *Server) handleEsimListProfiles(c *gin.Context) {
	id := deviceIDParam(c)
	worker := s.pool.GetWorker(id)
	if worker == nil || worker.EsimMgr == nil {
		fail(c, http.StatusNotFound, "", "设备或esim管理器未找到")
		return
	}

	refresh := c.Query("refresh") == "true"
	if refresh {
		if err := worker.EsimMgr.RefreshProfiles(); err != nil {
			if isEsimBusyError(err) {
				respondEsimBusy(c, "refresh_profiles", err)
				return
			}
			fail(c, http.StatusInternalServerError, "", err.Error())
			return
		}
	}

	profiles, err := worker.EsimMgr.GetProfiles()
	if err != nil {
		if isEsimBusyError(err) {
			respondEsimBusy(c, "list_profiles", err)
			return
		}
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}
	c.JSON(http.StatusOK, profiles)
}

// esimSwitchRequest 包含切换的目标 ICCID
type esimSwitchRequest struct {
	ICCID  string `json:"iccid" binding:"required"`
	AIDHex string `json:"aid_hex"` // 可选，前端已知时直接传，跳过遍历
}

type esimSwitchResponse struct {
	Message            string `json:"message"`
	TargetICCID        string `json:"target_iccid"`
	SwitchToken        uint64 `json:"switch_token"`
	SwitchPhase        string `json:"switch_phase"`
	SwitchAccepted     bool   `json:"switch_accepted"`
	RecoveryPending    bool   `json:"recovery_pending"`
	DegradedReason     string `json:"degraded_reason,omitempty"`
	PostSwitchAsync    bool   `json:"post_switch_async"`
	CachePatched       bool   `json:"cache_patched"`
	SIMReloadAttempted bool   `json:"sim_reload_attempted"`
	SIMReloadOK        bool   `json:"sim_reload_ok"`
	SIMReloadWarning   string `json:"sim_reload_warning,omitempty"`
}

const esimBusyRetryAfterMs = 1200

func isEsimBusyError(err error) bool {
	return errors.Is(err, esim.ErrOperationInProgress) || errors.Is(err, apduarbiter.ErrAPDUBusy)
}

func esimDeleteHTTPStatus(err error) int {
	switch {
	case isEsimBusyError(err) || esim.IsDeleteProfileBusy(err):
		return http.StatusConflict
	case esim.IsDeleteProfileInvalidInput(err):
		return http.StatusBadRequest
	case esim.IsDeleteProfileNotFound(err):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

func respondEsimBusy(c *gin.Context, reason string, err error) {
	retryAfterSec := (esimBusyRetryAfterMs + 999) / 1000
	c.Header("Retry-After", strconv.Itoa(retryAfterSec))
	failWith(c, http.StatusConflict, "ESIM_BUSY", err.Error(), gin.H{
		"busy":   true,
		"reason": reason,
		// retryAfterMs 是全局 snake_case 里唯一的 camelCase 字段。前端与外部
		// 脚本都在读它，改名是破坏性的；两个一起给，新代码用 retry_after_ms。
		"retryAfterMs":   esimBusyRetryAfterMs,
		"retry_after_ms": esimBusyRetryAfterMs,
	})
}

func esimDeleteSuccessBody(result esim.DeleteProfileResult) gin.H {
	body := gin.H{
		"status":  "ok",
		"message": "Profile 删除成功",
	}
	if warning := strings.TrimSpace(result.Warning); warning != "" {
		body["warning"] = warning
	}
	if warningCode := strings.TrimSpace(result.WarningCode); warningCode != "" {
		body["warning_code"] = warningCode
	}
	if result.SpaceDelta != nil {
		body["space_delta"] = result.SpaceDelta
	}
	return body
}

func writeEsimDeleteSuccessJSON(c *gin.Context, result esim.DeleteProfileResult) {
	c.JSON(http.StatusOK, esimDeleteSuccessBody(result))
}

// esimNotificationSource 是 eSIM 通知的来源。
//
// 真实实现就是设备的 *esim.Manager，它要跟 eUICC 走 APDU；本机与 CI 都没有硬件，
// 所以必须可替换。此前的做法是两个包级 `var xxxExec = func(run, args)`，
// 把真实方法当参数传进去再由测试整个换掉——全局可变，而且那层间接本身
// 不表达任何东西（它只是 `return run(args)`）。
//
// 现在的边界表达的是"通知从哪儿来"，不是"把这次调用包一层"。
type esimNotificationSource interface {
	ListNotifications(aidHex string) ([]esim.NotificationItem, error)
	RetryNotification(sequence int64, aidHex string) error
}

// esimNotifications 返回该设备的通知来源；未注入时用设备自己的 eSIM 管理器。
func (s *Server) esimNotifications(worker *device.Worker) esimNotificationSource {
	if s.esimNotificationsFor != nil {
		return s.esimNotificationsFor(worker)
	}
	return worker.EsimMgr
}

func esimDeleteExec(run func(string, string) (esim.DeleteProfileResult, error), iccid, aidHex string) (esim.DeleteProfileResult, error) {
	return run(iccid, aidHex)
}

func esimNotificationHTTPStatus(err error) int {
	switch esim.ClassifyNotificationError(err) {
	case esim.NotificationErrorBusy:
		return http.StatusConflict
	case esim.NotificationErrorInvalidSequence, esim.NotificationErrorInvalidAIDHex:
		return http.StatusBadRequest
	case esim.NotificationErrorNotFound:
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

func (s *Server) handleEsimListNotifications(c *gin.Context) {
	id := deviceIDParam(c)
	worker := s.pool.GetWorker(id)
	if worker == nil || worker.EsimMgr == nil {
		fail(c, http.StatusNotFound, "", "设备或esim管理器未找到")
		return
	}
	items, err := s.esimNotifications(worker).ListNotifications(strings.TrimSpace(c.Query("aid_hex")))
	if err != nil {
		if esimNotificationHTTPStatus(err) == http.StatusConflict {
			respondEsimBusy(c, "list_notifications", err)
			return
		}
		fail(c, esimNotificationHTTPStatus(err), "", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (s *Server) handleEsimRetryNotification(c *gin.Context) {
	id := deviceIDParam(c)
	worker := s.pool.GetWorker(id)
	if worker == nil || worker.EsimMgr == nil {
		fail(c, http.StatusNotFound, "", "设备或esim管理器未找到")
		return
	}
	sequence, err := strconv.ParseInt(strings.TrimSpace(c.Param("sequence")), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, "", "无效的通知序号")
		return
	}
	err = s.esimNotifications(worker).RetryNotification(sequence, strings.TrimSpace(c.Query("aid_hex")))
	if err != nil {
		if esimNotificationHTTPStatus(err) == http.StatusConflict {
			respondEsimBusy(c, "retry_notification", err)
			return
		}
		fail(c, esimNotificationHTTPStatus(err), "", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "通知重试发送成功"})
}

// handleEsimSwitchProfile 切换 eSIM Profile
func (s *Server) handleEsimSwitchProfile(c *gin.Context) {
	id := deviceIDParam(c)
	var req esimSwitchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", err.Error())
		return
	}

	worker := s.pool.GetWorker(id)
	if worker == nil || worker.EsimMgr == nil {
		fail(c, http.StatusNotFound, "", "设备或esim管理器未找到")
		return
	}

	// Profile 切换：EnableProfile 后等待目标 profile 生效；切卡后按 Ready+Delay 门控执行后处理（不等待搜网）
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := worker.EsimMgr.SwitchProfileWithResult(ctx, req.ICCID, req.AIDHex)
	if err != nil {
		if isEsimBusyError(err) {
			respondEsimBusy(c, "switch_profile", err)
			return
		}
		fail(c, http.StatusInternalServerError, "", "esim配置切换失败: "+err.Error())
		return
	}

	c.JSON(http.StatusOK, esimSwitchResponse{
		Message:            "eSIM Profile 切换指令已提交，设备信息将异步刷新",
		TargetICCID:        result.TargetICCID,
		SwitchToken:        result.SwitchToken,
		SwitchPhase:        string(result.Phase),
		SwitchAccepted:     result.SwitchAccepted,
		RecoveryPending:    result.RecoveryPending,
		DegradedReason:     result.DegradedReason,
		PostSwitchAsync:    result.PostSwitchAsync,
		CachePatched:       result.CachePatched,
		SIMReloadAttempted: result.PowerCycleAttempt,
		SIMReloadOK:        result.PowerCycleAttempt && result.SIMReloadWarning == "",
		SIMReloadWarning:   result.SIMReloadWarning,
	})

}

// handleEsimGetEID 获取所有 eUICC 的 EID 列表
func (s *Server) handleEsimGetEID(c *gin.Context) {
	id := deviceIDParam(c)
	worker := s.pool.GetWorker(id)
	if worker == nil || worker.EsimMgr == nil {
		fail(c, http.StatusNotFound, "", "设备或esim管理器未找到")
		return
	}

	eids, err := worker.EsimMgr.GetEIDs()
	if err != nil {
		if isEsimBusyError(err) {
			respondEsimBusy(c, "get_eids", err)
			return
		}
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"eids": eids})
}

// handleEsimGetChipInfo 获取 eUICC 芯片硬件信息（名称、序列号、固件版本、可用空间）
func (s *Server) handleEsimGetChipInfo(c *gin.Context) {
	id := deviceIDParam(c)
	worker := s.pool.GetWorker(id)
	if worker == nil || worker.EsimMgr == nil {
		fail(c, http.StatusNotFound, "", "设备或esim管理器未找到")
		return
	}

	forceRefresh := c.Query("refresh") == "true"
	chipInfo, err := worker.EsimMgr.GetEUICCChipInfo(forceRefresh)
	if err != nil {
		if isEsimBusyError(err) {
			respondEsimBusy(c, "get_chip_info", err)
			return
		}
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}
	c.JSON(http.StatusOK, chipInfo)
}

// handleEsimGetOverview 获取 eSIM 总览（合并芯片信息和 profiles）
func (s *Server) handleEsimGetOverview(c *gin.Context) {
	id := deviceIDParam(c)
	worker := s.pool.GetWorker(id)
	if worker == nil || worker.EsimMgr == nil {
		fail(c, http.StatusNotFound, "", "设备或esim管理器未找到")
		return
	}

	refresh := c.Query("refresh") == "true"
	if refresh {
		if err := worker.EsimMgr.RefreshOverview(); err != nil {
			if isEsimBusyError(err) {
				respondEsimBusy(c, "refresh_overview", err)
				return
			}
			fail(c, http.StatusInternalServerError, "", err.Error())
			return
		}
	}

	overview, err := worker.EsimMgr.GetEsimOverview()
	if err != nil {
		if isEsimBusyError(err) {
			respondEsimBusy(c, "get_overview", err)
			return
		}
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}

	c.JSON(http.StatusOK, overview)
}

// handleEsimRenameProfile 修改 eSIM profile 名称
func (s *Server) handleEsimRenameProfile(c *gin.Context) {
	id := deviceIDParam(c)
	iccid := c.Param("iccid")
	worker := s.pool.GetWorker(id)
	if worker == nil || worker.EsimMgr == nil {
		fail(c, http.StatusNotFound, "", "设备或esim管理器未找到")
		return
	}

	var req struct {
		Name   string `json:"name"`
		AIDHex string `json:"aid_hex"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		fail(c, http.StatusBadRequest, "", "name 为必填项")
		return
	}

	if err := worker.EsimMgr.RenameProfile(iccid, req.Name, req.AIDHex); err != nil {
		if isEsimBusyError(err) {
			respondEsimBusy(c, "rename_profile", err)
			return
		}
		fail(c, http.StatusInternalServerError, "", "修改名称失败: "+err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "Profile 名称修改成功"})
}

// handleEsimDeleteProfile 删除 eSIM profile
func (s *Server) handleEsimDeleteProfile(c *gin.Context) {
	id := deviceIDParam(c)
	iccid := c.Param("iccid")
	worker := s.pool.GetWorker(id)
	if worker == nil || worker.EsimMgr == nil {
		fail(c, http.StatusNotFound, "", "设备或esim管理器未找到")
		return
	}

	if iccid == "" {
		fail(c, http.StatusBadRequest, "", "iccid 为必填项")
		return
	}

	aidHex := strings.TrimSpace(c.Query("aid_hex"))

	result, err := esimDeleteExec(worker.EsimMgr.DeleteProfile, iccid, aidHex)
	if err != nil {
		// 删除主路径当前是阻塞等待写锁，通常不会快速返回 busy；
		// 保留该分支用于底层未来显式返回 busy 的防御处理。
		if esimDeleteHTTPStatus(err) == http.StatusConflict {
			respondEsimBusy(c, "delete_profile", err)
			return
		}
		fail(c, esimDeleteHTTPStatus(err), "", "删除 profile 失败: "+err.Error())
		return
	}

	writeEsimDeleteSuccessJSON(c, result)
}
