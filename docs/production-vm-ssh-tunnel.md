# NAT 后生产 VM：systemd + SSH 隧道

最后验证：2026-08-19。

本文是当前生产拓扑的运行手册，适用于 VoDoge 硬件 VM 位于 NAT 后、无法稳定接收
WireGuard UDP 回包，而云服务器负责 PostgreSQL 和 HTTPS 入口的场景。它不替代通用
[DEPLOY.md](../DEPLOY.md)：有本机 PostgreSQL 且网络直接可达时，优先使用标准 Linux
Compose 部署。

不在本文记录主机 IP、密码、私钥、公钥或数据库 DSN 全文。以下名称均为占位符，须替换为
自己的值。

## 拓扑与边界

```text
Internet
  |
  v
Caddy container on cloud
  |  Docker bridge gateway :17575 / :17576
  v
reverse SSH tunnel <------------------------- production VM
                                                 VoDoge :7575 / :7576

production VM 127.0.0.1:15432
  |  local SSH tunnel
  v
PostgreSQL private endpoint on cloud :5432
```

- 管理面和插件运行时必须分属两个 origin，并分别回源 `7575`、`7576`。
- PostgreSQL 不向公网发布；VM 仅连接本机 `127.0.0.1:15432`。
- 两条 SSH 隧道分别使用独立的云端账户和密钥，且云端 `authorized_keys` 仅允许所需的
  转发目标或监听端口。
- WireGuard 可以保留给其他私网流量，但没有最新握手时不能作为这条业务链路的可用性依据。

本文使用以下占位符：

| 占位符 | 含义 |
|---|---|
| `CLOUD_SSH_HOST` | 云端 SSH 主机名或地址 |
| `DB_PRIVATE_HOST` | 云端 PostgreSQL 的私网可达地址 |
| `APP_LISTEN_HOST` | VM 上 VoDoge 实际绑定的私网地址 |
| `CADDY_BRIDGE_GATEWAY` | Caddy 容器所属 Docker 网桥的宿主机网关地址 |
| `CADDY_CONTAINER_IP` | Caddy 容器在该网桥上的地址 |
| `BRIDGE_INTERFACE` | 该 Docker 网桥对应的宿主机接口 |

## VM 上的 VoDoge

发行二进制/systemd 的标准位置如下：

| 路径或服务 | 用途 |
|---|---|
| `/usr/local/bin/vodoge` | VoDoge 二进制 |
| `/etc/vodoge/config.yaml` | 主配置，权限应为 `0600` |
| `/etc/vodoge/vodoge.env` | 数据库环境变量，权限应为 `0600` |
| `/var/lib/vodoge` | systemd 工作目录；文件日志位于其下的 `logs/` |
| `vodoge.service` | 应用 systemd 单元 |

配置中显式声明两个 HTTPS origin。`trust_proxy_headers` 只有在应用端口不能被不可信客户端
直接访问，且请求均由 Caddy 转发时才可开启：

```yaml
server:
  port: "APP_LISTEN_HOST:7575"
  plugin_port: "APP_LISTEN_HOST:7576"
  public_url: "https://vodoge.example.com"
  plugin_public_url: "https://plugins.vodoge.example.com"
  debug: false
  access:
    mode: public
    trust_proxy_headers: true
```

数据库连接串放在 `/etc/vodoge/vodoge.env`，令应用只连接本地转发端口。不要把真实值打印到
终端、日志或提交到仓库：

```ini
VODOGE_DB_DSN="host=127.0.0.1 user=vodoge password=<secret> dbname=vodoge port=15432 sslmode=disable TimeZone=UTC"
```

`vodoge.service` 必须读取该环境文件，并以 `-c` 明确指定配置文件；当前二进制以 `-c` 为准，
单独修改 `CONFIG_PATH` 环境变量不会改变读取路径。修改 `config.yaml` 或 `vodoge.env` 后重启
服务；只有修改 unit 文件后才执行 `daemon-reload`。

```bash
sudo systemctl restart vodoge
sudo journalctl -u vodoge -n 200 --no-pager
curl -fsS --max-time 5 "http://APP_LISTEN_HOST:7575/ping"
```

若应用绑定的是 `wg0` 上的地址，保持 `wg-quick@wg0.service` 在应用之前启动即可；接口存在
不代表 WireGuard 对端已经握手，业务健康仍以本页的 HTTP 与 SSH 隧道检查为准。

## VM 到 PostgreSQL 的本地转发

在 VM 创建 `/etc/vodoge/ssh/`，目录权限 `0700`，私钥权限 `0600`，并固定云端主机密钥到
`known_hosts`。不要使用 `StrictHostKeyChecking=no`。

`/etc/systemd/system/vodoge-db-tunnel.service` 的核心配置如下，实际单元可按站点补充账号或
审计字段：

```ini
[Unit]
Description=VoDoge PostgreSQL SSH tunnel
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
ExecStart=/usr/bin/ssh -N -i /etc/vodoge/ssh/db-tunnel_ed25519 -o BatchMode=yes -o ExitOnForwardFailure=yes -o IdentitiesOnly=yes -o ServerAliveInterval=15 -o ServerAliveCountMax=3 -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/etc/vodoge/ssh/known_hosts -L 127.0.0.1:15432:DB_PRIVATE_HOST:5432 vodoge-db-tunnel@CLOUD_SSH_HOST
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

应用单元应依赖该隧道，避免数据库尚未就绪时反复启动：

```ini
# /etc/systemd/system/vodoge.service.d/db-tunnel.conf
[Unit]
Requires=vodoge-db-tunnel.service
After=vodoge-db-tunnel.service
```

云端 `vodoge-db-tunnel` 账户不需要 shell。其 `authorized_keys` 对应条目应使用
`permitopen="DB_PRIVATE_HOST:5432"`，并禁用 PTY、X11、agent forwarding 与 user rc。数据库
端还必须仅允许云端实际发起连接的源地址通过 PostgreSQL HBA；隧道不会让数据库看到 VM 的
原始地址。

## 云端到 VM 的反向转发

这条隧道由 VM 主动连出，云端在 Docker 网桥网关上监听两个仅供 Caddy 使用的端口：

```ini
[Unit]
Description=VoDoge API reverse SSH tunnel
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
ExecStart=/usr/bin/ssh -N -i /etc/vodoge/ssh/api-tunnel_ed25519 -o BatchMode=yes -o ExitOnForwardFailure=yes -o IdentitiesOnly=yes -o ServerAliveInterval=15 -o ServerAliveCountMax=3 -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/etc/vodoge/ssh/known_hosts -R CADDY_BRIDGE_GATEWAY:17575:APP_LISTEN_HOST:7575 -R CADDY_BRIDGE_GATEWAY:17576:APP_LISTEN_HOST:7576 vodoge-api-tunnel@CLOUD_SSH_HOST
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

云端 SSH 配置只对该账户允许指定绑定地址：

```text
Match User vodoge-api-tunnel
    GatewayPorts clientspecified
```

相应公钥仅应拥有 `permitlisten="CADDY_BRIDGE_GATEWAY:17575"` 和
`permitlisten="CADDY_BRIDGE_GATEWAY:17576"`，并同样禁用交互会话能力。变更 SSH 配置前先
执行 `sshd -t`，再 reload SSH 服务。

## Caddy、Docker 网桥与防火墙

Caddy 在 Docker 容器中时，容器的 `127.0.0.1` 不是云端宿主机的回环地址。因此 Caddy 必须
反代到 Docker 网桥上的宿主机网关。先确认实际网络，不要照抄某一台机器的 IP：

```bash
docker network inspect <caddy-network>
docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' <caddy-container>
ip -br addr show BRIDGE_INTERFACE
```

Caddyfile 中两个 upstream 对应反向转发端口：

```caddyfile
vodoge.example.com {
    reverse_proxy CADDY_BRIDGE_GATEWAY:17575
}

plugins.vodoge.example.com {
    reverse_proxy CADDY_BRIDGE_GATEWAY:17576
}
```

若云端启用 UFW，需要只放行 Caddy 容器到网桥网关的这两个 TCP 端口。例如：

```bash
sudo ufw allow in on BRIDGE_INTERFACE from CADDY_CONTAINER_IP to CADDY_BRIDGE_GATEWAY port 17575 proto tcp comment 'VoDoge Caddy API tunnel'
sudo ufw allow in on BRIDGE_INTERFACE from CADDY_CONTAINER_IP to CADDY_BRIDGE_GATEWAY port 17576 proto tcp comment 'VoDoge Caddy plugin tunnel'
```

不要把 `17575` 或 `17576` 对公网开放。若 Caddyfile 以**单个文件 bind mount**方式只读挂进
容器，使用原子替换工具更新宿主文件会让容器继续绑定旧 inode；此时 `caddy reload` 仍会读取
旧配置。先验证宿主文件，再重启 Caddy 容器重新挂载，或采用不会替换 inode 的受控编辑方式。

## 启动、验收与重启

在 VM 上安装或修改 unit 后：

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now vodoge-db-tunnel vodoge-api-tunnel vodoge
systemctl is-enabled vodoge-db-tunnel vodoge-api-tunnel vodoge
systemctl is-active vodoge-db-tunnel vodoge-api-tunnel vodoge
curl -fsS --max-time 5 "http://APP_LISTEN_HOST:7575/ping"
```

在云端验证 Caddy 容器到宿主网桥端口的连接，再验证 HTTPS 入口：

```bash
docker exec <caddy-container> nc -zvw 5 CADDY_BRIDGE_GATEWAY 17575
docker exec <caddy-container> nc -zvw 5 CADDY_BRIDGE_GATEWAY 17576
curl -fsS --max-time 10 -k --resolve vodoge.example.com:443:127.0.0.1 https://vodoge.example.com/ping
```

管理面 `/ping` 应返回 `{"message":"pong"}`。插件运行时没有同名健康路由；对其发起一个
无效路径并得到后端 `404`（而不是 Caddy `502`）即可证明其隧道和反代已连通。

三项 VM 服务均需 `enabled`；云端 SSH/socket、Docker 服务和 Caddy 容器的 restart policy
也必须配置为开机恢复。重启后先检查两条隧道，再检查 `/ping`，不要只以 `wg show` 的握手
时间判断服务可用性。

## 故障处理边界

- `vodoge` 日志显示 PostgreSQL 认证失败：检查本地 `15432` 监听、云端 SSH 隧道账户的
  `permitopen`、数据库 HBA 和 `/etc/vodoge/vodoge.env`，不要重建数据库卷。
- Caddy 返回 `502`：先从 Caddy 容器内连接 `CADDY_BRIDGE_GATEWAY:17575`；随后检查 UFW、
  反向隧道和 Caddy 实际加载的配置。
- WireGuard 双方有发送字节但 VM 没有接收字节：这是底层 NAT/虚拟化 UDP 回程问题；保留
  隧道作为业务路径，在修复网络前不要把 Caddy upstream 切回 WireGuard 地址。

任何升级、重建或路由切换前都先备份 PostgreSQL。业务数据位于 PostgreSQL，不位于 VM 的
`data/` 目录。
