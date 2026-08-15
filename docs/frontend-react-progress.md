# VoDog 前端 React 重写 — 进度日志

---

### 2026-08-11 — 计划确认

- **状态**：done  
- **内容**：技术栈 Next+shadcn+TW；前后端分离；优先级 短信+eSIM > 代理 > 流量  
- **下一步**：阶段 0 API 矩阵 → 阶段 1 脚手架  

---

### 2026-08-11 — 版权与仓库归属

- **状态**：done  
- **内容**：模块路径改为 `github.com/yuanshuai1122/vodog`；LICENSE 更新为维护方专有声明  
- **业务实现**：未改 PG/React 功能代码  

---

### 2026-08-12 — [P1-1 / P1-3 部分] 构建链修复与形态定稿

- **状态**：done  
- **背景**：脚手架与仓库构建脚本不匹配，导致 `docker compose build` / `make build` / `make ci` 三条链路
  在 `cp web/dist` 步骤全部失败；`//go:embed all:dist` 使干净克隆 `go build ./...` 无法编译。
- **决策**：ADR-005（开发分离 / 生产静态导出嵌入单镜像）。
- **变更文件**：
  - `web/next.config.ts` — 生产 `output: "export"` + `distDir: "dist"`；开发 `/api/*` rewrites → `:7575`
  - `web/package.json` — 补 `typecheck` 脚本（`web-ci.yml` 一直在调用一个不存在的脚本）
  - `web/.gitignore` — 忽略 `/dist/`
  - `internal/web/dist/.gitkeep` — 新增占位并纳入版本控制，保证干净克隆可编译
  - `.gitignore` — `/internal/web/dist/*` + `.gitkeep` 例外；解除对 `docs/*` 的忽略
  - `Makefile` / `scripts/ci.sh` — 拷贝产物后 `touch .gitkeep`，避免构建弄脏工作树
  - `internal/api/server.go` — 缓存头补 `_next/static/`（原仅识别 Vite 的 `assets/`）；`/debug/embed` 探测文件改为 Next 约定
- **命令/验证**：
  - `npm run typecheck --prefix web` ✅
  - `npm run build --prefix web` ✅ → 产出 `web/dist/index.html` + `_next/`
  - `git add -An` 确认仅 `.gitkeep` 入库、前端产物全部被忽略 ✅
  - Go 侧编译**未验证**（本机无 Go 工具链）
- **备注/风险**：前端为纯静态 SPA，不可用 Server Actions / Route Handlers / middleware / ISR。

---

### 2026-08-13 — 部署到 Docker，发现并修复 3 个阻塞问题

- **状态**：部署成功。`docker compose -f docker-compose.windows.yml` 起 postgres + vohive，
  日志确认 `数据库已初始化 {"driver": "postgres"}`。**这是该仓库第一次真正以 PostgreSQL 运行**。
- **冒烟**：`scripts/smoke-api.mjs` **全部通过**，含 `日志流 SSE — 已建立事件流`。

**① 镜像自 `0868b32` 起就构建不了（KI-001）**

5 个 `_test.go` 存在 UTF-8 损坏，`go build` 加载包时即失败。详见
[`known-issues.md`](./known-issues.md)。已在 `.dockerignore` 排除测试文件让镜像可构建；
**损坏本身未修**，需要原作者确认 65 处被吞掉的文案。

**② PostgreSQL 迁移不彻底，服务起不来（已修）**

```
初始化数据库失败: iccid rekey migration:
ERROR: function pragma_table_info(unknown) does not exist (SQLSTATE 42883)
```

`internal/db/iccid_rekey_migration.go` 仍在用 SQLite 专有的 `pragma_table_info()`，
导致容器无限重启。已改为 `tx.Migrator().HasColumn()`（方言中立）。
这一项 `backend-db-progress.md` 曾标记为「已完成」，实际有遗漏。

**③ Go 静态托管与 Next 静态导出不兼容，生产页面白屏（已修）**

Next 静态导出为每个路由同时产出 `login.html` 和一个同名目录 `login/`（内含框架文件）。
原 `handleStatic` 按请求路径打开 `/login` 会命中**目录**，进而回退到 `index.html`——
浏览器拿到根路由的 HTML 而地址栏是 `/login`，页面白屏。
现在会先尝试 `<route>.html`，未命中才做 SPA 回退。已补 `static_route_test.go`。

**④ Next dev 的 rewrite 代理会缓冲 SSE（未修，属开发期限制）**

`next dev` 下经 `/api` 代理访问 `/logs/stream`，`fetch` 与 `EventSource` 都收不到任何数据
（status 200、content-type 正确，但 body 始终为空）；直连后端则正常收到
`event:connected` 与 `event:log`。

**不影响生产**：生产是静态导出 + Go 同源托管，不经过该代理。
开发期需要调试 SSE 时，直连后端并临时放开 CORS。

---

### 2026-08-13 — 首次真实后端联调（部分）

- **状态**：进行中
- **环境**：Docker Desktop 启动后自动恢复了一个**旧容器**（镜像构建于 2026-08-11，
  早于 PostgreSQL 迁移，日志显示仍在用 `data/vohive.db`）。它占用 7575 端口且可用，
  因此先拿它验证了 API 契约；新镜像构建中。

**`scripts/smoke-api.mjs` 对真实服务的结果**

| 检查 | 结果 |
|------|------|
| 登录 | ✅ 返回 token 与 `expires_at` |
| 未授权请求被拒绝 | ✅ 确实返回 401 |
| 系统信息 / 设备列表 / 短信会话 / 代理概览 / 通知设置 | ✅ 响应形状与 api-matrix 一致 |
| 日志流 SSE | ❌ 401 —— **预期内**：旧镜像没有 `?token=` 白名单（正是本次后端改动） |

**由此确认**：`unwrap.ts` 对 `{devices}` / `{items}` / 裸数组等形状的处理**未触发任何异常**，
说明这些形状判断是对的。这是这些 DTO 假设第一次得到真实响应验证。

**新发现（已修）**：`expires_at` 实际形如 `2026-09-12T09:52:33+08:00`，
是**带偏移的本地时间**而非 UTC `Z`。`new Date()` 能正确解析，前端无需改动，但值得记录。

**脚本自身的 bug（已修）**：SSE 检查用 `res.body.cancel()` 关闭不会结束的流，
导致进程退出时在 Windows 上命中 libuv 断言
`!(handle->flags & UV_HANDLE_CLOSING)`。改为 `AbortController.abort()`，
并用 `process.exitCode` 代替 `process.exit()` 让 Node 自行清理。

---

### 2026-08-12 — [阶段 1–9] 前端实现

- **状态**：主体完成（**未经真实后端联调**，本机无 Go 工具链，无法启动服务端）
- **验证**：`npm run typecheck` / `npx eslint --quiet` / `npm run build` 全部通过，9 个路由静态预渲染

| 阶段 | 内容 | 状态 |
|------|------|------|
| 1 | shadcn/ui（Base UI 版）17 组件、主题、系统字体栈 | ✅ |
| 2 | API 基础设施：unwrap / errors / client / SSE / 类型 / Query | ✅ |
| 3 | 登录、路由守卫、侧边栏壳、改密（强制重登） | ✅ |
| 4 | 短信中心：游标分页、发送、删除、轮询 | ✅ |
| 5 | 设备列表、状态模型、详情壳、概览 + SSE | ✅ |
| 6 | eSIM：列表、下载(SSE)、切换、改名、删除、ESIM_BUSY 互斥 | ✅ |
| 7 | AT 终端、USSD 多轮会话、卡策略；设备配置**只读** | ✅ |
| 8 | 代理：实例启停重启、上游代理探测/删除 | ✅ |
| 9 | 实时日志(SSE)、通知设置 8 渠道 | ✅ |
| 9 | 流量图（P9-4）：色板经校验后替换灰阶 token | ✅ |
| 10 | 错误边界、敏感信息显隐、冒烟脚本、README | ✅ |

**实现中发现并修正的契约问题**

1. **AT 的请求字段是 `cmd`，USSD 是 `command`** —— 两者不一致，初版写错了 AT，已修。
2. **静态导出不支持动态路由** —— 设备详情改为 `/devices/detail?id=`，信息架构已同步。
3. **eSIM `GetProfiles` 返回按 eUICC 分组的裸数组**（`[]EUICCProfiles`），不是扁平 profile 列表。
4. **上游代理列表接口已对密码脱敏**（`maskSecret`），前端不能把该值回写。

5. **Next 16 的错误边界回调是 `retry` 而非 `reset`** —— 与既有认知不同，已按本地文档更正。
6. **shadcn 默认 `--chart-1..5` 是灰阶**（chroma 0）且 light/dark 同值，多序列无法辨识；
   已替换为经校验的分类色板（两种表面对比度 ≥3:1，最差相邻对 CVD ΔE 10.1）。

**补齐批次（同日完成）**

- 设备添加：走「发现列表 → 选择」而非手填 `usb_path`/`at_port`；degraded（探不到 IMEI）
  与已配置项不可选；`started=false + warning` 的 200 响应会如实提示
- eSIM 通知队列（含重试）与 eUICC 芯片信息
- 上游代理新增/编辑：**编辑时密码框留空表示不修改**——列表接口返回的是脱敏占位值，
  原样回写会把真实密码覆盖成星号
- 上游代理国家路由规则（P8-3），含 MCC/MNC 表未就绪（503）的可读提示
- 运营商选择 Tab（新增）：当前选网、SSE 流式扫描、锁定/恢复自动；forbidden 与
  current 的候选不可锁定
- 系统信息 + 在线更新 check/apply（P9-3），apply 有二次确认

**仍未完成（有意保留）**

- 设备配置 Tab 只读：`PUT /devices/:id` 的完整字段集未核对，贸然给表单有写坏配置的风险
- 通知设置的 weixin 渠道无表单：其 QR 接口在 OpenAPI 中声明但后端未实现
- VoWiFi 重连与 E911 websheet 入口未做（websheet 需代理运营商页面，交互形态待定）
- 代理实例的新增/编辑（后端为整体 `PUT /proxy-instances/config`，需先确认字段语义）

---

### 2026-08-12 — [阶段 0] API 契约梳理完成，计划重写为 v1.0

- **状态**：done
- **做法**：通读 `internal/api/`（路由注册、认证、SSE、各域 handler）、`internal/device/lifecycle.go`，
  并与 `openapi.vodog.yaml` 做端点级 diff。**未运行验证**（本机无 Go 工具链）。
- **产出**：`docs/frontend-api-matrix.md` — 97 条实际端点、认证机制、响应/错误形状、
  4 个 SSE 协议、游标分页、9 个生命周期状态、spec 偏差、5 个后端遗留问题。
- **推翻的三个旧假设**：
  1. **不能以 OpenAPI 为准** — spec 缺 17 条真实端点，且含 3 条不存在的端点（微信 QR 配置）。
     照 spec 生成类型会同时漏功能和生成死代码。
  2. **原生 EventSource 不可用** — token 仅从 `Authorization` 头读取，无 query 回退，
     而 EventSource 设不了请求头。4 个流式端点全部需要 fetch-based 自研客户端。
  3. **API 归一化是主要工作量** — 108 个成功响应中仅 47 个用 `{status:"ok"}` 包装，
     成功形状 6 种、错误形状 3 种，且 `retryAfterMs` 为 camelCase。
- **计划变更**：`frontend-react-migration-plan.md` 重写为 **v1.0**，新增
  **阶段 2「API 基础设施」**（2–3 天，v0.3 里仅是 P2-1 一个条目），并给出全量工期估算
  （MVP 约 9–12 人日，全量约 19–24 人日）。
- **顺带修正**：v0.3 遗留的 §10 与 §11「下一步」自相矛盾，重写时已统一。
- **待决**：① 两个无鉴权端点（`/system/uninstall`、`/rotateip`）是否有意设计；
  ② SSE 走自研客户端还是推动后端加 `?token=` 回退。
  → **均已在下一条中解决。**

---

### 2026-08-12 — 后端配合改造：卸载端点鉴权 + SSE token 回退

- **状态**：done（**未编译验证**，本机无 Go 工具链）
- **决策来源**：用户确认「① 漏了 ② 按你的来」。

**① `/api/system/uninstall` 补鉴权**

原注册在 `authMiddleware` 之前，handler 进来即执行自毁（停服务 + 删数据目录 + 删配置），
**任何能访问端口者都可触发**。已移入鉴权组。

**更正**：同时提出的 `/api/rotateip` **判断有误**——其 handler 首行即调用 `authorizeRotate`，
支持 Bearer 或 username/password 双模式（POST-only + 复用登录限流），是给外部脚本的有意设计。
**未改动**，若移入 `authMiddleware` 会打断既有集成。已在 server.go 补注释说明，避免后人再次误判。

**② SSE 支持 `?token=`（白名单）**

`requestSessionToken` 增加 query 回退，但**仅对 4 个流式路由开放**
（`sseTokenQueryRoutes`）：logs/stream、overview/stream、operator_selection/scan/stream、
esim/actions/download。`Authorization` 头优先；白名单外端点继续拒绝 query 凭证。

配套：`gin.Default()` → `gin.New()` + `LoggerWithFormatter(accessLogFormatter)` + `Recovery()`，
对访问日志中的 `token=` 做脱敏。gin 默认 Logger 会把整个查询串写入 stdout，
在容器/journal 下会直接落盘 token。

- **变更文件**：`internal/api/server.go`、`internal/api/auth_sse_token_test.go`（新增 6 个用例：
  白名单内外的 query 回退、header 优先级、日志脱敏、其它 query 参数不被误伤）
- **文档同步**：`frontend-api-matrix.md` §0/§1.3/§3/§5.8/§8；
  `frontend-react-migration-plan.md` 选型表、P2-5、§6.1、工期、风险表、§10
- **前端影响**：P2-5 从「自研 fetch+ReadableStream 解析器」降级为「原生 EventSource 薄封装」，
  阶段 2 工期 2–3 天 → 1.5–2 天，总工期 19–24 → 18–23 人日
- **遗留**（api-matrix §8，均不阻塞前端）：eSIM 激活码仍走 GET query；
  `/api/docs` 免鉴权但其 spec 需鉴权导致页面空白；`/api/health` 需鉴权与"外部监控用"注释矛盾

---

### 2026-08-12 — CI 与配置样例补齐

- **状态**：done  
- **变更文件**：
  - `.github/workflows/ci.yml` — 新增 postgres:16 service + `TEST_DATABASE_URL`
  - `scripts/ci.sh` — 测试包列表补 `./internal/db ./internal/api`（此前 PG 改造无任何 CI 覆盖）
  - `config/config.example.yaml` — 新增（`.gitignore` 早有例外规则，但文件从未存在）
  - `cmd/vohive/main.go` — 删除 SQLite 遗留死代码 `migrateLegacyServerDB` 及随之失效的 `fmt` / `path/filepath` import
- **备注**：CI 效果需推送后在 Actions 上确认。

---
