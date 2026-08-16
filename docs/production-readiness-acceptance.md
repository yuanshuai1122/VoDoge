# 生产就绪审阅与软件验收

日期：2026-08-16
上游基线：`origin/main` / `367018b8325dc0b4cfed083b75edfc04233e3a60`
状态：**软件验收通过；真实模组、读卡器和 Linux 宿主安装仍需现场验收**

本记录覆盖当前未提交工作区中的生产加固改动。它不是硬件兼容性证明，也不替代合并后
GitHub Actions、发行签名和目标 Linux 主机上的安装验收。

## 1. 架构结论

| 边界 | 主入口 | 关键职责 |
|------|--------|----------|
| 进程装配 | `cmd/vodoge` | 配置、PostgreSQL、设备池、代理、通知、SIP、API 的启动与关闭 |
| 管理面 | `internal/api` | Bearer 鉴权、访问策略、OpenAPI、SSE、插件与 WebSheet 会话 |
| 设备运行时 | `internal/device` | QMI/MBIM/PCSC Worker 生命周期、热插拔、网络与 VoWiFi 编排 |
| 卡与 APDU | `internal/pcsc`、`internal/esim` | PC/SC daemon 协议、占用仲裁、eSIM 与 AKA 数据通道 |
| 语音与短信 | `internal/sipgw`、`internal/sms` | SIP 注册/鉴权、IMS 通路、短信编解码与交付状态 |
| 持久化 | `internal/db`、`cmd/dbmigrate` | PostgreSQL schema、业务数据、旧 SQLite 一次性事务迁移 |
| 前端 | `web`、`internal/web` | Next 静态导出、管理 UI、SSE 客户端、PWA Service Worker |
| 交付 | `Dockerfile`、`.github/workflows`、`scripts/install.sh` | Linux 镜像、校验和、Compose/systemd 安装与凭据落盘 |

高风险数据流是“管理请求 -> API 授权 -> 设备 Worker/APDU/SIP -> PostgreSQL”，以及
“插件或运营商页面 -> 独立 capability -> 受限反向代理”。本轮加固集中在这些边界，
没有改写模组协议和 eSIM 核心算法。

## 2. 已收口问题

- 管理 API 统一使用 Bearer；认证凭证读写、改密和并发关闭不再存在数据竞争。
- 插件运行时使用独立端口、短期且按路径限定的 capability；静态入口不再因
  `index.html` 被标准库重定向而丢失启动契约。插件状态通过临时文件原子替换，安装、启停、
  卸载只在持久化成功后提交内存状态；后端 stderr 的并发读写也已同步。
- WebSheet 使用路径 capability、sandbox/CSP、无凭据 bridge；外连执行 DNS 固定并在
  重定向后重新校验，降低 SSRF 和凭据外泄面。
- SIP Digest 严格校验 method、URI、realm、qop、nc、cnonce、nonce 有效期和重放；
  ACK/CANCEL 只继承已认证 INVITE，注册绑定源地址与传输。
- 设备池和 API 关闭并发幂等；停止接入后等待受管循环退出，并完整释放 Worker、udev、
  QMI/MBIM、代理、eSIM、PC/SC 和 SIP 资源。
- 设备池按 Worker 等待自身 bootstrap；一个永久卡死的新设备探测不再阻塞其它稳定设备
  清理。CS 外呼先原子挂接 SIP transaction，再创建媒体资源；关闭会封住新 workflow、只发送
  一个最终响应，并对并发返回的 Dial/Answer 做补偿挂断。RTP 序号与时间戳已串行化；ALSA
  子进程在部分启动失败和正常 Stop 时都会 Kill+Wait，同一音频桥实例拒绝重复 Start。
  主进程先停设备池、再停 Registrar 和语音网关。
- udev 防抖使用 timer generation；旧回调不能清除新 timer 或重复扫描，并发 Stop 会取消
  待执行 callback、等待已认领 callback 收尾。
- PC/SC 接受“daemon 正常但零读卡器”，验证 reader 长度，限制 APDU 响应为 64 KiB，
  失败时释放占用；超长响应主动断连，避免协议流失步。
- SQLite 到 PostgreSQL 的正式迁移在单事务内完成建模、复制、源主键校验和序列修复；
  表清单由生产 AutoMigrate 模型唯一生成。dry-run 的 AutoMigrate 也会回滚；旧
  `sim_cards` 号码按 IMSI/ICCID 派生到 subscription/pending，逐字段验证且不覆盖新模型值。
- SSE 统一流式响应头、heartbeat 和取消语义；前端会响应 token 变化并区分永久 4xx 与
  可重试状态。Service Worker 只管理自身版本缓存，导航按原请求精确缓存。
- Release 发布 `SHA256SUMS`；安装前验证二进制，原子替换可执行文件，随机 PostgreSQL
  凭据持久化，Compose、配置、凭据和 systemd 环境文件权限为 `0600`。运行镜像包含
  `alsa-utils`；二进制安装会对缺失的 `alsa-utils` 和 `pcscd` socket 给出可选能力告警。
- 依赖 hygiene 在 `go list -m all` 失败时硬失败，不能再假绿；前端所有生产入口统一显式
  使用 webpack。Release 镜像覆盖 amd64、arm64、arm/v7。

## 3. 软件验收证据

| 门禁 | 结果 |
|------|------|
| Linux + PostgreSQL 16 全量 Go 测试 | `go test -count=1 -p 1 ./internal/... ./pkg/... ./cmd/...` 通过 |
| 数据库与迁移实跑 | `go test -count=1 -p 1 ./internal/db ./cmd/dbmigrate` 通过 |
| Race | `internal/api`、`internal/cscall`、`internal/extensions`、`internal/device`、`internal/pcsc`、`internal/sipgw`、`pkg/logger` 全部通过；`cscall` 最终快照连续 20 轮通过 |
| 静态检查 | 全量 `go vet`、`go mod tidy -diff`、`gofmt -l`、`git diff --check` 通过 |
| 发布构建 | Release 参数下 amd64、arm64、arm/v7 全部通过；分别确认为静态 stripped ELF x86-64、AArch64、ARM EABI5（GOARM=7） |
| 前端 | 22 个测试文件、104 个用例；lint、typecheck、webpack production build 全部通过 |
| PWA 产物 | 三份 `sw.js` 字节一致，SHA-256 为 `85a3800e2060f91797544e6a6475d89ce6ba17070ac573571aac8ad2b1551788` |
| API 契约 | 104 个公开契约端点均在 OpenAPI 或有理由的豁免清单中 |
| 安装器 | `scripts/ci.sh`、`install.sh`、`install_test.sh` 语法检查与安装器测试通过 |
| Workflow | `ci.yml`、`verify.yml`、`release.yml` YAML 解析通过 |
| 进程冒烟 | PostgreSQL AutoMigrate 完成，`GET /ping` 返回 pong，`SIGTERM` 后退出码 0 |
| 整镜像 | 本机 Docker 无 buildx，未本地执行；Release workflow 已安装 buildx/QEMU 并声明三架构 |

本机未安装 `actionlint`，所以没有执行 GitHub Actions 语义 lint；workflow YAML 已解析，
真正的 Actions 执行仍以推送后的 GitHub 门禁为准。本机也不是 systemd Linux 宿主，
安装器的真实 unit 启停和目标目录权限需要在发行候选机复验。Docker 也没有 buildx，
所以没有把 legacy builder 无法解析 `$BUILDPLATFORM` 误记为代码失败或整镜像通过。

## 4. 真实硬件验收清单

以下项目没有硬件就不能诚实地判定通过：

- **EC25-CN**：发现、添加、断电重启、Hub 拔插、热插拔恢复；蜂窝驻网；真实短信各收、
  发一条；真实 CS 来电、外呼和双向音频，多轮挂断后无残留 `arecord`/`aplay`；切卡后
  ICCID 归属；生命周期与 SSE 状态一致。
- **EG25-G**：蜂窝驻网时优先蜂窝、未驻网或发送失败时回退 IMS；真实短信各收、发一条；
  VoWiFi 重连、IMS 注册状态和代理出口符合线路策略。
- **CCID + `pcscd` + eUICC**：零/单/多读卡器发现；占用冲突与释放；基础 APDU；profile
  列表、下载、启停、改名、删除；AKA/VoWiFi；超长/断连后的恢复。
- **Linux 安装**：Compose 首装和重装凭据复用；systemd 首装、重装、重启、日志、权限；
  `alsa-utils`、ALSA 设备和 `pcscd` socket 告警符合实际能力；使用发行页 `SHA256SUMS`
  验证真实下载产物。

只有上述现场项完成，才能把结论从“软件验收通过”提升为“目标硬件生产验收通过”。
