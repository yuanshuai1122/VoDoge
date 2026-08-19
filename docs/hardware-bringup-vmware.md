# VMware 虚机的 USB 模组直通

最后验证：2026-08-19，Windows 宿主机 + VMware Workstation（`VMware20,1`）+ Ubuntu 24.04
（`6.8.0-137-generic`）客户机，Quectel EC20-CE 一根。

本文只讲**怎么让虚机看见 USB 模组**。VoDoge 自身的部署见
[production-vm-ssh-tunnel.md](./production-vm-ssh-tunnel.md) 与 [../DEPLOY.md](../DEPLOY.md)。

沿用生产文档的规矩：不在本文记录主机 IP、密码或密钥。

> **本文取代了原来的 WSL2 路线。** WSL2 被放弃的原因是微软默认内核没有编进任何 WWAN
> 驱动（`CONFIG_USB_NET_QMI_WWAN` / `CONFIG_USB_WDM` / `CONFIG_USB_SERIAL_OPTION` 全部
> 未开），usbipd 能把设备转发进去但没人认领它，表现为「设备发现列表为空」而不报错。
> 走通需要自建内核。**普通 Ubuntu 虚机没有这个问题**——这些驱动都在，以模块形式随插随载，
> 这也是换到 VMware 的主要理由。

## 判据只有一条

`/dev/cdc-wdm0` 出没出现。它在，QMI 控制面就通了；它不在，上层一切免谈。

完整的健康链条，从下往上：

| 层 | 检查 | 正常长相 |
|---|---|---|
| 宿主机枚举 | `Get-PnpDevice`（见下） | 父复合设备 `Status=OK` |
| VMware 放行 | `.vmx` 的 `usb.restrictions.*` | 见下一节 |
| 客户机 USB | `lsusb` | `ID 2c7c:0125 Quectel Wireless Solutions` |
| 内核驱动 | `lsmod` | `qmi_wwan` `cdc_wdm` `option` `usb_wwan` `usbnet` |
| 控制节点 | `ls /dev/cdc-wdm*` | `/dev/cdc-wdm0` |
| AT 口 | `ls /dev/ttyUSB*` | `ttyUSB0`–`ttyUSB3`，其中 **`ttyUSB2` 是 AT 口** |
| 网络接口 | `ip -br link show wwan0` | 存在即可，未拨号时 `DOWN` 正常 |
| QMI 通信 | `qmicli -d /dev/cdc-wdm0 --dms-get-ids` | 返回 IMEI |

## 坑一：`.vmx` 默认禁掉所有 USB 直通

**症状**：菜单里点 Connect，弹

```
Cannot connect 'Quectel Wireless Android' to this virtual machine.
The device is not allowed by the virtual machine configuration.
```

**成因**：`.vmx` 里有

```
usb.restrictions.defaultAllow = "FALSE"
```

而**一条 `usb.restrictions.deviceN` 放行项都没有**。默认禁 + 零放行 = 任何物理 USB 设备
都进不来。虚拟鼠标照常能用，因为它是 `usb_xhci:N.deviceType = "hid"` 的虚拟设备，
根本不走直通那条路——所以「鼠标好使」不能作为 USB 子系统正常的证据。

**改法**，二选一：

```
# 放开（设备会不断增加时选这个）
usb.restrictions.defaultAllow = "TRUE"

# 或定向放行（保持默认禁的姿态）
usb.restrictions.defaultAllow = "FALSE"
usb.restrictions.device0 = "vid:2c7c pid:0125 allow:true"
```

产品要接的模组会越来越多（EC20 / EC25-CN / EG25-G / 读卡器，配置上限 10 台），
每来一个新硬件关一次机去加白名单不划算，所以这套环境选了放开。

### 有两个闸门不受 `defaultAllow` 管

这是最容易空转的地方——**按类别的独立开关**：

| 键 | 建议 | 说明 |
|---|---|---|
| `usb.generic.allowHID` | 保持 `FALSE` | 否则虚机会来抢宿主机的键鼠 |
| `usb.generic.allowCCID` | **`TRUE`** | 写 eSIM 的 USB CCID 读卡器**单独受它管**，`defaultAllow=TRUE` 也照样进不来 |

读卡器还没到货就先把 `allowCCID` 加上，省一次关机。

### 改 `.vmx` 必须在关机状态

VMware 在关机时用内存里的配置**重写** `.vmx`，虚机运行中改的内容会被覆盖。顺序是
**关机 → 改 → 开机**，而且关机后要回头确认改动还在：

```powershell
Select-String -Path "<VM 目录>\<名字>.vmx" -Pattern '^usb' | ForEach-Object { $_.Line }
```

## 坑二：`Unknown error`

放行改对之后，点 Connect 可能变成另一个报错：

```
Unknown error.
```

**这次是时序，不是配置。** VMware 的 USB 仲裁器要在设备已被宿主机枚举后去抢占，
错过窗口就给这个毫无信息量的错。

**解法：把棒子物理拔下来、等 5 秒、再插回去**（虚机保持开机）。实测这一步就好了，
`.vmx` 里有 `autoConnect` 时甚至会自动挂进虚机，不用再点菜单。

其余两个备选，按代价递增：把 USB 控制器兼容性降到 **USB 2.0**（EC20 本来就是 USB 2.0
高速设备，降档无损失，要关机改）；或 `Restart-Service VMUSBArbService -Force`
（会短暂断开该宿主机上所有虚机的 USB 直通设备）。

## 宿主机侧速查

```powershell
Get-Service VMUSBArbService
```

`Stopped` 则「可移动设备」菜单里根本不会列出任何东西。

```powershell
Get-PnpDevice -PresentOnly | Where-Object { $_.InstanceId -match 'VID_2C7C|VID_05C6' } | Format-List FriendlyName,Status,InstanceId
```

EC20-CE 正常长这样：父 `USB Composite Device` **`Status=OK`**，下挂 `MI_00`~`MI_04`
五个子接口、`FriendlyName` 全是 `Android`、`Status` 全是 `Error`。

**那五个 `Error` 是好事**，不要去修：它表示 Windows 没装 Quectel 驱动、没有任何驱动
占着这些接口，VMware 夺过来更干净。直通只看父复合设备，它是 `OK` 就行。

五接口布局同时说明棒子已经是 **QMI 组合**（DIAG / NMEA / AT / modem / RmNet），
不是 RNDIS，不需要 `AT+QCFG="usbnet",0`。菜单里它显示为 **`Android Android`** 或
**`Quectel Wireless Android`**，不会显示型号。

## 客户机侧验收

```bash
lsusb | grep -iE "quectel|2c7c|05c6"
```

```bash
ls -l /dev/cdc-wdm* /dev/ttyUSB*; lsmod | grep -E "qmi_wwan|option|cdc_wdm"
```

```bash
qmicli -d /dev/cdc-wdm0 --dms-get-ids
```

`--dms-get-revision` 能拿到真型号。`lsusb` 会把 EC20-CE 标成 “EC25 LTE modem”——
**EC20-CE 和 EC25 共用 PID `0125`**，usb.ids 库里就是那么写的，以固件串为准
（EC20-CE 形如 `EC20CEHDLGR06A07M1G`）。

卡和驻网：

```bash
qmicli -d /dev/cdc-wdm0 --uim-get-card-status | grep -iE "card state|application state"
```

`Card state: 'present'` + `Application state: 'ready'` 才谈得上收发短信。
`--nas-get-serving-system` 刚上电几十秒内是 `not-registered-searching`，属正常；
持续如此要查天线和这张卡在该位置的信号。

VoDoge 侧不用重启：`udev` 热插拔监听器常驻，插入即触发内部 rescan；而「添加设备」
对话框的发现列表是**按需扫 sysfs**，打开就现扫。发现流程需要读到 IMEI 才能确立身份，
读不到会显示「身份不可确立」并禁用添加按钮。

## 未验证的部分

`usb.autoConnect.device0 = "vid:2c7c pid:0125 autoclean:1"` 已写入，但**开机那次它没有
生效**（最终是靠物理重插挂上的）。因此宿主机重启后 EC20 是否自动回到虚机，**尚未验证**。

下次重启时留意。若确实不自动，改用 `path:` 形式（绑定具体 USB 口，更可靠但换口就失效）：

```
usb.autoConnect.device0 = "path:<宿主机 USB 路径> autoclean:1"
```

## 相关文档

| 文档 | 内容 |
|---|---|
| [production-vm-ssh-tunnel.md](./production-vm-ssh-tunnel.md) | 该虚机上 VoDoge 的部署与隧道 |
| [hardware-support.md](./hardware-support.md) | 模组、短信通道、读卡器 |
| [architecture.md](./architecture.md) | 设备发现与后端模式在代码里的位置 |
| [pve-lab-deploy.md](./pve-lab-deploy.md) | 历史 PVE 快照（整颗 xHCI 走 PCI 直通，与本文是两套做法） |
