# VoHive 前端 React 重写 — 进度日志

---

### 2026-08-11 — 计划确认

- **状态**：done  
- **内容**：技术栈 Next+shadcn+TW；前后端分离；优先级 短信+eSIM > 代理 > 流量  
- **下一步**：阶段 0 API 矩阵 → 阶段 1 脚手架  

---

### 2026-08-11 — 版权与仓库归属

- **状态**：done  
- **内容**：模块路径改为 `github.com/yuanshuai1122/vohive`；LICENSE 更新为维护方专有声明  
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

**仍未完成（下一批）**

- 设备**添加**与已发现硬件列表（P5-1 只做了列表/删除/重扫）
- 上游代理新增/编辑表单（当前只有探测与删除）
- 上游代理国家路由规则（P8-3）
- 系统在线更新 check/apply（P9-3）
- eSIM 的 EID / 芯片信息 / 通知列表（P6-1 只做了 profiles）
- 设备配置 Tab 仍只读：`PUT /devices/:id` 的完整字段集未核对，贸然给表单有写坏配置的风险
- 通知设置的 weixin 渠道无表单：其 QR 接口在 OpenAPI 中声明但后端未实现

---

### 2026-08-12 — [阶段 0] API 契约梳理完成，计划重写为 v1.0

- **状态**：done
- **做法**：通读 `internal/api/`（路由注册、认证、SSE、各域 handler）、`internal/device/lifecycle.go`，
  并与 `openapi.vohive.yaml` 做端点级 diff。**未运行验证**（本机无 Go 工具链）。
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
