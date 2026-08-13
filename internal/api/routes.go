package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// 路由以**一张表**的形式声明，而不是散在 newRouter 里的一串注册语句。
//
// 起因是这些信息本来分在三处：注册语句、SSE 的 ?token= 白名单、OpenAPI spec。
// 改一处忘了另一处不会有任何编译期或测试期信号。eSIM 下载路径从
// `.../download` 挪到 `.../download/stream` 时就漏了白名单，结果是 SSE 一律 401，
// 而编译、vet、单测全绿。
//
// 现在白名单由本表派生（sseTokenQueryRoutes），check-routes.mjs 也读本表，
// 那类分叉在结构上就不成立了。

type authMode int

const (
	// authRequired 走 authMiddleware，无有效令牌一律 401。
	authRequired authMode = iota
	// authNone 真正公开，无任何凭证要求。
	authNone
	// authInHandler 中间件放行，由 handler 自己校验。
	//
	// 单独一档是因为"不挂中间件"这件事有两种完全不同的含义：真的公开，
	// 和用另一套凭证。混在一起看，`/rotateip` 会被误读成无鉴权——
	// 它其实在 handler 内经 authorizeRotate 校验（Bearer 或用户名口令双模式）。
	authInHandler
)

type route struct {
	method  string
	path    string // /api 之下的路径，不含 /api 前缀
	handler gin.HandlerFunc
	auth    authMode
	// sseToken 允许用 ?token= 传凭证。
	//
	// 浏览器原生 EventSource 无法设置请求头，流式端点若只认 Authorization
	// 就没法在前端用。只对流式端点开放：token 进 URL 会落入访问日志、
	// Referer 与浏览器历史。
	sseToken bool
	// desc 出现在路由表里，方便按域浏览；不参与任何逻辑。
	desc string
}

func (s *Server) routes() []route {
	return concatRoutes(
		s.metaRoutes(),
		s.dashboardRoutes(),
		s.smsRoutes(),
		s.settingsRoutes(),
		s.deviceRoutes(),
		s.cardPolicyRoutes(),
		s.operatorSelectionRoutes(),
		s.proxyRoutes(),
		s.upstreamProxyRoutes(),
		s.esimRoutes(),
		s.vowifiRoutes(),
		s.websheetRoutes(),
		s.logRoutes(),
	)
}

// metaRoutes 是文档、登录、以及自带凭证体系的那几条。
func (s *Server) metaRoutes() []route {
	return []route{
		{"GET", "/docs", s.handleAPIDocs, authNone, false, "Swagger UI 页面"},
		{"GET", "/docs/assets/*filepath", s.handleDocsAsset, authNone, false, "Swagger UI 静态资源"},
		// spec 必须与承载它的 Swagger UI 页面同级：/docs 免鉴权而 spec 需鉴权时，
		// 页面能打开却永远拉不到定义，只会一直空白。
		// 这里公开的只是接口形状，spec 中描述的端点本身依然需要 Bearer token。
		{"GET", "/openapi.yaml", s.handleOpenAPIYAML, authNone, false, "OpenAPI 定义(YAML)"},
		{"GET", "/openapi.json", s.handleOpenAPIJSON, authNone, false, "OpenAPI 定义(JSON)"},
		{"POST", "/auth/login", s.handleLogin, authNone, false, "登录"},
		{"OPTIONS", "/logs/stream", s.handleLogStreamOptions, authNone, false, "日志流 CORS 预检"},
		// handler 内 authorizeRotate 双模式校验（Bearer 或用户名口令），供外部脚本调用
		{"POST", "/rotateip", s.handleRotate, authInHandler, false, "换 IP"},
	}
}

func (s *Server) dashboardRoutes() []route {
	return []route{
		{"GET", "/dashboard/devices", s.handleListDevices, authRequired, false, "设备概览（仪表盘卡片）"},
		{"GET", "/devices/:device_id/status", s.handleStatusDetail, authRequired, false, "单设备详细状态"},
		// 返回逐设备的健康明细（设备 ID、信号、联网状态），因此需要鉴权。
		// 外部监控请用免鉴权的 GET /ping，那个只回 pong、不含任何设备信息。
		{"GET", "/health", s.handleHealth, authRequired, false, "健康明细"},
		{"GET", "/traffic/analysis", s.handleTrafficAnalysis, authRequired, false, "流量分析统计"},
	}
}

func (s *Server) smsRoutes() []route {
	return []route{
		{"POST", "/sms/send", s.handleSendSMS, authRequired, false, "发送短信（自动选 AT 或 VoWiFi）"},
		{"GET", "/sms/delivery/:message_id", s.handleSMSDelivery, authRequired, false, "查询投递状态"},
		{"GET", "/sms/contacts", s.handleGetSMSContacts, authRequired, false, "联系人列表"},
		{"GET", "/sms/thread", s.handleGetSMSThread, authRequired, false, "会话消息"},
		{"DELETE", "/sms/messages/:id", s.handleDeleteSMSMessage, authRequired, false, "删除单条短信"},
		{"DELETE", "/sms/thread", s.handleDeleteSMSThread, authRequired, false, "删除会话"},
	}
}

func (s *Server) settingsRoutes() []route {
	return []route{
		{"GET", "/settings/notifications", s.handleGetNotificationSettings, authRequired, false, "获取通知设置"},
		{"PUT", "/settings/notifications", s.handleUpdateNotificationSettings, authRequired, false, "更新通知设置"},
		{"POST", "/settings/notifications/webhook/test", s.handleTestWebhookNotification, authRequired, false, "测试 Webhook"},
		{"POST", "/settings/notifications/bark/test", s.handleTestBarkNotification, authRequired, false, "测试 Bark"},
		{"POST", "/settings/notifications/email/test", s.handleTestEmailNotification, authRequired, false, "测试邮件"},
		{"POST", "/settings/password", s.handleChangePassword, authRequired, false, "修改登录密码"},
		{"GET", "/system/info", s.handleSystemInfo, authRequired, false, "系统运行与版本信息"},
		{"GET", "/system/update/check", s.handleCheckUpdate, authRequired, false, "检查更新"},
		{"POST", "/system/update/apply", s.handleApplyUpdate, authRequired, false, "应用更新"},
		// 破坏性且不可撤销，必须鉴权（曾经完全无校验，见 api-matrix §8-1）
		{"POST", "/system/uninstall", s.handleUninstall, authRequired, false, "卸载/自毁"},
	}
}

func (s *Server) deviceRoutes() []route {
	return []route{
		{"GET", "/devices", s.handleDeviceMgmtList, authRequired, false, "设备列表（管理页）"},
		{"POST", "/devices", s.handleDeviceMgmtAddDevice, authRequired, false, "添加设备"},
		{"GET", "/devices/discovered", s.handleDeviceMgmtDiscovered, authRequired, false, "已发现的硬件设备"},
		{"POST", "/devices/actions/rescan", s.handleDeviceRescan, authRequired, false, "手动重扫描"},
		{"GET", "/devices/:device_id/overview/stream", s.handleDeviceMgmtOverviewStreamSingle, authRequired, true, "单设备深层实时流(SSE)"},
		{"GET", "/devices/:device_id/overview", s.handleDeviceMgmtOverviewLite, authRequired, false, "设备详情（轻量）"},
		{"GET", "/devices/:device_id/config", s.handleDeviceMgmtGetDeviceConfig, authRequired, false, "获取设备配置"},
		{"PUT", "/devices/:device_id", s.handleDeviceMgmtUpdateDevice, authRequired, false, "更新设备配置"},
		{"DELETE", "/devices/:device_id", s.handleDeviceMgmtDeleteDevice, authRequired, false, "删除设备"},
		{"POST", "/devices/:device_id/actions/refresh", s.handleDeviceMgmtRefreshInfo, authRequired, false, "刷新设备缓存信息"},
		{"POST", "/devices/:device_id/actions/reboot", s.handleDeviceMgmtReboot, authRequired, false, "重启模组"},
		{"POST", "/devices/:device_id/actions/at", s.handleDeviceMgmtExecuteAT, authRequired, false, "执行 AT 命令"},
		{"POST", "/devices/:device_id/actions/ussd", s.handleDeviceMgmtExecuteUSSD, authRequired, false, "执行 USSD"},
		{"POST", "/devices/:device_id/actions/ussd/continue", s.handleDeviceMgmtContinueUSSD, authRequired, false, "USSD 续轮输入"},
		{"POST", "/devices/:device_id/actions/ussd/cancel", s.handleDeviceMgmtCancelUSSD, authRequired, false, "取消 USSD 会话"},
		{"PATCH", "/devices/:device_id/usbnet-mode", s.handleDeviceMgmtSetUSBNetMode, authRequired, false, "设置 USBNET 模式"},
		{"PATCH", "/devices/:device_id/flight-mode", s.handleDeviceMgmtSetFlightMode, authRequired, false, "切换飞行模式"},
		{"PATCH", "/devices/:device_id/network", s.handleDeviceNetworkPatch, authRequired, false, "启停数据网络"},
	}
}

func (s *Server) cardPolicyRoutes() []route {
	return []route{
		{"GET", "/cards/policies", s.handleListCardPolicies, authRequired, false, "卡策略列表"},
		{"GET", "/cards/:iccid/policy", s.handleGetCardPolicy, authRequired, false, "获取卡策略"},
		{"PUT", "/cards/:iccid/policy", s.handlePutCardPolicy, authRequired, false, "保存卡策略"},
	}
}

func (s *Server) operatorSelectionRoutes() []route {
	return []route{
		{"GET", "/devices/:device_id/operator_selection/scan", s.handleDeviceMgmtOperatorScan, authRequired, false, "扫描运营商"},
		{"GET", "/devices/:device_id/operator_selection/scan/stream", s.handleDeviceMgmtOperatorScanStream, authRequired, true, "扫描运营商(SSE)"},
		{"GET", "/devices/:device_id/operator_selection", s.handleDeviceMgmtGetOperatorSelection, authRequired, false, "获取选网配置"},
		{"POST", "/devices/:device_id/operator_selection", s.handleDeviceMgmtSetOperatorSelection, authRequired, false, "锁定运营商或恢复自动"},
	}
}

func (s *Server) proxyRoutes() []route {
	return []route{
		{"GET", "/proxy-instances/overview", s.handleProxyOverview, authRequired, false, "代理实例概览"},
		{"PUT", "/proxy-instances/config", s.handleProxyUpdateConfig, authRequired, false, "保存代理配置"},
		{"GET", "/proxy-instances/:instance_id", s.handleProxyInstanceGet, authRequired, false, "获取单个代理实例"},
		{"POST", "/proxy-instances/:instance_id/actions/start", s.handleProxyInstanceStart, authRequired, false, "启动代理实例"},
		{"POST", "/proxy-instances/:instance_id/actions/stop", s.handleProxyInstanceStop, authRequired, false, "停止代理实例"},
		{"POST", "/proxy-instances/:instance_id/actions/restart", s.handleProxyInstanceRestart, authRequired, false, "重启代理实例"},
	}
}

func (s *Server) upstreamProxyRoutes() []route {
	return []route{
		{"GET", "/upstream-proxies", s.handleListUpstreamProxies, authRequired, false, "列出前置代理"},
		{"POST", "/upstream-proxies", s.handleCreateUpstreamProxy, authRequired, false, "新增前置代理"},
		{"PUT", "/upstream-proxies/:proxy_id", s.handleUpdateUpstreamProxy, authRequired, false, "更新前置代理"},
		{"DELETE", "/upstream-proxies/:proxy_id", s.handleDeleteUpstreamProxy, authRequired, false, "删除前置代理"},
		{"POST", "/upstream-proxies/:proxy_id/actions/probe", s.handleProbeUpstreamProxy, authRequired, false, "探测前置代理"},
		{"GET", "/upstream-proxy-countries", s.handleListUpstreamProxyCountries, authRequired, false, "可配置国家列表"},
		{"GET", "/upstream-proxy-country-rules", s.handleListUpstreamProxyCountryRules, authRequired, false, "国家规则列表"},
		{"PUT", "/upstream-proxy-country-rules/:country_code", s.handleUpsertUpstreamProxyCountryRule, authRequired, false, "保存国家规则"},
		{"DELETE", "/upstream-proxy-country-rules/:country_code", s.handleDeleteUpstreamProxyCountryRule, authRequired, false, "删除国家规则"},
	}
}

func (s *Server) esimRoutes() []route {
	return []route{
		{"GET", "/devices/:device_id/esim", s.handleEsimGetOverview, authRequired, false, "eSIM 总览"},
		{"GET", "/devices/:device_id/esim/profiles", s.handleEsimListProfiles, authRequired, false, "Profile 列表"},
		{"GET", "/devices/:device_id/esim/notifications", s.handleEsimListNotifications, authRequired, false, "通知列表"},
		{"POST", "/devices/:device_id/esim/notifications/:sequence/actions/retry", s.handleEsimRetryNotification, authRequired, false, "重试通知"},
		{"POST", "/devices/:device_id/esim/actions/switch", s.handleEsimSwitchProfile, authRequired, false, "切换 Profile"},
		{"GET", "/devices/:device_id/esim/eids", s.handleEsimGetEID, authRequired, false, "获取 EID"},
		{"GET", "/devices/:device_id/esim/chip-info", s.handleEsimGetChipInfo, authRequired, false, "eUICC 芯片信息"},
		// 下载分两步：POST 建任务（激活码走 body），GET 按 task_id 订阅 SSE 进度。
		// 旧的「单个 GET + query 传激活码」已移除，那样会把一次性激活码留在
		// 浏览器历史与访问日志里。
		{"POST", "/devices/:device_id/esim/actions/download", s.handleEsimDownloadStart, authRequired, false, "发起 Profile 下载"},
		{"GET", "/devices/:device_id/esim/actions/download/stream", s.handleEsimDownloadStream, authRequired, true, "订阅下载进度(SSE)"},
		{"PATCH", "/devices/:device_id/esim/profiles/:iccid", s.handleEsimRenameProfile, authRequired, false, "重命名 Profile"},
		{"DELETE", "/devices/:device_id/esim/profiles/:iccid", s.handleEsimDeleteProfile, authRequired, false, "删除 Profile"},
	}
}

func (s *Server) vowifiRoutes() []route {
	return []route{
		{"PATCH", "/devices/:device_id/vowifi", s.handleDeviceVoWiFiPatch, authRequired, false, "启停 VoWiFi"},
		{"POST", "/devices/:device_id/vowifi/actions/reconnect", s.handleDeviceMgmtReconnectVoWiFi, authRequired, false, "重连 VoWiFi"},
		{"POST", "/devices/:device_id/vowifi/e911/websheet", s.handleDeviceE911Websheet, authRequired, false, "创建 E911 websheet 会话"},
	}
}

// websheetRoutes 是运营商 E911 页面的反向代理通道。
//
// 凭证是会话自带的一次性 token（?token= 或 X-Websheet-Token），不是用户令牌——
// 承载页在浏览器里直接打开，且要接收运营商侧的回调，两者都拿不到用户的
// Authorization 头。校验在 authorizedWebsheetSession 里做。
func (s *Server) websheetRoutes() []route {
	proxy := func(path string) route {
		return route{"", path, s.handleWebsheetProxy, authInHandler, false, "运营商页面反向代理"}
	}
	var out []route
	for _, r := range []route{
		{"GET", "/websheets/:id", s.handleWebsheetBootstrap, authInHandler, false, "承载页"},
		{"GET", "/websheets/:id/status", s.handleWebsheetStatus, authInHandler, false, "会话状态（也接受用户令牌）"},
		{"POST", "/websheets/:id/callback", s.handleWebsheetCallback, authInHandler, false, "桥接脚本回调"},
		{"POST", "/websheets/:id/done", s.handleWebsheetDone, authInHandler, false, "流程结束"},
	} {
		out = append(out, r)
	}
	// 代理要转发运营商页面发起的任意方法与路径
	for _, path := range []string{"/websheets/:id/proxy", "/websheets/:id/proxy/*target"} {
		for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
			r := proxy(path)
			r.method = method
			out = append(out, r)
		}
	}
	return out
}

func (s *Server) logRoutes() []route {
	return []route{
		{"GET", "/logs/stream", s.handleLogStream, authRequired, true, "实时日志流(SSE)"},
		{"GET", "/logs/history", s.handleLogHistory, authRequired, false, "历史日志"},
	}
}

func registerRoutes(group *gin.RouterGroup, routes []route) {
	for _, rt := range routes {
		group.Handle(rt.method, rt.path, rt.handler)
	}
}

func concatRoutes(groups ...[]route) []route {
	var out []route
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// sseTokenQueryPaths 返回允许 ?token= 的完整路径集合，由路由表派生。
//
// 这里返回的是加了 /api 前缀的路径，与 gin 的 c.FullPath() 一致。
func (s *Server) sseTokenQueryPaths() map[string]struct{} {
	out := map[string]struct{}{}
	for _, r := range s.routes() {
		if r.sseToken {
			out["/api"+r.path] = struct{}{}
		}
	}
	return out
}

func (s *Server) newRouter() *gin.Engine {
	// 等价于 gin.Default()（Logger + Recovery），仅替换为脱敏的日志格式化器。
	r := gin.New()
	r.Use(gin.LoggerWithFormatter(accessLogFormatter))
	r.Use(gin.Recovery())
	r.Use(s.requestIDMiddleware())

	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})
	if s.cfg.Debug {
		r.GET("/debug/embed", s.authMiddleware(), s.handleDebugEmbed)
	}

	// 静态文件服务 (SPA)
	r.NoRoute(s.handleStatic)

	api := r.Group("/api")

	// 先注册不挂中间件的，再 api.Use(authMiddleware) 之后注册需要鉴权的。
	// gin 的 Group.Use 只影响其后注册的路由，顺序在这里是语义的一部分。
	open := api.Group("")
	authed := api.Group("")
	authed.Use(s.authMiddleware())

	var openRoutes, authedRoutes []route
	for _, rt := range s.routes() {
		if rt.auth == authRequired {
			authedRoutes = append(authedRoutes, rt)
			continue
		}
		openRoutes = append(openRoutes, rt)
	}
	registerRoutes(open, openRoutes)
	registerRoutes(authed, authedRoutes)

	return r
}

func (s *Server) handleDebugEmbed(c *gin.Context) {
	if s.fs == nil {
		fail(c, http.StatusServiceUnavailable, "static_disabled", "静态资源未启用")
		return
	}

	testFiles := []string{"index.html", "_next", "favicon.ico"}
	results := make(map[string]string)
	for _, name := range testFiles {
		f, err := s.fs.Open(name)
		if err != nil {
			results[name] = "ERROR: " + err.Error()
			continue
		}
		stat, _ := f.Stat()
		if stat.IsDir() {
			results[name] = "DIR"
		} else {
			results[name] = fmt.Sprintf("FILE (size=%d)", stat.Size())
		}
		f.Close()
	}
	c.JSON(http.StatusOK, results)
}
