# VoHive

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go)](go.mod)
[![License](https://img.shields.io/badge/License-Proprietary-red.svg)](LICENSE)

> 面向高通 4G/LTE/5G 模组（Quectel EC20/EC25/EC21/EG25/EM20 等）的综合管理与代理服务平台。

**维护仓库**：[github.com/yuanshuai1122/vohive](https://github.com/yuanshuai1122/vohive)

VoHive 把模组热插拔管理、SOCKS5/HTTP 代理编排、短信收发、VoWiFi/IMS 通话、eSIM 全生命周期管理整合到一个服务里，并提供 Web 管理后台。

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
- **Frontend**: 规划迁移至 Next.js + React + shadcn/ui（见 `docs/`）
- **Database**: **PostgreSQL only**（配置 `database.dsn` 或环境变量 `VOHIVE_DB_DSN` / `DATABASE_URL`）
- **CI/CD**: GitHub Actions

## 文档

见 [`docs/README.md`](docs/README.md)。

## 免责声明

- 本软件涉及底层通信操作，部署与使用风险由使用者自行承担。
- 与 Quectel / 高通等厂商无官方关联。
- 请遵守当地法律法规与运营商条款，禁止违法用途。
- 软件按「现状」提供，不作任何明示或暗示担保。

## License

Copyright (c) 2026 yuanshuai1122. 专有软件，详见 [LICENSE](LICENSE)。
