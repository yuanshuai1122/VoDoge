# 对照 VoCat 的产品与抄作业计划

日期：2026-08-15  
对照仓库：https://github.com/MengMengCode/VoCat（开源控制台，Quectel EC20/EC25/EG25）  
本仓库：VoDoge（专有，已在 PVE lab `vodoge.lab.lan` 跑着）

这不是「把 VoCat 贴进本仓库」。VoCat 的 LICENSE 在 GitHub 上标成 `NOASSERTION`，**禁止整文件搬代码**。本计划只抄**产品边界、验收标准和工程手法**，实现落在现有 `internal/esim`、`internal/api`、PostgreSQL 信封上。

## 一句话产品

**多 USB 模组上的短信中枢，分国内线 / 国外线。**  
代理、VoWiFi 往后排。不做 VoCat 那种「只服务境外卡」。

## 对照结论

| 维度 | VoCat | VoDoge 现状 | 本计划 |
|---|---|---|---|
| 用户 | 境外 eSIM / WiFi Calling 工具人 | 实验室 + 国内场景 | **国内短信 + 国外短信** 两条线 |
| 中国卡 MCC 460/461 | 源码 `BlockedMCCs` **拉黑**数据/短信/VoWiFi | 不拉黑；UFI 登不上网 | **禁止抄拉黑** |
| 发现 | 只认 Quectel USB `2c7c` | QMI/MBIM/AT；另有 SIMCOM 方言 | 保留多方言；产品机只承诺 Quectel |
| 短信 | 蜂窝 + IMS，会话/回执/未读 | 代码有，lab **未真机跑通** | **第一期验收核心** |
| eSIM | `AT+CSIM` LPA；XeSIM / eSTK 多 AID | `internal/esim` 已列 eSTK / 5ber / XeSIM / eSIM.me / GlocalMe AID | 先打通**卡槽 eUICC 列表+切换**；写卡用读卡器 |
| USB 读卡器 | `internal/pcsc` + `pcscd` | Manager 注释里有 PC/SC 通道工厂，**无读卡器发现/加设备** | 第二期抄发现与加设备，不抄 SQLite |
| 代理 | 设备绑定 + **按国家**上游 | 按 `wwan` 绑 SOCKS/HTTP | 第三期；国内/国外用不同出口规则 |
| 交付 | 单二进制、SQLite、一键装、自更新 | Compose + PostgreSQL + `install.sh` + GHCR + tag 发版 | **保持 PG**，不抄 SQLite |
| 硬件 | EC20/EC25/EG25/EG600 | lab 两根 UFI103S-CT，管理面能见、短信不能 | UFI 退出产品承诺 |

VoCat 比我们超前的、值得抄的三块：

1. **卡槽里的 eUICC 当一等公民**（不只是模组焊盘）。  
2. **USB CCID 读卡器进设备列表**，写 profile 和模组收短信拆开。  
3. **IMS 短信当蜂窝登不上时的补通路**（国外线更有用）。

明确不抄：

- 地区拉黑、只发现 `2c7c`、SQLite、一键 `curl | bash`、整份前端。  
- 5ber 官方 App 协议（无公开规范；卡已在 AID 表里，能列/切就够，下载仍走标准 LPA 或读卡器）。

## 硬件与线路（先定，再写代码）

```
                    ┌─ 国内线  EC25-CN USB
                    │    实体卡：移动 / 联通 / 电信
  vohive VM 113  ───┤    主责：国内验证码、通知
  192.168.2.80      │
                    └─ 国外线  EG25-G USB
                         实体卡或「已写好的」一卡多 eSIM
                         主责：国外号收发；切套餐第二期
```

- 现有两根 UFI：**不进产品承诺**。只留实验室看热插拔。  
- 一卡多 eSIM（9eSIM / eSTK / 5ber）：第一期模组只当「当前启用的那一张 USIM」。写套餐：手机或 USB 读卡器。  
- 同一台机上限仍是代码里的 **5 根**。

## 分期（可验收）

### 第 0 期 — 边界写死（文档，无硬件）

- 设备打标：`lane=cn | intl`（配置或 UI，不靠 MCC 拉黑）。  
- 短信列表可按 lane / ICCID 过滤。  
- 文档承诺表（见文末）对外只写「通用 Quectel + 报到后的短信」。

### 第 1 期 — 国内线短信通（产品能用）

**前置：** 一根 `EC25-CN` 插在已直通的 xHCI 口，卡用手机上已验证能上网的国内实体卡。

验收（必须全过）：

1. 发现为 QMI，按 IMEI 添加，`radio_registered` 或数据/CS 至少一侧报到。  
2. `node scripts/smoke-api.mjs` 过。  
3. 网页发一条短信，有回执或明确失败原因。  
4. 收一条验证码，会话按 ICCID 落库；拔卡再插，历史还在。  
5. 通知渠道（已有 Telegram 等）能推出这条入站短信。

对照 VoCat：只对齐「蜂窝短信主路径」，不对齐 IMS。

落到：`internal/sms`、`internal/device` 启停、`web` 短信页、`docs/pve-lab-deploy.md` 补 EC25 台账。

第 1 期网页必须能在手机浏览器里完成「看会话、点进一条、发一条」。不做离线缓存。新短信响不响走已有 Telegram / Bark，不靠网页推送。

### 第 1b 期 — 个人入口：响应式 + 轻量 PWA

个人部署的目标：手机主屏幕一个图标，打开就能看短信、管设备。**不是**再做一个原生 App，也**不是**离线先读缓存。

形态：

| 用途 | 用什么 |
|---|---|
| 锁屏/横幅「来了一条验证码」 | 已有通知渠道（Telegram / Bark 等） |
| 打开翻会话、发短信、看设备 | 同一套 Web，可安装为 PWA |

约束：

- Service Worker **只缓存壳和静态资源**（HTML/JS/CSS/图标）。`/api/*`、SSE、`/ping` **一律不缓存**。  
- 安装提示和独立窗口需要 **HTTPS**（或 localhost）。现网 `http://vodoge.lab.lan:7575` 不够；入口走 Caddy / Tailscale / 已有反代，**不要把 7575 裸暴露公网**。  
- iOS 推送弱，不把 Web Push 写进第 1b 验收。有余力再做第 5 期。  
- 不做离线队列、不做两套 API。

验收：

1. 手机竖屏能走完：登录 → 短信列表 → 会话 → 发送。按钮和列表按触控排，不靠宽表格。  
2. `manifest` + 图标 + `display: standalone`。Android Chrome 能「添加到主屏幕」，打开无浏览器地址栏。  
3. HTTPS 入口打开上述流程；HTTP 局域网仍可当开发入口，不保证可安装。  
4. 杀进程再开 PWA，会话未过期则仍在短信页；过期回登录，不白屏。  
5. 断网时打开 PWA：壳可以出，短信数据明确失败，不展示过期列表装成成功。

落到：`web/`（manifest、图标、极简 SW）、短信/设备页的窄屏布局。反代和证书记在 `docs/pve-lab-deploy.md`，不写进应用配置。

排在第 1 期真机短信之后、第 2 期国外线之前。没有报到成功的短信，不先做 PWA。

### 第 2 期 — 国外线 + 当前 profile

**软件先行（2026-08-15）：** 对照 VoCat 的交互和验收语义已合入，**不搬源码**。硬件（EG25-G）到了只做验证，不再等棒子才写列表/切换/禁用。

软件已对齐 VoCat 的这些点：

- 当前启用的 Profile 是一等公民（设备列表、概览、eSIM 页顶部「当前号码」）。  
- 启用要确认；当前卡可**禁用**（`POST .../esim/actions/disable`），禁用后该槽没有活动号码。  
- 使用中的 Profile **不能删**（缓存命中即 409 `PROFILE_ENABLED`）。  
- 切换/禁用失败走 400/404/409，不一律 500 装成内部错误。  
- 切卡后刷新设备与短信查询，短信仍按 ICCID 归属。  
- `lane=intl` 人工分线，不按 MCC 推断。

**硬件验收（EG25-G 到货后必须再跑一遍）：**

1. 与第 1 期同样的收发，设备标 `lane=intl`。  
2. 国内线、国外线同时在线，短信互不串 ICCID。  
3. 模组侧列出卡上 profile（已有 AID 表）；**切换启用**后短信身份变成新 ICCID。  
4. 下载/删/禁用若卡或模组拒绝，UI 说清，不装成成功。

不抄 VoCat 的下载实现细节、中国卡拉黑、SQLite。AID 表已含 eSTK SE0/SE1、5ber、XeSIM。

### 第 3 期 — 读卡器写卡（对齐 VoCat PC/SC）

**软件先行（2026-08-15）：** 对照 VoCat 的职责划分已重写进本仓库，**不搬源码、不加 CGO**。

软件已对齐：

- 添加设备对话框列出读卡器；`pcscd` 没起来明示。  
- 读卡器可作为 `device_backend=pcsc` 设备加入列表，进同一套 eSIM 页。  
- 模组与读卡器按读卡器名 / ICCID 互斥（409 `ESIM_IN_USE`）。  
- 真卡 APDU 已接到 pcscd（公开 MUSCLE Unix 协议，无 CGO）。pcscd 没起来才是 `APDU 连不上 pcscd`。

**硬件验收（CCID 读卡器 + pcscd 到货后）：**

1. 对话框能看见读卡器；pcscd 没起来明示。  
2. 读卡器上完成：列 profile、下载（标准激活码）、启用。  
3. 卡移到 EG25/EC25 后第 2 期切换仍可用。  
4. 读卡器与模组 APDU 不能同时占同一张卡。

下一步硬件验证时只插读卡器跑列/写/启用，不必再改加设备/互斥/通道。

读卡器上的 VoWiFi / AKA 也已软件先行：加设备默认 `vowifi_enabled=true`，身份从卡读，AKA 用 USIM 逻辑通道，启动不切飞行模式。真机仍要一张能做 IMS 的卡 + 可达的 ePDG。

### 第 3b / 4g 期 — 插件体系

**软件先行（2026-08-15）：** 对照 VoCat 的 zip 清单、安装/启停/卸载、侧栏 iframe、本机后端反代，**不搬源码**。

- 清单 `vodoge-plugin.json`，`schema_version=1`。
- `GET/POST/PUT/DELETE /api/extensions*`；上传字段 `package`。
- 从 URL 安装只走 HTTPS，解析后的地址不能是内网。
- 后端 `VODOGE_PLUGIN_ID` / `LISTEN` / `DATA_DIR`。
- 设置页安装；侧栏按 contribution 插入；`/plugins?plugin=&contribution=` iframe。

插件以管理员权限跑，只装信任的包。硬件无关。

### 第 4 期 — IMS 短信补通路 + 代理分线

**软件先行（2026-08-15）：** 对照 VoCat 的发送通道语义已合入，**不搬源码**。

- 国外线：已驻网先走蜂窝，失败或未驻网再走 IMS；国内线仍是 VoWiFi 开着只走 IMS。发送结果带 `transport`。  
- 代理：国内线忽略国家前置代理，走模组出口；国外线/未分线仍按 MCC 国家规则。  

**硬件验收：** EG25 登不上网时能靠 IMS 发出短信；国内线代理出口不是外国 IP。

### 第 4b 期 — 全局短信发送限额

**软件先行（2026-08-15）：** 对照 VoCat 的滚动一小时全局配额，**不搬源码**。

- 所有设备 / 通道共用一条额度；接收不限。  
- 独立表 `sms_send_attempts`，删短信历史不能回补。  
- `GET/PUT /settings/sms`；网页发送与 Telegram `/send` 同路。  
- 超限 429 `sms_rate_limited`，带 `retry_after_seconds`。  
- 默认 20 条/小时，0 表示不限制，上限 200。

硬件到了只验证真机发送也吃同一条额度，不必再改设置页。

### 第 4c 期 — Profile 绑前置代理

**软件先行（2026-08-15）：** 对照 VoCat 的「一张 ICCID 绑一个 SOCKS」语义，**不搬源码**。

- `GET/POST/DELETE /upstream-proxy-profile-bindings`。  
- 同一 ICCID 不能绑两个代理（409 `profile_already_bound`）。  
- 国外线 / 未分线：当前 ICCID 的绑定优先于国家规则。  
- 国内线仍直连，绑定会保存但不走代理。  
- 删上游代理会顺带清掉绑定。当前卡若正在用该 ICCID，绑定变更会尝试重连 VoWiFi。

硬件到了只验证切卡后 ePDG 出口跟着绑定走。

### 第 4d 期 — 设备配额

**软件先行（2026-08-15）：** 对照 VoCat 的设备上限设置，**不搬源码、不引入开发者模式**。

- 默认仍是 5 台；设置页可改 1–10，写入 `server.max_devices`。  
- `GET /devices` 的 `meta.device_limit` 跟当前配额走。  
- 调低不会删除已有设备；再添加会 409。  
- 启动 / 热插拔也读同一条配额。

### 第 4e 期 — 本机自签 HTTPS

**软件先行（2026-08-15）：** 对照 VoCat 的本机自签 HTTPS，**不搬源码、不藏在开发者模式**。

- 与 HTTP 共用 `server.port`；开启后明文 308 到 HTTPS。  
- 证书写 `data/tls/`，覆盖 localhost、主机名和本机网卡 IP。  
- `GET/PUT /settings/https`，`GET /settings/https/certificate` 下载 PEM。  
- 设置页与证书下载在开启后仍可走 HTTP，避免浏览器拒证后锁死。  
- 自签需要系统/浏览器信任；不替代反代上的正式证书。

第 1b 装 PWA 可以走这条入口，不必先配 Caddy。

### 第 4f 期 — 网络访问策略

**软件先行（2026-08-15）：** 对照 VoCat 的来源网段控制，**不搬源码**。

- 默认 `internal`：回环 / RFC1918 / 链路本地 / IPv6 ULA。  
- `public` 不限制来源。可额外放行 CIDR 或单个 IP。  
- 默认不信 `X-Forwarded-For`，避免伪造内网地址绕过。  
- `GET/PUT /settings/security` 回显 `client_ip` / `client_allowed`。  
- 中间件罩整站（含 `/ping` 和 SPA）。

### 第 5 期 — PWA 推送（可选）

仅当 Telegram/Bark 不够、且入口已是 HTTPS 时再做。Android 优先；iOS 失败不算第 1b 的回归。不替代通知渠道。

## 工程约束

- API 继续用 `{data, meta, request_id}`，见 [api-envelope-design.md](./api-envelope-design.md)。  
- 持久化只走 PostgreSQL。  
- 设备仍按 IMEI 重绑，不写死 `cdc-wdmN` / `wwanN`。  
- 真机验收顺序与 [remaining-work.md](./remaining-work.md) P1 一致：发现 → 状态 → 短信 → eSIM。  
- 抄 VoCat 前先在本机 `gh` 对照接口语义，用自己的测试补一层，禁止 `curl | bash` 装它的二进制进 lab。

## 关键决策

| 决策 | 理由 |
|---|---|
| 产品是短信中枢，不是代理池 | 用户主需求是收发短信；代理已有，排期靠后 |
| 国内/国外用两根不同频段模组 | 一张「全球棒」在国内三大 + 国外并不省事；EC25-CN / EG25-G 分工清楚 |
| 不拉黑 MCC 460 | 国内线就是产品一半；VoCat 拉黑与目标相反 |
| 一卡多 eSIM 分「用」和「写」 | 用：模组当当前 USIM。写：读卡器或手机。避免承诺 5ber App |
| UFI 退出承诺 | 电信定制基带，实验室已证出厂也登不上现网 |
| 不搬 VoCat 源码 | 许可证不清；本仓库专有，且已有更宽的 AID 表和 PG |
| 个人端用轻量 PWA，通知仍走机器人 | 个人部署要手机看管，不必养原生 App；Web Push 在 iOS 上不稳 |
| SW 不缓存 API/SSE | 短信以服务器为准；缓存列表会把过期验证码显示成未读 |

## 承诺表（对外能说的）

| 说法 | 第 1 期 | 第 2 期 | 第 3 期 |
|---|---|---|---|
| 多 USB 集中管（最多 5） | 是 | 是 | 是 |
| 国内实体卡短信 | 是（EC25-CN 报到后） | 是 | 是 |
| 国外实体卡短信 | 否 | 是（EG25-G 报到后） | 是 |
| 网页里切一卡多 eSIM | 否 | 列表+切换 | 读卡器写 + 模组切 |
| 5ber 官方 App 替代 | 否 | 否 | 否 |
| 现有两根 UFI 收发短信 | 否 | 否 | 否 |
| 手机浏览器看管短信 | 是（窄屏能用） | 是 | 是 |
| 加到主屏幕的 PWA | 否（第 1b，需 HTTPS） | 是 | 是 |
| 靠 PWA 推送替代 Telegram | 否 | 否 | 否 |

## 下一步（执行顺序）

1. 买 **EC25-CN USB 成品**，插 VM 113 已直通的口，跑第 1 期验收（含手机浏览器走通短信）。  
2. 国内短信通了：上 HTTPS 入口 + 第 1b 轻量 PWA。  
3. 第 2 期软件已先行；**EG25-G** 到货只跑第 2 期硬件验收，不必再等棒子写列表/切换。  
4. 再买一只 CCID 读卡器验证第 3 期写卡（软件已通）。  
5. 第 4 期代理/IMS、第 5 期 Web Push 都可选，不挡个人日常看短信。

没有 EC25 之前，不把 UFI 上的搜网失败当短信回归。

## 无硬件先行（2026-08-15 已合入）

EC25 未到时先做完、不依赖报到/真卡的软件：

- 设备 `lane`：`cn` / `intl` / 空，配置 → persist → API → 添加/编辑表单 → 列表徽章  
- `GET /api/sms/contacts?lane=` + 短信页「全部 / 国内 / 国外」  
- 窄屏短信：只显示列表或只显示会话，有返回  
- PWA 壳：`manifest.webmanifest` + 只缓存静态资源的 `sw.js`（`/api`、SSE、`/ping` 不缓存）  
- `GET /api/readers`：pcscd 缺失时 200 + `daemon=missing` 明示；添加设备对话框一行提示  
- 读卡器 APDU：pcscd Unix 协议通道，无 CGO  
- 国外线 IMS 补通路 + 国内线直连代理  
- 插件：zip 安装 / 启停 / 卸载 / 侧栏 iframe / 本机后端  

仍等硬件：EC25 报到与真短信、HTTPS 安装 PWA、读卡器插卡写卡、EG25 IMS。承诺表不提前勾第 1 期「国内实体卡短信」。模组对照见 [hardware-support.md](./hardware-support.md)。
