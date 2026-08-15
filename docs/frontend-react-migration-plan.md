# VoDog 前端 React 重写计划 v1.0

> **状态**：计划定稿，脚手架与构建链已就位，**业务代码未开始**  
> **日期**：2026-08-12  
> **依据**：[`frontend-api-matrix.md`](./frontend-api-matrix.md)（逐条读源码得出的真实契约）  
> **决策**：[`frontend-react-decisions.md`](./frontend-react-decisions.md)（ADR-001～005）

---

## 0. 本次重写相对 v0.3 变了什么

v0.1–v0.3 的阶段拆分建立在两个后来被证伪的假设上：

| v0.3 的假设 | 实际情况 | 影响 |
|-------------|----------|------|
| 以 `openapi.vodog.yaml` 为接口依据 | spec 缺 17 条真实端点、含 3 条不存在端点 | **不能用 spec 生成类型**，否则漏功能 + 生成死代码 |
| 用原生 `EventSource` 接 SSE | token 原仅从 `Authorization` 头读，EventSource 设不了头 | 已推动后端修复（白名单 `?token=`），**结论回到可用原生 EventSource** |
| 「后端契约兼容，前端照着做」 | 6 种成功形状、3 种错误形状、camelCase 混入 | **API 归一化层是主要工作量，不是顺带的事** |
| 对照旧 Vue 前端实现 | 旧 Vue 代码已从仓库删除（`internal/web/` 仅剩 `fs.go`） | 无参照物，只能以后端契约为准 |

因此 v1.0 的核心结构性调整是：**把「API 基础设施」提升为独立阶段，排在所有业务页面之前**。
v0.3 里它散落在 P2-1 一个条目中，严重低估。

---

## 1. 现状基线（2026-08-12）

| 项 | 状态 |
|----|------|
| 脚手架 | ✅ Next 16.3 + React 19 + Tailwind 4 + TS |
| 构建链 | ✅ 静态导出 → `web/dist` → `internal/web/dist` 嵌入（ADR-005） |
| 开发代理 | ✅ `next dev` 的 `/api/*` → `:7575`（**必需**，后端无全局 CORS） |
| typecheck / build | ✅ 本地通过 |
| API 契约梳理 | ✅ 本轮完成，见 api-matrix |
| **业务代码** | ❌ **零**。`app/` 仍是 create-next-app 默认页 |
| shadcn/ui、主题 | ❌ 未接入 |

---

## 2. 技术选型

ADR-001 全部维持有效，静态导出不影响其中任何一项：

| 层 | 选型 | 本轮确认 |
|----|------|----------|
| 框架 | Next.js App Router + TS | 静态导出模式 |
| UI | shadcn/ui + Tailwind | 不变 |
| 服务端状态 | TanStack Query | 游标分页用 `useInfiniteQuery` |
| 客户端状态 | Zustand | token / 主题 / USSD 会话态 |
| 表单 | React Hook Form + Zod | Zod 同时用于响应校验 |
| 图标 | Lucide | 不变 |
| 图表 | Recharts | 流量图，优先级最低 |
| 实时 | 原生 `EventSource` + 薄封装 | 后端已支持白名单 `?token=`（2026-08-12） |

**因静态导出而禁用**：Server Actions、Route Handlers、middleware、ISR、`next/image` 优化。
所有页面为 `"use client"` 或静态预渲染 + 客户端取数。

---

## 3. 架构分层

```
app/                     路由与页面（薄，只做编排）
  (auth)/login
  (app)/ | devices | devices/detail?id=&tab={overview,esim,at,ussd,card-policy,config}
        | sms | proxy | logs | settings
components/
  ui/                    shadcn
  layout/ devices/ sms/ proxy/ settings/
lib/
  api/
    client.ts            fetch 封装：注入 Bearer、401 登出、超时
    unwrap.ts            ★ 6 种成功形状 → 统一返回
    errors.ts            ★ 3 种错误形状 → ApiError{status,code,message,busy,retryAfterMs}
    endpoints/           按域分文件，每个导出类型化函数
  sse/
    client.ts            ★ fetch + ReadableStream 解析器（重连/退避/心跳）
    parse.ts             SSE 分帧（含无 event 名的裸 data 帧）
  auth/                  token 存储、过期判定
types/                   手写 DTO（★ 不从 OpenAPI 生成）
stores/                  zustand
```

★ = 本项目特有、不能省的收敛层。

> **路由约束（2026-08-12 实测）**：设备详情用 `/devices/detail?id=&tab=` 而非
> `/devices/[id]`。静态导出**不支持没有 `generateStaticParams()` 的动态路由**
> （见 `node_modules/next/dist/docs/01-app/02-guides/static-exports.md`），
> 而设备 ID 只有运行时才知道，无法在构建期枚举。
> 使用 `useSearchParams` 的页面必须包在 `<Suspense>` 内，否则构建报错。

---

## 4. 阶段拆分

### 阶段 0 — 契约梳理 ✅ 已完成

产出 [`frontend-api-matrix.md`](./frontend-api-matrix.md)：97 条实际端点、认证机制、
6 种响应形状、3 种错误形状、4 个 SSE 端点协议、游标分页、9 个生命周期状态、spec 偏差、
5 个后端遗留问题。

---

### 阶段 1 — 工程基建（剩余部分）

| ID | 任务 | 验收 | 状态 |
|----|------|------|------|
| P1-1 | Next + TS + Tailwind | 默认页 200 | ✅ |
| P1-3 | `/api` 开发代理 | 打到 7575 | ✅ |
| P1-6 | 构建链（静态导出 + 嵌入） | 三条构建链可跑 | ✅ |
| P1-2 | shadcn/ui：button/input/card/dialog/table/tabs/sonner/badge/skeleton | 组件可用 | 待做 |
| P1-4 | 亮/暗主题 token | 切换不闪白 | 待做 |
| P1-5 | `NEXT_PUBLIC_API_BASE` 约定写进 `web/README.md` | 新人可启动 | 待做 |

**工期**：0.5 天

---

### 阶段 2 — API 基础设施 ★ 新增，最关键

v0.3 无此阶段。契约的全部不一致都必须在这一层吃掉，**绝不能漏进业务组件**。

| ID | 任务 | 验收 |
|----|------|------|
| P2-1 | `client.ts`：注入 `Authorization`、超时、`X-Request-Id` 透传 | 单测覆盖 |
| P2-2 | `unwrap.ts`：识别 `{status:ok,...}` / `{devices}` / `{items}` / `{config}` / `{policies}` / 裸数组、裸对象 | 6 种形状各一条单测 |
| P2-3 | `errors.ts`：归一 `{status:error,...}` / `{error}` / `ESIM_BUSY`，产出统一 `ApiError` | 含 409 busy 字段解析 |
| P2-4 | 401 拦截：清 token + 跳登录（**含改密后失效场景**） | 手工验证 |
| P2-5 | SSE 薄封装：`EventSource(url+"?token=")`，订阅具名事件 + 无名 `message` 帧，接入 React 生命周期 | 断网由浏览器自动重连 |
| P2-6 | 手写核心 DTO 类型（device overview 38 字段 + `modem.DeviceStatus` 33 字段、sms、esim、proxy） | typecheck 通过 |
| P2-7 | TanStack Query provider + 默认重试策略（**409 ESIM_BUSY 特殊处理**） | 配置就位 |

**工期**：1.5–2 天（SSE 改用原生 EventSource 后较初版下调）。
**这仍是整个项目风险最集中的部分**，不要压缩。

---

### 阶段 3 — 登录与应用壳

| ID | 任务 | 验收 |
|----|------|------|
| P3-1 | 登录页（含 429 限流提示） | `admin` 可登录真实后端 |
| P3-2 | 路由守卫 + token 持久化 + `expires_at` 临期提示 | 刷新保持登录 |
| P3-3 | 侧边栏 6 菜单 + 退出 | 与既有信息架构一致 |
| P3-4 | 改密页 + **成功后强制重新登录** | 不出现「假登录」状态 |

**工期**：1 天

---

### 阶段 4 — 短信中心（业务最高优先，ADR-003）

| ID | 任务 | 验收 |
|----|------|------|
| P4-1 | 会话列表（`useInfiniteQuery` + 游标 `before_ts`/`before_peer`） | 滚动加载正常 |
| P4-2 | 会话详情（游标 `before_ts`/`before_id`，`peer` 必填） | 历史可回溯 |
| P4-3 | 发送短信 + 投递状态查询 | 成功有反馈 |
| P4-4 | 轮询刷新（无 SSE，contacts 10–15s） | 新短信可见 |
| P4-5 | 设备/ICCID 关系说明（换卡后历史跟 ICCID 走） | UI 有提示 |
| P4-6 | 删除单条 / 删除会话 | 二次确认 |

**工期**：2–3 天

---

### 阶段 5 — 设备列表与详情

| ID | 任务 | 验收 |
|----|------|------|
| P5-1 | 设备列表 + `device_limit` 展示 + 发现/添加/删除 | 与后端字段对齐 |
| P5-2 | **状态灯**：9 个 `lifecycle_phase` + 8 个布尔位的组合展示规则 | 状态可解释，无「未知」 |
| P5-3 | 详情壳 + 6 Tabs，切换不丢设备 | 路由正确 |
| P5-4 | 概览 Tab（注意取 `data.devices[0]`） | 字段齐全 |
| P5-5 | SSE 概览流（`overview`/`traffic`/`ussd` 三事件） | 断线重连 |
| P5-6 | 设备配置 Tab（GET config / PUT device） | 保存生效 |

**工期**：3–4 天。P5-2 需要先设计状态映射表，别边写边想。

---

### 阶段 6 — eSIM（复杂度最高）

| ID | 任务 | 验收 |
|----|------|------|
| P6-1 | Profile 列表 + EID + 芯片信息 | 只读先行 |
| P6-2 | **ESIM_BUSY 并发模式**：操作互斥、禁用入口、按 `retryAfterMs` 重试 | 并发点击不炸 |
| P6-3 | 下载（SSE 无事件名，`{step,msg,pct}` 进度条） | 进度可见 |
| P6-4 | 下载失败码展示（`code`/`details`，如空间不足） | 错误可读 |
| P6-5 | 切换 / 改名 / 删除（删除需展示 `warning`/`space_delta`） | 警告不吞 |

**工期**：3–4 天。P6-2 建议在 P6-1 之后立刻做，否则后续每个操作都要返工。

> **安全前置**：P6-3 涉及激活码经 URL query 传输（api-matrix §8-3）。开工前确认是否调整为 POST body。

---

### 阶段 7 — 仪表盘 / 卡策略 / AT / USSD

| ID | 任务 | 验收 |
|----|------|------|
| P7-1 | 仪表盘：系统信息 + 设备汇总 + 快捷入口 | 可用 |
| P7-2 | 卡策略（裸对象；不存在返回默认值而非 404） | 读写正常 |
| P7-3 | AT 终端（`{status:ok, response}`） | 回显正确 |
| P7-4 | **USSD 多轮会话**：send / continue / cancel + 会话态管理 | 多轮交互不串 |

**工期**：2 天

---

### 阶段 8 — 代理管理

| ID | 任务 | 验收 |
|----|------|------|
| P8-1 | 本机实例概览 + start/stop/restart | CRUD 可用 |
| P8-2 | 上游代理 + 探测 | 探测有反馈 |
| P8-3 | 国家规则 | 增删改 |

**工期**：2 天

---

### 阶段 9 — 日志、设置、流量图

| ID | 任务 | 验收 |
|----|------|------|
| P9-1 | 实时日志 SSE（`connected`/`log`，`level` 过滤） | 滚动 + 过滤 |
| P9-2 | 通知设置（**仅 webhook/bark/email 给测试按钮**） | 不给不存在的能力 |
| P9-3 | 系统更新 check/apply | 可用 |
| P9-4 | 流量图（Recharts，`range=day/week/month`） | 优先级最低 |

**工期**：2 天

---

### 阶段 10 — 打磨与交付

| ID | 任务 | 验收 |
|----|------|------|
| P10-1 | 空状态 / 错误边界 / 骨架屏 | 无设备无短信友好 |
| P10-2 | 敏感信息（IMEI/ICCID）显隐 | 默认打码 |
| P10-3 | 冒烟脚本：登录 → 列表 → 发短信 | 可重复执行 |
| P10-4 | `web/README.md` + 主 README 更新 | 新人可启动 |

**工期**：1–2 天

---

## 5. 工期汇总

| 阶段 | 工期 |
|------|------|
| 1 基建剩余 | 0.5 |
| **2 API 基础设施** | **1.5–2** |
| 3 登录与壳 | 1 |
| 4 短信 | 2–3 |
| 5 设备 | 3–4 |
| 6 eSIM | 3–4 |
| 7 仪表盘/策略/AT/USSD | 2 |
| 8 代理 | 2 |
| 9 日志/设置/流量 | 2 |
| 10 打磨 | 1–2 |
| **合计** | **约 18–23 人日** |

MVP（阶段 1–5，可登录 + 短信收发 + 设备列表详情）：**约 8.5–11 人日**。

---

## 6. 关键技术方案

### 6.1 SSE 封装（P2-5）

后端已支持白名单 `?token=`（2026-08-12），直接用原生 `EventSource`：

```
new EventSource(`${base}${path}?token=${token}`)
  → addEventListener("overview"|"traffic"|"ussd"|"log"|"operator_scan", ...)  具名事件
  → addEventListener("message", ...)                                          eSIM 下载（无 event: 行）
  → 重连、Last-Event-ID 由浏览器负责
  → onerror 仅用于展示连接状态，不要手动 close 后重建（会打断内建退避）
```

注意事项：

- token 进入 URL → **不要把流地址渲染成用户可见/可复制的链接**；后端访问日志已脱敏，
  但浏览器历史与 Referer 仍会留存
- `EventSource` 不会因 401 停止重连，需在 `onerror` 中结合一次性 `fetch` 探测判断是否已登出
- 组件卸载务必 `close()`，否则设备详情页切换会累积连接（后端有 `IncStreamSub` 计数）

### 6.2 响应归一化（P2-2）

```ts
unwrap(res):
  裸数组                       → res
  {status:"ok", ...rest}       → rest 中唯一有效载荷键，或 rest 本身
  {devices} {items} {policies} {config} → 对应值
  其它裸对象                    → res
```
每个 endpoint 函数显式声明期望形状，不做运行时猜测——猜测会在字段名撞车时静默出错。

### 6.3 eSIM 并发（P6-2）

全局 Zustand `esimBusy: Record<deviceId, {reason, until}>`：
- 任一 eSIM 请求返回 409 `ESIM_BUSY` → 记录 `until = now + retryAfterMs`
- 该设备所有 eSIM 操作入口在 `until` 前禁用并显示倒计时
- TanStack Query 对 409 不做默认重试，交由此机制统一调度

---

## 7. 风险

| 风险 | 影响 | 应对 |
|------|------|------|
| ~~SSE 自研客户端边界 bug~~ | — | **已消除**：后端支持白名单 `?token=`，改用原生 EventSource |
| token 经 URL 进入浏览器历史 | 凭证泄漏面扩大 | 仅白名单流式端点；访问日志已脱敏；流地址不渲染为可见链接 |
| eSIM 并发模型理解偏差 | 操作互相打断 | P6-2 前先用 curl 实测 409 行为 |
| 后端契约继续漂移 | 前端返工 | api-matrix 作为唯一依据，后端改动同步更新本文件 |
| 无旧 UI 参照 | 交互需重新设计 | 以后端能力为准，不追求还原历史 UI |
| Windows/WSL 下 USB 不可用 | 设备页无真实数据 | 需 Linux 或 WSL+usbipd；否则准备 mock 数据 |
| 后端 5 处遗留问题（api-matrix §8） | 可能影响前端设计 | 开工前确认第 1、2 条是否有意为之 |

---

## 8. 验收标准（MVP）

2026-08-13 已对**真实部署**（Docker + PostgreSQL，Go 嵌入托管前端）验证。

| # | 标准 | 状态 |
|---|------|------|
| 1 | `admin` 可登录真实后端，刷新保持登录，改密后要求重新登录 | ✅ 登录与刷新保持已验证；⬜ 改密未测（会使当前会话失效） |
| 2 | 六个一级菜单可进入 | ✅ 逐个点击验证 |
| 3 | 短信可收（游标分页）、可发、可查投递状态 | ⬜ **无法验证**：无真实模组，库中无短信 |
| 4 | 设备可列表、可看详情，9 种生命周期状态均有明确展示 | ✅ 列表与配额（0/5）正确；⬜ 详情与状态灯需真实设备 |
| 5 | 概览 SSE 实时刷新，断线可自动重连 | ✅ **SSE 链路已验证**（日志流实时收到 INFO/WARN，含 fields 与级别着色）；⬜ 概览流需真实设备 |
| 6 | eSIM 列表可用，并发冲突不产生错误状态 | ⬜ **无法验证**：需真实 eUICC |
| 7 | 镜像可 `docker compose up` 并访问 UI | ✅ 已部署并访问；`make ci` ❌ 仍被 KI-001 阻塞 |
| 8 | 计划 / 进度 / 决策 / API 矩阵四份文档同步 | ✅ |

**已验证**：镜像构建 → compose 起 postgres + vohive → `driver: postgres` 且 17 张表
AutoMigrate 完成 → `smoke-api.mjs` 全绿（含 SSE 探测）→ 浏览器访问 Go 托管的 UI，
登录、六菜单、仪表盘统计、日志实时流全部正常；访问日志中 token 已脱敏为 `***`。

**仍无法验证**：一切需要真实模组的功能（设备详情、eSIM、短信收发、概览流、USSD、选网）。
Windows/Docker Desktop 无 USB 透传，`QMI 硬件扫描失败：未发现调制解调器` 属预期。
这些需要在接有模组的 Linux 主机上继续。

---

## 9. 文档约定（不变）

| 文档 | 何时更新 |
|------|----------|
| 本文件 | 计划变更时升版本 |
| `frontend-react-progress.md` | **每个任务完成后追加** |
| `frontend-react-decisions.md` | 选型有分歧时加 ADR |
| `frontend-api-matrix.md` | **后端契约变动时必须同步** |
| `web/README.md` | 脚手架/环境变量变动时 |

---

## 10. 下一步

| # | 动作 | 状态 |
|---|------|------|
| 1 | 确认无鉴权端点 → `/system/uninstall` 已加鉴权；`/rotateip` 判定为有意设计 | ✅ 2026-08-12 |
| 2 | SSE 方案 → 后端加白名单 `?token=`，前端用原生 EventSource | ✅ 2026-08-12 |
| 3 | 阶段 1 剩余：shadcn/ui + 主题 | 待做 |
| 4 | **阶段 2：API 基础设施**（最关键） | 待做 |
| 5 | 阶段 3 登录与壳 → 阶段 4 短信 | 待做 |

---

**版本记录**

| 版本 | 日期 | 说明 |
|------|------|------|
| v0.1–v0.2 | 2026-08-11 | 初稿；确认栈与优先级 |
| v0.3 | 2026-08-12 | ADR-005 生产形态；构建链修复 |
| **v1.0** | 2026-08-12 | **基于源码实测契约重写**：新增阶段 2 API 基础设施；SSE 方案更正；禁用 OpenAPI 生成类型；补工期估算与关键技术方案 |
