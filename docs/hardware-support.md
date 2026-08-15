# 硬件支持

日期：2026-08-15  
产品承诺只覆盖 **Quectel EC20 / EC25 / EG25** USB 成品。实验室里的 UFI103S **不进承诺**。

软件路径已经按这些模组写好；棒子到货只做验收，不再等硬件才补代码。

## 模组

| 模组 | 典型用途 | 蜂窝短信 | IMS / VoWiFi 短信 | 数据面 | 说明 |
|---|---|---|---|---|---|
| **EC20** | 备用 / 老模组 | 有（AT / QMI） | 不作为产品出口 | QMI | 能管、能收发蜂窝短信即可 |
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
