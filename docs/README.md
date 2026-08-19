# VoDoge 文档索引

维护仓库：https://github.com/yuanshuai1122/VoDoge  

## 当前架构与运维

| 文档 | 说明 |
|------|------|
| [../DEPLOY.md](../DEPLOY.md) | **当前部署与升级入口**：Linux Compose、二进制/systemd、持久 PostgreSQL 卷与反向代理 |
| [production-vm-ssh-tunnel.md](./production-vm-ssh-tunnel.md) | **NAT 后生产 VM**：systemd、最小权限 SSH 隧道、云端 Caddy 与验收 |
| [hardware-bringup-vmware.md](./hardware-bringup-vmware.md) | **VMware 虚机 USB 直通**：`.vmx` 默认全禁、`Unknown error` 要靠重插、`allowCCID` 是独立闸门、客户机验收链 |
| [architecture.md](./architecture.md) | **架构总览**：进程模型、分层、设备路径、代码地图、改动去哪儿、不变量 |
| [hardware-support.md](./hardware-support.md) | **支持硬件**：EC20/EC25/EG25、真短信、读卡器 APDU、IMS 出口、插件 |
| [frontend-api-matrix.md](./frontend-api-matrix.md) | **API 契约矩阵（接口唯一依据）** |
| [db-migrate-runbook.md](./db-migrate-runbook.md) | 旧 SQLite 数据导入 PostgreSQL 的运维手册 |
| [production-readiness-acceptance.md](./production-readiness-acceptance.md) | **生产就绪审阅与软件验收**：改动范围、自动化证据、真机待验清单 |
| [NOTICE.md](./NOTICE.md) | 版权声明 |

## 历史计划与实验室记录

| 文档 | 说明 |
|------|------|
| [pve-lab-deploy.md](./pve-lab-deploy.md) | **历史 PVE lab 快照**：VM 113、xHCI 直通、两根棒子台账与 2026-08-15 验收 |
| [ufi103s-qmi-host.md](./ufi103s-qmi-host.md) | UFI103S 原厂 QMI 接入实验与多棒部署边界 |
| [remaining-work.md](./remaining-work.md) | 历史剩余工作清单（P0-P4） |
| [known-issues.md](./known-issues.md) | 已解决的 UTF-8 损坏问题记录 |
| [frontend-react-migration-plan.md](./frontend-react-migration-plan.md) | 前端 React 重写计划 v1.0 |
| [frontend-react-decisions.md](./frontend-react-decisions.md) | 前端决策记录 |
| [frontend-react-progress.md](./frontend-react-progress.md) | 前端迁移进度记录 |
| [backend-sqlite-to-postgres-plan.md](./backend-sqlite-to-postgres-plan.md) | SQLite 到 PostgreSQL 的历史计划 |
| [backend-db-decisions.md](./backend-db-decisions.md) | 数据库决策记录 |
| [backend-db-progress.md](./backend-db-progress.md) | 数据库改造进度记录 |
| [backend-api-refactor-plan.md](./backend-api-refactor-plan.md) | `internal/api` 重构方案与验收记录 |
| [api-envelope-design.md](./api-envelope-design.md) | 响应信封方案与破坏性变更记录 |

## 已确认方向

- 前端：Next.js + shadcn/ui + Tailwind  
- 部署：**开发前后端分离**（`next dev` + rewrites）／**生产静态导出嵌入 Go 单镜像**（ADR-005）  
- 业务优先级：短信 + eSIM > 代理 > 流量图  
- 产品边界：国内线 / 国外线短信中枢（见 [hardware-support.md](./hardware-support.md)），不按 MCC 拉黑中国卡
- 数据库：**仅 PostgreSQL**（切断 SQLite 运行时）
