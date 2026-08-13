# VoHive 前端 ↔ 后端 API 契约矩阵

> **来源**：直接阅读 `internal/api/` 源码得出，**不以 `openapi.vohive.yaml` 为准**（两者偏差见 §7）。  
> **日期**：2026-08-12  
> **用途**：前端重写的唯一接口依据。实现每个页面前先查本文件。

---

## 0. 一句话结论

后端契约**不统一**：成功响应有 6 种形状、错误响应有 3 种形状、SSE 有 2 种帧风格。
前端必须在 API 层做一次归一化收敛，不能让这些不一致漏进业务组件。

> SSE 鉴权问题（原生 `EventSource` 无法带 token）已于 2026-08-12 在后端修复，见 §3。

---

## 1. 认证

### 1.1 机制

| 项 | 事实 | 位置 |
|----|------|------|
| 登录 | `POST /api/auth/login` `{username, password}` | `server.go:1870` |
| 成功 | `{status:"ok", token, expires_at}`（RFC3339） | `server.go:1905` |
| Token 构造 | `base64(exp_unix + "." + HMAC_SHA256(密码, exp_unix))` | `server.go:140` |
| 有效期 | **30 天**，无 refresh 机制 | `server.go:141` |
| 传递方式 | **仅** `Authorization: Bearer <token>` | `server.go:2076` |
| 服务端状态 | **无**（纯无状态校验，不存 session） | `server.go:2050` |
| 登出接口 | **不存在**（前端本地清 token 即可） | — |
| 登录限流 | 2 分钟内 10 次/IP，超限 `429` + `code:"rate_limited"` | `server.go:156` |

### 1.2 两个必须处理的后果

1. **改密码 = 所有 token 立即失效**。HMAC 密钥就是密码本身，改密后旧 token 校验必然失败。
   前端在 `POST /api/settings/password` 成功后**必须**主动清 token 并跳登录，否则用户会陷入
   「操作全部 401 但界面看着已登录」的状态。
2. **401 语义单一**。`{status:"error", code:"unauthorized"}`，无法区分「过期」与「无效」，
   统一按登出处理即可。

### 1.3 无需鉴权的端点（注册在 `api.Use(authMiddleware)` 之前）

`server.go:224-230`：

| 端点 | 说明 |
|------|------|
| `GET /api/docs`、`/api/docs/assets/*` | Swagger UI 页面 |
| `GET /api/openapi.yaml`、`/api/openapi.json` | 接口定义。**2026-08-13 起免鉴权**，与承载它的 Swagger UI 同级；此前 spec 需鉴权导致页面永远空白 |
| `POST /api/auth/login` | 登录 |
| `POST /api/rotateip` | **并非无鉴权**：handler 内经 `authorizeRotate` 校验，支持 Bearer 或 username/password 双模式（POST-only + 限流），供外部脚本调用 |
| `OPTIONS /api/logs/stream` | CORS 预检 |
| `/api/websheets/*`（13 条） | E911 websheet 代理；以随机 session id 作为能力凭证，且需接收运营商侧回调 |

> `POST /api/system/uninstall` 原本也在此列且**完全无校验**（handler 进来即执行自毁）。
> 2026-08-12 已移入鉴权组，现需 Bearer token。

> 另有免鉴权的 `GET /ping`（不在 `/api` 下），只返回 `{"message":"pong"}`，供外部监控使用。

---

## 2. 响应形状（前端归一化的主要工作量）

### 2.1 成功响应：至少 6 种

`internal/api/` 中 108 处 `c.JSON(http.StatusOK, ...)`，仅 47 处使用 `{status:"ok"}` 包装。

| 形状 | 示例端点 | 位置 |
|------|----------|------|
| `{status:"ok", message}` | `POST /devices/:id/actions/refresh` | `device_mgmt.go:808` |
| `{status:"ok", response:...}` | `POST /devices/:id/actions/at` | `device_mgmt.go:1648` |
| `{devices:[...]}` | `GET /devices`、`GET /devices/:id/overview` | `device_mgmt.go:344` |
| `{devices:[...], device_limit:N}` | `GET /devices`（管理页） | `device_mgmt.go:782` |
| `{config:{...}}` / `{items:[...]}` / `{policies:[...]}` | 设备配置 / 通知 / 卡策略列表 | `device_mgmt.go:985` 等 |
| **裸数组 / 裸对象** | `GET /sms/contacts`、`GET /sms/thread`、`GET /cards/:iccid/policy` | `server.go:1730`、`1807`、`card_policy.go:49` |

**注意**：`GET /devices/:id/overview` 返回的是 `{devices:[单个元素]}`，不是单对象——
详情页要取 `data.devices[0]`。

### 2.2 错误响应：3 种

| 形状 | 使用范围 | 位置 |
|------|----------|------|
| `{status:"error", message, code?, request_id?}` | 主流（约 218 处错误响应的多数） | 全局 |
| `{error:"..."}` | **整个 eSIM 模块** | `device_mgmt.go:2065` 等 |
| `{error, busy:true, code:"ESIM_BUSY", reason, retryAfterMs}` + `Retry-After` 头 | eSIM 并发冲突（409） | `device_mgmt.go:1759` |

`code` 字段仅在 21 处出现，**不能依赖它做错误分支**；多数情况只能靠 HTTP 状态码 + `message` 文案。

> `retryAfterMs` 是 camelCase，与全局 snake_case 约定相反。

---

## 3. SSE：可用原生 EventSource（2026-08-12 起）

### 3.1 鉴权方式

流式端点原先只认 `Authorization` 头，而浏览器原生 `EventSource` 无法设置请求头，
导致这些端点在前端不可用。**已于 2026-08-12 修复**：`requestSessionToken` 增加
`?token=` 回退，但**仅对下列白名单路由开放**（`server.go` 的 `sseTokenQueryRoutes`）：

```
/api/logs/stream
/api/devices/:device_id/overview/stream
/api/devices/:device_id/operator_selection/scan/stream
/api/devices/:device_id/esim/actions/download/stream
```

规则：

- `Authorization` 头**优先**；仅当无该头时才读 `?token=`
- 白名单外的端点**继续拒绝** query 凭证（普通请求走 header 即可，无需暴露 token 到 URL）
- 访问日志已对 `token=` 做脱敏（`accessLogFormatter`），但 token 仍会进入
  **浏览器历史与 Referer**，前端不要把带 token 的流地址渲染成可见链接

**前端结论**：直接用原生 `EventSource(url + "?token=" + token)`，自动获得重连与
`Last-Event-ID` 支持，无需自研解析器。

### 3.2 四个流式端点

| 端点 | 事件名 | Payload | 备注 |
|------|--------|---------|------|
| `GET /api/logs/stream?level=` | `connected`、`log` | `{message}` / LogEntry | 带 CORS 头 |
| `GET /api/devices/:id/overview/stream` | `overview`、`traffic`、`ussd` | `{devices:[item]}` / 流量快照 / USSD 事件 | 10s ticker + VoWiFi 状态变更 + 实时流量 |
| `GET /api/devices/:id/operator_selection/scan/stream` | `operator_scan` | 扫描结果 | 长耗时 |
| `GET /api/devices/:id/esim/actions/download/stream?task_id=` | **无事件名**（默认 `message`） | `{step,msg,pct}` | 见下 |

### 3.3 eSIM 下载流（最特殊）

`internal/api/esim_download.go`。**两步**，不是一个 GET：

1. `POST /api/devices/:id/esim/actions/download`，body `{smdp, matching_id?,
   confirmation_code?, aid_hex?, imei?}` → `202 {status:"ok", task_id}`。
   同一设备已有进行中的任务时返回 `409 {error, busy:true,
   code:"ESIM_DOWNLOAD_IN_PROGRESS", task_id}`——**那个 task_id 可直接订阅**，
   前端把它当正常结果处理（`startDownloadProfile` 已封装）。
2. `GET /api/devices/:id/esim/actions/download/stream?task_id=...` 订阅进度。

流的形状：

- 手写 `data: {...}\n\n`，**不带 `event:` 行**——按 `event:"message"` 监听
- 进度：`{step, msg, pct}`
- 完成：`{step:"done", msg, pct:100, space_delta?, warning?, warning_code?}`
- 失败：`{step:"error", msg, pct:-1, code?, details?}`
- 服务端 **5 分钟**超时；任务完成后再保留 **10 分钟**供断线重连取回结果

拆成两步有两个原因：激活参数不再进 URL（见 §8-4），以及下载不再与某一条连接
绑定——订阅时会**补发已产生的全部事件**，断线重连后进度不会从中途开始。

---

## 4. 分页：游标式，非页码

| 端点 | 游标参数 | 默认 limit |
|------|----------|-----------|
| `GET /api/sms/contacts` | `before_ts`(RFC3339) + `before_peer` | 50 |
| `GET /api/sms/thread` | `before_ts`(RFC3339) + `before_id` | 50 |

无总数字段、无 `has_more`。**判断「还有更多」只能靠「返回条数 == limit」**。
适合 TanStack Query 的 `useInfiniteQuery`，不适合页码分页组件。

`/api/sms/thread` 必填 `peer`，缺失返回 400。

---

## 5. 端点 → 页面矩阵

### 5.1 登录 / 全局

| 端点 | 方法 | 用途 |
|------|------|------|
| `/api/auth/login` | POST | 登录 |
| `/api/settings/password` | POST | 改密（`{old_password,new_password,confirm_password}`，成功后必须重新登录） |
| `/api/system/info` | GET | 版本 / 构建信息 |
| `/api/health` | GET | 逐设备健康明细（设备 ID/信号/联网），**需鉴权**。外部监控请用免鉴权的 `GET /ping` |

### 5.2 仪表盘

| 端点 | 方法 | 用途 |
|------|------|------|
| `/api/dashboard/devices` | GET | 设备卡片汇总 |
| `/api/traffic/analysis?range=day&device_id=` | GET | 流量图（`range` 默认 `day`） |

### 5.3 设备管理（最大页面）

| 端点 | 方法 | 用途 |
|------|------|------|
| `/api/devices` | GET / POST | 列表（含 `device_limit`）/ 添加 |
| `/api/devices/discovered` | GET | 已发现硬件 |
| `/api/devices/actions/rescan` | POST | 重扫描 |
| `/api/devices/:id` | PUT / DELETE | 更新 / 删除 |
| `/api/devices/:id/overview` | GET | 详情（**返回 `{devices:[1]}`**） |
| `/api/devices/:id/overview/stream` | GET SSE | 实时详情 |
| `/api/devices/:id/config` | GET | 设备配置 |
| `/api/devices/:id/status` | GET | 单设备状态 |
| `/api/devices/:id/actions/refresh`、`/reboot` | POST | 刷新缓存 / 重启模组 |
| `/api/devices/:id/actions/at` | POST | AT 命令 → `{status:"ok", response}` |
| `/api/devices/:id/actions/ussd`、`/ussd/continue`、`/ussd/cancel` | POST | **USSD 是多轮会话**，需维护会话态 |
| `/api/devices/:id/usbnet-mode`、`/flight-mode`、`/network` | PATCH | 模式开关（`network` 需 `{enabled}`，可带 `ip_version`/`apn`） |
| `/api/devices/:id/operator_selection` | GET / POST | 选网配置 / 锁定运营商 |
| `/api/devices/:id/operator_selection/scan`、`/scan/stream` | GET | 扫描（同步 / SSE） |

**设备状态**：`lifecycle_phase` 有 9 个取值（`internal/device/lifecycle.go`）：
`offline`、`rebooting`、`usb_wait`、`worker_starting`、`qmi_starting`、`recovering`、
`online`、`degraded`、`evicting`。状态灯需全覆盖。

另有多个独立布尔位需组合展示：`running`、`healthy`、`control_online`、`physical_present`、
`worker_running`、`data_connected`、`radio_registered`、`network_connected`。

核心 DTO 见 `deviceMgmtOverviewLiteItem`（`device_mgmt.go`），38 字段，内嵌
`modem.DeviceStatus`（33 字段：IMEI/ICCID/IMSI/信号/注册态/小区/APN 等）。

### 5.4 eSIM

| 端点 | 方法 | 用途 |
|------|------|------|
| `/api/devices/:id/esim` | GET | 总览 |
| `/api/devices/:id/esim/profiles` | GET | Profile 列表 |
| `/api/devices/:id/esim/profiles/:iccid` | PATCH / DELETE | 改名（`{name, aid_hex}`）/ 删除 |
| `/api/devices/:id/esim/actions/download` | POST | 发起下载，返回 `task_id`（见 §3.3） |
| `/api/devices/:id/esim/actions/download/stream` | GET SSE | 按 `task_id` 订阅下载进度 |
| `/api/devices/:id/esim/actions/switch` | POST | 切换 |
| `/api/devices/:id/esim/eids`、`/chip-info` | GET | EID / 芯片信息 |
| `/api/devices/:id/esim/notifications` | GET | 通知列表 |
| `/api/devices/:id/esim/notifications/:seq/actions/retry` | POST | 重试通知 |

**并发模型**：eSIM 操作经 APDU 仲裁器串行化。任一操作可能返回
`409 + {code:"ESIM_BUSY", retryAfterMs, reason}` + `Retry-After` 头。
前端**必须**实现：禁用并发操作入口 + 按 `retryAfterMs` 自动重试或明确提示。

删除成功可能带 `warning` / `warning_code` / `space_delta`（如空间回收异常），需展示而非忽略。

### 5.5 短信

| 端点 | 方法 | 用途 |
|------|------|------|
| `/api/sms/contacts?device_id=&imsi=&limit=&before_ts=&before_peer=` | GET | 会话列表（**裸数组**） |
| `/api/sms/thread?peer=&device_id=&imsi=&limit=&before_ts=&before_id=` | GET | 会话详情（**裸数组**，`peer` 必填） |
| `/api/sms/send` | POST | 发送（自动选 AT 或 VoWiFi 通道） |
| `/api/sms/delivery/:message_id` | GET | 投递状态 |
| `/api/sms/messages/:id` | DELETE | 删单条 |
| `/api/sms/thread` | DELETE | 删会话 |

**无短信 SSE**。新短信只能靠 TanStack Query 轮询（建议 contacts 10–15s）。

设备维度用 `device_id`，但存储主键是 **ICCID**；后端通过 `resolveSMSICCID(device_id, imsi)`
换算。换卡后历史短信跟 ICCID 走，不跟设备走——UI 需说明这一点。

### 5.6 代理

| 端点 | 方法 | 用途 |
|------|------|------|
| `/api/proxy-instances/overview` | GET | `{instances, devices, status}` |
| `/api/proxy-instances/config` | PUT | 保存配置 |
| `/api/proxy-instances/:id` | GET | 单实例 |
| `/api/proxy-instances/:id/actions/start`、`/stop`、`/restart` | POST | 生命周期 |
| `/api/upstream-proxies` | GET / POST | 上游代理 |
| `/api/upstream-proxies/:id` | PUT / DELETE | 改 / 删 |
| `/api/upstream-proxies/:id/actions/probe` | POST | 探测 |
| `/api/upstream-proxy-countries` | GET | 可选国家 |
| `/api/upstream-proxy-country-rules` | GET | 国家规则列表 |
| `/api/upstream-proxy-country-rules/:code` | PUT / DELETE | 保存 / 删除规则 |
| `/api/rotateip` | POST | 换 IP（**无鉴权**） |

### 5.7 卡策略

| 端点 | 方法 | 备注 |
|------|------|------|
| `/api/cards/policies` | GET | `{policies:[...]}` |
| `/api/cards/:iccid/policy` | GET / PUT | **裸对象**；不存在时返回 `DefaultCardPolicy` 而非 404 |

### 5.8 日志与设置

| 端点 | 方法 | 备注 |
|------|------|------|
| `/api/logs/stream?level=` | GET SSE | `level`: debug/info/warn/error |
| `/api/logs/history` | GET | 历史日志 |
| `/api/settings/notifications` | GET / PUT | 通知配置 |
| `/api/settings/notifications/webhook/test`、`/bark/test`、`/email/test` | POST | 测试发送（**仅这 3 个渠道有测试接口**） |
| `/api/system/update/check`、`/update/apply` | GET / POST | 在线更新 |
| `/api/system/uninstall` | POST | 卸载/自毁，**破坏性且不可撤销**；需鉴权。UI 上必须二次确认 |

### 5.9 VoWiFi / E911

| 端点 | 方法 | 备注 |
|------|------|------|
| `/api/devices/:id/vowifi` | PATCH | 启停 |
| `/api/devices/:id/vowifi/actions/reconnect` | POST | 重连 |
| `/api/devices/:id/vowifi/e911/websheet` | POST | 开 E911 websheet 会话 |
| `/api/websheets/:id`、`/:id/proxy[/*target]`、`/:id/callback`、`/:id/done` | 多 | 运营商页面反向代理，**全部无鉴权** |

E911 websheet 本质是把运营商网页代理进本服务。前端按「新窗口/iframe 打开 `/api/websheets/:id`
→ 轮询或等待 `/done`」处理，**不要试图解析其内容**。

---

## 6. 后端未提供、前端需自行处理

| 缺口 | 影响 | 前端对策 |
|------|------|----------|
| 无登出接口 | — | 本地清 token |
| 无 token 刷新 | 30 天后强制重登 | 记录 `expires_at`，临期提示 |
| 无短信 SSE / webhook | 新短信不实时 | 轮询 |
| 无分页总数 | 无法显示「共 N 条」 | 无限滚动，不做页码 |
| 通知渠道仅 3 个有测试接口 | TG/飞书/QQ/PushPlus 无法测 | UI 上不给测试按钮，避免误导 |

---

## 7. 与 `openapi.vohive.yaml` 的偏差（**不要以 spec 为准**）

- 实际注册：**84 条**（`server.go`）+ **13 条**（websheet）
- OpenAPI 声明：**59 条**
- **实际有、spec 无：17 条**，含 `overview/stream`、`operator_selection*`、`cards/*`、
  `ussd/continue`、`ussd/cancel`、`system/update/*`、`bark/test`、`email/test`、`system/uninstall`
- **spec 有、实际无：3 条**（全部是微信 QR 通知配置）：
  `/settings/notifications/weixin/qr/start`、`/qr/status`、`/qr/cancel`
  —— **按 spec 实现这三个会得到 404**

> 结论：spec 已显著滞后。**不要用它生成 TypeScript 类型**，会同时漏掉真实端点、
> 生成不存在端点的调用代码。本矩阵是唯一依据。

---

## 8. 顺带发现的后端问题（不属于前端范围，建议单独处理）

| # | 问题 | 状态 |
|---|------|------|
| 1 | `POST /api/system/uninstall` 完全无校验，任何能访问端口者可触发自毁 | ✅ **已修复**（2026-08-12）移入鉴权组 |
| 2 | ~~`POST /api/rotateip` 无鉴权~~ | ❌ **判断有误**：handler 内有 `authorizeRotate` 双模式校验，非漏洞，未改动 |
| 3 | SSE 端点无法用原生 EventSource | ✅ **已修复**：白名单 `?token=` 回退 + 访问日志脱敏 |
| 4 | eSIM 激活码经 URL query 传输 | ✅ **已修复**（2026-08-14）拆成 POST 建任务 + GET 按 `task_id` 订阅，激活参数只走请求体（见 §3.3） |
| 5 | `/api/docs` 免鉴权但其 spec 需鉴权，页面必然空白 | ✅ **已修复**（2026-08-13）spec 移至免鉴权区 |
| 6 | `/api/health` 需鉴权，与"外部监控用"注释矛盾 | ✅ **已澄清**：该端点返回逐设备明细（设备 ID/信号），理应鉴权；注释已更正，外部监控应改用免鉴权的 `/ping` |

§8 已全部收口。

---

## 9. 本文件的验证状态

| 内容 | 状态 |
|------|------|
| 路由清单、鉴权分组、SSE 事件名、响应/错误形状、分页参数、生命周期取值、spec 偏差 | **已逐条读源码确认** |
| 部分端点的完整响应字段（proxy overview、traffic analysis、esim profile 明细） | **仅确认了顶层结构**，实现对应页面时需再核对 |
| 任何运行时行为（实际返回值、边界情况） | **未运行验证**，本机无 Go 工具链 |
