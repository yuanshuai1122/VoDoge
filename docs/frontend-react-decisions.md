# VoDoge 前端 React 重写 — 决策记录（ADR）

> 与 `docs/frontend-react-migration-plan.md` 配套。有新决策时在此追加，勿覆盖历史条目。

---

### ADR-001 前端技术栈

- **日期**：2026-08-11  
- **状态**：已确认  
- **背景**：Vue 3 + Element Plus 需要现代化重写，需选定 React 方案。  
- **决策**：采用 **Next.js（App Router）+ TypeScript + Tailwind CSS + shadcn/ui**。  
- **配套**：**TanStack Query**（服务端状态）、**Zustand**（客户端状态）、**Lucide**（图标）。  
- **备选**：Vite + React SPA（更轻，但路由/工程化弱于 Next）。  
- **后果**：需熟悉 App Router；开发与部署与纯 SPA 略有不同。

---

### ADR-002 部署形态：前后端分离

- **日期**：2026-08-11  
- **状态**：**部分被 ADR-005 修订**（开发期分离维持不变；生产形态改为静态导出嵌入）  
- **背景**：原 Vue 构建后嵌入 Go（`internal/web/dist`）。可选继续嵌入或独立前端服务。  
- **决策**：**前后端分离**。  
  - 后端：Go API，默认 `http://<host>:7575`  
  - 前端：独立 Next 服务（开发 `next dev`，生产 `next start` 或容器单独部署）  
  - 浏览器通过 CORS 或反向代理访问 API  
- **备选**：静态导出嵌入 Go（运维更简单，但与 SSE/动态能力可能冲突）。  
- **后果**：  
  1. 需配置 CORS（后端）或网关统一域名  
  2. 需明确 `NEXT_PUBLIC_API_BASE`（或同域反代）  
  3. Docker 至少两个进程/服务（或 compose 双服务）  
  4. 不再以「拷贝到 internal/web/dist」为第一目标  

---

### ADR-003 功能实现优先级

- **日期**：2026-08-11  
- **状态**：已确认  
- **背景**：菜单多、设备子功能深，需排期。业务场景为 eSIM 短信 Hub。  
- **决策**优先级：  
  1. **短信中心**  
  2. **设备管理 + eSIM**  
  3. 登录 / 壳 / 仪表盘（基础设施，与 1–2 并行前置）  
  4. **代理管理**（次于短信/eSIM）  
  5. **流量分析等图表**（最后）  
  6. 日志、设置、打磨  
- **一句话**：**短信 + eSIM > 代理 > 流量图**  
- **后果**：阶段实施顺序调整为：脚手架 → 登录壳 → 短信 + 设备/eSIM → 仪表盘补齐 → 代理 → 日志设置 → 交付。

---

### ADR-005 生产形态修订：静态导出 + 嵌入单镜像

- **日期**：2026-08-12  
- **状态**：已确认（修订 ADR-002 的生产部分）  
- **背景**：ADR-002 定的「生产独立 Next 服务」与仓库现状冲突——`Dockerfile`、`Makefile`、`scripts/ci.sh`
  共 6 处仍按 `web/dist` → `internal/web/dist` 的嵌入链路组织，而 `create-next-app` 脚手架
  默认产出 `.next`，导致 `docker compose build`、`make build`、`make ci` 全部在拷贝步骤失败；
  同时 `internal/web/fs.go` 的 `//go:embed all:dist` 让干净克隆连 `go build ./...` 都编译不过。
- **决策**：
  - **开发期**：维持前后端分离。`next dev`（:3000）+ `next.config.ts` rewrites 把 `/api/*` 反代到 Go（:7575），无 CORS 负担。
  - **生产期**：`next build` 以 `output: "export"` + `distDir: "dist"` 静态导出到 `web/dist`，
    由构建脚本拷入 `internal/web/dist` 供 Go 嵌入，**交付仍为单镜像**。
- **备选**：双容器（前端独立 Next 服务）。否决理由：本项目是运维控制台，不需要 SSR/SEO；
  单镜像对 GHCR 最终用户友好得多（`DOCKERHUB.md` 的部署说明保持一条 `docker compose up`）。
- **后果**：
  1. 前端为纯静态 SPA：**不可用** Server Actions、Route Handlers、middleware、ISR、`next/image` 优化服务。
  2. **不受影响**：SSE（EventSource 由客户端发起，流在 Go 侧）、TanStack Query、Zustand、shadcn/ui、React Hook Form —— ADR-001 选型无一项需要调整。
  3. Go 侧 `handleStatic` 已有 SPA fallback，静态导出的多 HTML 产物可直接托管；缓存头已补 `_next/static/` 前缀。
  4. `internal/web/dist/.gitkeep` 纳入版本控制，保证干净克隆可编译。
  5. 若将来确需 SSR，退回双容器方案的成本仅限于构建与部署配置，不涉及业务代码。

---

### ADR-004（预留）API 访问方式

- **日期**：待定  
- **状态**：待实现时确认  
- **选项**：  
  A. 前端直连 `http://host:7575`，后端开 CORS  
  B. 前端同源，Nginx/Caddy 反代 `/api` → 后端  
- **倾向**：开发期用 Next rewrites；生产推荐 B（同域，少 CORS 麻烦）。

---

## 变更索引

| ID | 标题 | 日期 |
|----|------|------|
| ADR-001 | 前端技术栈 | 2026-08-11 |
| ADR-002 | 前后端分离（生产部分已被 ADR-005 修订） | 2026-08-11 |
| ADR-003 | 功能优先级 | 2026-08-11 |
| ADR-004 | API 访问方式 | 预留 |
| ADR-005 | 生产形态：静态导出 + 嵌入单镜像 | 2026-08-12 |
