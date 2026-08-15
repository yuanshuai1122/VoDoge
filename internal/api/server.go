// Server 的结构、构造与生命周期。
//
// 路由表在 routes.go，各域的 handler 按域分文件（auth/sms/logs/device_*/...）。
// 这里只保留所有域共用的东西：依赖注入、HTTP 服务器的启停、请求 ID。
package api

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/boa-z/vowifi-go/runtimehost/voicehost"
	"github.com/yuanshuai1122/vodoge/internal/config"
	"github.com/yuanshuai1122/vodoge/internal/data/repo"
	"github.com/yuanshuai1122/vodoge/internal/device"
	"github.com/yuanshuai1122/vodoge/internal/extensions"
	"github.com/yuanshuai1122/vodoge/internal/httpsmode"
	"github.com/yuanshuai1122/vodoge/internal/netaccess"
	"github.com/yuanshuai1122/vodoge/internal/notify"
	"github.com/yuanshuai1122/vodoge/internal/pcsc"
	"github.com/yuanshuai1122/vodoge/internal/proxy/server"
	proxytraffic "github.com/yuanshuai1122/vodoge/internal/proxy/traffic"
	vwebsheet "github.com/yuanshuai1122/vodoge/internal/websheet"

	"github.com/yuanshuai1122/vodoge/pkg/logger"

	"github.com/gin-gonic/gin"
)

// Server 是 API 服务器的核心结构
type Server struct {
	cfg        config.ServerConfig // HTTP 服务器配置
	fullCfg    *config.Config      // 完整配置引用
	pool       *device.Pool        // 设备工作器池
	auth       config.WebConfig    // Web 认证配置
	fs         http.FileSystem     // 静态文件系统
	configPath string              // 配置文件路径
	proxyMgr   *server.Manager     // 代理实例管理器
	trafficRT  realtimeTrafficSubscriber
	// store 是本层唯一的持久化入口。handler 不再直接调 internal/db，
	// 因此可以注入假实现来测试，不必连真库（见 internal/data/repo）。
	store *repo.Store
	// hardware 是硬件枚举/探测的边界（见 device_discovery.go）。
	// nil 时用真实实现；测试注入假实现，无需 /dev 下真有 QMI 设备。
	hardware hardwareProbe
	// pcsc 是读卡器发现。nil 时用系统 pcscd 套接字探测。
	pcsc pcsc.Backend
	// esimNotificationsFor 决定某设备的 eSIM 通知从哪儿来（见 device_esim.go）。
	// nil 时用设备自己的 *esim.Manager，它要走 APDU 摸真卡。
	esimNotificationsFor func(*device.Worker) esimNotificationSource
	proxyRepo            repo.ProxyInstanceRepository
	proxySyncMu          sync.Mutex
	voiceGW              *voicehost.Gateway
	notifyMgr            *notify.Manager
	websheets            *vwebsheet.Broker

	https *httpsmode.Manager

	extensions *extensions.Manager

	accessMu sync.RWMutex
	access   netaccess.Parsed

	httpSrvMu sync.Mutex
	httpSrv   *http.Server
	httpsSrv  *http.Server
	httpsMux  *httpsmode.Multiplexer

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
	store := repo.NewStore()
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
		store:         store,
		proxyRepo:     store.ProxyInstance,
		websheets:     vwebsheet.New(vwebsheet.Config{BasePath: "/api/websheets"}),
		loginAttempts: make(map[string]loginAttempt),
		shutdownCh:    make(chan struct{}),
		esimDownloads: newEsimDownloadRegistry(),
	}
	httpsMgr, err := httpsmode.New(filepath.Join("data", "tls"), cfg.Server.Port, cfg.Server.SelfSignedHTTPS)
	if err != nil {
		logger.Error("初始化本机自签 HTTPS 失败", "err", err)
	} else {
		s.https = httpsMgr
	}
	s.loadAccessPolicyFromConfig()

	extMgr, err := extensions.Open(filepath.Join("data", "plugins"))
	if err != nil {
		logger.Error("初始化插件目录失败", "err", err)
	} else {
		s.extensions = extMgr
	}

	return s
}

// defaultStore 是未显式注入 store 时的回退。
//
// 直接构造 &Server{...} 的测试有几十处，逐个补 store 字段只会制造噪音；
// 回退到真实实现后它们的行为与从前完全一致（本来就在调 internal/db）。
// 需要脱库的测试显式注入假实现即可。
var defaultStore = repo.NewStore()

// data 返回本次请求应使用的持久化入口。
func (s *Server) data() *repo.Store {
	if s.store != nil {
		return s.store
	}
	return defaultStore
}

func (s *Server) SetRealtimeTraffic(m *proxytraffic.RealtimeManager) {
	s.trafficRT = m
}

// pruneExpiredSessionsLocked is removed.

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

func newAPIHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       120 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

func listenAddr(port string) string {
	port = strings.TrimSpace(port)
	if port == "" {
		return ":7575"
	}
	if !strings.Contains(port, ":") {
		return ":" + port
	}
	return port
}

func (s *Server) Run() error {
	r := s.newRouter()
	addr := listenAddr(s.cfg.Port)
	handler := s.wrapHTTPSRedirect(r)

	if s.https == nil {
		srv := newAPIHTTPServer(addr, handler)
		s.httpSrvMu.Lock()
		s.httpSrv = srv
		s.httpSrvMu.Unlock()
		logger.Info("启动 API 服务器", "port", addr, "self_signed_https", false)
		return srv.ListenAndServe()
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	mux := httpsmode.NewMultiplexer(ln, s.https)
	httpSrv := newAPIHTTPServer(addr, handler)
	httpsSrv := newAPIHTTPServer(addr, r)
	httpsSrv.TLSConfig = s.https.TLSConfig()

	s.httpSrvMu.Lock()
	s.httpSrv = httpSrv
	s.httpsSrv = httpsSrv
	s.httpsMux = mux
	s.httpSrvMu.Unlock()

	logger.Info("启动 API 服务器",
		"port", addr,
		"self_signed_https", s.https.Enabled())

	errCh := make(chan error, 2)
	go func() { errCh <- httpSrv.Serve(mux.Plain()) }()
	go func() { errCh <- httpsSrv.Serve(tls.NewListener(mux.TLS(), s.https.TLSConfig())) }()
	err = <-errCh
	_ = mux.Close()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) wrapHTTPSRedirect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s != nil && s.https != nil && s.https.Enabled() && r.TLS == nil && !httpsRedirectExempt(r.URL.Path) {
			target := "https://" + r.Host + r.URL.RequestURI()
			http.Redirect(w, r, target, http.StatusPermanentRedirect)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func httpsRedirectExempt(path string) bool {
	switch strings.TrimSpace(path) {
	case "/api/settings/https", "/api/settings/https/certificate":
		return true
	default:
		return false
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	// 广播关闭信号给所有内部持有的长连接（如 SSE），让它们主动退出
	select {
	case <-s.shutdownCh:
	default:
		close(s.shutdownCh)
	}

	s.httpSrvMu.Lock()
	httpSrv := s.httpSrv
	httpsSrv := s.httpsSrv
	mux := s.httpsMux
	s.httpSrvMu.Unlock()

	var errs []error
	if httpSrv != nil {
		errs = append(errs, httpSrv.Shutdown(ctx))
	}
	if httpsSrv != nil {
		errs = append(errs, httpsSrv.Shutdown(ctx))
	}
	if mux != nil {
		errs = append(errs, mux.Close())
	}
	if s.extensions != nil {
		s.extensions.Close()
	}
	return errors.Join(errs...)
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

// ---------------- 鉴权与静态服务 ----------------
