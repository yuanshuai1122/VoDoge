# UFI103S 原厂 QMI 接入

这份手册适用于让 **UFI103S 继续运行原厂 Android/CPE 系统**，由一台原生 Linux
主机上的 VoDoge 集中管理。不要把每根棒子刷成 Debian 主机：VoDoge 的主机才运行
Debian、Docker、PostgreSQL 和一个 VoDoge 实例。

## 已实测的单根样品

- 型号/构建：UFI103S，`UFI103S_V02_QR_QB_DD_230515`
- Android 正常模式：`05c6:90b4`，RNDIS + 厂商串口接口 + ADB
- 原厂 QMI 候选模式：`05c6:9091`，`diag,serial_smd,rmnet_bam,adb`

在这根样品上，后一个组合已通过**运行时**属性切换并稳定保持超过一分钟；没有写入
分区、bootloader 或 `persist.*` 属性。重启、断电及不同批次固件的行为尚未作为量产
承诺验证，因此必须逐根验收后再自动化。

批量部署门槛：代表样品必须分别通过棒子断电重启、Hub 拔插和 Debian 主机重启三项测试，
且每次都能重新完成 QMI、IMEI 与代理验收。当前只证明了运行时切换，不能假设它在断电后
仍保持 QMI；门槛未通过前，禁止批量接入。

`05c6:9091` 的 interface 2 被上游 Linux `qmi_wwan` 专门匹配，随后由 `cdc-wdm`
提供 QMI 控制节点。请使用 Linux 6.4 或更新内核，或确认发行版已回移该匹配；不要把
默认 Debian 12 的 6.1 内核当作已验证环境。

## 目标主机要求

使用原生 Debian/Linux 主机，不能用 macOS 或 Docker Desktop 来接管 USB WWAN。主机
内核需要实际加载：

```bash
sudo modprobe usbnet
sudo modprobe qmi_wwan
sudo modprobe cdc_wdm
```

把一根已切换的棒子插入主机后，以下全部成立才算通过：

```bash
lsusb -d 05c6:9091
ls -l /dev/cdc-wdm*
ip -br link | grep '^wwan'
readlink -f /sys/class/net/wwan*/device/driver
```

最后一项应指向 `qmi_wwan`。多棒时不能假设控制节点恒为 `/dev/cdc-wdm0`；按该棒子的
ADB/USB 序列号找到对应 sysfs 路径，再做只读身份验证：

```bash
USB_SERIAL=34d12d26
USB_PATH="$(grep -lFx -- "$USB_SERIAL" /sys/bus/usb/devices/*/serial | sed 's#/serial$##' | head -n 1)"
test -n "$USB_PATH"
# 非 root 的 find 对 usbmisc 常得到空路径；从 /sys/class/usbmisc 反查更稳
CTRL="$(readlink -f /sys/class/usbmisc/cdc-wdm* | grep -F "$USB_PATH" | sed 's#.*/usbmisc/#/dev/#' | head -n 1)"
test -n "$CTRL"
sudo qmicli -d "$CTRL" --dms-get-ids
```

出现 `05c6:90b4`、只有 `rndis*` 网卡或没有 `/dev/cdc-wdm*` 时，当前棒子不应添加到
VoDoge 的 QMI 后端。RNDIS/CPE 模式只能作为恢复路径；它们通常共享相同私网地址，且
当前项目没有纯 RNDIS 的 DHCP、路由、IP 轮换和设备控制实现。

## 接管目标主机后的执行顺序

主机准备好后，需要提供可使用 `sudo` 的 SSH 账户、实际 Debian 版本/内核版本，以及
计划接入的棒子数量。先记录主机基线，不要一开始就整排插入：

```bash
uname -a
docker version
lsusb -nn
lsmod | grep -E 'qmi_wwan|cdc_wdm|usbnet'
```

现场操作按下面顺序进行：

1. 只接一根样品，运行本仓库脚本并完成上面的 QMI 驱动、`cdc-wdm`、`wwan` 和 IMEI 验收。
2. 依照 [DEPLOY.md](../DEPLOY.md) 启动唯一的 VoDoge Compose 实例；不要为同一 Hub 起第二个
   实例。
3. 在 VoDoge 发现页确认该设备的后端是 QMI，添加唯一设备 ID 与 IMEI，再创建一条测试代理。
4. 用 `node scripts/smoke-api.mjs` 和一次真实出口请求确认代理绑定到该 `wwan` 接口。
5. 单根稳定后，再逐根切换、发现和登记；每一根都保存其 ADB USB 序列号与 IMEI 对照。

验收之前不要批量插入默认 RNDIS 模式的设备，也不要依赖网页 CPE 管理界面切换 USB 模式。
默认 RNDIS 设备常使用相同的私网地址，不能作为多棒数据平面。

## 单根切换与验收

先在受控环境中启用 ADB，并取到 USB 序列号。脚本强制指定一个序列号，且只接受构建
标识里含 `UFI103S` 的设备，避免一键改变 Hub 上的未知 Android 设备。

```bash
adb devices -l
bash scripts/ufi103s-enable-qmi.sh --serial 34d12d26
```

脚本执行的是临时 `sys.usb.*` 设置，并在退出时关闭 root ADB。它不会写入固件、分区、
GPT 或 `persist.sys.usb.config`。若验收失败，断电或恢复原厂 RNDIS 组合即可回到已知
状态；不要在未通过 Linux 验收前批量写持久 USB 属性，也不要用网页中的 MTP/网络共享
开关替代 QMI 设置。

PVE lab（2026-08-15）补充：样品上 `persist.sys.usb.config` 已被写成 QMI 组合后，
`sys.usb.tethering` 仍会保持 `true`，但运行时再切一次 QMI 可以连续保持 `05c6:9091`
超过两分钟，并不必然在 20 秒内掉回 RNDIS。Android `ril-daemon` 与主机 QMI 抢 NAS
时，能读 IMEI 但驻不了网；临时 `stop ril-daemon` 之后控制面变健康，搜网仍未完成。
细节见 [pve-lab-deploy.md](./pve-lab-deploy.md)。
在 Proxmox 上两根同型号棒子应整卡直通 xHCI，不要按 `05c6:90b4` 映射。

在本样品已验证的构建上，RNDIS 回退同样是运行时操作：

```bash
bash scripts/ufi103s-enable-qmi.sh --serial 34d12d26 --restore-rndis
```

该回退只适用于通过本手册验收的 `UFI103S` 构建；它会要求同一台设备的 ADB 序列号，并
在结束前验证 root ADB 已关闭。回退后应确认 USB PID 回到 `05c6:90b4`。

这台样品的 eMMC 用户区和关键分区备份已经保存在本地工作区的忽略目录，且不随 Git
推送。批量动作前，应为每个新批次保留等价、可校验的恢复备份。

## VoDoge 接入

通过后，保留默认 Linux Compose 的 `network_mode: host`、`privileged: true` 和 `/dev`
挂载。一个 VoDoge 实例应独占整台 Hub，而不是用多个容器争抢同一批设备。

在 VoDoge 的设备发现页确认每根棒子都显示为 QMI，并用唯一 IMEI 添加设备，后端选
`qmi`。不要持久化 `/dev/cdc-wdmN` 或 `wwanN`：热插拔会改变这些路径，VoDoge 会以 IMEI
重新关联设备。

验证阶段停用 ModemManager，避免它与 VoDoge 同时打开 QMI/串口节点：

```bash
sudo systemctl disable --now ModemManager
```

## 多棒部署

- 先拿一根完成 QMI、IMEI 和拨号验收，再按相同样品批次逐根扩展。
- Hub 电源按每根至少 500 mA 的 USB 声明值预留余量；不要依赖主机 USB 口供电。
- 当前 VoDoge 代码硬限制为 **5 台**。第 6 根起必须先调整设备上限及热插拔测试，不能
  通过启动多个 VoDoge 容器绕过。
- QMI 比 MBIM、RNDIS 更适合当前多棒架构：项目的 QMI 路径按设备网卡绑定代理出口，且
  不会重写主机全局 DNS。

## 不采用的路线

OpenStick 的通用底包会重写分区表和启动链，而且没有为 UFI103S HW1.3 / 230515 发布
经过验证的完整 Debian 固件组合。它不是把 VoDoge 接到多根原厂棒子的必要步骤，也不应
作为批量部署前置条件。
