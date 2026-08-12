# VoHive 前端 React 重写计划

> 状态：**仅计划，未开始业务实现**  
> 创建日期：2026-08-11  
> 目标：用现代 React 栈重写 Web 管理台，后端 API 保持兼容，逐步替换现有 Vue 前端。

---

## 1. 背景与目标

### 1.1 现状

| 项 | 内容 |
|----|------|
| 当前前端 | Vue 3 + Vite + Pinia + Element Plus + Tailwind |
| 入口 | 构建产物嵌入 Go 后端（`internal/web/dist`） |
| 后端 API | Gin，`/api/*`，Bearer Token 鉴权 |
| 主菜单 | 6 个：仪表盘 / 设备管理 / 代理管理 / 短信中心 / 实时日志 / 系统设置 |
| 设备详情子页 | 概览 / eSIM / AT / USSD / 卡策略 / 配置 |

### 1.2 目标

1. **技术栈现代化**：React 生态，开发体验与可维护性更好。  
2. **视觉现代化**：深色/浅色、清晰信息架构，适合运维控制台。  
3. **功能对等优先**：先覆盖现有菜单与核心流程，再谈增强。  
4. **后端不动或少动**：优先兼容现有 OpenAPI / JSON 契约。  
5. **可嵌入部署**：最终仍可 `npm run build` 产出静态资源，由 Go 托管，或开发期独立 `next dev` + 代理。

### 1.3 非目标（本阶段不做）

- 不重写 Go 后端业务逻辑  
- 不先做移动端 App  
- 不顺带大改代理/VoWiFi 协议  
- 不追求第一期就 100% 像素级还原 Vue 旧 UI  

---

## 2. 推荐技术选型（已定）

| 层级 | 选型 | 说明 |
|------|------|------|
| 框架 | **Next.js（App Router）+ TypeScript** | 现代路由、布局、工程化成熟 |
| UI | **shadcn/ui** | 源码级组件，可定制，观感现代 |
| 样式 | **Tailwind CSS** | 与 shadcn 天然配合 |
| 图标 | **Lucide React** | 与 shadcn 生态一致 |
| 请求 | **TanStack Query（React Query）** | 列表/轮询/缓存/重试 |
| 客户端状态 | **Zustand** | 登录态、主题、局部 UI 状态 |
| 表单 | **React Hook Form + Zod** | 配置表单、登录、设置 |
| 图表 | **Recharts** 或 **ECharts for React** | 流量分析（二选一，实现期定） |
| 实时 | 原生 **EventSource / fetch stream** 封装 | 对齐现有 SSE：概览流、日志流、eSIM 下载进度 |
| 包管理 | npm（与仓库现状一致） | 不强制 pnpm，除非后续统一 |

### 2.1 部署形态（2026-08-12 修订：开发分离 / 生产嵌入）

> **2026-08-11 用户确认**：开发期前后端分离（ADR-002）。  
> **2026-08-12 用户确认**：生产期改为静态导出嵌入 Go 单镜像（ADR-005，修订 ADR-002 的生产部分）。

**开发**（不变）

- 前端：`next dev`（`:3000`）
- 后端：Go API（`:7575`）
- 联调：`next.config.ts` 的 `rewrites` 代理 `/api/*` → 后端（已落地），无需 CORS

**生产**

- `next build` 以 `output: "export"` + `distDir: "dist"` 静态导出到 `web/dist`
- 构建脚本拷入 `internal/web/dist`，由 Go 嵌入托管（`handleStatic` 已有 SPA fallback）
- **交付物：单个镜像**，`docker compose up` 一条命令起 postgres + vohive
- **约束**：前端为纯静态 SPA，不可用 Server Actions / Route Handlers / middleware / ISR；
  SSE 与 ADR-001 选定的全部客户端库均不受影响

---

## 3. 信息架构（菜单与页面）

与现网保持一致的一级导航：

```
/login                 登录
/                      仪表盘
/devices               设备管理
/devices/[id]          设备详情（Tabs）
  - overview           概览 + 流量
  - esim               eSIM
  - at                 AT 终端
  - ussd               USSD
  - card-policy        卡策略
  - config             配置
/proxy                 代理管理（本机实例 + 上游代理）
/sms                   短信中心
/logs                  实时日志
/settings              系统设置（密码 / 通知渠道）
```

---

## 4. 目录结构规划（目标）

```
web/                          # 新前端根目录（React）
  app/
    (auth)/login/page.tsx
    (app)/layout.tsx          # 侧边栏壳
    (app)/page.tsx            # 仪表盘
    (app)/devices/...
    (app)/proxy/page.tsx
    (app)/sms/page.tsx
    (app)/logs/page.tsx
    (app)/settings/page.tsx
    layout.tsx
    globals.css
  components/
    ui/                       # shadcn
    layout/                   # Sidebar, Header
    devices/
    sms/
    proxy/
    settings/
  lib/
    api/                      # fetch 封装、类型
    auth/                     # token 存储与拦截
    sse/                      # EventSource 封装
    utils.ts
  hooks/
  stores/                     # zustand
  types/                      # 从 OpenAPI 生成或手写 DTO
docs/
  frontend-react-migration-plan.md   # 本文件
  frontend-react-progress.md         # 进度日志（实现时维护）
  frontend-react-decisions.md        # 决策记录 ADR（实现时维护）
```

> 说明：若仓库里仍保留旧 Vue 代码，建议临时目录 `web-vue-legacy/` 或 git 分支保留，避免直接覆盖后无法对照。  
> **本计划阶段不执行目录搬迁。**

---

## 5. 任务拆分（按阶段）

### 阶段 0：准备与基线（文档 + 对照）

| ID | 任务 | 产出 | 验收 |
|----|------|------|------|
| P0-1 | 锁定 API 契约清单 | 从 `openapi.vohive.yaml` 导出关键路径表 | 覆盖登录、设备、短信、代理、设置、日志 |
| P0-2 | 旧前端功能对照表 | 菜单/按钮/接口矩阵 | 每个菜单至少列出主操作 |
| P0-3 | 选定构建与嵌入方案 | ADR：static export 或 standalone | 能说明 Go/Docker 如何接 |
| P0-4 | 建立进度文档模板 | `docs/frontend-react-progress.md` | 每完成一步追加一条 |

**本阶段不改业务代码。**

---

### 阶段 1：工程脚手架

| ID | 任务 | 产出 | 验收 | 状态 |
|----|------|------|------|------|
| P1-1 | 初始化 Next.js + TS + Tailwind | 可 `npm run dev` | 默认页 200 | ✅ 2026-08-12 |
| P1-2 | 接入 shadcn/ui 基础组件 | button/input/card/dialog/table/tabs/sonner | 文档页可展示 | 待做 |
| P1-3 | 配置 `/api` 开发代理 | `next.config` rewrites | 浏览器打到 7575 | ✅ 2026-08-12 |
| P1-4 | 主题（亮/暗）与基础 layout token | CSS 变量 + Tailwind | 切换主题不闪白 | 待做 |
| P1-5 | 环境变量约定 | `NEXT_PUBLIC_API_BASE` 等 | README 小节 | 部分（已用于 rewrites，README 未写） |
| P1-6 | 构建链打通（新增） | 静态导出 + 嵌入，`make ci` 可跑 | 三条构建链不再断 | ✅ 2026-08-12 |

**文档要求**：完成后在 progress 中记录命令、版本号、截图或路径。

---

### 阶段 2：认证与壳子

| ID | 任务 | 产出 | 验收 |
|----|------|------|------|
| P2-1 | API Client + Token 存储 | `lib/api` + localStorage | 登录拿 token |
| P2-2 | 登录页 | 用户名/密码/错误提示 | `admin` 可登录现网后端 |
| P2-3 | 路由守卫 | 未登录跳登录 | 刷新后 token 仍有效 |
| P2-4 | 认证布局壳 | 侧边栏 6 菜单 + 退出 | 与现菜单一致 |
| P2-5 | 改密 API | 设置页最小能力 | 成功后可重新登录 |

**关键接口**：

- `POST /api/auth/login`
- `POST /api/settings/password`（或现网实际路径）
- 401 统一清 token 并回登录

---

### 阶段 3：仪表盘

| ID | 任务 | 产出 | 验收 |
|----|------|------|------|
| P3-1 | 系统信息卡片 | version/build/config | 对上 `/api/system/info` |
| P3-2 | 设备汇总 | 在线数/总数 | 对上设备列表摘要 |
| P3-3 | 快捷入口 | 跳转设备/短信/代理 | 可点 |

---

### 阶段 4：设备管理（核心，工作量最大）

| ID | 任务 | 产出 | 验收 |
|----|------|------|------|
| P4-1 | 设备列表 + 发现/添加 | 列表、状态灯、搜索 | 列表与旧版字段对齐 |
| P4-2 | 设备详情壳 + Tabs | 6 子页签 | 切换不丢选中设备 |
| P4-3 | 概览 Tab | 信号/IMEI/ICCID/IP/状态 | 与 overview API 对齐 |
| P4-4 | SSE 概览流 | 实时刷新 | 断线重连 |
| P4-5 | 流量分析面板 | 日/周/月图 | 数据接口对齐 |
| P4-6 | eSIM Tab | 列表/下载/切换/改名/删除 | 含错误码提示（空间不足等） |
| P4-7 | eSIM 下载 SSE 进度 | 进度条 | 可取消或展示失败原因 |
| P4-8 | AT 终端 | 发送/回显/模板 | 与 AT session API 对齐 |
| P4-9 | USSD 终端 | 发送/会话 | 与 USSD API 对齐 |
| P4-10 | 卡策略 | 读写策略 | `GET/PUT /api/cards/:iccid/policy` |
| P4-11 | 设备配置 | 保存/校验 | `GET/PUT` 设备 config |
| P4-12 | 运营商选择/扫描 | 若旧版有 | 流式扫描可用 |

**优先级建议**：P4-1 → P4-2 → P4-3 → P4-6 → P4-11 → 其余。  
**eSIM + 短信**是你业务场景（短信 Hub）的高优路径。

---

### 阶段 5：短信中心（业务高优）

| ID | 任务 | 产出 | 验收 |
|----|------|------|------|
| P5-1 | 收件箱列表 | 按设备/ICCID 过滤 | 分页或滚动加载 |
| P5-2 | 短信详情 | 正文/时间/号码 | 可读 |
| P5-3 | 发送短信 | 选设备 + 号码 + 内容 | 发送成功有反馈 |
| P5-4 | 实时/轮询刷新 | Query interval 或 SSE | 新短信可见 |

---

### 阶段 6：代理管理

| ID | 任务 | 产出 | 验收 |
|----|------|------|------|
| P6-1 | 本机代理实例列表 | SOCKS5/HTTP | CRUD 基本可用 |
| P6-2 | 上游代理 | 列表/探测 | 与现 API 对齐 |
| P6-3 | 绑定设备关系展示 | 设备-代理映射 | 可读可改（按旧能力） |

---

### 阶段 7：日志与设置

| ID | 任务 | 产出 | 验收 |
|----|------|------|------|
| P7-1 | 实时日志 SSE | 滚动、过滤、下载 | 对齐 `/logs/stream` |
| P7-2 | 通知设置 | TG/飞书/QQ/Webhook/Bark/Email/PushPlus | 保存 + 测试按钮 |
| P7-3 | 其他系统设置 | 按 OpenAPI 补齐 | 不回归登录 |

---

### 阶段 8：打磨、嵌入、交付

| ID | 任务 | 产出 | 验收 |
|----|------|------|------|
| P8-1 | 错误边界与空状态 | Empty/Error 组件 | 无设备/无短信友好 |
| P8-2 | 敏感信息模糊 | IMEI/ICCID 显隐 | 对齐旧行为 |
| P8-3 | 构建嵌入 Go | Dockerfile/Makefile 更新 | 一键镜像可用 |
| P8-4 | 基础 E2E/冒烟 | 登录 + 列表 + 发短信脚本 | CI 可跑或本地脚本 |
| P8-5 | 清理旧 Vue | 删除或归档 | README 更新 |
| P8-6 | 使用说明 | `web/README.md` + 主 README 前端小节 | 新人可启动 |

---

## 6. 文档落盘约定（强制）

每一步实现都必须更新文档，不只写代码。

| 文档 | 用途 | 何时更新 |
|------|------|----------|
| `docs/frontend-react-migration-plan.md` | 总计划（本文件） | 计划变更时改版本说明 |
| `docs/frontend-react-progress.md` | 进度日志 | **每个任务完成后追加** |
| `docs/frontend-react-decisions.md` | 决策 ADR | 选型/方案有分歧时 |
| `docs/frontend-api-matrix.md` | 页面-接口对照 | 阶段 0 建立，阶段中增量 |
| `web/README.md` | 前端开发说明 | 脚手架完成后建立 |

### progress 条目模板

```markdown
### YYYY-MM-DD — [任务ID] 标题
- 状态：done / blocked
- 变更文件：
- 命令/验证：
- 备注/风险：
```

### ADR 条目模板

```markdown
### ADR-00X 标题
- 背景：
- 决策：
- 备选：
- 后果：
```

---

## 7. 风险与应对

| 风险 | 影响 | 应对 |
|------|------|------|
| ~~Next 静态导出与 SSE 不兼容~~ | — | **已排除（2026-08-12）**：SSE 由客户端 `EventSource` 发起，流在 Go 侧，静态导出不影响。静态导出真正的限制是 Server Actions / Route Handlers / middleware / ISR，本项目均不需要。见 ADR-005 |
| OpenAPI 与真实 API 不一致 | 联调返工 | 以浏览器 Network + 旧 Vue service 为准补齐 |
| eSIM/AT 交互复杂 | 周期拉长 | 先列表/只读，再写操作 |
| Windows Docker USB | 功能像空壳 | 文档标明需 Linux/WSL+usbipd；前端仍可 mock |
| 旧 Vue 已部分被脚手架替换 | 对照困难 | 从 git 历史 / 远程仓库恢复 `web-vue-legacy` 对照（实现阶段处理） |

---

## 8. 建议实施顺序（落地时）

```
阶段0 文档基线
  → 阶段1 脚手架
  → 阶段2 登录+壳
  → 阶段5 短信（业务优先）与 阶段4 设备列表/eSIM 并行
  → 阶段3 仪表盘
  → 阶段6 代理
  → 阶段7 日志设置
  → 阶段8 嵌入交付
```

对你「eSIM 短信 Hub」场景，**短信 + 设备/eSIM** 优先于代理与流量图。

### 8.1 已确认实施顺序（2026-08-11）

用户确认优先级：**短信 + eSIM > 代理 > 流量图**（ADR-003）。

落地顺序调整为：

```
阶段0 文档基线（API 矩阵、功能对照）
  → 阶段1 脚手架（Next + shadcn + Tailwind）
  → 阶段2 登录 + 布局壳
  → 阶段5 短信中心  ∩  阶段4 设备列表 + eSIM   （业务并行，高优）
  → 阶段3 仪表盘补齐
  → 阶段6 代理管理
  → 阶段4 余量（AT/USSD/流量图等）
  → 阶段7 日志 / 设置
  → 阶段8 前后端分离部署交付（compose 双服务、CORS/反代）
```

---

## 9. 验收总标准（MVP）

MVP 视为完成，当且仅当：

1. 可用 `admin` 登录现有后端  
2. 六个一级菜单可进入  
3. 设备可列表、可看详情概览  
4. eSIM 至少能列表 + 切换或下载之一  
5. 短信可收、可发  
6. **前后端分离**部署可运行（前端独立服务 + 后端 API）  
7. 计划/进度/决策文档齐全  

---

## 10. 当前状态（2026-08-11）

| 项 | 状态 |
|----|------|
| 本计划文档 | **v0.3** |
| 用户确认 | ① 栈=Next+shadcn+TW **是** ② 开发分离 / **生产嵌入单镜像**（ADR-005 修订 ADR-002） ③ 优先级 **短信+eSIM>代理>流量** |
| 决策文档 | `docs/frontend-react-decisions.md`（ADR-001～005） |
| 进度日志 | `docs/frontend-react-progress.md` |
| 脚手架与构建链 | **已就位**：静态导出 → `web/dist` → `internal/web/dist` 嵌入；`typecheck` / `build` 本地通过 |
| 业务代码改动 | **尚未开始**（`app/` 仍是 create-next-app 默认页） |
| 下一动作 | 阶段 0：API 矩阵 + 旧功能对照；然后 P1-2 shadcn/ui + P1-4 主题 |

> 注：`internal/web/dist/.gitkeep` 为编译期占位，勿删；前端产物本身不入库。

---

## 11. 下一步（已确认，待开工）

| # | 动作 | 文档 |
|---|------|------|
| 1 | 阶段 0：梳理 `openapi` + 旧 service，写 API 矩阵 | `docs/frontend-api-matrix.md` |
| 2 | 阶段 1：脚手架（前后端分离配置、CORS/代理说明） | progress 追加 |
| 3 | 阶段 2：登录 + 壳 | progress 追加 |
| 4 | 阶段 5 + 4 高优：短信 / 设备+eSIM | progress 追加 |

你说「开始实现」后再动代码；当前仅文档已更新。

---

**文档版本**：v0.2  
**作者**：迁移计划  
**变更记录**：

| 版本 | 日期 | 说明 |
|------|------|------|
| v0.1 | 2026-08-11 | 初稿：选型、阶段拆分、文档约定、验收标准 |
| v0.2 | 2026-08-11 | 确认：前后端分离、优先级、更新部署与实施顺序 |
| v0.3 | 2026-08-12 | ADR-005 修订生产形态为静态导出嵌入；构建链修复；阶段 1 状态更新；排除 SSE 风险条目 |
