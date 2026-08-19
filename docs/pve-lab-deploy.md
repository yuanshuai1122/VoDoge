# PVE 实验室部署快照（2026-08-15，非当前手册）

日期：2026-08-15  
机器：Proxmox VE 宿主机 `pve.lab.lan`（`192.168.2.50`）上的虚拟机 **113 `vohive`**

> 这是 2026-08-15 的实验室快照，不是当前生产部署手册。文中的 `vohive`、`/opt/vohive`、
> IP、镜像标签和棒子台账均为当日事实，不应复制到新部署。当前通用流程见
> [DEPLOY.md](../DEPLOY.md)；NAT 后生产 VM 的现行方案见
> [production-vm-ssh-tunnel.md](./production-vm-ssh-tunnel.md)。

本文只记这套 lab 的真实落点和已经验证过的硬件步骤。

## 拓扑

```
PVE 宿主机 192.168.2.50
  └─ xHCI 0000:00:14.0  —— PCI 直通 ——▶  VM 113 vohive
                                              192.168.2.80
                                              vodoge.lab.lan
                                              Ubuntu 24.04 / 6.8.0-137
                                              /opt/vohive + docker compose
                                              USB 2-4  34d12d26
                                              USB 2-8  6726c019
```

不要把 VoDoge 放在 PVE 宿主机、不要放 LXC、不要先上 k8s。棒子默认 RNDIS，宿主机一旦认成网卡会抢走默认路由。

## 虚拟机

| 项 | 值 |
|---|---|
| VMID | 113 |
| 名称 | `vohive` |
| IP | `192.168.2.80/24`，网关 `192.168.2.1`，DNS `192.168.2.51` |
| 域名 | `vodoge.lab.lan` |
| 规格 | 4 vCPU / 6G / 60G |
| 模板 | VMID 9000 Ubuntu 24.04 |
| 用户 | `ops`（sudo；**不在** `docker` 组，docker 命令要 sudo） |
| USB | `hostpci0: 0000:00:14.0`（整颗 Intel 8 Series xHCI） |
| 启动 | `onboot=1`，`startup: order=2,up=20`（跟在 jumpserver 后面） |
| 代码 | 本机克隆 `Documents/local/vodoge`；虚机上仍是 `/opt/vohive`（compose 工作目录，未改以免断挂载） |

两根同型号棒子必须按控制器直通或按口绑定，不能 `host=05c6:90b4`。

## 棒子台账

两根都是 `UFI103S_V02_QR_QB_DD_230515`，原厂 Android，没有刷 OpenStick。

| 客户机 USB | ADB 序列号 | VoDoge ID | SIM | 出厂 PID | QMI PID | 备注 |
|---|---|---|---|---|---|---|
| `2-4` | `34d12d26` | `ufi-34d12d26` | **有卡**（电信 USIM，应用态曾报 `illegal`） | `05c6:90b4` | `05c6:9091` | 样品；本机工作区有 eMMC 备份 |
| `2-8` | `6726c019` | `ufi-6726c019` | **无卡**（UIM `no-atr` / `unknown`） | `05c6:90b4` | `05c6:9091` | 同批次；尚未做同等级备份 |

IMEI 用 `qmicli` 现查，不要写进 git。查法：

```bash
# 按序列号对齐控制节点，不要假设恒是 cdc-wdm0
readlink -f /sys/class/usbmisc/cdc-wdm0
# 期望路径里带 /2-4/（第一根）或 /2-8/（第二根）
sudo qmicli -d /dev/cdc-wdm0 --dms-get-ids
```

[ufi103s-qmi-host.md](./ufi103s-qmi-host.md) 里的 `find "$USB_PATH" -path '*/usbmisc/cdc-wdm*'` 以非 root 跑会得到空路径；用 `/sys/class/usbmisc/` 或 `sudo find`。

## 客户机基线（已做）

```bash
uname -r                                          # 6.8.0-137-generic
ls /sys/bus/usb/drivers | grep -E 'qmi_wwan|cdc_wdm|usbnet'
sudo systemctl disable --now ModemManager
# 已装：adb libqmi-utils usbutils docker.io docker-compose-v2
# 为加载 qmi_wwan：linux-modules-extra-$(uname -r) ，后来改装 linux-generic
```

RNDIS 网卡允许存在，但必须 **DOWN**，默认路由只能是 `192.168.2.1`：

```
enx0206060b3663  DOWN     # 6726c019 的 RNDIS
enx025706523132  DOWN     # 34d12d26 切走 QMI 后会消失
```

## QMI 切换（2026-08-15 实测）

两根的 `persist.sys.usb.config` 已经是 `diag,serial_smd,rmnet_bam,adb`。运行时却经常停在 RNDIS，因为 `sys.usb.tethering=true`。

只对第一根执行：

```bash
cd /opt/vohive
bash scripts/ufi103s-enable-qmi.sh --serial 34d12d26
```

当天结果：

- 立刻变成 `05c6:9091`，`sys.usb.config=diag,serial_smd,rmnet_bam,adb`
- `sys.usb.tethering` **仍是 true**（脚本写成 false 会被 CPE 打回）
- **tethering=true 并不等于掉回 RNDIS**：QMI 组合连续观察 2 分钟仍是 `9091`，`/dev/cdc-wdm0` 和 `wwan0`（驱动 `qmi_wwan`）一直在
- 直通之前在宿主机上、以及当天早些时候在虚拟机里，出现过「切到 9091 约 22 秒再掉回 90b4」。persist 写成 QMI 之后，用仓库脚本再切一次就站住了
- 第二根保持 `90b4`，没有一起切

验收命令：

```bash
lsusb -d 05c6:9091
ls -l /dev/cdc-wdm*
ip -br link | grep wwan
readlink -f /sys/class/net/wwan0/device/driver    # 必须是 qmi_wwan
sudo qmicli -d /dev/cdc-wdm0 --dms-get-ids
```

回退（运行时，不改 persist）：

```bash
bash scripts/ufi103s-enable-qmi.sh --serial 34d12d26 --restore-rndis
```

断电 / 拔插 / 虚拟机重启后 QMI 会不会自己回来，还没测。若启动后又是 RNDIS，再加 oneshot 按序列号跑脚本，不要再写更多 persist。

## 服务（当日历史记录，勿直接复用）

```bash
cd /opt/vohive
# 当前副本的 config/config.yaml 与 .env 已就位
# .env 至少含：VODOGE_IMAGE=vodoge:lab 和 VODOGE_POSTGRES_PASSWORD=<随机密码>
sudo docker compose --env-file .env build --build-arg VERSION=lab-$(date +%Y%m%d) vodoge
sudo docker compose --env-file .env up -d
curl -sS http://127.0.0.1:7575/ping
```

这里的 `pgdata` 是持久业务数据。若沿用当日的旧卷，先备份并核对角色、数据库与
`VODOGE_DB_DSN`，不能依赖新的 `POSTGRES_*` 变量重置旧实例，更不能用
`docker compose down -v` 清卷。通用升级步骤见 [DEPLOY.md](../DEPLOY.md)。

访问：`http://vodoge.lab.lan:7575` 或 `http://192.168.2.80:7575`。  
装 PWA 可在系统设置打开「本机自签 HTTPS」（同一端口，先下载并信任证书）。  
`network_mode: host` + `privileged` + `/dev`，一个实例独占这台机器上的全部棒子。

发现页必须显示 QMI，用 IMEI 添加，不要把 `cdc-wdmN` / `wwanN` 写进配置。  
添加接口是 `{ "config": { "id", "modem_imei", "device_backend": "qmi", ... } }`，不是扁平 JSON。

国内构建 Alpine 官方源很慢（`apk add` 能通但要数分钟）。不要和 GitLab 重负载叠在一起；宿主机 CPU 空载约 75°C。

`.dockerignore` 里原来的 `data` 会把 `internal/data` 一起排除，镜像 `go build` 报找不到 `internal/data/repo`。必须写成 `/data`。`/opt/vohive` 那份代码当初 rsync 时也漏了这个目录，已补齐。

容器会把 `config/config.yaml` 改成 root:600。`ops` 随后读配置要先 `chown`，或改读 `.admin-password`。

## DNS

infra 容器 `100` 的 `/etc/dnsmasq.d/lab.conf`：

```
host-record=vodoge.lab.lan,192.168.2.80
```

```bash
dig @192.168.2.51 vodoge.lab.lan +short    # 192.168.2.80
```

## 明确不做

- 新建另一台 106 / `.57`
- 把棒子刷成 Debian / OpenStick
- 为第二根再起一个 compose
- 用网页 CPE 的「网络共享」代替脚本
- 让 RNDIS 网卡当默认路由

## 当日状态快照

| 项 | 状态 | 日期 |
|---|---|---|
| VM 113 + xHCI 直通 | 已完成 | 2026-08-15 之前 |
| 客户机基线 / ModemManager 关闭 | 已完成 | 2026-08-15 |
| `34d12d26` 切 QMI、读到 IMEI | 已完成 | 2026-08-15 |
| DNS `vodoge.lab.lan` + 启动顺序 | 已完成 | 2026-08-15 |
| 修 `.dockerignore` + 补 `internal/data` + 镜像 + compose + `/ping` | 已完成 | 2026-08-15 |
| 发现页登记 `ufi-34d12d26`（QMI 后端起来，SIM 身份入库） | 已完成 | 2026-08-15 |
| 第二根 `6726c019` 切 QMI + 登记 `ufi-6726c019` | 已完成 | 2026-08-15 |
| 数据面：注册 + `wwan0` 拨号 + 代理冒烟 | **卡住** | 见下（只有有卡那根才谈这个） |
| 拔插 / 虚拟机重启门槛 | 未做 | |
| JumpServer 收纳 | 未做 | |

### 两根都进 VoDoge 之后（2026-08-15）

两根都是 `05c6:9091`，控制节点按口对齐：`2-4` → `/dev/cdc-wdm0`/`wwan0`，`2-8` → `/dev/cdc-wdm1`/`wwan1`。不要把这些路径写进配置。

- **有卡 `34d12d26`**：`healthy`/`control_online` 真，ICCID 已入库，卡印「中国电信」，搜到的小区却是「中国联通」，`reg_status=2`，`wwan0` DOWN。UIM 应用态见过 `illegal`。APN 试过 `3gnet`。
- **无卡 `6726c019`**：启动时 `等待 SIM ready` 超时是预期的。起来后 `healthy`/`control_online` 真，无 ICCID，`network_enabled=false`。日志有 `VOWIFI_DESIRED_RECOVER_SKIPPED_IDENTITY`。
- 两根的 Android `ril-daemon` 都已运行时 `stop`。重启棒子后还会起来。

添加第二根：`python3 scripts/lab-register-device.py --serial 6726c019 --usb-suffix 2-8`。

### RNDIS/CPE 对照（2026-08-15）

只动有卡那根，`--restore-rndis` 后默认路由仍是 `192.168.2.1`。CPE 网段是 `192.168.100.0/24`，网关 `192.168.100.1` 能 ping、80 能开（标题 `4G Wireless Modem`）。

Android 侧（`rild` 已拉起、SIM `READY`、`gsm.sim.operator=中国电信/46011`，基带 `UFI103_CT`）：

- 状态栏：`只能拨打紧急呼救电话|中国电信`，`emergencyOnly=true`，`未连接互联网`
- `mServiceState` 脱网，`mDataConnectionPossible=false`，原因 `roamingOff`
- 信号图标 `stat_sys_signal_null`

**结论：不是 QMI/APN 独有的问题。** 原厂 CPE 同样没网。有卡这根在这台机器所在位置驻不上电信网（电信定制棒 + 现场能看到的是联通小区）。代理冒烟要等换卡、换地点，或确认这张卡本身可用之后再做。

验证后把 `34d12d26` 切回 QMI 时，USB `2-4` 枚举几次后从总线上消失，需要**拔插有卡那根**。无卡 `6726c019` 已重新切回 `05c6:9091`。默认路由全程仍是 `192.168.2.1`。
