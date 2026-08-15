# `internal/api` 重构方案

日期：2026-08-14
状态：**已完成**（三批均已合入，见文末验收记录）

## 0. 为什么要动

不是"代码不好看"。三处具体的结构问题各自已经造成过实际代价：

### 0.1 路由的三份平行清单

同一条路由的信息散在三个地方，改一处不改另一处不会报错，只会在运行时坏掉：

| 清单 | 位置 | 内容 |
|------|------|------|
| 注册 | `server.go:newRouter()` 的 160 行 `api.METHOD(...)` | 路径 → handler |
| SSE 凭证白名单 | `server.go:sseTokenQueryRoutes` | 哪些路径允许 `?token=` |
| 契约 | `openapi.vodog.yaml` | 路径 → 请求/响应 |

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
func failErr(c *gin.Context, status int, code string, err error, fallback string)
func failWith(c *gin.Context, status int, code, message string, extra gin.H)
```

统一输出 `{status:"error", code, message, request_id}`。243 处错误站点全部走它；
`busy` / `retryAfterMs` / `task_id` 这类调用方依赖的附加字段用 `failWith`
保留在同一层级（且不允许覆盖那四个固定字段）。

**行为变化（前端需同步）：**

1. eSIM 与 card_policy / operator_selection 的错误体从 `{error:"..."}`
   变为 `{status:"error", code, message, request_id}`。
   前端 `parseApiError` 本就同时认这两种，改完之后第二个分支不再被命中，
   但**保留**——外部脚本可能还在按旧形状解析，且容错本身没有代价。
2. 所有错误响应新增 `request_id`。
3. `retryAfterMs` 保留原名（前端与外部调用方都在读它），
   同时补一个 snake_case 的 `retry_after_ms`，新代码用后者。

`code` **留空时按 HTTP 状态推导**（`bad_request` / `not_found` / `conflict` / …）。
给两百多个站点各编一个专属码只会产出一堆没人分支的字符串；真正需要客户端据以
决策的场景（`ESIM_BUSY`、`ESIM_DOWNLOAD_IN_PROGRESS`、`e911_*`、`websheet_*`）
本来就带着自己的 code。

### 批次 3 — 按域拆文件

`server.go` →

| 文件 | 内容 |
|------|------|
| `server.go` | Server 结构体、New、Run、Shutdown、请求 ID |
| `routes.go` | 路由表与 `newRouter()` |
| `auth.go` | 登录、令牌、限流、`authMiddleware`、`requestSessionToken` |
| `static.go` | SPA 静态文件回退 |
| `sms.go` | 短信收发、会话、联系人 |
| `logs.go` | 日志流与历史、SSE 的 CORS 头 |
| `dashboard.go` | 设备列表、健康、统计、状态、系统信息 |
| `vowifi.go` | VoWiFi 启停与状态 |
| `network.go` | 数据网络启停与换 IP |

`device_mgmt.go` →

| 文件 | 内容 |
|------|------|
| `device_mgmt.go` | 设备增删改查与配置 |
| `device_actions.go` | AT / USSD / 重启 / 飞行模式 / USBNET / VoWiFi 重连 |
| `device_esim.go` | eSIM（下载已在 `esim_download.go`） |
| `device_overview.go` | 概览的 DTO 与构建逻辑 |
| `device_overview_stream.go` | 单设备概览 SSE 流 |
| `device_discovery.go` | 硬件发现 |

**行为变化：无。** 纯文件搬运，声明逐字节复制，import 由 goimports 修正。

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

---

## 5. 验收记录（2026-08-14）

`bash scripts/ci.sh` 全绿：hygiene / encoding / routes / web / vet-all / test / image。
14 个测试包 + `cmd/dbmigrate` 全部通过（真实 PostgreSQL）。

另在 Docker 里起了一套完整实例（`vohive:latest` + `postgres:16-alpine`）实测：

| 验证项 | 结果 |
|--------|------|
| `scripts/smoke-api.mjs` | 全部通过（登录、系统信息、设备列表、短信会话、代理概览、通知设置、SSE 日志流） |
| 前端登录 → 仪表盘 → 日志页 | 正常，无控制台错误；日志页 SSE 显示"已连接" |
| 错误形状 | `{"status":"error","code":"not_found","message":"...","request_id":"..."}`，eSIM 端点也一样 |
| 401 | `{"status":"error","code":"unauthorized",...}` |
| `OPTIONS /api/logs/stream` | 204 |
| `?token=` 白名单：`/api/logs/stream` | 200（放行） |
| `?token=` 白名单：`/api/devices` | **401**（非流式端点正确拒绝 query 凭证） |
| `?token=` 白名单：`.../download/stream` | 404 任务不存在（即鉴权已通过——**这正是重构前失效的那条路径**） |
| 访问日志脱敏 | `token=***` |

### 文件行数变化

| 文件 | 之前 | 之后 |
|------|------|------|
| `server.go` | 2281 | 190 |
| `device_mgmt.go` | 2598 | 650 |
| 单文件最大 | 2598 | 683（`device_overview.go`） |

### 新增的结构性约束（写成测试，不是靠自觉）

- `TestSSETokenWhitelistIsDerivedFromTheRouteTable` —— 白名单必须来自路由表
- `TestOnlyStreamingRoutesAcceptQueryToken` —— 非流式端点不得开 `?token=`
- `TestRoutesOutsideAuthMiddlewareAreDeliberate` —— 任何脱离鉴权中间件的路由
  都必须在测试里连同理由一起登记，否则测试失败
- `TestRouteTableHasNoDuplicateMethodPathPairs` —— 重复注册在测试期报错而非运行期 panic
- `respond_test.go` —— 错误形状、`request_id`、code 推导、附加字段不得覆盖固定字段

---

## 6. 第二轮（2026-08-14 晚）

上一轮把 `internal/api` 的**内部结构**理顺了，但它与外部的两条依赖仍是隐式的：
数据库靠包级全局，硬件靠包级函数指针。这一轮处理这两条。

### 6.1 持久化边界（B1）

`db.DB` 是包级全局，11 个 API 文件直接调 `db.XxxFunc()`。代价是具体的：

- handler 与表结构直接耦合；
- **任何碰持久化的 handler 测试都得连真 PostgreSQL**——起容器、清空全库、十几秒；
- 各包共用一个测试库，所以整个 Go 测试套件被钉死在 `-p 1` 串行；
- `OpenTestDB` 会 TRUNCATE 目标 schema 的所有表，DSN 指错就是一次事故（KI-002）。

新增 `internal/data/repo`：按域定义接口（CardPolicy / SIM / SMS / Traffic /
UpstreamProxy / ProxyInstance），`Server` 持有 `*repo.Store`。API 层非测试文件里
**已无任何查库调用**，剩下的 `db.` 引用全是类型、哨兵错误与纯函数。

**刻意只做接口与转发**：实现直接委托给 `internal/db` 里已有的函数，不重写查询。
那些函数有真库测试覆盖，重写等于把风险从"没有边界"换成"边界后面是新代码"。
全局 `db.DB` 因此仍在，只是 API 层够不着——把 `*gorm.DB` 一路下推、彻底干掉全局
要连 device / notify / proxy 三层一起改，属于硬件路径，等现场验证之后再动。

收益已经兑现：11 个 handler 测试跑在 **0.012 秒**、不连数据库。两个原本要往
`devices` 和 `card_policies` 各插一行、只为验证一段取值优先级的测试，现在注入
假实现即可。反过来，若有人把 handler 改回直接调 `internal/db`，这些测试会因
`DB` 为 nil 而失败——边界有了守卫。

`device_mgmt_cardpolicy_writethrough_test.go` **保留真库**：它验的是跨层写穿
不变式，用假实现等于自己断言自己。

### 6.2 测试缝改为注入（B5）

7 个包级 `var xxxFn = ...` 全部去掉，换成 `Server` 上的两个字段：

| 字段 | 边界 |
|------|------|
| `hardware hardwareProbe` | QMI/MBIM 枚举与 IMEI 探测 |
| `esimNotificationsFor func(*device.Worker) esimNotificationSource` | eSIM 通知来源 |

留 nil 时回落真实实现，生产行为不变。

eSIM 那对顺带改好了形状：`esimNotificationListExec(run, args)` 把真实方法当参数
传进来再调用，那层间接不表达任何东西，纯粹为了可替换而存在。现在接口描述的是
依赖本身，handler 读起来是"问这台设备要它的通知"而不是"执行别人递给我的函数"。
`TestEsimNotificationExecHelpersPropagateResults` 随之删除——它断言的是
`return run(args)` 会返回 `run` 的返回值。

**`internal/device` 的 8 处保持不动**：它们在启动与恢复路径上，去全局化要把依赖
穿过 `Pool` 的构造。那正是明天要在真实模组上跑的代码——万一台架上出问题，
不该让人先怀疑是不是这次重构。

### 6.3 前端测试（D1）

此前前端**零测试**：8 个页面、20+ 组件、整个 API 归一化层全靠人工点。

引入 vitest + testing-library，**67 例**，接进 `scripts/ci.sh web`。优先覆盖
归一化层，因为那里 tsc 帮不上忙——后端响应体在类型系统里是 `unknown`，解析
错了只会在运行时以"错误提示变成『请求失败』"的形式出现。

| 文件 | 盯住什么 |
|------|---------|
| `lib/api/errors.test.ts` | 统一错误形状、`request_id` 不丢、`retry_after_ms` 新旧拼写都认、busy 分支优先级 |
| `lib/api/unwrap.test.ts` | 该抛的时候要抛；`{devices:[单元素]}` 必须取 `[0]` |
| `lib/api/client.test.ts` | 鉴权头、401 登出（登录页除外）、超时与断网的区分、非 JSON 响应不崩 |
| `lib/api/endpoints/esim.test.ts` | **激活码不得出现在 URL 里**；409 带 `task_id` 当正常结果 |
| `lib/device-status.test.ts` | 9 phase × 8 布尔位的判定顺序与信号分级边界 |
| `components/devices/e911-card.test.tsx` | 弹窗必须先于 await 打开（否则被静默拦截）；完成态靠轮询 |
