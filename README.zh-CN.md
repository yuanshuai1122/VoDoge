# VoDoge

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go)](go.mod)
[![License](https://img.shields.io/badge/License-Proprietary-red.svg)](LICENSE)

[English](README.md) · **简体中文**

一台机器上管多根 USB 模组的 **短信中枢**：国内线、国外线分开，网页看会话、发短信、切 eSIM。

仓库：[github.com/yuanshuai1122/VoDoge](https://github.com/yuanshuai1122/VoDoge)

产品不是「全球一张卡」，也不是代理池。代理和 VoWiFi 有，但排在短信后面。不拉黑中国卡。

## 它做什么

| | |
|---|---|
| 多 USB 模组 | 按 IMEI 添加和重绑，不写死 `cdc-wdmN`（USB 重新枚举后节点名会变）。默认最多 5 台，设置里可调 1–10 |
| 国内线 / 国外线 | 设备上标 `lane=cn` 或 `intl`，人工分线，不按 MCC 推断 |
| 短信 | 按 ICCID 进会话；国外线已驻网走蜂窝，否则 IMS 补通路；国内线 VoWiFi 开着只走 IMS。全局每小时发送限额（默认 20） |
| eSIM | 列出 / 切换 / 禁用 / 下载当前卡上的 profile。写套餐用 USB 读卡器，模组只当「当前启用的那张 USIM」 |
| 读卡器 | `pcscd` 发现并写卡，无 CGO。读卡器与模组不能同时摸同一张卡 |
| 通知 | 新短信走 Telegram / Bark 等，不靠网页推送 |
| 个人入口 | 同一套 Web，可装成 PWA（要 HTTPS）。Service Worker 只缓存壳，不缓存 `/api`。顶栏切深色 / 浅色，中英界面 |
| 插件 | zip 安装侧栏页和本机后端。以管理员权限跑，只装信任的包 |

可选：按设备出站的 SOCKS/HTTP、按 ICCID 或国家绑前置代理（国内线仍直连模组出口）、本机自签 HTTPS、只放行内网打开管理面。

## 支持什么硬件

产品承诺只覆盖 Quectel **EC20 / EC25 / EG25** USB 成品。实验室里的 UFI103S **不进承诺**。

| 硬件 | 用途 | 软件 | 真机 |
|---|---|---|---|
| EC25-CN | 国内线短信 | 已通 | 要报到后再验收发 |
| EG25-G | 国外线短信 + IMS | 已通 | 棒子到了再验 |
| EC20 | 备用，蜂窝短信 | 已通 | 能管、能发即可 |
| USB CCID 读卡器 | 写 eSIM，并跑 VoWiFi / AKA | 已接 pcscd | 要读卡器 + `pcscd` |

数据面是 **QMI**，不是 RNDIS。一台进程。详见 [docs/hardware-support.md](docs/hardware-support.md)。

## 快速开始

一键安装（优先 Docker Compose，拉 GHCR 镜像 `ghcr.io/yuanshuai1122/vodoge`）：

```bash
curl -fsSL https://raw.githubusercontent.com/yuanshuai1122/VoDoge/main/scripts/install.sh | bash
```

本机已有仓库时：

```bash
cp config/config.example.yaml config/config.yaml
docker compose up -d
```

- 管理面：`http://127.0.0.1:7575`（默认账密见配置里的 `web`，登录后立刻改）
- Compose 自带 PostgreSQL，后端用 `VODOGE_DB_DSN` 连（也认 `DATABASE_URL`）
- 一键安装首次运行会生成 PostgreSQL 随机密码，并以 `0600` 权限保存在 `${VODOGE_DIR:-/opt/vodoge}/.postgres-credentials`；重跑安装器会继续使用原凭据
- 默认只放行内网访问；公网要先在设置里改网络策略，并上 HTTPS

没有可用的 PostgreSQL 时进程会退出。没有 SQLite。

### 本机开发

```bash
docker compose up -d postgres
go run ./cmd/vodoge -c config/config.yaml
npm install --prefix web && npm run dev --prefix web
```

前端 `:3000`，`/api/*` 反代到 `:7575`。后端没有全局 CORS，不要跨源直连。

生产二进制要先打前端再编 Go：

```bash
make frontend-dist
go build -o vodoge ./cmd/vodoge
```

### 验证

```bash
node scripts/smoke-api.mjs          # 登录 / 设备 / 短信 / 代理
bash scripts/ci.sh                  # 本机验证流水线；打 tag v* 会走 GitHub Actions 发版
bash scripts/testdb.sh ensure       # 一次性测试库
```

**不要**把 `TEST_DATABASE_URL` 指到正在跑业务的库，测试会清空所有表。

备份用 `pg_dump`：

```bash
docker exec vodoge-postgres pg_dump -U vodoge vodoge > vodoge-$(date +%F).sql
```

## 文档

| 文档 | 内容 |
|---|---|
| [docs/architecture.md](docs/architecture.md) | 架构总览：代码怎么摆、改东西去哪儿 |
| [docs/hardware-support.md](docs/hardware-support.md) | 模组、短信通道、读卡器、插件 |
| [docs/frontend-api-matrix.md](docs/frontend-api-matrix.md) | HTTP 契约 |
| [docs/pve-lab-deploy.md](docs/pve-lab-deploy.md) | PVE 实验室落点 |
| [docs/README.md](docs/README.md) | 其余索引 |

## 免责声明

- 涉及底层通信，硬件、资费和网络风险由使用者承担。
- 与 Quectel / 高通无官方关联。
- 遵守当地法律和运营商条款。禁止违法用途。
- 按现状提供，不作担保。

## License

Copyright (c) 2026 yuanshuai1122。专有软件，见 [LICENSE](LICENSE)。
