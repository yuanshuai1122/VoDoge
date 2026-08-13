# `internal/api` 重构方案

日期：2026-08-14
状态：执行中

## 0. 为什么要动

不是"代码不好看"。三处具体的结构问题各自已经造成过实际代价：

### 0.1 路由的三份平行清单

同一条路由的信息散在三个地方，改一处不改另一处不会报错，只会在运行时坏掉：

| 清单 | 位置 | 内容 |
|------|------|------|
| 注册 | `server.go:newRouter()` 的 160 行 `api.METHOD(...)` | 路径 → handler |
| SSE 凭证白名单 | `server.go:sseTokenQueryRoutes` | 哪些路径允许 `?token=` |
| 契约 | `openapi.vohive.yaml` | 路径 → 请求/响应 |

本次会话就踩到过：eSIM 下载从 `GET .../download` 改成
`GET .../download/stream` 时，白名单还指着旧路径——前端拿不到 token 认证，
SSE 直接 401，而编译、`go vet`、单测全部通过。

spec 那一份已经由 `scripts/check-routes.mjs` 兜住了（它 grep 源码里的
`api.METHOD("...")`）。但那是**从代码文本里反推结构**，脆弱且只能校验路径本身，
校验不了"这条路由该不该鉴权""该不该收 query token"。

### 0.2 三种错误响应形状

| 形状 | 出现次数 | 谁在用 |
|------|---------|--------|
| `{status:"error", message, code?, request_id?}` | 163 | 主流 |
| `{error:"..."}` | 48 | eSIM 全模块、card_policy、operator_selection |
| `{error, busy:true, code:"ESIM_BUSY", reason, retryAfterMs}` | 其中若干 | eSIM 并发冲突 |

后果是前端**每一个请求**都得同时准备三套解析（`web/lib/api/errors.ts`），
因为它无从预知会拿到哪一种。更实际的损失是：`{error:"..."}` 这一支
**丢掉了 `request_id`**——排查线上问题时，用户报的错误信息在日志里搜不到对应请求。

`retryAfterMs` 还是全局 snake_case 里唯一的 camelCase 字段。

### 0.3 两个巨型文件

`server.go` 2281 行、`device_mgmt.go` 2598 行，各自混着互不相干的东西：
`server.go` 里同时有 Server 生命周期、鉴权、登录限流、路由表、静态文件 SPA 回退、
短信收发、日志流、健康检查。找任何一处都要靠搜索，改动时也看不出影响范围。

---

## 1. 明确不做什么

**不改成功响应的形状。** 现有 6 种（`{status:"ok"}`、`{devices:[...]}`、
`{items:[...]}`、`{config:X}`、裸数组、裸对象）看着不整齐，但它们只是
**同一个载荷的不同键名**，而前端已经确立了"每个 endpoint 显式声明自己期望的形状、
不做运行时猜测"的原则（`web/lib/api/unwrap.ts` 开头）。统一键名要动 108 处响应、
整个前端端点层和 spec，换来的只是少写几个键名——不划算，而且会在没有测试覆盖的
地方静默改变行为。

错误不一样：错误形状是**前端无法预知**的，所以必须每次都试三遍。那是真缺陷。

**不拆成多个 Go 包。** handler 全是 `*Server` 的方法，拆包要么导出一堆内部状态，
要么造一个上下文结构体传来传去。同一个包内按域分文件能拿到九成的可读性收益，
风险接近零。

**不动业务逻辑。** 这次只改结构。任何行为变化都应该是显式的、在下面列出来的。

---

## 2. 三批改动

### 批次 1 — 路由表变成数据

`newRouter()` 里的注册语句改为遍历一张表：

```go
type route struct {
    method   string
    path     string      // /api 之下的路径
    handler  gin.HandlerFunc
    auth     authMode    // authRequired | authNone | authInHandler
    sseToken bool        // 允许 ?token= 传凭证
}
```

- `sseTokenQueryRoutes` 由表派生，不再手工维护 → 0.1 的那类事故不可能再发生
- `auth: authInHandler` 显式标出 `/rotateip` 与 websheet 这类"看着免鉴权、
  实际在 handler 内校验"的路由，替代现在写在注释里的说明
- `scripts/check-routes.mjs` 改为读这张表，比 grep 注册语句可靠

**行为变化：无。** 表按原顺序注册，gin 的路由树与之前一致。

### 批次 2 — 统一错误响应

新增 `respond.go`：

```go
func fail(c *gin.Context, status int, code, message string)
func failErr(c *gin.Context, status int, code string, err error)
```

统一输出 `{status:"error", code, message, request_id}`。48 处 `{error:"..."}`
全部转换过来；`busy` / `retryAfterMs` / `task_id` 这类调用方依赖的附加字段
保留在同一层级。

**行为变化（前端需同步）：**

1. eSIM 与 card_policy / operator_selection 的错误体从 `{error:"..."}`
   变为 `{status:"error", code, message, request_id}`。
   前端 `parseApiError` 本就同时认这两种，改完之后第二个分支不再被命中，
   但**保留**——外部脚本可能还在按旧形状解析，且容错本身没有代价。
2. 所有错误响应新增 `request_id`。
3. `retryAfterMs` 保留原名（前端与外部调用方都在读它），
   同时补一个 snake_case 的 `retry_after_ms`，新代码用后者。

每个错误都必须带 `code`。这一步顺带把 48 处"只有一句中文"的错误变成可判别的。

### 批次 3 — 按域拆文件

`server.go` →

| 文件 | 内容 |
|------|------|
| `server.go` | Server 结构体、New、Run、Shutdown、SetRealtimeTraffic |
| `routes.go` | 路由表与 `newRouter()` |
| `auth.go` | 登录、令牌、限流、`authMiddleware`、`requestSessionToken` |
| `static.go` | SPA 静态文件回退 |
| `sms.go` | 短信收发、会话、联系人 |
| `logs.go` | 日志流与历史 |
| `dashboard.go` | 设备列表、健康、统计、状态 |

`device_mgmt.go` →

| 文件 | 内容 |
|------|------|
| `device_mgmt.go` | 设备增删改查与配置 |
| `device_actions.go` | 重启 / AT / USSD / 刷新 |
| `device_esim.go` | eSIM（下载已在 `esim_download.go`） |
| `device_vowifi.go` | VoWiFi 与网络开关 |
| `device_overview.go` | 概览与概览流 |

**行为变化：无。** 纯文件搬运，不改函数签名。

---

## 3. 每批的验收

```bash
bash scripts/ci.sh          # hygiene encoding routes web vet-all test image
```

批次 2 额外需要：前端 `npm run typecheck && npm run lint && npm run build`，
以及 spec 里 `ErrorResponse` 的字段更新。

---

## 4. 顺序理由

路由表先做：它让第二、三批的移动有一张"哪条路由归哪个域"的依据。
错误统一在拆文件之前做：拆完再改会让 diff 同时包含移动和修改，评审时分不清哪是哪。
