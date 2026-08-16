# API 响应结构统一方案

日期：2026-08-14
状态：**已完成**（四批均已合入，见文末验收记录）

## 0. 现状：112 个成功响应站点，约 60 种形状

不是"6 种"——按顶层键分类实测下来接近 60 种。摘录出现频次最高的一批：

| 形状 | 次数 |
|------|------|
| `{status, message}` | 9 |
| `{devices}` | 6 |
| `{status}` | 6 |
| `{status, result, channel}` | 4 |
| `{status, message, device}` | 4 |
| 裸对象 / 裸数组 | ~20 |

问题不止是"键名不统一"。真正的麻烦是**元数据与载荷混在同一层**：

```
{status, requires_restart, warning}        保存设备
{status, started, requires_restart, warning}  添加设备
{devices, device_limit}                    设备列表
{status, thread_empty, imsi, peer}         删短信
{status, deleted, iccid, peer}             删会话
{status, logs, note}                       日志历史
{status:"ok", message, warning, warning_code, space_delta}  删 eSIM profile
```

`requires_restart`、`device_limit`、`warning`、`thread_empty`、`space_delta`
描述的是**这次操作**，不是返回的资源；它们和资源字段平铺在一起，调用方无从
区分哪些是数据、哪些是关于数据的说明。加一个新的元数据字段就有撞名风险。

前端为此维护了 6 个解包函数（`pick` / `pickOr` / `raw` / `rawArray` / `ok` /
`pickFirstDevice`），并确立了"每个 endpoint 显式声明自己期望哪种形状"的原则。
那个原则是**对当时局面的合理妥协**，不是理想终局：它把契约知识散进了 60 处调用点。

---

## 1. 新结构

```jsonc
// 成功（2xx）
{
  "data": <载荷，可为 null>,
  "meta": { ... },          // 可选，为空时不出现
  "request_id": "9f2c…"
}

// 失败（4xx/5xx）
{
  "error": {
    "code": "not_found",
    "message": "设备未找到",
    "details": { ... }      // 可选
  },
  "request_id": "9f2c…"
}
```

### 三条不变式

1. **`data` 与 `error` 互斥且必有其一。** 判别是结构性的（`"error" in body`），
   不再靠 `status:"ok"` 这种魔法字符串——那个字符串曾经出现在 200 响应里表示失败
   （日志历史读不到文件时回 `200 + {status:"error"}`），自相矛盾且无法防。
2. **`request_id` 恒在**，成功失败都有，与 `X-Request-Id` 头一致。
3. **`meta` 只放"关于这次操作/这批数据"的信息**，绝不放资源本身。

### data 的三种取值

| 场景 | data |
|------|------|
| 单个资源 | 对象 |
| 集合 | 数组（**空集合是 `[]` 不是 `null`**） |
| 纯动作，无资源可返回 | `null` |

### meta 承载什么

现有的元数据字段按语义归位，**不再与载荷同层**：

| 原字段 | 归入 | 含义 |
|--------|------|------|
| `message` | `meta.message` | 给人看的操作结果说明 |
| `warning` / `warning_code` | `meta.warning` / `meta.warning_code` | 操作成功但有保留 |
| `requires_restart` | `meta.requires_restart` | 需重启才生效 |
| `started` | `meta.started` | 配置已存但运行时未起来 |
| `applied` | `meta.applied` | 是否已实际生效 |
| `device_limit` | `meta.device_limit` | 集合的额度上限 |
| `space_delta` | `meta.space_delta` | eSIM 空间变化 |
| `thread_empty` / `deleted` | `meta.thread_empty` / `meta.deleted` | 删除的副作用 |
| `note` | `meta.note` | 降级说明 |

### error.details 承载什么

需要客户端据以决策的结构化数据，而不是给人读的文字：

| 场景 | details |
|------|---------|
| `ESIM_BUSY` (409) | `{busy, reason, retry_after_ms}` |
| `ESIM_DOWNLOAD_IN_PROGRESS` (409) | `{busy, task_id}` |

`retryAfterMs` 这个 camelCase 遗留字段**就此删除**——它当初保留是为了不破坏
既有调用方，而这次本来就是破坏性变更，再留一个错误拼写没有意义。

---

## 2. 明确不动的三处

**SSE 事件帧不套信封。** `data: {"step":"install","pct":80}` 是一串领域事件，
不是 HTTP 响应；套上 `{data:…,request_id:…}` 只会让每帧多带一份无用的重复信息，
而且 `request_id` 对一条持续几分钟的流没有意义。

**`GET /ping` 保持 `{"message":"pong"}`。** 它在 `/api` 之外，是给外部监控用的
存活探针；改它等于要求所有监控配置跟着改，换不来任何东西。

**websheet 的承载页与代理通道不套信封。** 那些响应的内容完全由运营商页面决定
（HTML、重定向、任意 content-type），本就不是本服务的 JSON 契约。
`GET /websheets/:id/status` 是我们自己的接口，**要**套。

---

## 3. 服务端实现

`internal/api/respond.go`：

```go
func respondOK(c *gin.Context, data any)                        // 200
func respondOKWith(c *gin.Context, data any, meta gin.H)        // 200 + meta
func respond(c *gin.Context, status int, data any, meta gin.H)  // 任意 2xx
func fail(c *gin.Context, status int, code, message string)
func failWith(c *gin.Context, status int, code, message string, details gin.H)
```

信封用两个独立结构体而非一个带 `omitempty` 的：`data` 在成功响应里**必须出现**
（哪怕是 `null`），而错误响应里**不该出现**。一个结构体做不到这一点。

---

## 4. 前端实现

`apiFetch` 返回 `ApiResult<T> = {data: T; meta: Record<string, unknown>; requestId: string}`。
端点层取 `.data`，需要元数据时取 `.meta`。

`unwrap.ts` 的 6 个函数**全部删除**——契约统一之后它们没有存在理由。
`pickFirstDevice` 这类"固化易错点"的辅助函数同理：
`GET /devices/:id/overview` 返回 `{devices:[单元素]}` 本身就是个应当修掉的怪癖，
它的 `data` 应当直接是那个设备对象。

---

## 5. 破坏性

这是**破坏性变更**，且刻意不做兼容期：

- 前端与后端同一个仓库、同一个二进制发布，不存在版本错配；
- 留兼容层意味着两套形状同时活着，那正是现在这个局面的成因。

受影响的外部调用方：`scripts/smoke-api.mjs`（本仓库，同步改）。
若你有外部脚本在调 `/api/rotateip` 或其它端点，需要同步调整——这一条会写进
`DEPLOY.md` 的升级说明。

---

## 6. 分批

| 批次 | 内容 | 验收 |
|------|------|------|
| 1 | `respond.go` 新信封 + Go 侧 112 成功站点 + 243 错误站点 | `scripts/ci.sh vet-all test` |
| 2 | OpenAPI spec 全量响应 schema | `scripts/ci.sh routes` + 人工核对 |
| 3 | 前端 client / unwrap / errors / 11 个端点模块 / 组件 | `typecheck` `lint` `test` `build` |
| 4 | smoke 脚本、文档、Docker 实测 | `scripts/ci.sh` 全绿 + 浏览器验证 |

---

## 7. 验收记录（2026-08-14）

`bash scripts/ci.sh` 全绿：hygiene / encoding / routes / web（当前 22 个测试文件 / 104 例）/
vet-all / test（32 个 Go 包）/ image。

### 实测（Docker 起 `vohive:latest` + `postgres:16-alpine`）

| 验证项 | 结果 |
|--------|------|
| 登录 | `{"data":{"expires_at":…,"token":…},"request_id":…}` |
| 集合 | `{"data":[],"meta":{"device_limit":5},"request_id":…}` |
| 纯动作 | `{"data":null,"meta":{"message":"设备重新扫描完成"},…}` |
| 添加设备（启动失败） | `data:null` + `meta:{requires_restart,started,warning}` |
| 空集合 | `{"data":[],…}`——不是 `null` |
| 404 | `{"error":{"code":"not_found","message":"设备或esim管理器未找到"},…}` |
| 401 | `{"error":{"code":"unauthorized",…},…}` |
| `GET /ping` | `{"message":"pong"}`——保持裸形状 |
| `GET /health` | 恒 200，`data.healthy` |
| `GET /devices/nope/overview` | **404**（原先是空数组） |
| 前端登录 → 仪表盘 → 设备列表 | 正常，"已接入 1 / 5 台设备"来自 `meta.device_limit` |
| `scripts/smoke-api.mjs` | 全部通过 |

### 落地规模

| 层 | 改动 |
|----|------|
| Go | 112 处成功站点 + 243 处错误站点 |
| OpenAPI | 76 个 2xx 响应包信封，17 个 payload schema 按实现重写，删除 1 个 |
| 前端 | `unwrap.ts`（6 个函数）删除，8 个端点模块、client、errors 重写 |
| smoke | 全部断言改为信封 |

### 新增的结构性约束

- `respond_test.go` —— data/error 互斥、`data` 为 null 时字段不得消失、
  空 meta 不出现、meta 不得混进 data、`request_id` 恒在
- `openapi_test.go` —— 每个 2xx JSON 响应必须引用 `Envelope`，
  每个错误响应组件必须引用 `ErrorEnvelope`
- `client.test.ts` —— 2xx 却不符合信封结构时抛错，而不是把它当载荷静默透传

### 一个 YAML 陷阱

纯量不能以反引号开头，`description: `data` 恒为 null` 会被解析成语法错误。
用块标量（`|`）绕开。这个错误只在完整解析 YAML 时才暴露，grep 看不出来。
