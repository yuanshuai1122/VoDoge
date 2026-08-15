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

	"github.com/yuanshuai1122/vodoge/internal/apduarbiter"
	"github.com/yuanshuai1122/vodoge/internal/device"
	"github.com/yuanshuai1122/vodoge/internal/esim"
	"github.com/yuanshuai1122/vodoge/internal/pcsc"

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
	respondOK(c, profiles)
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
	case esim.IsProfileEnabled(err):
		return http.StatusConflict
	case esim.IsDeleteProfileInvalidInput(err):
		return http.StatusBadRequest
	case esim.IsDeleteProfileNotFound(err):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

// esimWriteHTTPStatus 给切换/禁用用。删 profile 的结构化错误也会从 findAID 冒上来。
func esimWriteHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if isEsimBusyError(err) {
		return http.StatusConflict
	}
	if errors.Is(err, pcsc.ErrInUse) {
		return http.StatusConflict
	}
	if errors.Is(err, pcsc.ErrAPDUUnavailable) || errors.Is(err, pcsc.ErrNoCard) {
		return http.StatusServiceUnavailable
	}
	if esim.IsDeleteProfileInvalidInput(err) {
		return http.StatusBadRequest
	}
	if esim.IsDeleteProfileNotFound(err) {
		return http.StatusNotFound
	}
	msg := err.Error()
	if strings.Contains(msg, "无效的 ICCID") || strings.Contains(msg, "无效的 AID") {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func esimWriteErrorCode(err error) string {
	switch {
	case isEsimBusyError(err):
		return "ESIM_BUSY"
	case esim.IsDeleteProfileInvalidInput(err):
		return "INVALID_INPUT"
	case esim.IsDeleteProfileNotFound(err):
		return "PROFILE_NOT_FOUND"
	case esim.IsProfileEnabled(err):
		return "PROFILE_ENABLED"
	default:
		if esimWriteHTTPStatus(err) == http.StatusBadRequest {
			return "INVALID_INPUT"
		}
		return ""
	}
}

func respondEsimBusy(c *gin.Context, reason string, err error) {
	retryAfterSec := (esimBusyRetryAfterMs + 999) / 1000
	c.Header("Retry-After", strconv.Itoa(retryAfterSec))
	failWith(c, http.StatusConflict, "ESIM_BUSY", err.Error(), gin.H{
		"busy":   true,
		"reason": reason,
		// camelCase 的 retryAfterMs 已随信封改造一并删除：它当初保留是为了不破坏
		// 既有调用方，而这次本来就是破坏性变更，再留一个错误拼写没有意义。
		"retry_after_ms": esimBusyRetryAfterMs,
	})
}

// esimDeleteSuccessMeta 是删除成功时的元数据。
//
// 全部内容都在描述"这次删除"——提示语、通知未确认的告警、eUICC 空间变化，
// 没有一项是被删掉的资源本身，所以整体归 meta 而不是 data。
// 删除操作的 data 是 null。
func esimDeleteSuccessMeta(result esim.DeleteProfileResult) gin.H {
	meta := gin.H{"message": "Profile 删除成功"}
	if warning := strings.TrimSpace(result.Warning); warning != "" {
		meta["warning"] = warning
	}
	if warningCode := strings.TrimSpace(result.WarningCode); warningCode != "" {
		meta["warning_code"] = warningCode
	}
	if result.SpaceDelta != nil {
		meta["space_delta"] = result.SpaceDelta
	}
	return meta
}

func writeEsimDeleteSuccessJSON(c *gin.Context, result esim.DeleteProfileResult) {
	respondOKWith(c, nil, esimDeleteSuccessMeta(result))
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
	respondOK(c, items)
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
	respondOKWith(c, nil, gin.H{"message": "通知重试发送成功"})
}

func (s *Server) holdModemESIM(c *gin.Context, worker *device.Worker) bool {
	if s.pool == nil || worker == nil {
		return true
	}
	if err := s.pool.HoldModemESIM(worker.ID, worker.CurrentICCID()); err != nil {
		fail(c, http.StatusConflict, "ESIM_IN_USE", err.Error())
		return false
	}
	return true
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
	if !s.holdModemESIM(c, worker) {
		return
	}
	defer s.pool.ReleaseESIMHold(worker.ID)

	// Profile 切换：EnableProfile 后等待目标 profile 生效；切卡后按 Ready+Delay 门控执行后处理（不等待搜网）
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := worker.EsimMgr.SwitchProfileWithResult(ctx, req.ICCID, req.AIDHex)
	if err != nil {
		if isEsimBusyError(err) {
			respondEsimBusy(c, "switch_profile", err)
			return
		}
		status := esimWriteHTTPStatus(err)
		fail(c, status, esimWriteErrorCode(err), "切换 Profile 失败: "+err.Error())
		return
	}

	respondOK(c, esimSwitchResponse{
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

// handleEsimDisableProfile 禁用当前启用的 Profile。
// 对照 VoCat：当前卡可单独禁用，禁用后该 eUICC 没有活动号码，短信身份会空，直到再启用一张。
func (s *Server) handleEsimDisableProfile(c *gin.Context) {
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
	if !s.holdModemESIM(c, worker) {
		return
	}
	defer s.pool.ReleaseESIMHold(worker.ID)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := worker.EsimMgr.DisableProfile(ctx, req.ICCID, req.AIDHex); err != nil {
		if isEsimBusyError(err) {
			respondEsimBusy(c, "disable_profile", err)
			return
		}
		status := esimWriteHTTPStatus(err)
		fail(c, status, esimWriteErrorCode(err), "禁用 Profile 失败: "+err.Error())
		return
	}

	respondOKWith(c, gin.H{
		"target_iccid": strings.TrimSpace(req.ICCID),
	}, gin.H{
		"message": "eSIM Profile 已禁用。该卡槽现在没有活动号码，短信会停到重新启用一张为止。",
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
	respondOK(c, eids)
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
	respondOK(c, chipInfo)
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

	respondOK(c, overview)
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

	respondOKWith(c, nil, gin.H{"message": "Profile 名称修改成功"})
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

	if !s.holdModemESIM(c, worker) {
		return
	}
	defer s.pool.ReleaseESIMHold(worker.ID)

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
