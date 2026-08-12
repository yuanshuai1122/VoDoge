# VoHive 文档索引

维护仓库：https://github.com/yuanshuai1122/vohive  

## 计划与决策

| 文档 | 说明 |
|------|------|
| [NOTICE.md](./NOTICE.md) | 版权声明 |
| [frontend-react-migration-plan.md](./frontend-react-migration-plan.md) | 前端 React 重写计划 |
| [frontend-react-decisions.md](./frontend-react-decisions.md) | 前端决策 |
| [frontend-react-progress.md](./frontend-react-progress.md) | 前端进度 |
| [backend-sqlite-to-postgres-plan.md](./backend-sqlite-to-postgres-plan.md) | SQLite → PostgreSQL 计划 |
| [backend-db-decisions.md](./backend-db-decisions.md) | 数据库决策 |
| [backend-db-progress.md](./backend-db-progress.md) | 数据库改造进度 |

## 已确认方向

- 前端：Next.js + shadcn/ui + Tailwind  
- 部署：**开发前后端分离**（`next dev` + rewrites）／**生产静态导出嵌入 Go 单镜像**（ADR-005）  
- 业务优先级：短信 + eSIM > 代理 > 流量图  
- 数据库：**仅 PostgreSQL**（切断 SQLite 运行时）
