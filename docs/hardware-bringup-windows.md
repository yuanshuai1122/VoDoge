# Windows + WSL2 硬件联调环境

日期：2026-08-14
状态：**环境就绪，硬件尚未接入验证**

这份文档记录在 Windows 上把 VoDoge 跑起来、并让它有可能看到 USB 模组所需的全部准备。
两件事必须分开看：**服务能跑**是一回事，**服务能看到模组**是另一回事——后者在
Windows 上有一个默认满足不了的前提。

---

## 1. 部署：必须用 `docker-compose.windows.yml`

```bash
docker compose -f docker-compose.windows.yml up -d
```

**不要在 Windows 上用默认的 `docker-compose.yml`。** 它用 `network_mode: host`，
而 Docker Desktop 的 host 网络是把容器放进 docker-desktop 这个 WSL 发行版的网络
命名空间——不是 Windows 主机的。实测结果：

| 从哪访问 | 结果 |
|----------|------|
| 容器内 `wget http://127.0.0.1:7575/ping` | ✅ `{"message":"pong"}` |
| Windows `curl http://127.0.0.1:7575` | ❌ 连不上 |
| WSL 的 Ubuntu 发行版内 | ❌ 连不上（与 docker-desktop 是不同命名空间） |
| `netstat` 在 Windows 侧 | 无 7575 监听 |

即：服务本身完全正常，只是端口被关在虚拟机里。`docker-compose.windows.yml`
改用发布端口（`7575:7575`）+ 服务名连库（`host=postgres`），这才是 Windows 上的正解。

**Linux 主机上仍然用默认的 `docker-compose.yml`**——那里 host 网络是真的 host，
而且模组的网络接口需要它。

验证：`node scripts/smoke-api.mjs`，界面 `http://127.0.0.1:7575`。

---

## 2. WSL2 默认内核看不见蜂窝模组

这是最容易浪费时间的一环，因为**它的表现不是报错，而是"设备发现列表为空"**。

微软的默认 WSL2 内核（`5.15.167.4-microsoft-standard-WSL2`）没有编译进任何
WWAN 驱动。从运行中内核的 `/proc/config.gz` 直接读出来：

```
CONFIG_USB_USBNET=y                     ← 有
# CONFIG_USB_NET_QMI_WWAN is not set    ← 缺，QMI 通道靠它
# CONFIG_USB_NET_CDC_MBIM is not set    ← 缺，MBIM 通道靠它
# CONFIG_USB_SERIAL_OPTION is not set   ← 缺，AT 通道靠它（/dev/ttyUSB*）
# CONFIG_USB_WDM is not set             ← 缺，/dev/cdc-wdm* 靠它
CONFIG_USBIP_VHCI_HCD=y                 ← usbip 接收端本身是有的
```

后果：usbipd 能把设备转发进 WSL，`/dev/bus/usb` 下会出现它，但**没有任何驱动
去认领**，于是既没有 `/dev/ttyUSB*` 也没有 `/dev/cdc-wdm*`。VoDoge 的三条通路
（QMI / MBIM / AT）全部落空，表现为设备扫不到或 degraded——很容易被误判成程序 bug。

### 已构建的自定义内核

| 项 | 值 |
|----|----|
| 版本 | `5.15.167.4-microsoft-standard-WSL2+`（末尾 `+` 是自建标记） |
| 内核文件 | `E:\wsl\kernel\vohive-wsl2-kernel`（14 MB） |
| 配置副本 | `E:\wsl\kernel\vohive-wsl2-kernel.config` |
| 原 `.wslconfig` 备份 | `E:\wsl\kernel\.wslconfig.backup` |

做法是取微软 `WSL2-Linux-Kernel` 对应版本的**原始运行配置**，只补开四项：

```
CONFIG_USB_SERIAL=y  CONFIG_USB_SERIAL_WWAN=y  CONFIG_USB_SERIAL_OPTION=y
CONFIG_USB_WDM=y
CONFIG_USB_NET_QMI_WWAN=y
CONFIG_USB_NET_CDC_MBIM=y
```

**全部编成内建（`=y`）而不是模块。** WSL2 各发行版的文件系统互相独立，模块方案
要求每个发行版（包括 Docker 的 `docker-desktop`）都放一份
`/lib/modules/<版本>`；内建可以绕开这一整类问题。

编译在一个一次性发行版 `KBuild` 里做，**没有动现有的 Ubuntu**。

### 生效与验证

`C:\Users\admin\.wslconfig` 里加：

```ini
kernel=E:\\wsl\\kernel\\vohive-wsl2-kernel
```

然后 `wsl --shutdown`。验证（这是判据，不要只看配置文件）：

```bash
wsl -d Ubuntu -e sh -c 'uname -r; ls /sys/bus/usb/drivers/ | grep -E "option|qmi_wwan|cdc_mbim|cdc_wdm"'
```

实测结果：四个驱动 `option` / `qmi_wwan` / `cdc_mbim` / `cdc_wdm` **全部已注册**。
`docker-desktop` 用的是同一个内核，容器在 `wsl --shutdown` 后自动恢复，冒烟全过。

### 回滚

注释掉 `.wslconfig` 里的 `kernel=` 行，`wsl --shutdown`。
原文件备份在 `E:\wsl\kernel\.wslconfig.backup`。

---

## 3. 还没做的：usbipd

Windows 侧尚未安装 `usbipd-win`。装法（管理员）：

```bash
winget install --exact dorssel.usbipd-win
```

之后的流程是 `usbipd list` → `usbipd bind --busid <x>` → `usbipd attach --wsl`，
再回 WSL 里看 `/dev/ttyUSB*` 与 `/dev/cdc-wdm*` 是否出现。

> Docker 容器要用 `privileged: true` + `-v /dev:/dev` 才能看到这些节点。
> `docker-compose.yml`（Linux 版）已经这么配了；`docker-compose.windows.yml`
> 目前没挂 `/dev`，接硬件时需要补上。

---

## 4. 手上这根棒子的识别结果

型号：**UFI-B08A**，卖家称高通 410。插在 Windows 上枚举为：

```
USB\VID_05C6&PID_90B4            复合设备（VID 05C6 = Qualcomm）
  ├─ MI_00  Remote NDIS based Internet Sharing Device   （RNDIS 网卡）
  ├─ MI_02  "Android"  ← 驱动缺失（Error），大概率是 DIAG/AT 串口
  └─ MI_03  ADB Interface
```

三点结论：

1. **当前不在 9008（EDL）模式**，是正常固件在跑。
2. **ADB 通道是开的**——这类棒子的标准入口。
3. `MI_02` 缺驱动，需要装高通 USB 驱动才能拿到那个串口。

`adb` 在本机尚未安装。

---

## 5. 刷机：调研中止，以下是**未经验证**的线索

用户希望刷入自定义固件，并按卖家建议"把 boot 换成 103s 的，否则 LED 有问题"。
调研做到一半被叫停，这里只记录已查到的线索，**没有一条在这台设备上验证过**。

> **刷错会变砖。** 下面的内容不足以据此操作，需要先针对 UFI-B08A 这块**具体主板**
> 确认，尤其是 boot 分区的对应关系。

已查到的：

- **`103s` 是另一个型号**（UFI-103S），骁龙 410 / MSM8916，不是某个 boot 版本号。
  社区做法是把它的 `boot.img` 用到别的板子上，各板 LED 行为由 boot 决定——
  这与卖家的说法吻合。
- 生态项目是 **OpenStick**，`base-generic` 包 + 按板子替换 `boot-*.img` 为 `boot.img`
  再跑 `flash.bat`。
- **进 9008 EDL**：按住 reset 不放插入电脑，松开后在设备管理器端口里看到结尾
  9008 的设备即成功。
- 常用工具：9008 驱动、ADB、MiKo 备份工具、`adb reboot bootloader` 进 fastboot。
- **刷之前应当先做全量备份**（9008 模式下），这是唯一的后悔药。

**待确认**：UFI-B08A 是否被 OpenStick 明确支持、它与 UFI-103S 的主板差异、
以及 `boot-103s.img` 用在 B08A 上的实际后果。

参考（均为第三方博客，未逐条核实）：

- [高通410 随身WiFi 刷入Debian](https://blog.talen.top/posts/aec50521)
- [随身WIFI折腾日记-高通410方案UFI103s](https://steammilk.fun/2023/05/30/2023-all/sui-wifi/)
- [在高通骁龙410主控的USB网卡上玩GNU/Linux](https://techie-s.work/posts/2022/07/openstick-msm8916/)
- [Mainline Linux for OpenStick UFI003](https://evsio0n.com/archives/10/)

---

## 6. 一件待处理的遗留

`data/vohive.db` 还在，WAL 有 712 KB——切 PostgreSQL 之前的旧 SQLite 库，
里面可能有此前的设备配置与短信。

想看里面有什么，用**只读演练**（不写入任何东西，只报告每张表的行数）：

```bash
go run ./cmd/dbmigrate --sqlite ./data/vohive.db --dry-run
```

完整说明见 [db-migrate-runbook.md](./db-migrate-runbook.md)。
