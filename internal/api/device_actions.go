// 设备动作：AT、USSD、重启、飞行模式、USBNET 模式、VoWiFi 重连。
//
// 共同点是都对模组下发指令并等待结果，因而都有超时与瞬时失败的处理。
package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/yuanshuai1122/vodoge/internal/backend"
	"github.com/yuanshuai1122/vodoge/internal/config"
	"github.com/yuanshuai1122/vodoge/internal/db"
	"github.com/yuanshuai1122/vodoge/internal/device"
	"github.com/yuanshuai1122/vodoge/internal/modem"
	"github.com/yuanshuai1122/vodoge/pkg/logger"

	"github.com/gin-gonic/gin"
)

type executeATRequest struct {
	Cmd       string `json:"cmd"`
	TimeoutMs int    `json:"timeout_ms"`
}

type manualATSession interface {
	Execute(cmd string, timeout time.Duration) (string, error)
	Close() error
}

var openManualATSession = func(port string) (manualATSession, error) {
	return modem.NewSerialAT(port, 115200, 8, 1, "N")
}

func executeManualATOnPort(port, cmd string, timeout time.Duration) (string, error) {
	port = strings.TrimSpace(port)
	if port == "" {
		return "", fmt.Errorf("当前设备没有可用 AT 端口")
	}
	session, err := openManualATSession(port)
	if err != nil {
		return "", fmt.Errorf("打开 AT 端口 %s 失败: %w", port, err)
	}
	defer session.Close()
	return session.Execute(cmd, timeout)
}

func manualATPortForWorker(worker *device.Worker) string {
	return worker.ResolvedATPort()
}

func (s *Server) handleDeviceMgmtExecuteAT(c *gin.Context) {
	id := deviceIDParam(c)
	var req executeATRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", "参数错误")
		return
	}
	cmd := strings.TrimSpace(req.Cmd)
	if cmd == "" {
		fail(c, http.StatusBadRequest, "", "cmd 不能为空")
		return
	}
	if len(cmd) > 512 {
		fail(c, http.StatusBadRequest, "", "cmd 过长")
		return
	}

	worker := s.pool.GetWorker(id)
	if worker == nil {
		fail(c, http.StatusNotFound, "", "设备未找到或未运行")
		return
	}

	timeout := 10 * time.Second
	if req.TimeoutMs > 0 {
		timeout = time.Duration(req.TimeoutMs) * time.Millisecond
	}
	if timeout > 60*time.Second {
		timeout = 60 * time.Second
	}

	if worker.Backend != nil && isTransientATBackend(worker.Backend.Mode()) {
		resp, err := executeManualATOnPort(manualATPortForWorker(worker), cmd, timeout)
		if err != nil {
			fail(c, http.StatusInternalServerError, "", err.Error())
			return
		}
		respondOK(c, resp)
		return
	}

	if worker.Modem == nil {
		fail(c, http.StatusBadRequest, "", "当前设备没有可用 AT 管理器")
		return
	}
	if !worker.Modem.HasATPort() {
		fail(c, http.StatusBadRequest, "", "当前设备没有可用 AT 端口")
		return
	}
	if !worker.Modem.CanExecuteAT() {
		fail(c, http.StatusBadRequest, "", "AT 管理器未启动或不可用")
		return
	}
	resp, err := worker.Modem.ExecuteAT(cmd, timeout)
	if err != nil {
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}
	respondOK(c, resp)
}

func isTransientATBackend(mode string) bool {
	return mode == backend.BackendQMI || mode == backend.BackendMBIM
}

type setUSBNetModeRequest struct {
	Mode int `json:"mode"`
}

func (s *Server) handleDeviceMgmtSetUSBNetMode(c *gin.Context) {
	id := deviceIDParam(c)
	var req setUSBNetModeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", "参数错误")
		return
	}

	worker := s.pool.GetWorker(id)
	if worker == nil {
		fail(c, http.StatusNotFound, "", "设备未找到或未运行")
		return
	}

	if worker.Modem == nil {
		fail(c, http.StatusBadRequest, "", "当前设备为纯 QMI 模式，不支持 USBNET 模式设置")
		return
	}
	if err := worker.Modem.SetUSBNetMode(req.Mode); err != nil {
		logger.Error("设置 USBNET 模式失败", "device", id, "err", err)
		fail(c, http.StatusInternalServerError, "", "设置模式失败: "+err.Error())
		return
	}

	respondOKWith(c, nil, gin.H{"message": "指令已发送，设备正在重启..."})
}

type executeUSSDRequest struct {
	Command   string `json:"command" binding:"required"`
	TimeoutMs int    `json:"timeout_ms"`
}

// handleDeviceMgmtExecuteUSSD 执行 USSD 指令
// 路由策略：VoWiFi 在线时优先使用 VoWiFi 通道，否则回退到 CS 域
func (s *Server) handleDeviceMgmtExecuteUSSD(c *gin.Context) {
	id := deviceIDParam(c)
	var req executeUSSDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", "参数错误: "+err.Error())
		return
	}

	cmd := strings.TrimSpace(req.Command)
	if cmd == "" {
		fail(c, http.StatusBadRequest, "", "command 不能为空")
		return
	}

	worker := s.pool.GetWorker(id)
	if worker == nil {
		fail(c, http.StatusNotFound, "", "设备未找到")
		return
	}

	timeout := 45 * time.Second // 默认 45 秒（与 CS 域 USSD 一致）
	if req.TimeoutMs > 0 {
		timeout = time.Duration(req.TimeoutMs) * time.Millisecond
	}
	if timeout > 120*time.Second {
		timeout = 120 * time.Second
	}

	// VoWiFi 在线时优先走 VoWiFi
	if s.pool.IsVoWiFiActive(id) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		resp, err := s.pool.SendVoWiFiUSSD(ctx, id, cmd)
		if err != nil {
			failWith(c, http.StatusInternalServerError, "", err.Error(), gin.H{
				"channel": "vowifi",
			})
			return
		}
		respondOKWith(c, resp, gin.H{
			"channel": "vowifi",
		})
		return
	}

	// 回退到 CS 域 USSD
	provider, ok := worker.Backend.(backend.USSDProvider)
	if !ok || provider == nil {
		fail(c, http.StatusBadRequest, "", "当前设备后端不支持 USSD")
		return
	}
	resp, err := provider.ExecuteUSSD(c.Request.Context(), cmd, timeout)
	if err != nil {
		failWith(c, http.StatusInternalServerError, "", err.Error(), gin.H{
			"channel": "cs",
		})
		return
	}
	markCSUSSDSession(resp)
	respondOKWith(c, resp, gin.H{
		"channel": "cs",
	})
}

type continueUSSDRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Input     string `json:"input" binding:"required"`
	TimeoutMs int    `json:"timeout_ms"`
}

// handleDeviceMgmtContinueUSSD 发送 USSD 后续输入（多轮菜单选择）
func (s *Server) handleDeviceMgmtContinueUSSD(c *gin.Context) {
	id := deviceIDParam(c)
	var req continueUSSDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", "参数错误: "+err.Error())
		return
	}

	input := strings.TrimSpace(req.Input)
	if input == "" {
		fail(c, http.StatusBadRequest, "", "input 不能为空")
		return
	}

	timeout := 45 * time.Second
	if req.TimeoutMs > 0 {
		timeout = time.Duration(req.TimeoutMs) * time.Millisecond
	}
	if timeout > 120*time.Second {
		timeout = 120 * time.Second
	}

	if !s.pool.IsVoWiFiActive(id) {
		worker := s.pool.GetWorker(id)
		if worker == nil {
			fail(c, http.StatusNotFound, "", "设备未找到")
			return
		}
		provider, ok := worker.Backend.(backend.USSDContinueProvider)
		if !ok || provider == nil {
			fail(c, http.StatusBadRequest, "", "当前设备后端不支持多轮 USSD")
			return
		}
		resp, err := provider.ContinueUSSD(c.Request.Context(), input, timeout)
		if err != nil {
			failWith(c, http.StatusInternalServerError, "", err.Error(), gin.H{
				"channel": "cs",
			})
			return
		}
		markCSUSSDSession(resp)
		respondOKWith(c, resp, gin.H{
			"channel": "cs",
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()
	resp, err := s.pool.ContinueVoWiFiUSSD(ctx, id, req.SessionID, input)
	if err != nil {
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}
	respondOKWith(c, resp, gin.H{
		"channel": "vowifi",
	})
}

type cancelUSSDRequest struct {
	SessionID string `json:"session_id"`
}

// handleDeviceMgmtCancelUSSD 取消活跃的 USSD 会话
func (s *Server) handleDeviceMgmtCancelUSSD(c *gin.Context) {
	id := deviceIDParam(c)
	var req cancelUSSDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// session_id 是可选的
		req.SessionID = ""
	}

	if !s.pool.IsVoWiFiActive(id) {
		worker := s.pool.GetWorker(id)
		if worker == nil {
			fail(c, http.StatusNotFound, "", "设备未找到")
			return
		}
		provider, ok := worker.Backend.(backend.USSDProvider)
		if !ok || provider == nil {
			fail(c, http.StatusBadRequest, "", "当前设备后端不支持 USSD 取消")
			return
		}
		if err := provider.CancelUSSD(c.Request.Context()); err != nil {
			fail(c, http.StatusInternalServerError, "", err.Error())
			return
		}
		respondOKWith(c, nil, gin.H{
			"message": "USSD 会话已取消",
			"channel": "cs",
		})
		return
	}

	if err := s.pool.CancelVoWiFiUSSD(c.Request.Context(), id, req.SessionID); err != nil {
		fail(c, http.StatusInternalServerError, "", err.Error())
		return
	}
	respondOKWith(c, nil, gin.H{"message": "USSD 会话已取消"})
}

func markCSUSSDSession(resp *backend.USSDResult) {
	if resp != nil && resp.Status == 1 {
		resp.SessionID = "cs"
	}
}

type setFlightModeRequest struct {
	Enabled bool `json:"enabled"`
}

func isFlightModeEnabled(mode int) bool {
	return mode == int(backend.ModeLowPower) || mode == int(backend.ModeRFOff)
}

func flightModeSuccessMessage(enabled bool) string {
	if enabled {
		return "飞行模式已开启"
	}
	return "飞行模式已关闭"
}

func setWorkerFlightMode(ctx context.Context, worker *device.Worker, flightModeEnabled bool) (operatingMode int, flightMode bool, err error) {
	targetMode := backend.ModeOnline
	expectedMode := int(backend.ModeOnline)
	if flightModeEnabled {
		targetMode = backend.ModeRFOff
		expectedMode = int(backend.ModeRFOff)
	}

	if worker.Backend != nil {
		if err := worker.Backend.SetOperatingMode(ctx, targetMode); err != nil {
			return 0, false, err
		}
		opMode, err := worker.Backend.GetOperatingMode(ctx)
		if err != nil {
			return expectedMode, isFlightModeEnabled(expectedMode), nil
		}
		operatingMode = int(opMode)
		return operatingMode, isFlightModeEnabled(operatingMode), nil
	}

	return 0, false, fmt.Errorf("设备后端未初始化，无法切换飞行模式")
}

// handleDeviceMgmtReboot 执行模组重启 (发送 AT+CFUN=1,1)
func (s *Server) handleDeviceMgmtSetFlightMode(c *gin.Context) {
	id := deviceIDParam(c)
	if id == "" {
		fail(c, http.StatusBadRequest, "", "参数错误")
		return
	}

	if s.pool == nil {
		fail(c, http.StatusServiceUnavailable, "", "服务未就绪")
		return
	}

	var req setFlightModeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "", "参数错误")
		return
	}

	worker := s.pool.GetWorker(id)
	if worker == nil {
		fail(c, http.StatusNotFound, "", "设备未找到或未运行")
		return
	}
	if s.pool.IsESIMSwitching(id) {
		fail(c, http.StatusConflict, "", "设备正在切卡，请稍后再切换飞行模式")
		return
	}

	if s.pool.IsVoWiFiActive(id) {
		fail(c, http.StatusConflict, "", "VoWiFi 正在接管飞行模式，请先停用或退出 VoWiFi")
		return
	}

	flightModeEnabled := req.Enabled

	// 先落库卡策略（飞行模式跟卡走）：开飞行与 network/vowifi 互斥，关飞行仅清 airplane。
	// best-effort：落库失败不阻断热切（与 network/vowifi 热切路径一致）。
	s.patchCardPolicyForDevice(id, func(p *db.CardPolicy) {
		if flightModeEnabled {
			p.AirplaneEnabled = true
			p.VoWiFiEnabled = false
			p.NetworkEnabled = false
		} else {
			p.AirplaneEnabled = false
		}
	})
	// 同步 w.Config，使概览即时反映飞行/在线模式（setWorkerFlightMode 只切硬件不碰 Config）。
	s.pool.SetWorkerAirplanePolicy(id, flightModeEnabled)

	operatingMode, flightMode, err := setWorkerFlightMode(c.Request.Context(), worker, flightModeEnabled)
	if err != nil {
		fail(c, http.StatusInternalServerError, "", "切换飞行模式失败: "+err.Error())
		return
	}
	go func(disabled bool) {
		_ = worker.RefreshRuntime(nil, "flight_mode_change")
		if !disabled {
			// 切回在线后补一次延迟刷新，覆盖“先注册后PLMN”恢复窗口。
			time.Sleep(3 * time.Second)
			_ = worker.RefreshRuntime(nil, "flight_mode_recover")
		}
	}(!flightModeEnabled)

	respondOKWith(c, gin.H{
		"operating_mode": operatingMode,
		"flight_mode":    flightMode,
	}, gin.H{
		"message": flightModeSuccessMessage(flightModeEnabled),
	})
}

// shouldUseATFirstReboot 判断重启时是否应优先尝试 AT+CFUN=1,1。
// QMI 模式设备直接走 QMI ModeReset（backend.Reboot）；AT 优先路径仅保留给 AT 模式设备，
// 原先"QMI 模式也优先走 AT"是为了规避部分模组 QMI ModeReset 假死的历史问题，
// 现已实测确认本机型号 QMI ModeReset 正常工作，因此 QMI 模式不再绕道 AT。
func shouldUseATFirstReboot(backendMode string) bool {
	return backendMode != backend.BackendQMI
}

// handleDeviceMgmtReboot 执行模组重启 (QMI 模式走 QMI ModeReset，AT 模式走 AT+CFUN=1,1)
func (s *Server) handleDeviceMgmtReboot(c *gin.Context) {
	id := deviceIDParam(c)

	worker := s.pool.GetWorker(id)
	if worker == nil {
		fail(c, http.StatusNotFound, "", "设备未找到")
		return
	}

	if err := validateRebootWorkerIdentity(c.Request.Context(), worker); err != nil {
		fail(c, http.StatusConflict, "", err.Error())
		return
	}

	rebootSent := false

	useATFirst := worker.Backend == nil || shouldUseATFirstReboot(worker.Backend.Mode())

	// AT 模式设备优先尝试使用 AT 端口软重启；QMI 模式设备直接走 QMI ModeReset（见下方 fallback）
	if useATFirst && worker.Modem != nil && worker.Modem.HasATPort() && worker.Modem.CanExecuteAT() {
		_, err := worker.Modem.ExecuteAT("AT+CFUN=1,1", 20*time.Second)
		if err == nil {
			rebootSent = true
		} else {
			// 如果发送后立刻断开，可能会报错，视同成功发送
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "timeout") || strings.Contains(msg, "eof") || strings.Contains(msg, "closed") || strings.Contains(msg, "no such file") {
				rebootSent = true
			}
		}
	}

	// QMI 模式设备的主路径；AT 模式设备在 AT 端口不可用/发送失败时的降级路径
	if !rebootSent && worker.Backend != nil {
		if err := worker.Backend.Reboot(c.Request.Context()); err != nil {
			fail(c, http.StatusInternalServerError, "", "重启指令失败: "+err.Error())
			return
		}
		rebootSent = true
	}

	if !rebootSent {
		fail(c, http.StatusInternalServerError, "", "无法发送重启指令，无可用通道")
		return
	}

	s.pool.MarkLifecycleRecovery(id, device.LifecyclePhaseRebooting, "manual_reboot", 3*time.Minute)
	s.pool.ScheduleModemRebootRecovery(id, "manual_reboot")

	// 因为重启后设备会脱网并暂时下线，前端仅需知道命令已送达
	respondOKWith(c, nil, gin.H{"message": "重启指令已发送"})
}

func validateRebootWorkerIdentity(ctx context.Context, worker *device.Worker) error {
	if worker == nil || worker.Backend == nil {
		return nil
	}
	expectedIMEI := strings.TrimSpace(worker.Config.ModemIMEI)
	if expectedIMEI == "" {
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	currentIMEI, err := worker.Backend.GetIMEI(probeCtx)
	if err != nil || strings.TrimSpace(currentIMEI) == "" {
		return nil
	}
	currentIMEI = strings.TrimSpace(currentIMEI)
	if !config.IMEIMatches(currentIMEI, expectedIMEI) {
		return fmt.Errorf("设备路径已漂移：当前控制面 IMEI=%s，不匹配配置 IMEI=%s，请先重新扫描/重新绑定后再重启", currentIMEI, expectedIMEI)
	}
	return nil
}

// handleDeviceMgmtReconnectVoWiFi 执行重连 VoWiFi 的操作
func (s *Server) handleDeviceMgmtReconnectVoWiFi(c *gin.Context) {
	id := deviceIDParam(c)

	// 验证设备存在（硬件/传输配置仍在 config.yaml）
	md, err := config.GetDeviceByID(id)
	if err != nil || md == nil {
		fail(c, http.StatusNotFound, "", "设备配置不存在")
		return
	}

	// VoWiFi 开关已跟卡走、只存在于运行时投影。门禁读 worker 的有效策略；
	// 无 worker 时跳过友好门禁，交由 RestartVoWiFi 报告底层错误。
	if worker := s.pool.GetWorker(id); worker != nil && !worker.Config.VoWiFiEnabled {
		fail(c, http.StatusBadRequest, "", "设备未开启 VoWiFi，无法重连")
		return
	}

	if err := s.pool.RestartVoWiFi(id); err != nil {
		fail(c, http.StatusInternalServerError, "", "VoWiFi 重连失败: "+err.Error())
		return
	}

	respondOKWith(c, nil, gin.H{"message": "已触发 VoWiFi 重连"})
}

// handleDeviceMgmtRefreshInfo 主动触发设备底层重新采集各种信息（SIM、信号等）
func (s *Server) handleDeviceMgmtRefreshInfo(c *gin.Context) {
	id := deviceIDParam(c)
	worker := s.pool.GetWorker(id)
	if worker == nil {
		fail(c, http.StatusNotFound, "", "设备未找到或未运行")
		return
	}

	// 阻塞式刷新，后续执行的 overview-lite 就能马上获取到最新的状态
	if worker.Backend != nil && worker.Backend.Mode() != "at" {
		_ = worker.RefreshRuntime(c.Request.Context(), "manual_refresh")
		_ = worker.RefreshIdentityLive(c.Request.Context(), "manual_refresh")
		s.pool.PersistRuntimeState(worker)
		s.pool.PersistIdentityState(worker)
	} else if worker.Modem != nil {
		worker.Modem.RefreshDeviceInfo()
		_ = worker.RefreshRuntime(c.Request.Context(), "manual_refresh")
		_ = worker.RefreshIdentityLive(c.Request.Context(), "manual_refresh")
		s.pool.PersistRuntimeState(worker)
		s.pool.PersistIdentityState(worker)
	}

	respondOKWith(c, nil, gin.H{"message": "设备信息刷新完成"})
}
