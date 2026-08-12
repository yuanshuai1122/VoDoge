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

### 2026-08-12 — CI 与配置样例补齐

- **状态**：done  
- **变更文件**：
  - `.github/workflows/ci.yml` — 新增 postgres:16 service + `TEST_DATABASE_URL`
  - `scripts/ci.sh` — 测试包列表补 `./internal/db ./internal/api`（此前 PG 改造无任何 CI 覆盖）
  - `config/config.example.yaml` — 新增（`.gitignore` 早有例外规则，但文件从未存在）
  - `cmd/vohive/main.go` — 删除 SQLite 遗留死代码 `migrateLegacyServerDB` 及随之失效的 `fmt` / `path/filepath` import
- **备注**：CI 效果需推送后在 Actions 上确认。

---
