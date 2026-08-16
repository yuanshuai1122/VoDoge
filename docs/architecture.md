# 架构总览

日期：2026-08-16

这份文档回答一个问题：**代码是怎么摆的，一件事该去哪儿改。**

它不重复接口细节（见 [frontend-api-matrix.md](./frontend-api-matrix.md)）、硬件行为（见
[hardware-support.md](./hardware-support.md)）和部署步骤（见 [../DEPLOY.md](../DEPLOY.md)）。

## 进程模型

**一个进程**。`cmd/vodoge/main.go` 里没有 worker 进程、没有消息队列、没有第二个二进制。
唯一的外部依赖是 PostgreSQL；连不上就退出，没有 SQLite 兜底。

前端是 Next.js 静态导出，`make frontend-dist` 打进 `internal/web/dist`，由
`//go:embed all:dist` 编进同一个二进制。`-backend-only` 跳过挂载，此时进程不带 UI。

单进程是刻意的：模组是独占资源，一根棒子同一时刻只能有一个持有者。多进程要先解决
「谁持有 `cdc-wdmN`」，而这件事目前由进程内的设备池直接管着（见下）。

### 启动顺序

`main()` 的顺序不是随意的，后一步依赖前一步的产物：

| 步骤 | 做什么 | 失败时 |
|---|---|---|
| 1 | `config.InitGlobalManager` 读 yaml | `log.Fatalf` |
| 2 | `logger.Setup`，并把标准库 `slog` 重定向进来 | — |
| 3 | `db.Open`（仅 PostgreSQL，`AutoMigrate` 可配） | `log.Fatalf` |
| 3.5 | MCC/MNC 国家表、短信联系人回填（后台 goroutine） | 降级：国家规则按未知国家直连 |
| 4 | `device.NewPool` + 注入 db-backed 卡策略 resolver，旧 yaml 策略一次性种子迁移 | 跳过种子 |
| 5 | 代理实例管理器 `proxy/server.NewManager` | — |
| 6 | 语音网关 `voicehost.NewGateway`；`voice_gateway.sip.listen` 非空才起 SIP Registrar | 记日志，继续 |
| 7 | `notify.NewManager` | 记日志，继续 |
| 8 | `pool.StartAll()` 起各设备工作器 | 单设备失败不影响进程 |
| 9 | 流量采样器 + 实时流量管理器 | — |
| 10 | `api.New(...).Run()` | 错误进 `apiErrCh`，主循环退出 |

只有配置、日志、数据库是硬失败。硬件相关的每一步都能降级——**没插棒子也要能打开管理面**，
否则现场没法排查。

### 关闭顺序

关闭顺序同样有依赖，写在 `main.go` 的 defer 块里：

```
API → notify → 流量采样 → 代理实例 → 设备池 → 语音网关 → SIP Registrar
```

设备池排在语音网关**前面**：Worker 收尾时要发 BYE、要清 RTP，那需要语音网关还活着。
反过来先拆语音，关闭期间的挂断和媒体清理就会静默丢掉。

10 秒优雅超时，12 秒强制退出；期间再收一次信号立刻走。

## 分层

```
  浏览器 / PWA
      |
      |  :7575 管理面        :7576 插件运行时     两个监听器，端口必须不同
      |  netaccess           来源 IP 门禁
      v
  internal/api               gin；路由 = 一张表
      |
      v
  internal/data/repo         API 层唯一的持久化入口
      |
      v
  internal/db                gorm + PostgreSQL
== == == == == == == == ==  以上无硬件也能跑
  internal/device            Pool / Worker；硬件的唯一入口
      |
      v
  qmi / mbim / modem(AT) / pcsc      传输层
      |
      v
  USB 模组 / 读卡器
```

那条虚线以上的部分，测试可以完全脱离 `/dev`；以下要么摸真设备，要么注入假实现。
这条线是 API 层测试不需要 QMI 设备的原因。

### API 层

**路由以一张表声明**（`internal/api/routes.go`），不是散在注册函数里的一串语句。
起因写在文件头：同一份信息本来分在注册语句、SSE 的 `?token=` 白名单、OpenAPI spec 三处，
改一处漏另一处没有任何编译期信号——eSIM 下载路径挪窝时就漏了白名单，SSE 一律 401，
而编译、vet、单测全绿。

现在白名单由表派生，`scripts/check-routes.mjs` 也读同一张表校验 spec。加端点时**只改表**。

鉴权分三档，不能合并：

| 档 | 含义 | 例 |
|---|---|---|
| `authRequired` | 走中间件，无令牌 401 | 绝大多数 |
| `authNone` | 真正公开 | `/auth/login`、`/docs`、`/openapi.yaml` |
| `authInHandler` | 中间件放行，handler 自己校验另一套凭证 | `/rotateip`、websheet 系列 |

第三档单列，是因为「不挂中间件」有两种完全不同的含义，混在一起看会把 `/rotateip`
误读成无鉴权（它在 handler 内经 `authorizeRotate` 校验）。

挂在根而非 `/api` 组下的 `/ping`、`/debug/embed` 不在表里，spec 也不描述。`/ping` 只回
`pong`、不含任何设备信息，外部监控用它；带设备明细的 `/health` 需要鉴权。

路由按域分组：meta / dashboard / sms / settings / device / cardPolicy /
operatorSelection / proxy / upstreamProxy / esim / vowifi / websheet / log / extension。
handler 按同样的域分文件。

### 持久化边界

`internal/data/repo.Store` 是 API 层唯一的持久化入口，handler 不再直接调 `internal/db`。

这层**刻意只做接口与转发**，实现直接委托给 `internal/db` 里已有的函数，不重写查询。
换来的是 API 包测试不用连真库——此前必须起 PostgreSQL 容器、跑十几秒、还得 `-p 1` 串行
（各包共用一个库，一个包的 truncate 会清掉另一个包的数据）。

全局 `db.DB` 仍然存在，`device` / `notify` / `proxy` 三层还直接用它。彻底下推
`*gorm.DB` 要连硬件路径一起改，等真机验证之后再动。

## 设备路径

`device.Pool` 持有一组 `Worker`，按设备 ID 索引，每个 Worker 一根棒子（或一个读卡器）。
`internal/device` 是全仓最大的包（约 1.5 万行非测试代码），因为硬件的失败模式都在这儿。

Worker 上挂着这根棒子的全部状态：`Modem`（AT）、`Backend`、`QMICore`、`MBIMCore`、
`CSCallMgr`、eSIM 管理器等。设备按 **IMEI** 重绑，不写死 `cdc-wdmN`——USB 重新枚举后
节点名会变，绑节点名就会张冠李戴。

`generation` 字段用来判定「这个回调属于当前这一代 Worker 吗」。重启、eSIM 切换、传输恢复
都会重建 Worker，旧 Worker 的异步回调仍会陆续到达，没有代号就会写脏新 Worker 的状态。

### 后端模式

`device_backend` 有四个取值，但它们不在同一层：

| 取值 | 控制面 | 说明 |
|---|---|---|
| `at` | AT 命令 | 默认；`NormalizeBackendMode` 把无法识别的值也归到这儿 |
| `qmi` | QMI | 数据面也是 QMI，不用 RNDIS |
| `mbim` | MBIM | |
| `pcsc` | PC/SC APDU | 读卡器，不走 `NewBackend`，另有一条路径（`pool_pcsc.go` + `internal/pcsc`），不启无线电 |

前三个由 `backend.NewBackend(mode, ...)` 造实例，`internal/backend` 的文件按后端分
（`at_backend.go` / `qmi_backend.go` / `mbim_backend.go`），共用语义（注册、运营商选择、
SMSC、USSD、SIM 鉴权）各有一份中立定义。

校验与归一化是两件事，别混：`ValidateBackendMode` 在**写入之前**拒绝非法值，判据是原始
输入；`NormalizeBackendMode` 在**写入之后**兜底，认不出的一律当 AT——配置已经落盘了，
此时报错没人接，降级跑比起不来强。

### 短信通路

`smsMode` 四种，选哪种由设备状态定，不由配置直接指定：

| 模式 | 驱动 | 用在 |
|---|---|---|
| `AT` | URC `+CMTI` → `AT+CMGR` | AT 后端 |
| `QMI` | WMS `EventNewSMS` + 定时轮询 | QMI 后端 |
| `MBIM` | SMS `READ` indication + 定时轮询 | MBIM 后端 |
| `VoWiFi` | IMS，AT/QMI 短信全禁 | VoWiFi 起来时 |

发送方向由 `planSMSSend(lane, vowifiActive, radioOK)` 决定主通道和回退通道
（`internal/api/sms_transport.go`）。它是纯函数、有单测，因为「先走哪条」是产品语义，
不该埋在设备层的分支里：

- `lane=intl`：已驻网先蜂窝，失败或未驻网再 IMS；
- 国内 / 未分线：VoWiFi 开着只走 IMS，否则只走蜂窝。

线路由人标（`lane=cn|intl|empty`），不按 MCC 推断，不拉黑中国卡。

### 卡资源互斥

一张卡同一时刻只能被一个东西摸。`internal/apduarbiter` 用租约把 APDU 通道排队：

- 租约类型：`session` / `oneshot` / `transport` / `barrier`
- 调用分类：`EUICCWrite` / `EUICCRead` / `USIMAKA` / `SMSC` / `SwitchBarrier` / `Recovery`

于是 eSIM 写卡、VoWiFi 的 AKA、SMSC 读取不会撞在一起；eSIM 切换期间靠 `SwitchBarrier`
挡住其他访问。

更外一层是绑定查重：添加设备时，IMEI、控制节点、USB 路径、接口、AT 口、读卡器名
这几个键都不允许绑到两台设备上（`deviceBindingConflict`）。读卡器与模组本来就不能
同时摸同一张卡，绑重了就是两条路径抢同一张卡。

## 旁路子系统

| 子系统 | 包 | 要点 |
|---|---|---|
| 出站代理 | `proxy/server` | 按设备起 SOCKS/HTTP 实例；QMI 数据面连上会触发 `SyncProxyConfigs` |
| 流量 | `proxy/traffic` | 采样器落库 + 实时快照订阅（SSE） |
| 前置代理 | `upstreamproxy` | 按 ICCID / 国家绑上游；国内线仍直连模组出口。国家表拉不到就按未知国家直连 |
| 语音 | `sipgw` + 外部 `boa-z/vowifi-go` | Registrar 仅在配置了 `sip.listen` 时启动，用于 Linphone 接入 |
| 通知 | `notify` | telegram / bark / feishu / email / webhook / pushplus / qq，不靠网页推送 |
| 插件 | `extensions` + `api/plugin_runtime.go` | 见下 |
| 承载页 | `websheet` | 反代运营商自己的页面；跨源看不到完成与否，靠状态轮询，会话按 TTL 回收 |
| 自签 HTTPS | `httpsmode` | 与 HTTP 共用端口：握手首字节 `0x16` 走 TLS，其余走明文 |
| 门禁 | `netaccess` | 默认只放行回环 / RFC1918 / 链路本地 / ULA；**默认不看 `X-Forwarded-For`** |
| 自更新 | `updater` | 拉 GitHub Release 对应架构的二进制自替换 |

### 为什么插件要独立端口

插件页面跑在 `:7576`，与管理面 `:7575` **不同源**，进程启动时会拒绝两者相同。插件是
zip 装进来的第三方代码、以后端管理员权限运行，同源就意味着它能碰管理面的凭证与
DOM。独立端口 + 沙箱 iframe + capability 中间件（HMAC 签名的短期会话）把它隔在外面，
反代时还会剥掉 `Cookie` / `Authorization` / `Set-Cookie`。

插件自带的后端起在 `127.0.0.1` 随机端口，只经 `/api/extensions/:id/backend` 反代出去。

**这仍不是安全边界，只是减面。** 插件以后端权限跑，只装完全信任的包。

## 端口

| 端口 | 用途 | 备注 |
|---|---|---|
| 7575 | 管理面 + `/api` | `server.port` |
| 7576 | 插件资源与插件后端反代 | `server.plugin_port`，必须与上者不同 |
| 3000 | `next dev` | 仅开发；`/api/*` rewrite 到 7575 |
| 10000–20000 | RTP | 仅启用语音网关时 |

后端**没有全局 CORS**。开发期靠 Next 的 rewrites 走同源，不要跨源直连。

## 代码地图

非测试代码行数，用来判断「这块有多少东西」：

| 包 | 行 | 职责 |
|---|---|---|
| `internal/device` | 14.9k | 设备池、Worker 生命周期、发现、恢复、VoWiFi 编排 |
| `internal/api` | 10.5k | HTTP 层：路由表、handler、SSE、插件运行时 |
| `internal/esim` | 5.4k | LPA、profile 列举 / 切换 / 下载 |
| `internal/modem` | 5.3k | AT 通道与解析 |
| `pkg/mbim` | 4.2k | MBIM 协议编解码 |
| `internal/backend` | 3.6k | 后端接口与 AT / QMI / MBIM 三份实现 |
| `internal/db` | 3.4k | gorm 模型、迁移、查询 |
| `internal/qmi` | 2.7k | QMI 通道与 indication |
| `internal/notify` | 2.5k | 通知渠道 |
| `internal/cscall` | 2.1k | CS 通话 |
| `pkg/smscodec` | 2.1k | PDU 编解码 |
| `internal/vowifihost` | 1.9k | VoWiFi 运行时宿主 |
| `internal/config` | 1.8k | yaml 配置与回写 |
| `internal/proxy` | 1.7k | 代理实例与流量 |
| `internal/sipgw` | 1.6k | SIP Registrar |
| `internal/qqbot` | 1.5k | QQ 通知渠道的机器人侧 |
| `internal/pcsc` | 1.4k | 读卡器发现与 APDU 通道（无 CGO） |
| `internal/apduarbiter` | 1.1k | 卡访问租约 |
| `internal/websheet` | 1.0k | 运营商页面承载与反代 |
| 其余 | <1k 各 | `extensions` `pkg/logger` `sim` `upstreamproxy` `data/repo` `e911` `httpsmode` `simaid` `sms` `netprobe` `updater` `netaccess` `smsnotify` `pkg/smsutil` `pkg/taskpool` |

## 改东西去哪儿

| 要做的事 | 动这些 |
|---|---|
| 加一个 API 端点 | `internal/api/routes.go` 的表 + 对应域的 handler 文件 + `openapi.vodoge.yaml`；`node scripts/check-routes.mjs` 会盯着 |
| 加一列 / 改表结构 | `internal/db/models.go` + `migrate.go`；API 层经 `data/repo` 的接口 |
| 加一种通知渠道 | `internal/notify/` 加一个 channel 实现 + `settings_*.go` 里的配置项 |
| 改短信选路 | `internal/api/sms_transport.go` 的 `planSMSSend`（纯函数，先补测试） |
| 支持一款新模组 | `internal/backend/` 的能力探测 + `internal/device/` 的发现与绑定；先更新 [hardware-support.md](./hardware-support.md) |
| 改前端 | `web/`；生产要 `make frontend-dist` 重新嵌入才生效 |
| 加一条卡访问路径 | 先去 `internal/apduarbiter` 认领一个 `APDUClass`，别绕过仲裁 |

## 不变量

改动碰到下面任何一条，先想清楚再动：

1. **一个进程持有硬件。** 设备池是模组的唯一入口，不要在别处直接开 `cdc-wdmN`。
2. **只有 PostgreSQL。** 没有 SQLite 运行时路径，别加回来。
3. **路由表是唯一事实。** 端点、SSE 白名单、spec 都从它派生。
4. **API 层不碰 `db.DB`。** 走 `data/repo`，否则测试又要连真库。
5. **卡访问必须经仲裁。** 直接发 APDU 会和 eSIM 写卡、AKA 撞车。
6. **默认只放行内网。** 公网要显式改策略并上 HTTPS，且默认不信 `X-Forwarded-For`。
7. **设备按 IMEI 绑，不按节点名。**
8. **线路由人标。** 不按 MCC 推断，不拉黑中国卡。

## 相关文档

| 文档 | 内容 |
|---|---|
| [frontend-api-matrix.md](./frontend-api-matrix.md) | HTTP 契约，接口的唯一依据 |
| [api-envelope-design.md](./api-envelope-design.md) | 响应结构 `{data, meta, request_id}` |
| [backend-api-refactor-plan.md](./backend-api-refactor-plan.md) | `internal/api` 重构的过程与验收 |
| [hardware-support.md](./hardware-support.md) | 模组、短信通道、读卡器、插件 |
| [backend-db-decisions.md](./backend-db-decisions.md) | 数据库选型 |
| [../DEPLOY.md](../DEPLOY.md) | 部署 |
| [remaining-work.md](./remaining-work.md) | 剩余工作 |
