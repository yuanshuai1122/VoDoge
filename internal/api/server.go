package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yuanshuai1122/vohive/internal/config"
	"github.com/yuanshuai1122/vohive/internal/data/repo"
	"github.com/yuanshuai1122/vohive/internal/db"
	"github.com/yuanshuai1122/vohive/internal/device"
	"github.com/yuanshuai1122/vohive/internal/global"
	"github.com/yuanshuai1122/vohive/internal/notify"
	"github.com/yuanshuai1122/vohive/internal/proxy/server"
	proxytraffic "github.com/yuanshuai1122/vohive/internal/proxy/traffic"
	vwebsheet "github.com/yuanshuai1122/vohive/internal/websheet"
	"github.com/yuanshuai1122/vohive/pkg/smscodec"
	"github.com/boa-z/vowifi-go/runtimehost/messaging"
	"github.com/boa-z/vowifi-go/runtimehost/voicehost"

	"github.com/yuanshuai1122/vohive/pkg/logger"
	"github.com/spf13/viper"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type SMSWithDevice struct {
	db.SMS
	DeviceName string `json:"device_name"`
}

type smsInboxIMSIReader interface {
	GetCachedIMSI() string
	GetIMSI() string
}

func smsInboxIMSI(source smsInboxIMSIReader, liveRefresh bool) string {
	if source == nil {
		return ""
	}
	imsi := strings.TrimSpace(source.GetCachedIMSI())
	if imsi != "" || !liveRefresh {
		return imsi
	}
	return strings.TrimSpace(source.GetIMSI())
}

type loginAttempt struct {
	Count   int
	ResetAt time.Time
}

// Server 是 API 服务器的核心结构
type Server struct {
	cfg         config.ServerConfig // HTTP 服务器配置
	fullCfg     *config.Config      // 完整配置引用
	pool        *device.Pool        // 设备工作器池
	auth        config.WebConfig    // Web 认证配置
	fs          http.FileSystem     // 静态文件系统
	configPath  string              // 配置文件路径
	proxyMgr    *server.Manager     // 代理实例管理器
	trafficRT   realtimeTrafficSubscriber
	proxyRepo   repo.ProxyInstanceRepository
	proxySyncMu sync.Mutex
	voiceGW     *voicehost.Gateway
	notifyMgr   *notify.Manager
	websheets   *vwebsheet.Broker

	httpSrvMu sync.Mutex
	httpSrv   *http.Server

	loginMu       sync.Mutex
	loginAttempts map[string]loginAttempt

	shutdownCh chan struct{}

	// eSIM 下载任务表：POST 建任务、GET 按 task_id 订阅进度，
	// 使激活码不必出现在 URL 中（见 esim_download.go）。
	esimDownloads *esimDownloadRegistry

	// 允许 ?token= 的路径集合，从路由表派生（见 routes.go）。
	// 每个请求都要查，故缓存；路由表在运行期不会变。
	sseTokenOnce  sync.Once
	sseTokenCache map[string]struct{}
}

type realtimeTrafficSubscriber interface {
	Subscribe(ctx context.Context, deviceID string) (<-chan proxytraffic.RealtimeSnapshot, func())
}

// New 创建一个新的 API 服务器实例
// proxyMgr 参数可为 nil，此时代理管理功能不可用
func New(cfg *config.Config, pool *device.Pool, fs http.FileSystem, proxyMgr *server.Manager, voiceGW *voicehost.Gateway, notifyMgr *notify.Manager, configPath string) *Server {
	if !cfg.Server.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	if strings.TrimSpace(configPath) == "" {
		configPath = "config/config.yaml"
	}
	s := &Server{
		cfg:           cfg.Server,
		fullCfg:       cfg,
		auth:          cfg.Web,
		pool:          pool,
		fs:            fs,
		configPath:    configPath,
		proxyMgr:      proxyMgr,
		voiceGW:       voiceGW,
		notifyMgr:     notifyMgr,
		proxyRepo:     repo.NewDBRepo(),
		websheets:     vwebsheet.New(vwebsheet.Config{BasePath: "/api/websheets"}),
		loginAttempts: make(map[string]loginAttempt),
		shutdownCh:    make(chan struct{}),
		esimDownloads: newEsimDownloadRegistry(),
	}

	return s
}

func (s *Server) SetRealtimeTraffic(m *proxytraffic.RealtimeManager) {
	s.trafficRT = m
}

// checkPassword 验证密码，支持 bcrypt 哈希和明文（向后兼容）
// stored 是存储的密码（可能是哈希或明文），input 是用户输入的明文密码
func checkPassword(stored, input string) bool {
	// 如果存储的密码以 $2a$ 或 $2b$ 开头，说明是 bcrypt 哈希
	if strings.HasPrefix(stored, "$2a$") || strings.HasPrefix(stored, "$2b$") {
		err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(input))
		return err == nil
	}
	// 向后兼容：明文密码对比
	return stored == input
}

func (s *Server) issueSessionToken() (string, time.Time, error) {
	exp := time.Now().Add(30 * 24 * time.Hour) // 有效期 30 天
	expStr := strconv.FormatInt(exp.Unix(), 10)

	h := hmac.New(sha256.New, []byte(s.auth.Password))
	h.Write([]byte(expStr))
	sig := hex.EncodeToString(h.Sum(nil))

	tokenRaw := expStr + "." + sig
	token := base64.StdEncoding.EncodeToString([]byte(tokenRaw))

	return token, exp, nil
}

// pruneExpiredSessionsLocked is removed.

func (s *Server) allowLoginAttempt(ip string, now time.Time) bool {
	if ip == "" {
		ip = "unknown"
	}
	window := 2 * time.Minute
	limit := 10

	s.loginMu.Lock()
	defer s.loginMu.Unlock()

	cur := s.loginAttempts[ip]
	if cur.ResetAt.IsZero() || now.After(cur.ResetAt) {
		cur = loginAttempt{Count: 0, ResetAt: now.Add(window)}
	}
	if cur.Count >= limit {
		s.loginAttempts[ip] = cur
		return false
	}
	cur.Count++
	s.loginAttempts[ip] = cur
	return true
}

// reTokenQuery 匹配 URL 查询串中的 token 参数值，用于访问日志脱敏。
var reTokenQuery = regexp.MustCompile(`([?&]token=)[^&]*`)

// accessLogFormatter 等价于 gin 默认访问日志格式，但会抹掉 ?token= 的值。
// SSE 端点允许用 query 传凭证（见 sseTokenQueryRoutes），若沿用 gin.Default()
// 的 Logger，token 会随 query 串写入 stdout，进而落入 docker logs / journal。
func accessLogFormatter(p gin.LogFormatterParams) string {
	return fmt.Sprintf("[GIN] %v | %3d | %13v | %15s | %-7s %#v\n",
		p.TimeStamp.Format("2006/01/02 - 15:04:05"),
		p.StatusCode,
		p.Latency,
		p.ClientIP,
		p.Method,
		reTokenQuery.ReplaceAllString(p.Path, "${1}***"),
	)
}

func (s *Server) Run() error {
	r := s.newRouter()

	srv := &http.Server{
		Addr:              s.cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       120 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	s.httpSrvMu.Lock()
	s.httpSrv = srv
	s.httpSrvMu.Unlock()
	logger.Info("启动 API 服务器", "port", s.cfg.Port)
	return srv.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	// 广播关闭信号给所有内部持有的长连接（如 SSE），让它们主动退出
	select {
	case <-s.shutdownCh:
	default:
		close(s.shutdownCh)
	}

	s.httpSrvMu.Lock()
	srv := s.httpSrv
	s.httpSrvMu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

func (s *Server) requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-Id"))
		if requestID == "" {
			b := make([]byte, 8)
			if _, err := rand.Read(b); err == nil {
				requestID = hex.EncodeToString(b)
			} else {
				requestID = fmt.Sprintf("%d", time.Now().UnixNano())
			}
		}
		c.Set("request_id", requestID)
		c.Header("X-Request-Id", requestID)
		c.Next()
	}
}

func requestID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if v, ok := c.Get("request_id"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (s *Server) handleListDevices(c *gin.Context) {
	workers := s.pool.GetAllWorkers()
	cfgByID := map[string]config.DeviceConfig{}
	{
		managed := config.ListDevices()
		for _, d := range managed {
			cfgByID[d.ID] = d
		}
	}

	type DeviceStatus struct {
		ID               string            `json:"id"`
		Name             string            `json:"name"`
		Interface        string            `json:"interface"`
		ProxyPort        int               `json:"proxy_port"`
		PublicIP         string            `json:"public_ip"`
		PublicIPv6       string            `json:"public_ipv6,omitempty"`
		Healthy          bool              `json:"healthy"`
		Operator         string            `json:"operator"`
		SignalDBM        int               `json:"signal_dbm"`
		NetworkMode      string            `json:"network_mode"`
		NetworkDuplex    string            `json:"network_duplex"`
		VoWiFiActive     bool              `json:"vowifi_active"`
		VoWiFiRuntime    *voWiFiRuntimeDTO `json:"vowifi_runtime,omitempty"`
		Traffic          map[string]string `json:"traffic,omitempty"`
		NetworkConnected bool              `json:"network_connected"`
	}

	list := make([]DeviceStatus, 0, len(workers))
	for _, w := range workers {
		status := w.GetCachedDeviceStatus() // 仓表盘列表读缓存，0 IPC
		cfg := w.Config
		if v, ok := cfgByID[w.ID]; ok {
			cfg = v
		}
		item := DeviceStatus{
			ID:               cfg.ID,
			Name:             cfg.Name,
			Interface:        cfg.Interface,
			ProxyPort:        cfg.ProxyPort,
			PublicIP:         w.GetCachedIP(),
			PublicIPv6:       w.GetCachedIPv6(),
			Healthy:          w.GetCachedHealthy(), // 健康状态读缓存
			Operator:         status.Operator,
			SignalDBM:        status.SignalDBM,
			NetworkMode:      status.NetworkMode,
			NetworkDuplex:    status.NetworkDuplex,
			VoWiFiActive:     s.pool.IsVoWiFiActive(w.ID), // 逐个设备判断 VoWiFi 状态，支持多设备
			VoWiFiRuntime:    s.getVoWiFiRuntimeDTO(w.ID),
			NetworkConnected: w.NetworkConnected(),
		}
		// 添加格式化流量
		if w.Proxy != nil {
			item.Traffic = w.Proxy.GetFormattedStats()
		}
		list = append(list, item)
	}
	c.JSON(http.StatusOK, list)
}

// handleDeviceRescan 手动触发设备重新扫描
func (s *Server) handleDeviceRescan(c *gin.Context) {
	if s.pool == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "服务未就绪"})
		return
	}

	if err := s.pool.RescanAndReconnect(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "重新扫描失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": "设备重新扫描完成",
	})
}

// handleLogStream SSE 实时日志流
func (s *Server) handleLogStream(c *gin.Context) {
	// 设置 SSE 响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	s.setSSECORSHeaders(c)

	// 订阅日志流
	logChan := logger.GlobalBroadcaster.Subscribe()
	defer logger.GlobalBroadcaster.Unsubscribe(logChan)

	// 获取日志级别过滤参数
	levelFilter := c.Query("level") // debug, info, warn, error

	// 客户端连接上下文
	clientGone := c.Request.Context().Done()

	// 发送初始连接成功事件
	c.SSEvent("connected", gin.H{"message": "已连接日志流"})
	c.Writer.Flush()

	for {
		select {
		case <-clientGone:
			return
		case <-s.shutdownCh:
			return
		case entry, ok := <-logChan:
			if !ok {
				return
			}

			// 日志级别过滤
			if levelFilter != "" {
				entryLevel := strings.ToLower(entry.Level)
				filterLevel := strings.ToLower(levelFilter)
				if !matchLogLevel(entryLevel, filterLevel) {
					continue
				}
			}

			// 发送日志条目
			c.SSEvent("log", entry)
			c.Writer.Flush()
		}
	}
}

func (s *Server) handleLogStreamOptions(c *gin.Context) {
	s.setSSECORSHeaders(c)
	c.Status(http.StatusNoContent)
}

func (s *Server) setSSECORSHeaders(c *gin.Context) {
	origin := strings.TrimSpace(c.GetHeader("Origin"))
	if origin == "" {
		return
	}

	if s.isAllowedSSEOrigin(origin, c.Request.Host) {
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		c.Header("Access-Control-Max-Age", "600")
		c.Header("Vary", "Origin")
	}
}

func (s *Server) isAllowedSSEOrigin(origin, host string) bool {
	if origin == "" {
		return false
	}

	host = strings.TrimSpace(host)
	if host != "" {
		if origin == "http://"+host || origin == "https://"+host {
			return true
		}
	}

	if !s.cfg.Debug {
		return false
	}

	if strings.HasPrefix(origin, "http://localhost:") || origin == "http://localhost" {
		return true
	}
	if strings.HasPrefix(origin, "http://127.0.0.1:") || origin == "http://127.0.0.1" {
		return true
	}
	if strings.HasPrefix(origin, "http://[::1]:") || origin == "http://[::1]" {
		return true
	}
	return false
}

// matchLogLevel 判断日志级别是否符合过滤条件
// filterLevel: debug 显示所有，info 显示 info/warn/error，warn 显示 warn/error，error 只显示 error
func matchLogLevel(entryLevel, filterLevel string) bool {
	levels := map[string]int{
		"debug": 0,
		"info":  1,
		"warn":  2,
		"error": 3,
		"fatal": 4,
	}
	entryLvl, entryOk := levels[entryLevel]
	filterLvl, filterOk := levels[filterLevel]
	if !entryOk || !filterOk {
		return true
	}
	return entryLvl >= filterLvl
}

// handleLogHistory 获取历史日志
func (s *Server) handleLogHistory(c *gin.Context) {
	// 读取参数
	lines := 500 // 默认返回最近 500 行
	if n := c.Query("lines"); n != "" {
		if v, err := strconv.Atoi(n); err == nil && v > 0 && v <= 2000 {
			lines = v
		}
	}

	// 日志文件路径（使用 logger 的默认路径）
	logFile := "logs/app.log"

	recentLines, err := readLastLines(logFile, lines, 1<<20)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"logs": []logger.LogEntry{}, "error": "无法读取日志文件"})
		return
	}

	// 解析日志行
	entries := make([]logger.LogEntry, 0, len(recentLines))
	for _, line := range recentLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entry := parseLogLine(line)
		if entry.Message != "" {
			entries = append(entries, entry)
		}
	}

	c.JSON(http.StatusOK, gin.H{"logs": entries})
}

func readLastLines(path string, maxLines int, maxBytes int64) ([]string, error) {
	if maxLines <= 0 {
		return []string{}, nil
	}
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	if size <= 0 {
		return []string{}, nil
	}

	const blockSize int64 = 32 * 1024
	var offset = size
	var readBytes int64
	var newlines int
	chunks := make([][]byte, 0, 8)

	for offset > 0 && readBytes < maxBytes && newlines <= maxLines {
		step := blockSize
		if offset < step {
			step = offset
		}
		offset -= step

		b := make([]byte, step)
		n, rerr := f.ReadAt(b, offset)
		if rerr != nil && n == 0 {
			return nil, rerr
		}
		b = b[:n]

		chunks = append(chunks, b)
		readBytes += int64(n)
		newlines += bytes.Count(b, []byte{'\n'})
	}

	var buf bytes.Buffer
	for i := len(chunks) - 1; i >= 0; i-- {
		buf.Write(chunks[i])
	}

	all := strings.Split(buf.String(), "\n")
	for len(all) > 0 && strings.TrimSpace(all[len(all)-1]) == "" {
		all = all[:len(all)-1]
	}
	if len(all) <= maxLines {
		return all, nil
	}
	return all[len(all)-maxLines:], nil
}

// parseLogLine 解析日志行
// 格式: [2026-02-06 15:04:05] LEVEL  caller  message {fields}
func parseLogLine(line string) logger.LogEntry {
	entry := logger.LogEntry{}

	// 提取时间 [2026-02-06 15:04:05]
	if len(line) < 22 || line[0] != '[' {
		entry.Message = line
		return entry
	}

	timeEnd := strings.Index(line, "]")
	if timeEnd < 0 {
		entry.Message = line
		return entry
	}

	timeStr := line[1:timeEnd]
	// 使用 ParseInLocation 确保日志时间继承系统本地时区，避免被默认解析为 UTC
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", timeStr, time.Local); err == nil {
		entry.Time = t.Format(time.RFC3339)
	} else {
		entry.Time = timeStr
	}

	rest := strings.TrimSpace(line[timeEnd+1:])

	// 使用 Fields 按任意空白字符分割
	fields := strings.Fields(rest)
	if len(fields) >= 1 {
		entry.Level = fields[0]
	}
	if len(fields) >= 2 {
		entry.Caller = fields[1]
	}
	if len(fields) >= 3 {
		// message 是剩余部分，需要从原字符串中找到正确位置
		// 找到 caller 在 rest 中的位置
		callerIdx := strings.Index(rest, entry.Caller)
		if callerIdx >= 0 {
			msgStart := callerIdx + len(entry.Caller)
			entry.Message = strings.TrimSpace(rest[msgStart:])
		} else {
			entry.Message = strings.Join(fields[2:], " ")
		}
	}

	return entry
}

func (s *Server) handleDeviceDetail(c *gin.Context) {
	deviceID := deviceIDParam(c)
	worker := s.pool.GetWorker(deviceID)
	if worker == nil {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "设备未找到"})
		return
	}

	c.JSON(http.StatusOK, worker.GetStats())
}

func (s *Server) handleDeviceTraffic(c *gin.Context) {
	deviceID := deviceIDParam(c)
	worker := s.pool.GetWorker(deviceID)
	if worker == nil {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "设备未找到"})
		return
	}
	iface := worker.Config.Interface
	tag := deviceID + "@" + iface
	ps, rx, tx, _ := db.GetLatestMinuteDeltas("iface", tag)
	var ifaceObj any = nil
	if iface != "" {
		ifaceObj = gin.H{
			"interface":    iface,
			"period_start": ps,
			"rx_bytes":     rx,
			"tx_bytes":     tx,
			"rx":           server.FormatBytes(rx),
			"tx":           server.FormatBytes(tx),
			"rate":         server.FormatBytes(int64(float64(rx+tx)/60.0)) + "/s",
		}
	}

	type instTraffic struct {
		ID          string    `json:"id"`
		Name        string    `json:"name"`
		Mode        string    `json:"mode"`
		PeriodStart time.Time `json:"period_start"`
		RxBytes     int64     `json:"rx_bytes"`
		TxBytes     int64     `json:"tx_bytes"`
		Rx          string    `json:"rx"`
		Tx          string    `json:"tx"`
		Rate        string    `json:"rate"`
	}
	ctx := c.Request.Context()
	instances, err := s.proxyRepo.List(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "加载代理实例失败: " + err.Error()})
		return
	}
	var insts []instTraffic
	for _, inst := range instances {
		if inst.DeviceID != deviceID {
			continue
		}
		mode := strings.ToLower(strings.TrimSpace(inst.Mode))
		if mode == "" {
			mode = "socks5"
		}
		ips, irx, itx, _ := db.GetLatestMinuteDeltas("proxy_instance", inst.ID)
		if irx == 0 && itx == 0 {
			continue
		}
		insts = append(insts, instTraffic{
			ID:          inst.ID,
			Name:        inst.Name,
			Mode:        mode,
			PeriodStart: ips,
			RxBytes:     irx,
			TxBytes:     itx,
			Rx:          server.FormatBytes(irx),
			Tx:          server.FormatBytes(itx),
			Rate:        server.FormatBytes(int64(float64(irx+itx)/60.0)) + "/s",
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"device_id":       deviceID,
		"iface":           ifaceObj,
		"proxy_instances": insts,
	})
}

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
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "存在多个设备时必须指定 device_id"})
			return
		}
	}

	worker := s.pool.GetWorker(deviceID)
	if worker == nil {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "设备未找到"})
		return
	}
	nc := worker.NetworkController()
	if nc == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": "当前设备不支持网络控制",
		})
		return
	}
	if !worker.NetworkConnected() {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": "设备网络未连接，请先启动网络",
		})
		return
	}

	// 执行切换 (同步操作，带重试和通知)
	startTime := time.Now()
	oldIP, newIP, err := worker.Rotate()
	duration := time.Since(startTime)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":   "error",
			"message":  err.Error(),
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
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "设备未找到"})
		return
	}
	nc := worker.NetworkController()
	if nc == nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "当前设备不支持网络控制"})
		return
	}
	if s.pool.IsVoWiFiActive(deviceID) {
		c.JSON(http.StatusConflict, gin.H{"status": "error", "message": "VoWiFi 运行中，无法启动数据网络"})
		return
	}
	if err := worker.StartNetwork(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "启动数据网络失败: " + err.Error()})
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
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "设备未找到"})
		return
	}
	nc := worker.NetworkController()
	if nc == nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "当前设备不支持网络控制"})
		return
	}
	if err := worker.StopNetwork(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "停止数据网络失败: " + err.Error()})
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

func (s *Server) handleHealth(c *gin.Context) {
	workers := s.pool.GetAllWorkers()
	allHealthy := true

	type DeviceHealth struct {
		Healthy          bool `json:"healthy"`
		ModemOK          bool `json:"modem_ok"`
		IfaceUp          bool `json:"iface_up"`
		NetworkConnected bool `json:"network_connected"`
		Signal           int  `json:"signal,omitempty"`
	}

	status := make(map[string]DeviceHealth)
	for _, w := range workers {
		modemOK := w.IsDeviceHealthy()
		ifaceUp := false
		healthy := modemOK

		if w.QMICore != nil {
			ifaceUp = w.QMICore.IsInterfaceUp()
		}

		// 获取信号 (非阻塞)
		signal := 0
		stats := w.GetStats()
		if s, ok := stats["signal"].(int); ok {
			signal = s
		}

		status[w.ID] = DeviceHealth{
			Healthy:          healthy,
			ModemOK:          modemOK,
			IfaceUp:          ifaceUp,
			NetworkConnected: ifaceUp,
			Signal:           signal,
		}
		if !healthy {
			allHealthy = false
		}
	}

	if allHealthy {
		c.JSON(http.StatusOK, gin.H{"status": "healthy", "devices": status})
	} else {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy", "devices": status})
	}
}

func (s *Server) handleStats(c *gin.Context) {
	workers := s.pool.GetAllWorkers()

	var totalSent, totalReceived, totalConns int64

	tagByID := map[string]string{}
	tags := make([]string, 0, len(workers))
	for _, w := range workers {
		if w == nil {
			continue
		}
		iface := w.Config.Interface
		if iface == "" {
			continue
		}
		tag := w.ID + "@" + iface
		tagByID[w.ID] = tag
		tags = append(tags, tag)
	}

	byTag, _ := db.GetLatestMinuteDeltasBatch("iface", tags)

	deviceStats := make(map[string]map[string]int64)
	for _, w := range workers {
		if w == nil {
			continue
		}
		tag := tagByID[w.ID]
		if tag == "" {
			continue
		}
		d := byTag[tag]
		stats := map[string]int64{
			"bytes_sent":     d.TxBytes,
			"bytes_received": d.RxBytes,
			"connections":    0,
		}
		deviceStats[w.ID] = stats
		totalSent += d.TxBytes
		totalReceived += d.RxBytes
	}

	c.JSON(http.StatusOK, gin.H{
		"total": gin.H{
			"bytes_sent":         totalSent,
			"bytes_received":     totalReceived,
			"connections":        totalConns,
			"sent_formatted":     server.FormatBytes(totalSent),
			"received_formatted": server.FormatBytes(totalReceived),
		},
		"devices": deviceStats,
	})
}

func (s *Server) handleSendSMS(c *gin.Context) {
	type SendSMSRequest struct {
		DeviceID string `json:"device_id"`
		IMSI     string `json:"imsi"`
		Phone    string `json:"phone" binding:"required"`
		Message  string `json:"message" binding:"required"`
		Encoding string `json:"encoding"`
	}

	var req SendSMSRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "参数错误: " + err.Error()})
		return
	}

	deviceID := strings.TrimSpace(req.DeviceID)
	imsi := strings.TrimSpace(req.IMSI)
	encoding, err := smscodec.NormalizeSMSEncoding(req.Encoding)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "短信编码参数错误: " + err.Error()})
		return
	}
	sendOpts := smscodec.SubmitOptions{Encoding: encoding}

	var worker *device.Worker
	if deviceID != "" {
		worker = s.pool.GetWorker(deviceID)
	} else if imsi != "" {
		for _, w := range s.pool.GetAllWorkers() {
			if w != nil && w.GetIMSI() == imsi {
				worker = w
				deviceID = w.ID
				break
			}
		}
	} else {
		workers := s.pool.GetAllWorkers()
		if len(workers) == 1 {
			worker = workers[0]
			if worker != nil {
				deviceID = worker.ID
			}
		}
	}

	if worker == nil {
		msg := "存在多个设备时必须指定 device_id 或 imsi"
		if deviceID != "" {
			msg = "设备未找到: " + deviceID
		} else if imsi != "" {
			msg = "未找到匹配 IMSI 的设备: " + imsi
		}
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": msg})
		return
	}

	// 获取 IMSI 用于入库
	imsi = worker.GetIMSI()
	messageID := ""
	partsTotal := 1
	deliveryState := "acked"

	if s.pool.IsVoWiFiActive(deviceID) {
		// VoWiFi 模式下使用 IMS Core 发送；短信历史由宿主侧 runtime event / failure recorder 入库。
		outcome, err := s.pool.SendVoWiFiSMSWithOptions(c.Request.Context(), deviceID, req.Phone, req.Message, sendOpts)
		if outcome.PartsTotal > 0 {
			partsTotal = outcome.PartsTotal
		}
		if strings.TrimSpace(outcome.DeliveryState) != "" {
			deliveryState = strings.TrimSpace(outcome.DeliveryState)
		}
		messageID = strings.TrimSpace(outcome.MessageID)
		if err != nil {
			_ = device.RecordVoWiFiSMSSendFailure(s.pool, deviceID, req.Phone, req.Message, time.Now())
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":         "error",
				"message":        "VoWiFi 短信发送失败: " + err.Error(),
				"device":         deviceID,
				"phone":          req.Phone,
				"message_id":     messageID,
				"parts_total":    partsTotal,
				"delivery_state": deliveryState,
			})
			return
		}
	} else {
		// 普通模式使用 AT 发送
		if err := worker.SendSMSWithOptions(req.Phone, req.Message, sendOpts); err != nil {
			// 发送失败，入库记录（status=3）
			if imsi != "" {
				_ = db.SaveSMS(imsi, worker.ID, req.Phone, req.Message, 2, 3, time.Now())
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":  "error",
				"message": "发送失败: " + err.Error(),
				"device":  deviceID,
				"phone":   req.Phone,
			})
			return
		}
		// 发送成功，入库记录（status=2）
		if imsi != "" {
			_ = db.SaveSMS(imsi, worker.ID, req.Phone, req.Message, 2, 2, time.Now())
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":         "ok",
		"message":        "短信发送成功",
		"device":         deviceID,
		"phone":          req.Phone,
		"message_id":     messageID,
		"parts_total":    partsTotal,
		"delivery_state": deliveryState,
	})
}

func (s *Server) handleSMSDelivery(c *gin.Context) {
	messageID := strings.TrimSpace(c.Param("message_id"))
	if messageID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "message_id 不能为空"})
		return
	}
	if s.pool == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "服务未就绪"})
		return
	}
	services := s.pool.GetAllVoWiFiApps()
	for _, svc := range services {
		if svc == nil {
			continue
		}
		status, err := svc.GetSMSDeliveryStatus(messageID)
		if err != nil {
			continue
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "delivery": status})
		return
	}
	c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "未找到对应短信投递记录"})
}

func (s *Server) handleVoWiFiSMSStatus(c *gin.Context) {
	if s.pool == nil {
		c.JSON(http.StatusOK, gin.H{"enabled": false, "status": "no_pool"})
		return
	}
	svc := s.pool.GetVoWiFiApp()
	if svc == nil {
		c.JSON(http.StatusOK, gin.H{"enabled": false, "status": "not_running"})
		return
	}
	c.JSON(http.StatusOK, svc.Status())
}

func (s *Server) handleVoWiFiSendSMS(c *gin.Context) {
	type SendSMSRequest struct {
		To       string `json:"to" binding:"required"`
		Text     string `json:"text" binding:"required"`
		Encoding string `json:"encoding"`
	}

	var req SendSMSRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "参数错误: " + err.Error()})
		return
	}

	if s.pool == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "服务未就绪"})
		return
	}
	encoding, err := smscodec.NormalizeSMSEncoding(req.Encoding)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "短信编码参数错误: " + err.Error()})
		return
	}
	svc := s.pool.GetVoWiFiApp()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "IMS Core 未启动"})
		return
	}

	outcome, err := svc.SendSMSWithOptions(c.Request.Context(), req.To, req.Text, messaging.SendOptions{Encoding: string(encoding)})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":         "error",
			"message":        "发送失败: " + err.Error(),
			"message_id":     strings.TrimSpace(outcome.MessageID),
			"parts_total":    outcome.PartsTotal,
			"delivery_state": strings.TrimSpace(outcome.DeliveryState),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":         "ok",
		"message":        "IMS 短信发送成功",
		"message_id":     strings.TrimSpace(outcome.MessageID),
		"parts_total":    outcome.PartsTotal,
		"delivery_state": strings.TrimSpace(outcome.DeliveryState),
	})
}

// handleVoWiFiEnable 为指定设备启用 VoWiFi
func (s *Server) handleVoWiFiEnable(c *gin.Context) {
	deviceID := deviceIDParam(c)
	if deviceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "请指定设备 ID"})
		return
	}

	if s.pool == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "服务未就绪"})
		return
	}

	if err := s.pool.EnableVoWiFi(deviceID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "VoWiFi 启用失败: " + err.Error(),
			"device":  deviceID,
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
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "服务未就绪"})
		return
	}

	if err := s.pool.DisableVoWiFi(deviceID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "VoWiFi 禁用失败: " + err.Error(),
			"device":  deviceID,
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
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "语音网关未启用"})
		return
	}

	var req voicehost.SimulateCallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数：" + err.Error()})
		return
	}

	result, err := s.voiceGW.SimulateCall(c.Request.Context(), deviceID, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   err.Error(),
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

// handleStatus 返回所有设备的状态概览
func (s *Server) handleStatus(c *gin.Context) {
	workers := s.pool.GetAllWorkers()

	type DeviceStatusSummary struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		IMEI       string `json:"imei"`
		ICCID      string `json:"iccid"`
		Operator   string `json:"operator"`
		SignalDBM  int    `json:"signal_dbm"`
		RegStatus  string `json:"reg_status"`
		PublicIP   string `json:"public_ip"`
		PublicIPv6 string `json:"public_ipv6,omitempty"`
		ProxyPort  int    `json:"proxy_port"`
		Healthy    bool   `json:"healthy"`
	}

	list := make([]DeviceStatusSummary, 0, len(workers))
	for _, w := range workers {
		status := w.GetCachedDeviceStatus() // 设备摘要列表读缓存，0 IPC
		list = append(list, DeviceStatusSummary{
			ID:         w.ID,
			Name:       w.Config.Name,
			IMEI:       status.IMEI,
			ICCID:      status.ICCID,
			Operator:   status.Operator,
			SignalDBM:  status.SignalDBM,
			RegStatus:  status.RegStatusText,
			PublicIP:   w.GetCachedIP(),
			PublicIPv6: w.GetCachedIPv6(),
			ProxyPort:  w.Config.ProxyPort,
			Healthy:    w.GetCachedHealthy(), // 健康状态读缓存
		})
	}

	c.JSON(http.StatusOK, gin.H{"devices": list})
}

// handleStatusDetail 返回单个设备的详细状态
func (s *Server) handleStatusDetail(c *gin.Context) {
	deviceID := deviceIDParam(c)
	worker := s.pool.GetWorker(deviceID)
	if worker == nil {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "设备未找到"})
		return
	}

	_ = worker.RefreshRuntime(c.Request.Context(), "status_detail")
	_ = worker.RefreshIdentityLive(c.Request.Context(), "status_detail")
	status := worker.ProjectDeviceStatus()

	response := gin.H{
		"id":                worker.ID,
		"name":              worker.Config.Name,
		"imei":              status.IMEI,
		"firmware":          status.Firmware,
		"iccid":             status.ICCID,
		"imsi":              status.IMSI,
		"native_spn":        status.NativeSPN,
		"native_mcc":        status.NativeMCC,
		"native_mnc":        status.NativeMNC,
		"gid1":              status.GID1,
		"gid2":              status.GID2,
		"pnn":               status.PNN,
		"opl":               status.OPL,
		"sim_service_table": status.SIMServiceTable,
		"operator":          status.Operator,
		"sim_inserted":      status.SimInserted,
		"signal_dbm":        status.SignalDBM,
		"signal_rsrp":       status.SignalRSRP,
		"signal_rsrq":       status.SignalRSRQ,
		"signal_sinr":       status.SignalSINR,
		"nr5g_signal_sinr":  status.NR5GSignalSINR,
		"radio_band":        status.RadioBand,
		"radio_channel":     status.RadioChannel,
		"reg_status":        status.RegStatus,
		"reg_status_text":   status.RegStatusText,
		"lac":               status.LAC,
		"cell_id":           status.CellID,
		"apn":               status.APN,
		"ims_status":        status.IMSStatus,
		"public_ip":         worker.GetCachedIP(),
		"public_ipv6":       worker.GetCachedIPv6(),
		"interface":         worker.Config.Interface,
		"proxy_port":        worker.Config.ProxyPort,
		"healthy":           worker.IsDeviceHealthy(),
		"network_connected": worker.NetworkConnected(),
	}

	if worker.Proxy != nil {
		response["traffic"] = worker.Proxy.GetFormattedStats()
	}

	vowifi := gin.H{
		"active": s.pool.IsVoWiFiActive(worker.ID),
	}
	if obs := s.pool.GetVoWiFiObs(worker.ID); obs != nil {
		for k, v := range obs {
			vowifi[k] = v
		}
	} else {
		if app := s.pool.GetVoWiFiAppForDevice(worker.ID); app != nil {
			status := app.Status()
			vowifi["imscore"] = status
			vowifi["smsip"] = status
		}
	}
	if s.voiceGW != nil {
		vowifi["voice"] = s.voiceGW.DeviceStatus(worker.ID)
	}
	response["vowifi"] = vowifi

	c.JSON(http.StatusOK, response)
}

func (s *Server) handleGetSMSInbox(c *gin.Context) {
	deviceID := c.Query("device_id")
	limitStr := c.DefaultQuery("limit", "20")
	var limit int
	fmt.Sscanf(limitStr, "%d", &limit)
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// 如果未指定设备 ID 且只有一个设备，默认使用该设备
	// 如果未指定设备 ID，则返回全局最近短信
	if deviceID == "" || deviceID == "all" {
		smsList, err := db.GetRecentSMS(limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "查询数据库失败: " + err.Error()})
			return
		}

		cfgByID := map[string]config.DeviceConfig{}
		{
			managed := config.ListDevices()
			for _, d := range managed {
				cfgByID[d.ID] = d
			}
		}

		iccidToName := map[string]string{}
		enrichedList := make([]SMSWithDevice, 0, len(smsList))
		for _, w := range s.pool.GetAllWorkers() {
			if w == nil || w.Modem == nil {
				continue
			}
			iccid := w.CurrentICCID()
			if strings.TrimSpace(iccid) == "" {
				continue
			}
			name := ""
			if v, ok := cfgByID[w.ID]; ok {
				name = v.Name
			} else {
				name = w.Config.Name
			}
			if name == "" {
				name = w.ID
			}
			iccidToName[iccid] = name
		}

		for _, sms := range smsList {
			devName := iccidToName[sms.ICCID]
			enrichedList = append(enrichedList, SMSWithDevice{
				SMS:        sms,
				DeviceName: devName,
			})
		}

		c.JSON(http.StatusOK, enrichedList)
		return
	}

	worker := s.pool.GetWorker(deviceID)
	if worker == nil {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "设备未找到: " + deviceID})
		return
	}

	iccid := worker.CurrentICCID()
	logger.Debug("查询指定设备短信", "device_id", deviceID, "iccid", iccid)
	if iccid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "该设备未识别到 SIM 卡 ICCID"})
		return
	}

	smsList, err := db.GetSMSByICCID(iccid, limit)
	if err != nil {
		logger.Error("查询数据库短信失败", "err", err, "iccid", iccid)
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "查询数据库失败: " + err.Error()})
		return
	}

	enrichedList := make([]SMSWithDevice, 0, len(smsList))
	devName := worker.Config.Name
	if devName == "" {
		devName = worker.ID
	}

	for _, sms := range smsList {
		enrichedList = append(enrichedList, SMSWithDevice{
			SMS:        sms,
			DeviceName: devName,
		})
	}

	c.JSON(http.StatusOK, enrichedList)
}

type SMSContactWithDevice struct {
	db.SMSContact
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	LocalPhone string `json:"local_phone"` // 本机号码（收件人手机号），来自订阅手机号
}

func (s *Server) resolveSMSIMSI(deviceID, imsi string) (string, int, string) {
	deviceID = strings.TrimSpace(deviceID)
	imsi = strings.TrimSpace(imsi)
	if deviceID == "" || deviceID == "all" {
		if imsi == "" {
			return "", http.StatusBadRequest, "缺少 imsi 参数（device_id=all 时必须指定）"
		}
		return imsi, 0, ""
	}

	worker := s.pool.GetWorker(deviceID)
	if worker == nil {
		return "", http.StatusNotFound, "设备未找到: " + deviceID
	}
	imsi = strings.TrimSpace(worker.GetCachedIMSI())
	if imsi == "" {
		return "", http.StatusBadRequest, "该设备未识别到 SIM 卡 IMSI"
	}
	return imsi, 0, ""
}

// resolveSMSICCID 将 device_id 或 imsi 查询参数解析为 ICCID，供 ICCID 维度的 SMS 查询使用。
// 对于 ?imsi= 路径，通过 sim_cards 映射转换为 ICCID（无映射时使用 "imsi:" 前缀合成键）。
func (s *Server) resolveSMSICCID(deviceID, imsi string) (string, int, string) {
	deviceID = strings.TrimSpace(deviceID)
	imsi = strings.TrimSpace(imsi)
	if deviceID == "" || deviceID == "all" {
		if imsi == "" {
			return "", http.StatusBadRequest, "缺少 imsi 参数（device_id=all 时必须指定）"
		}
		return db.GetICCIDForIMSI(imsi), 0, ""
	}

	worker := s.pool.GetWorker(deviceID)
	if worker == nil {
		return "", http.StatusNotFound, "设备未找到: " + deviceID
	}
	iccid := worker.CurrentICCID()
	if iccid == "" {
		return "", http.StatusBadRequest, "该设备未识别到 SIM 卡 ICCID"
	}
	return iccid, 0, ""
}

func (s *Server) handleGetSMSContacts(c *gin.Context) {
	deviceID := c.Query("device_id")
	imsi := c.Query("imsi")

	limitStr := c.DefaultQuery("limit", "50")
	var limit int
	fmt.Sscanf(limitStr, "%d", &limit)

	var beforeTs *time.Time
	if v := strings.TrimSpace(c.Query("before_ts")); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			beforeTs = &t
		}
	}
	beforePeer := strings.TrimSpace(c.Query("before_peer"))

	var iccid string
	if strings.TrimSpace(deviceID) != "" {
		resolved, status, msg := s.resolveSMSICCID(deviceID, imsi)
		if status != 0 {
			c.JSON(status, gin.H{"status": "error", "message": msg})
			return
		}
		iccid = resolved
	}

	var contacts []db.SMSContact
	var err error
	if iccid != "" {
		contacts, err = db.GetSMSContactsByICCID(iccid, limit, beforeTs, beforePeer)
	} else {
		contacts, err = db.GetSMSContacts(limit, beforeTs, beforePeer)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "查询数据库失败: " + err.Error()})
		return
	}

	iccidDevice := make(map[string]struct {
		id   string
		name string
	})
	cfgByID := map[string]config.DeviceConfig{}
	{
		managed := config.ListDevices()
		for _, d := range managed {
			cfgByID[d.ID] = d
		}
	}
	workers := s.pool.GetAllWorkers()
	for _, w := range workers {
		wICCID := w.CurrentICCID()
		if wICCID == "" {
			continue
		}
		name := ""
		if v, ok := cfgByID[w.ID]; ok {
			name = v.Name
		} else {
			name = w.Config.Name
		}
		if name == "" {
			name = w.ID
		}
		iccidDevice[wICCID] = struct {
			id   string
			name string
		}{id: w.ID, name: name}
	}

	// 手机号仍通过 IMSI 从 sim_subscriptions 查询（sim_subscriptions 主键尚为 IMSI）。
	imsiPhone := make(map[string]string)
	if phones, err := db.GetSIMPhoneNumbersByIMSI(); err == nil {
		imsiPhone = phones
	}

	enriched := make([]SMSContactWithDevice, 0, len(contacts))
	for _, ct := range contacts {
		info := iccidDevice[ct.ICCID]
		enriched = append(enriched, SMSContactWithDevice{
			SMSContact: ct,
			DeviceID:   info.id,
			DeviceName: info.name,
			LocalPhone: imsiPhone[ct.IMSI],
		})
	}

	c.JSON(http.StatusOK, enriched)
}

func (s *Server) handleGetSMSThread(c *gin.Context) {
	deviceID := c.Query("device_id")
	imsi := c.Query("imsi")
	peer := strings.TrimSpace(c.Query("peer"))
	if peer == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "缺少 peer 参数"})
		return
	}

	limitStr := c.DefaultQuery("limit", "50")
	var limit int
	fmt.Sscanf(limitStr, "%d", &limit)

	var beforeTs *time.Time
	if v := strings.TrimSpace(c.Query("before_ts")); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			beforeTs = &t
		}
	}
	var beforeID uint
	if v := strings.TrimSpace(c.Query("before_id")); v != "" {
		var parsed uint64
		if _, err := fmt.Sscanf(v, "%d", &parsed); err == nil {
			beforeID = uint(parsed)
		}
	}

	var iccid string
	if strings.TrimSpace(deviceID) != "" || strings.TrimSpace(imsi) != "" {
		resolved, status, msg := s.resolveSMSICCID(deviceID, imsi)
		if status != 0 {
			c.JSON(status, gin.H{"status": "error", "message": msg})
			return
		}
		iccid = resolved
	}

	list, err := db.GetSMSByICCIDAndPeer(iccid, peer, limit, beforeTs, beforeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "查询数据库失败: " + err.Error()})
		return
	}

	devName := ""
	cfgByID := map[string]config.DeviceConfig{}
	{
		managed := config.ListDevices()
		for _, d := range managed {
			cfgByID[d.ID] = d
		}
	}
	workers := s.pool.GetAllWorkers()
	for _, w := range workers {
		if w.CurrentICCID() == iccid {
			if v, ok := cfgByID[w.ID]; ok {
				devName = v.Name
			} else {
				devName = w.Config.Name
			}
			if devName == "" {
				devName = w.ID
			}
			break
		}
	}

	enriched := make([]SMSWithDevice, 0, len(list))
	for _, sms := range list {
		enriched = append(enriched, SMSWithDevice{
			SMS:        sms,
			DeviceName: devName,
		})
	}

	c.JSON(http.StatusOK, enriched)
}

func (s *Server) handleDeleteSMSMessage(c *gin.Context) {
	id64, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id64 == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "无效的短信 id"})
		return
	}

	threadEmpty, imsi, peer, err := db.DeleteSMSByID(uint(id64))
	if err != nil {
		if errors.Is(err, db.ErrSMSNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "短信不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "删除短信失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":       "ok",
		"thread_empty": threadEmpty,
		"imsi":         imsi,
		"peer":         peer,
	})
}

func (s *Server) handleDeleteSMSThread(c *gin.Context) {
	deviceID := c.Query("device_id")
	imsi := c.Query("imsi")
	peer := strings.TrimSpace(c.Query("peer"))
	if peer == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "缺少 peer 参数"})
		return
	}

	resolved, status, msg := s.resolveSMSICCID(deviceID, imsi)
	if status != 0 {
		c.JSON(status, gin.H{"status": "error", "message": msg})
		return
	}

	deleted, err := db.DeleteSMSByICCIDAndPeer(resolved, peer)
	if err != nil {
		if errors.Is(err, db.ErrSMSNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "短信会话不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "删除短信会话失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"deleted": deleted,
		"iccid":   resolved,
		"peer":    peer,
	})
}

// ---------------- 鉴权与静态服务 ----------------

func (s *Server) handleLogin(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "参数错误"})
		return
	}

	clientIP := c.ClientIP()
	if !s.allowLoginAttempt(clientIP, time.Now()) {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"status":     "error",
			"code":       "rate_limited",
			"message":    "登录尝试过于频繁，请稍后再试",
			"request_id": requestID(c),
		})
		return
	}

	if req.Username == s.auth.Username && checkPassword(s.auth.Password, req.Password) {
		token, exp, err := s.issueSessionToken()
		if err != nil {
			logger.Error("生成登录 token 失败", "err", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":     "error",
				"code":       "internal_error",
				"message":    "登录失败",
				"request_id": requestID(c),
			})
			return
		}
		logger.Info("登录成功", "ip", clientIP, "username", req.Username)

		c.JSON(http.StatusOK, gin.H{
			"status":     "ok",
			"token":      token,
			"expires_at": exp.Format(time.RFC3339),
		})
	} else {
		logger.Warn("登录失败", "ip", clientIP, "username", req.Username)
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":     "error",
			"code":       "invalid_credentials",
			"message":    "用户名或密码错误",
			"request_id": requestID(c),
		})
	}
}

// handleChangePassword 处理修改密码请求
func (s *Server) handleChangePassword(c *gin.Context) {
	var req struct {
		OldPassword     string `json:"old_password"`
		NewPassword     string `json:"new_password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "参数错误"})
		return
	}

	// 校验当前密码
	if !checkPassword(s.auth.Password, req.OldPassword) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":  "error",
			"code":    "invalid_password",
			"message": "当前密码错误",
		})
		return
	}

	// 校验新密码与确认密码一致
	if req.NewPassword != req.ConfirmPassword {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"code":    "password_mismatch",
			"message": "两次输入的新密码不一致",
		})
		return
	}

	// 校验新密码不能为空
	if strings.TrimSpace(req.NewPassword) == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"code":    "empty_password",
			"message": "新密码不能为空",
		})
		return
	}

	// 生成 bcrypt 哈希
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		logger.Error("生成密码哈希失败", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "密码处理失败",
		})
		return
	}
	hashedPassword := string(hashed)

	// 持久化到配置文件
	if err := config.UpdateWebCredentialsInFile(s.configPath, s.auth.Username, hashedPassword); err != nil {
		logger.Error("更新密码配置失败", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "保存配置失败: " + err.Error(),
		})
		return
	}

	// 更新内存中的密码（已哈希）
	s.auth.Password = hashedPassword

	logger.Info("密码已更新", "username", s.auth.Username, "ip", c.ClientIP())
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": "密码已更新",
	})
}

func (s *Server) authorizeRotate(c *gin.Context, username string, password string, now time.Time) bool {
	token := strings.TrimSpace(c.GetHeader("Authorization"))
	if token != "" {
		token = strings.TrimPrefix(token, "Bearer ")
		token = strings.TrimSpace(token)
		if token != "" && s.isSessionTokenValid(token, now) {
			return true
		}
	}

	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" && password == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":     "error",
			"code":       "unauthorized",
			"message":    "未授权",
			"request_id": requestID(c),
		})
		return false
	}
	if c.Request.Method != http.MethodPost {
		c.JSON(http.StatusMethodNotAllowed, gin.H{
			"status":     "error",
			"code":       "method_not_allowed",
			"message":    "仅支持 POST 表单/JSON 认证",
			"request_id": requestID(c),
		})
		return false
	}

	clientIP := c.ClientIP()
	if !s.allowLoginAttempt(clientIP, now) {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"status":     "error",
			"code":       "rate_limited",
			"message":    "请求过于频繁，请稍后再试",
			"request_id": requestID(c),
		})
		return false
	}

	if username == s.auth.Username && checkPassword(s.auth.Password, password) {
		return true
	}

	c.JSON(http.StatusUnauthorized, gin.H{
		"status":     "error",
		"code":       "invalid_credentials",
		"message":    "用户名或密码错误",
		"request_id": requestID(c),
	})
	return false
}

func (s *Server) isSessionTokenValid(token string, now time.Time) bool {
	decodedBytes, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return false
	}
	parts := strings.SplitN(string(decodedBytes), ".", 2)
	if len(parts) != 2 {
		return false
	}
	expStr, sig := parts[0], parts[1]

	expInt, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return false
	}
	if now.After(time.Unix(expInt, 0)) {
		return false
	}

	h := hmac.New(sha256.New, []byte(s.auth.Password))
	h.Write([]byte(expStr))
	expectedSig := hex.EncodeToString(h.Sum(nil))

	return hmac.Equal([]byte(sig), []byte(expectedSig))
}

func (s *Server) requestSessionToken(c *gin.Context) string {
	token := strings.TrimSpace(c.GetHeader("Authorization"))
	if token != "" {
		token = strings.TrimPrefix(token, "Bearer ")
		return strings.TrimSpace(token)
	}

	// 只有路由表里标了 sseToken 的流式端点才接受 query 凭证。
	// 白名单从路由表派生（见 routes.go），不再是一份需要同步维护的清单。
	if _, ok := s.sseTokenPaths()[c.FullPath()]; ok {
		return strings.TrimSpace(c.Query("token"))
	}
	return ""
}

// sseTokenPaths 惰性求值一次并缓存：每个请求都要查它，不能每次重建路由表。
func (s *Server) sseTokenPaths() map[string]struct{} {
	s.sseTokenOnce.Do(func() {
		s.sseTokenCache = s.sseTokenQueryPaths()
	})
	return s.sseTokenCache
}

func (s *Server) isAuthenticatedRequest(c *gin.Context, now time.Time) bool {
	token := s.requestSessionToken(c)
	return token != "" && s.isSessionTokenValid(token, now)
}

func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s.requestSessionToken(c) == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"status":     "error",
				"code":       "unauthorized",
				"message":    "未授权",
				"request_id": requestID(c),
			})
			c.Abort()
			return
		}

		if s.isAuthenticatedRequest(c, time.Now()) {
			c.Next()
			return
		}

		c.JSON(http.StatusUnauthorized, gin.H{
			"status":     "error",
			"code":       "unauthorized",
			"message":    "未授权",
			"request_id": requestID(c),
		})
		c.Abort()
	}
}

func (s *Server) handleStatic(c *gin.Context) {
	requestPath := c.Request.URL.Path

	// 如果是 API 请求但未匹配到路由，返回 404
	if strings.HasPrefix(requestPath, "/api") {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "API 不存在"})
		return
	}

	// 纯后端模式：未启用静态文件系统时，非 API 路径统一返回 404。
	if s.fs == nil {
		c.String(http.StatusNotFound, "Not Found")
		return
	}

	filePath := strings.TrimPrefix(requestPath, "/")
	if filePath == "" {
		filePath = "index.html"
	}

	// 打开候选文件；若命中目录则视为未找到，交给下面的 .html 兜底。
	//
	// Next 静态导出会为每个路由同时生成 `<route>.html` 与一个同名目录
	// （目录里只放框架内部文件）。直接按请求路径打开 `/login` 会命中那个目录，
	// 若此时就回退到 index.html，浏览器拿到的是根路由的 HTML，
	// 与当前 URL 不匹配，页面会白屏。因此必须先试 `<route>.html`。
	open := func(name string) (http.File, bool) {
		file, oerr := s.fs.Open(name)
		if oerr != nil {
			return nil, false
		}
		if st, serr := file.Stat(); serr != nil || st.IsDir() {
			file.Close()
			return nil, false
		}
		return file, true
	}

	f, ok := open(filePath)
	if !ok {
		if candidate := filePath + ".html"; !strings.HasSuffix(filePath, ".html") {
			if file, found := open(candidate); found {
				f, ok, filePath = file, true, candidate
			}
		}
	}
	if !ok {
		// 仍未命中：按 SPA 回退到 index.html，交由客户端路由处理
		filePath = "index.html"
		file, found := open(filePath)
		if !found {
			c.String(http.StatusNotFound, "Not Found")
			return
		}
		f = file
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	// 设置缓存头
	if filePath == "index.html" {
		c.Header("Cache-Control", "no-cache")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
	} else if strings.HasPrefix(filePath, "_next/static/") || strings.HasPrefix(filePath, "assets/") {
		// _next/static: Next 静态导出的哈希资源；assets: 旧 Vite 构建产物
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		c.Header("Cache-Control", "public, max-age=3600")
	}

	// 设置 Content-Type
	contentType := "application/octet-stream"
	if strings.HasSuffix(filePath, ".html") {
		contentType = "text/html; charset=utf-8"
	} else if strings.HasSuffix(filePath, ".css") {
		contentType = "text/css; charset=utf-8"
	} else if strings.HasSuffix(filePath, ".js") {
		contentType = "application/javascript; charset=utf-8"
	} else if strings.HasSuffix(filePath, ".json") {
		contentType = "application/json; charset=utf-8"
	} else if strings.HasSuffix(filePath, ".svg") {
		contentType = "image/svg+xml"
	} else if strings.HasSuffix(filePath, ".png") {
		contentType = "image/png"
	} else if strings.HasSuffix(filePath, ".ico") {
		contentType = "image/x-icon"
	} else if strings.HasSuffix(filePath, ".woff") {
		contentType = "font/woff"
	} else if strings.HasSuffix(filePath, ".woff2") {
		contentType = "font/woff2"
	}
	c.Header("Content-Type", contentType)

	// 使用 http.ServeContent 直接响应文件内容
	http.ServeContent(c.Writer, c.Request, filePath, stat.ModTime(), f.(io.ReadSeeker))
}

func (s *Server) handleSystemInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version":    global.Version,
		"build_time": global.BuildTime,
		"config":     viper.ConfigFileUsed(),
		"docs":       currentAPIDocsLinks(),
	})
}
