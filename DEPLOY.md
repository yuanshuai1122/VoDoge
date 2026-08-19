# VoDoge 部署与升级

面向高通 4G/LTE/5G 模组（Quectel EC20/EC25/EC21/EG25/EM20 等）的综合管理与代理服务平台。

> **本版本需要 PostgreSQL。** 服务不再内置文件数据库；未提供可用的数据库连接串时会直接退出。
> 从 v0.x 的 SQLite 版本升级时，旧的 `data/vohive.db` **不会自动迁移**。

## 选择部署路径

- **Linux 硬件宿主机，具备 Docker Compose**：使用下文的 Compose 流程。安装器检测到
  `docker compose` 时也会选择这条路径。
- **没有 Docker Compose 的 Linux VM，或 PostgreSQL 已由外部服务提供**：使用发行二进制和
  `systemd`。标准路径为 `/usr/local/bin/vodoge`、`/etc/vodoge/config.yaml`、
  `/etc/vodoge/vodoge.env` 与 `vodoge.service`；二进制安装需要在执行安装器前提供
  `VODOGE_DB_DSN`。
- **位于 NAT 后的生产 VM，云端负责 PostgreSQL 与 HTTPS 入口**：请使用
  [生产 VM SSH 隧道运行手册](docs/production-vm-ssh-tunnel.md)。它记录了已验证的
  `systemd`、Caddy 与最小权限 SSH 隧道拓扑。

## 升级已有 PostgreSQL 卷

`POSTGRES_USER`、`POSTGRES_PASSWORD` 和 `POSTGRES_DB` 只在 PostgreSQL 数据目录首次初始化时
生效。若旧部署的持久卷仍使用 `vohive` 或 `vodog` 的角色/数据库，直接修改 Compose 环境变量
并启动新版本不会迁移这些对象，应用会因认证或数据库不存在而退出。

升级前先执行 `pg_dump` 并核对现有角色、数据库与连接串；不要用 `docker compose down -v`
解决启动问题，`-v` 会删除业务数据库卷。旧 SQLite 文件迁入 PostgreSQL 是另一条流程，见
[db-migrate-runbook.md](docs/db-migrate-runbook.md)。

## 🚀 快速开始（Linux Compose）

### 1. 准备目录

```bash
mkdir -p vodoge/{config,data,logs}
cd vodoge
```

### 2. 创建配置文件

```bash
cat > config/config.yaml << 'EOF'
server:
  port: 7575
  debug: false

web:
  username: admin
  # 首次登录后请在 Web 界面修改密码
  password: admin123
EOF
```

> 数据库连接串通过环境变量 `VODOGE_DB_DSN` 注入（见下方 compose），
> 优先级高于配置文件里的 `database.dsn`。

### 3. 启动

创建 `docker-compose.yml`：

```yaml
services:
  postgres:
    image: postgres:16-alpine
    container_name: vodoge-postgres
    restart: unless-stopped
    environment:
      POSTGRES_USER: vodoge
      POSTGRES_PASSWORD: ${VODOGE_POSTGRES_PASSWORD:-vodoge}
      POSTGRES_DB: vodoge
    volumes:
      - pgdata:/var/lib/postgresql/data
    ports:
      - "127.0.0.1:5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U vodoge -d vodoge"]
      interval: 5s
      timeout: 5s
      retries: 10

  vodoge:
    image: ghcr.io/yuanshuai1122/vodoge:latest
    container_name: vodoge
    restart: unless-stopped
    network_mode: host
    privileged: true
    volumes:
      - ./config:/app/config
      - ./data:/app/data
      - ./logs:/app/logs
      # USB 模组透传
      - /dev:/dev
      # 可选：CCID/eSIM 使用宿主 pcscd
      - /run/pcscd:/run/pcscd
    environment:
      - TZ=Asia/Shanghai
      - CONFIG_PATH=/app/config/config.yaml
      - VODOGE_DB_DSN=host=127.0.0.1 user=vodoge password=${VODOGE_POSTGRES_PASSWORD:-vodoge} dbname=vodoge port=5432 sslmode=disable TimeZone=UTC
      # 可选：访问 Telegram 等外部服务的代理
      # - HTTPS_PROXY=http://proxy-ip:port
    depends_on:
      postgres:
        condition: service_healthy

volumes:
  pgdata:
```

```bash
docker compose up -d
```

> `network_mode: host` 下容器与宿主共享网络，因此 DSN 用 `127.0.0.1`。
> PostgreSQL 只发布到宿主机回环地址；Linux Compose 与一键安装器的生产部署请在 `.env` 中设置
> `VODOGE_POSTGRES_PASSWORD`，不要使用默认值。
> Windows/Docker Desktop 不使用本段的 host 网络 Compose。当前仓库的
> `docker-compose.windows.yml` 仅用于本地管理面验证：它只发布 `7575`，并固定 PostgreSQL
> 凭据，不能承载插件运行时或生产密码轮换。完整生产部署请使用 Linux host-network Compose，
> 或先自行完善 Windows Compose。

容器镜像已经包含 CS 语音所需的 `arecord`/`aplay`。宿主机仍需提供 ALSA 声卡设备；
如需 CCID/eSIM，还需安装并启动 `pcscd`，并把 `/run/pcscd` 挂载到容器（仓库自带的
`docker-compose.yml` 和一键安装器已包含该挂载）。直接使用发行二进制/systemd 时，宿主机需
自行安装 `alsa-utils`；安装器会在缺少 `alsa-utils` 或 `pcscd` socket 时给出告警，但不会
因这些可选硬件能力缺失而阻断 SMS-only 部署。

### 4. 访问

浏览器打开 `http://YOUR_IP:7575`，默认账号 `admin` / `admin123`。

### 5. 公网 HTTPS 反向代理

插件运行时必须与管理面使用不同 origin。由 Caddy、Nginx 等反向代理终止 TLS 时，
在 `config/config.yaml` 中显式填写浏览器实际访问的两个 origin：

```yaml
server:
  port: 7575
  plugin_port: 7576
  public_url: https://vodoge.example.com
  plugin_public_url: https://plugins.vodoge.example.com
```

这里只接受 `http` 或 `https` origin，不得包含用户名、密码、路径（包括尾斜杠）、
查询参数或片段；两个 origin 不能相同。反向代理要分别回源两个监听端口，例如：

```caddyfile
vodoge.example.com {
    reverse_proxy 127.0.0.1:7575
}

plugins.vodoge.example.com {
    reverse_proxy 127.0.0.1:7576
}
```

显式 origin 会决定插件启动地址以及 capability Cookie 的 `Secure` 属性，避免 HTTPS
在代理处终止后生成 HTTP iframe 或非 Secure Cookie。只有无法显式配置 origin、服务又只能
经可信代理访问时，才设置 `server.access.trust_proxy_headers: true` 以读取标准
`Forwarded` 或 `X-Forwarded-Proto` / `X-Forwarded-Host`；不要在可被客户端直连的服务上
信任这些请求头。

## 📦 构建镜像

预构建镜像：`ghcr.io/yuanshuai1122/vodoge`（打 `v*` 标签由 GitHub Actions 推送）。

本机构建：

```bash
docker build -t vodoge:latest .
```

一键安装：

```bash
curl -fsSL https://raw.githubusercontent.com/yuanshuai1122/VoDoge/main/scripts/install.sh | bash
```

完整的本地流水线（编码检查、路由校验、前端构建、编译、测试、镜像）：

```bash
bash scripts/ci.sh
```

## 🔧 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `VODOGE_DB_DSN` | — | PostgreSQL 连接串的最高优先级覆盖 |
| `DATABASE_URL` | — | `VODOGE_DB_DSN` 未设置时的备选 |
| `VODOGE_POSTGRES_PASSWORD` | `vodoge` | Compose 使用的 PostgreSQL 密码；生产部署必须覆盖 |
| `CONFIG_PATH` | `/app/config/config.yaml` | 配置文件路径 |
| `TZ` | `UTC` | 时区 |
| `HTTPS_PROXY` | — | 外部服务代理（如 Telegram API） |

`VODOGE_DB_DSN`、`DATABASE_URL` 和配置文件中的 `database.dsn` 按以上顺序取第一个非空值；
三者至少一个必须能连上 PostgreSQL，否则进程会退出。

## 📁 数据卷

| 路径 | 说明 |
|------|------|
| `/app/config` | 配置文件 |
| `/app/data` | 运行期缓存（如 MCC/MNC 表）；**业务数据在 PostgreSQL** |
| `/app/logs` | 日志文件 |

## 💾 备份

业务数据全部在 PostgreSQL，备份方式是 `pg_dump`，不再是拷贝数据库文件：

```bash
docker exec vodoge-postgres pg_dump -U vodoge vodoge > vodoge-$(date +%F).sql
```

恢复：

```bash
cat vodoge-2026-01-01.sql | docker exec -i vodoge-postgres psql -U vodoge -d vodoge
```

## 🤖 Telegram Bot

支持通过 Telegram Bot 远程管理设备，在 Web 界面 **系统设置 → 通知** 中配置。

1. 通过 [@BotFather](https://t.me/BotFather) 创建 Bot，获取 Token
2. 获取你的 Chat ID（可通过 [@userinfobot](https://t.me/userinfobot) 查询）
3. 在设置页填入 Bot Token 与 Chat ID
4. 若服务器无法直连 Telegram，填写 TG API 代理地址

### 支持的命令

| 命令 | 说明 |
|------|------|
| `/list` | 列出设备列表 |
| `/rotate <设备>` | 重置设备 IP |
| `/sms <设备>` | 查看最近短信 |
| `/send <设备> <号码> <内容>` | 发送短信 |

## 🖥 支持架构

| 架构 | 状态 |
|------|------|
| `linux/amd64` | ✅ 默认构建目标 |
| `linux/arm64` | ✅ 已验证交叉构建 |
| `linux/arm/v7` | ✅ 已验证交叉构建 |

本地交叉构建：

```bash
bash scripts/ci.sh multiarch
```

前端与 Go 编译都固定在构建机架构上跑，用 Go 自己的交叉编译产出目标二进制，
不经 QEMU 模拟——每个架构约 1.5 分钟，模拟的话是十几分钟。

> 本地 `multiarch` 任务只构建、不推送；正式 `v*` Release 会向 GHCR 发布
> `linux/amd64`、`linux/arm64` 和 `linux/arm/v7` 镜像。

## ⚠️ 注意事项

- **设备透传**：管理模组需要访问 USB 设备，因此需要 `privileged` 与 `/dev` 挂载。
  Windows/macOS 的 Docker Desktop 默认没有 USB 透传。
  **Windows 上的推荐做法是跑一台 Linux 虚机、把 USB 设备直通进去**，
  见 [docs/hardware-bringup-vmware.md](docs/hardware-bringup-vmware.md)：普通 Ubuntu 内核
  自带 QMI/MBIM/AT 驱动，随插随载。
  WSL2 + usbipd 那条路已弃用——微软的默认 WSL2 内核一个 WWAN 驱动都没编进去，
  设备扫不到且不报错，走通要自建内核。
- **端口**：`network_mode: host` 时无需映射；否则需自行暴露管理 API `7575` 和
  隔离的插件运行时 `7576`。
  注意 Docker Desktop 的 host 网络是 `docker-desktop` 那个 WSL 发行版的命名空间，**不是** Windows 主机，
  端口从主机访问不到；Windows 上请用仓库里的 `docker-compose.windows.yml` 做本地管理面验证。
  它当前只发布 `7575`，不能作为插件或公网生产部署配置。

## 🔄 升级：API 响应结构变更（2026-08-14，破坏性）

**只影响直接调 HTTP API 的外部脚本。** Web 界面随镜像一同更新，无需你做任何事。

所有 JSON 响应改成了同一个信封：

```jsonc
// 成功（2xx）——原先约 60 种形状
{ "data": <载荷，可为 null>, "meta": { ... }, "request_id": "9f2c…" }

// 失败（4xx/5xx）
{ "error": { "code": "...", "message": "...", "details": { ... } }, "request_id": "9f2c…" }
```

如果你有脚本在调用本服务，按下表调整：

| 原先 | 现在 |
|------|------|
| `resp.token` | `resp.data.token` |
| `resp.devices` | `resp.data` |
| `resp.message` | `resp.meta.message` |
| `resp.warning` / `requires_restart` / `started` | `resp.meta.*` |
| `resp.status === "ok"` | 判 HTTP 状态码即可（或 `"error" in resp`） |
| `resp.message`（错误时） | `resp.error.message` |
| `resp.code`（错误时） | `resp.error.code` |
| `resp.retryAfterMs` | `resp.error.details.retry_after_ms` |

三个端点的行为也变了：

- `GET /api/devices/{id}/overview` 返回**单个设备对象**（原先是 `{devices:[单元素]}`）；
  设备不存在时返回 **404**（原先是空数组）。
- `GET /api/health` **恒返回 200**（原先有设备不健康时返回 503），
  判据改为 `data.healthy`。**按状态码判活请改用免鉴权的 `GET /ping`**，它未改变。
- eSIM 并发冲突的 `retryAfterMs` 已删除，只保留 `retry_after_ms`。

**不受影响**：`GET /ping`、所有 SSE 事件帧、websheet 的承载页与代理通道。

完整说明见 `docs/api-envelope-design.md` 与 `docs/frontend-api-matrix.md` §2。

## 📖 文档

完整文档见 [GitHub](https://github.com/yuanshuai1122/VoDoge)。

## 📝 License

Proprietary，详见 LICENSE。
