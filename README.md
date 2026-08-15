# VoDog

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go)](go.mod)
[![License](https://img.shields.io/badge/License-Proprietary-red.svg)](LICENSE)

> 面向高通 4G/LTE/5G 模组（Quectel EC20/EC25/EC21/EG25/EM20 等）的综合管理与代理服务平台。

**维护仓库**：[github.com/yuanshuai1122/VoDog](https://github.com/yuanshuai1122/VoDog)

VoDog 把模组热插拔管理、SOCKS5/HTTP 代理编排、短信收发、VoWiFi/IMS 通话、eSIM 全生命周期管理整合到一个服务里，并提供 Web 管理后台。

## 核心特性

| 模块 | 说明 |
| --- | --- |
| 多模组并发管理 | USB 热插拔自动发现、多设备实时状态监控 |
| 轻量级代理引擎 | 内建 SOCKS5 / HTTP，支持按设备网卡绑定出站 |
| 通信与短信中心 | 短信收发、会话与联系人、USSD，短信落库可查 |
| eSIM 管理 | Profile 下载、启用/停用、重命名、删除 |
| 全渠道通知 | Telegram、Email、PushPlus、Bark、飞书、QQ 等 |
| 多架构构建 | amd64 / arm64 / arm7 |

## 典型应用场景

- 私有 IP 代理池（多卡多实例）
- 统一接码 / 验证码中心
- VoWiFi 弱信号场景通信

## 技术栈

- **Backend**: Go 1.26+（Gin、GORM、Viper 等）
- **Frontend**: Next.js 16 + React 19 + Tailwind（迁移中，见 `docs/`）
- **Database**: **PostgreSQL only**（配置 `database.dsn` 或环境变量 `VOHIVE_DB_DSN` / `DATABASE_URL`）
- **构建**: 全部在本机完成，不依赖任何 CI 服务（`bash scripts/ci.sh`）

## 快速开始

```bash
cp config/config.example.yaml config/config.yaml
docker compose up -d
```

Compose 自带 PostgreSQL；后端默认监听 `:7575`。

### 本地开发

前端独立起服务、`/api/*` 自动反代到后端（后端无全局 CORS，必须走反代）：

```bash
npm install --prefix web && npm run dev --prefix web
```

生产构建时 `next build` 会静态导出到 `web/dist`，再由 `make frontend-dist` / Dockerfile 拷入
`internal/web/dist` 供 Go 嵌入——**先构建前端，`go build ./cmd/vodog` 才能产出带 UI 的二进制**。

详见 [`web/README.md`](web/README.md)。

### 冒烟检查

针对运行中的服务跑一遍 API 主路径（登录 / 设备 / 短信 / 代理 / SSE）：

```bash
node scripts/smoke-api.mjs
```

## 本地流水线

本项目不使用 CI 服务，构建与验证都在本机跑：

```bash
bash scripts/ci.sh
```

可单独执行某一环节，例如 `bash scripts/ci.sh routes`。
完整任务列表见 `bash scripts/ci.sh --help`。

测试用一次性数据库，**不要**把 `TEST_DATABASE_URL` 指向正在服务的实例——
测试会清空目标库所有表（见 [docs/known-issues.md](docs/known-issues.md) KI-002）：

```bash
bash scripts/testdb.sh ensure   # 起一个独立测试库
bash scripts/testdb.sh stop     # 用完删掉
```

### 备份

业务数据全部在 PostgreSQL，备份用 `pg_dump`，不再是拷贝数据库文件：

```bash
docker exec vohive-postgres pg_dump -U vohive vohive > vohive-$(date +%F).sql
```

## 文档

见 [`docs/README.md`](docs/README.md)。

## 免责声明

- 本软件涉及底层通信操作，部署与使用风险由使用者自行承担。
- 与 Quectel / 高通等厂商无官方关联。
- 请遵守当地法律法规与运营商条款，禁止违法用途。
- 软件按「现状」提供，不作任何明示或暗示担保。

## License

Copyright (c) 2026 yuanshuai1122. 专有软件，详见 [LICENSE](LICENSE)。
