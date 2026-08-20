# 硬件支持

日期：2026-08-15  
产品承诺只覆盖 **Quectel EC20 / EC25 / EG25** USB 成品。实验室里的 UFI103S **不进承诺**。

软件路径已经按这些模组写好；棒子到货只做验收，不再等硬件才补代码。

## 模组

| 模组 | 典型用途 | 蜂窝短信 | IMS / VoWiFi 短信 | 数据面 | 说明 |
|---|---|---|---|---|---|
| **EC20** | 备用 / 老模组 | 移动 / 联通可用；**电信不可用**（见下） | 不作为产品出口 | QMI | 能管、能收发蜂窝短信即可 |
| **EC25 / EC25-CN** | **国内线** | **主路径** | VoWiFi 开着时只走 IMS | QMI，不用 RNDIS | 第 1 期验收棒 |
| **EG25-G** | **国外线** | 已驻网先走蜂窝 | **未驻网或蜂窝失败时补 IMS** | QMI | 第 2 / 4 期验收棒 |

共同约束：

- 一台实例，设备按 IMEI 重绑。
- 数据面是 QMI，不是 RNDIS。
- `lane=cn|intl|empty` 由人标，不按 MCC 推断，不拉黑中国卡。
- 国内线代理直连模组出口；国外线才走国家 / Profile 前置代理。

## 真短信

- 收：AT 先扫 SM 再扫 ME（URC 仍按 +CMTI 存储读）；QMI / MBIM List/Read。按 ICCID 进会话。
- 蜂窝回执：解析 SMS-STATUS-REPORT，按 `+CMGS` 的 TP-MR 更新 `sms_delivery`。AKA / 写卡占用时暂缓 CMGL。
- 发：`planSMSSend`  
  - `lane=intl`：已驻网先蜂窝，失败或未驻网再 IMS。  
  - 国内 / 未分线：VoWiFi 开着只走 IMS，否则蜂窝。
- 全局滚动 1 小时发送限额（默认 20）。
- 通知走 Telegram / Bark，不靠网页推送。

硬件仍要在 EC25-CN、EG25-G 上各跑通一条收、一条发。

## EC20 + 中国电信 = 没有短信通道（2026-08-19 实测）

**结论**：EC20 配电信卡收发短信都不通，配移动卡立刻就通。数据面两者都正常。
电信卡要用，换 EC25-CN。

这不是推断，是把变量穷尽后的实验结果。花了整整一天才定位，判据记在这里，
下次照着查五分钟就能确认。

### 对照实验

同一台主机、同一张电信卡（ICCID `898603…`，IMSI `4601157…`），换三根棒子：

| 棒子 IMEI | 固件 | 发送结果 |
|---|---|---|
| `862547055142811` | `EC20CEHDLGR06A07M1G` | `+CMS ERROR: 350` |
| `867018069509705` | `EC20CEHCLGR06A08M1G_AUD` | `+CMS ERROR: 350` |
| `867018069514820` | `EC20CEHCLGR06A08M1G_AUD` | `+CMS ERROR: 350` |

再拿**同一根**棒子（`867018069509705`、同一固件、同一台机器）只换卡：

| | 电信卡 | 移动卡 |
|---|---|---|
| EPS 注册 `AT+CEREG?` | `0,1` 已驻 LTE | `0,1` 已驻 LTE |
| **CS 注册 `AT+CREG?`** | **`0,2` 从不注册** | **`0,1` 已注册** |
| `AT+CSCA?` 短信中心 | `+8613334113200` | `+8613800311500` |
| **发送** | **`+CMS ERROR: 350`** | **`+CMGS: 97` 成功** |
| **接收** | 零条 | 插卡即收，`+CMTI: "ME",3..9` 七条待读 |

变量只剩运营商。

### 为什么

电信没有 GSM，2G/3G 是 CDMA，**EC20 不支持 CDMA**。所以它在电信网络上：

- **CSFB / SMS over SGs 走不了** —— `CREG` 永远是 `2`
- **SMSoIP 也走不了** —— `AT+QCFG="ims"` 始终 `0,0`，IMS 注册不上；
  `AT+QMBNCFG="list"` 里只有 `Volte_OpenMkt-Commercial-CMCC`（移动的 VoLTE 档），
  **没有电信 VoLTE 档**

LTE 下短信只有这两条承载，两条都没有，于是网络直接拒绝。数据面不受影响，
因为数据只用 PS 域。移动有 GSM 可回落，CS 一上来就注册上，短信立刻通。

### 五分钟判据

拿到一根疑似收不到短信的模组，按顺序问三句：

```
AT+CEREG?    → 0,1  说明 LTE 驻网正常，问题不在信号/天线
AT+CREG?     → 0,2  CS 域没注册
AT+QCFG="ims" → 0,0  IMS 也没有
```

`CEREG` 好而 `CREG` + IMS 双缺 ⇒ **没有短信承载**，别再往软件上查。

两个特征码等价，见到任一个就是这个病：

- AT 侧：`+CMS ERROR: 350`（伴随 `AT+CEER` → `6,259`）
- QMI 侧：`service=0x05 msg=0x0020 error=0x0038` —— `0x38` = `QMI_ERR_CAUSE_CODE`，
  含义是「模组提交了，网络返回拒绝原因码」

### 已排除的方向（别重走）

天线与信号（`CSQ: 31` = -51 dBm）、卡本身（同卡在手机上收发正常）、卡座接触、
单根硬件故障（三根独立硬件同结果）、固件版本（两个版本同结果）、
应用软件（VoDoge / VoHive 两套实现，以及完全绕开应用的 AT 直发，同结果）。

> 顺带记一笔：`AT+CNUM` 读的是 SIM 上的 EF_MSISDN，运营商经常不写或写旧号，
> **不能当作这张卡的真实号码**。用它去验收短信会把人带偏。

## USB 读卡器（PC/SC）

- 发现：`GET /api/readers`。优先问 pcscd Unix 协议，没有 `pcsc_scan` 也能列出。
- 设备：`device_backend=pcsc` + `reader_name`，不启无线电。
- 写卡：CGO-free pcscd 通道，Connect 后 `BeginTransaction`，断开前 `EndTransaction`；走标准 LPA。
- **VoWiFi / AKA**：读卡器没有蜂窝射频，IMS 是它的短信出口。身份从 USIM 读 ICCID/IMSI，IMEI 用配置或从读卡器名派生；AKA 走同一条 APDU 通道。启动时跳过飞行模式。eSIM 写卡与 AKA 按读卡器名排队。
- 读卡器名与 ICCID 互斥，避免和模组同时摸同一张卡。
- 生产二进制 `CGO_ENABLED=0`，不链接 libpcsclite。

硬件仍要：CCID 读卡器 + 本机 `pcscd` + 一张 eUICC。

## 插件

- 包：zip ≤ 64MiB，根目录（或单层目录）放 `vodoge-plugin.json`。
- 安装：URL（仅 HTTPS，拒绝内网）或上传；同 id 须先卸载。上传也可带 `sha256`。
- 反代剥 `Cookie` / `Authorization` / `Set-Cookie`，加上 `X-VoDoge-Plugin-ID`。
- 后端：按 `GOOS/GOARCH` 起 `127.0.0.1:随机端口`，环境变量 `VODOGE_PLUGIN_*`。
- 页面：侧栏入口 + 沙箱 iframe；`/plugin-assets/:id/...`；`/api/extensions/:id/backend` 反代。

插件以后端管理员权限跑，只装完全信任的包。
