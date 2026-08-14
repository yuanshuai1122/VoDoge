# VoHive 文档索引

维护仓库：https://github.com/yuanshuai1122/vohive  

## 计划与决策

| 文档 | 说明 |
|------|------|
| [NOTICE.md](./NOTICE.md) | 版权声明 |
| [remaining-work.md](./remaining-work.md) | **剩余工作清单（P0–P4）** |
| [known-issues.md](./known-issues.md) | 已知问题（KI-001：UTF-8 损坏阻塞 CI） |
| [frontend-api-matrix.md](./frontend-api-matrix.md) | **API 契约矩阵（接口唯一依据）** |
| [frontend-react-migration-plan.md](./frontend-react-migration-plan.md) | 前端 React 重写计划 v1.0 |
| [frontend-react-decisions.md](./frontend-react-decisions.md) | 前端决策 |
| [frontend-react-progress.md](./frontend-react-progress.md) | 前端进度 |
| [backend-sqlite-to-postgres-plan.md](./backend-sqlite-to-postgres-plan.md) | SQLite → PostgreSQL 计划 |
| [backend-db-decisions.md](./backend-db-decisions.md) | 数据库决策 |
| [backend-db-progress.md](./backend-db-progress.md) | 数据库改造进度 |
| [db-migrate-runbook.md](./db-migrate-runbook.md) | 旧 SQLite 数据导入 PostgreSQL 的运维手册 |
| [hardware-bringup-windows.md](./hardware-bringup-windows.md) | **Windows + WSL2 硬件联调环境**：自建内核、host 网络陷阱、棒子识别结果 |
| [ufi103s-qmi-host.md](./ufi103s-qmi-host.md) | **UFI103S 原厂 QMI 接入**：单根验收、Debian 主机与多棒部署边界 |
| [backend-api-refactor-plan.md](./backend-api-refactor-plan.md) | `internal/api` 重构方案与验收记录（路由表 / 统一错误 / 分域拆文件 / repository 层） |
| [api-envelope-design.md](./api-envelope-design.md) | **响应结构统一方案**（`{data, meta, request_id}`），含破坏性变更清单 |

## 已确认方向

- 前端：Next.js + shadcn/ui + Tailwind  
- 部署：**开发前后端分离**（`next dev` + rewrites）／**生产静态导出嵌入 Go 单镜像**（ADR-005）  
- 业务优先级：短信 + eSIM > 代理 > 流量图  
- 数据库：**仅 PostgreSQL**（切断 SQLite 运行时）
