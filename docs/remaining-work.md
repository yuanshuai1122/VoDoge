# VoDoge 剩余工作

> 更新：2026-08-15（读卡器 APDU / 插件体系已合入）
> 相关：[known-issues.md](./known-issues.md)、[frontend-api-matrix.md](./frontend-api-matrix.md)、
> [frontend-react-progress.md](./frontend-react-progress.md)

---

## P0 — ✅ 已全部完成（2026-08-13）

仓库自 `0868b32` 起就无法完整编译，因此下面这些问题一直被掩盖着。
清掉编译阻塞后，才第一次能在真实 PostgreSQL 上跑测试，又连带暴露出 5 个迁移缺陷。

### 0-1 ✅ 编译阻塞已清除

两类互相独立的问题，都出自同一次提交：

1. **65 处 UTF-8 损坏**（5 个 `_test.go`）—— 见 [KI-001](./known-issues.md)，
   全部按上下文还原，多数有强约束（引号必须闭合、括号必须配对、同文件风格一致）。
2. **5 个文件残留未使用的 `path/filepath` import** —— 测试从
   `db.Init(filepath.Join(...))` 改为 `db.OpenTestDB(t)` 时没删干净，Go 拒绝未使用 import。

新增 `scripts/ci.sh` 的 `encoding` 环节防止编码问题再次潜入；
新增 `Dockerfile.vet` 用于编译含测试代码的全量源码——普通镜像构建覆盖不到这条路径。

### 0-2 ✅ 部署文档已修正

`DOCKERHUB.md` 此前 postgres 与 `VOHIVE_DB_DSN` 出现 **0 次**，照它部署必然启动即退出；
文件本身还有编码乱码。已重写，补上 postgres 服务、DSN 接线、host 网络与端口映射的区别、
以及 `pg_dump` 备份。OpenWrt 打包的 `config.yaml` 同样缺 database 段，已补。

### 0-3 ✅ 测试已在真实 PostgreSQL 上全绿

CI 列表中的全部包通过：`db, api, device, mbim, qmi, backend, esim, cscall,
proxy/traffic, notify, qqbot`。过程中修掉的 PG 迁移缺陷：

| 缺陷 | 症状 |
|------|------|
| `iccid_rekey_migration` 仍调用 `pragma_table_info()` | 服务启动即 `SQLSTATE 42883`，容器无限重启 |
| `testdb.go` 的清理表名是复数（`traffic_hours`），实际是单数 | 7 张表从不清理，测试间数据累积；改为从 `pg_tables` 动态取 |
| `unread_count` / `traffic_bytes` 在 `ON CONFLICT DO UPDATE` 中歧义 | `SQLSTATE 42702`，短信与流量写入全部失败 |
| 启动期 DDL 使缓存的 prepared plan 失效 | `SQLSTATE 0A000`，改用 `PreferSimpleProtocol` |
| 迁移测试自己也在用 `pragma_table_info()` | 断言恒不成立 |

`TestAddWorkerQMIManagedRebindsByIMEIWhenControlDeviceGone` 需要 libqmi 的
`qmi-proxy`，容器与 CI 都没有，改为条件跳过并注明原因——它测的是重绑定逻辑，
却要经完整的 QMI 启动才能到达断言点。

> 以上全部在本机 Docker 中验证。日常验证入口仍是 `bash scripts/ci.sh`。打 `v*` 标签会走 GitHub Actions：推 GHCR 并挂发行二进制。

---

## P1 — 需要真实模组才能验证（代码已写，未经现场）

需要一台能真正看到模组的机器。原生 Linux 主机最省事；Windows 上跑一台 Linux 虚机、
把 USB 直通进去也可以，见 [hardware-bringup-vmware.md](./hardware-bringup-vmware.md)
（VMware 默认禁掉全部 USB 直通，**不改 `.vmx` 设备根本进不了虚机**）。

| 项 | 验证什么 |
|----|----------|
| 设备发现与添加 | 发现列表字段、degraded（无 IMEI）判定、`started=false + warning` 的提示 |
| 状态灯 | 9 个 `lifecycle_phase` × 8 个布尔位的组合展示是否可解释 |
| 概览 SSE | `overview` / `traffic` / `ussd` 三种事件、断线重连、切页不泄漏连接 |
| eSIM | Profile 列表、**下载两步流程**（POST 建任务 → 按 `task_id` 订阅；断线重连补发）、切换、改名、删除的 warning/space_delta |
| **ESIM_BUSY 并发** | 真实 APDU 仲裁下 409 + `retry_after_ms` 的调度是否正确 |
| 短信 | 收发、游标分页翻页、换卡后按 ICCID 归属的行为 |
| USSD | 多轮会话 send/continue/cancel 的 `session_id` 传递 |
| 运营商选网 | SSE 流式扫描、锁定/恢复、forbidden 候选不可选 |
| VoWiFi | 启停与「重新注册」按钮、IMS 注册状态回显 |
| 卡策略 | 读写、APN/IP 版本下次连接生效 |

建议顺序：设备发现 → 状态灯 → 概览 SSE → 短信 → eSIM（最复杂放后面）。
每轮先跑 `node scripts/smoke-api.mjs`，它能在 UI 之前先暴露契约偏差。

---

## P2 — 功能缺口

| 项 | 状态 | 前置条件 |
|----|------|---------|
| VoWiFi 重新注册按钮 | ✅ 已做 | — |
| 发送短信显示分片数 | ✅ 已做（长短信按条计费，值得告知） | — |
| 设备配置 Tab 可编辑 | ✅ 已做 | 核对结论：PUT 是**整体替换**（漏传即清空），且策略字段被后端用当前有效值覆盖，故表单只放硬件/身份字段 |
| 代理实例新增/编辑 | ✅ 已做 | 核对结论：整体替换整个实例列表，故提交时带上全部实例；密码占位 `******` 后端会还原，可原样回传 |
| 短信投递状态明细 UI | ✅ 已做 | 发送后按 `message_id` 轮询回执（`acks/parts_total`），确认完即停；AT 通道无 message_id，404 时静默不显示 |
| weixin 通知渠道表单 | ❌ 不做 | 后端从未实现其 QR 接口；OpenAPI 里那 3 条声明已连同 schema 一并删除 |
| USBNET 模式入口 | ✅ 已做 | 设备配置页，独立于「保存配置」；切换会重启模组并换驱动，故要求二次确认 |
| 卸载入口（`/system/uninstall`） | ✅ 已做 | 系统设置页底部的独立危险区。要求逐字输入 `UNINSTALL` 才解锁——后端会删数据目录、配置文件与程序自身后退出，点击级别的确认挡不住手滑。成功即终态，不等轮询 |
| E911 websheet 入口 | ✅ 已做 | 定为**新窗口**：页面不受控，CSP/X-Frame-Options 可能拒绝内嵌，且跨源观察不到完成——完成信号改由新增的 `GET /websheets/:id/status` 轮询提供 |

---

## P3 — 后端遗留（api-matrix §8）

| # | 问题 | 影响 |
|---|------|------|
| 1 | ~~eSIM 激活码经 **GET query** 传输~~ | ✅ 已修复：POST 建任务 + GET 按 `task_id` 订阅，激活参数只走请求体 |
| 2 | ~~`/api/docs` 拉不到 spec~~ | ✅ 已修复：spec 移至免鉴权区 |
| 3 | ~~`/api/health` 注释与实现矛盾~~ | ✅ 已澄清：返回逐设备明细故需鉴权，监控改用 `/ping` |
| 4 | ~~Next dev 代理缓冲 SSE~~ | ✅ 已缓解：4 个 SSE 端点均加 CORS（仅 Debug 放行 localhost），前端 dev 直连 :7575 |
| 5 | ~~OpenAPI 已显著滞后~~ | ✅ 已对齐：路径由 `scripts/ci.sh routes` 校验，**响应形状**由 `openapi_test.go` 校验（每个 2xx 必须引用 `Envelope`） |
| 6 | ~~成功响应 6 种形状~~ | ✅ 已统一为一个信封 `{data, meta, request_id}`（实测是约 60 种，不是 6 种）。见 [api-envelope-design.md](./api-envelope-design.md) |

---

## P4 — 交付与运维

| 项 | 状态 | 说明 |
|----|------|------|
| 备份说明改 `pg_dump` | ✅ 已做 | README 与 DEPLOY.md 均已更新 |
| 构建与验证入口 | ✅ 已做 | 本机 `scripts/ci.sh`；发版 `.github/workflows/release.yml` 推 `ghcr.io/yuanshuai1122/vodoge` 并挂 GitHub Release |
| 一键安装 | ✅ 已做 | `scripts/install.sh`：优先 Compose + GHCR，否则发行二进制 + systemd |
| 读卡器 VoWiFi / AKA | ✅ 已做 | 无射频，AKA 走 PC/SC APDU，跳过飞行模式 |
| 深色 / 中英界面 | ✅ 已做 | next-themes + `web/lib/i18n` 完整中英目录，顶栏可切换 |
| `cmd/dbmigrate` | ✅ 已做 | PG 计划阶段 D。见 [db-migrate-runbook.md](./db-migrate-runbook.md) |
| 前端测试 | ✅ 已做 | vitest + testing-library，22 个测试文件 / 104 例；已接入验证流水线 |
| gofmt 进流水线 | ✅ 已做 | `.gitattributes` 把源码钉成 LF 后才可用——此前 Windows 检出为 CRLF，gofmt 会把 548 个 Go 文件里的 450 个都报成未格式化 |
| 多架构构建 | ✅ 已做 | `scripts/ci.sh multiarch`。arm64 与 armv7 各约 1.5 分钟——前端与 Go 编译都固定在 BUILDPLATFORM 上走交叉编译，不走 QEMU（后者会拖到十几分钟）。产物按 ELF 头验证：AArch64 / ARM 32-bit EABI5 |

---

## 下一步

```
P1 现场验证（需硬件）──► 发现的问题回流 P2

读卡器 VoWiFi / AKA、中英界面、一键安装与 GHCR 发版已软件先行。
真机仍要：CCID 读卡器 + `pcscd` + 能做 IMS 的卡。

UFI 不承担短信验收；第 1 期真机改为 EC25-CN。
个人入口：无硬件部分已合入（lane 分线、短信过滤、窄屏会话、PWA 壳、读卡器发现打桩）。
第 1 期真机短信仍要 EC25-CN；第 1b 可安装 PWA 仍要 HTTPS。通知走 Telegram/Bark。
第 2 期国外线软件已先行（当前 profile、禁用、切卡错误码）；EG25-G 到货只验证。
第 3 期读卡器软件已先行（加设备、互斥、eSIM 页、pcscd APDU）；插卡只验证。
第 4 期软件已先行（国外线 IMS 补通路、国内线不出国代理）。
第 4b 期软件已先行（全局滚动 1 小时发送限额，接收不限）。
第 4c 期软件已先行（按 ICCID 绑前置代理，国内线仍直连）。
第 4d 期软件已先行（设备配额默认 5、可调 1–10）。
第 4e 期软件已先行（本机自签 HTTPS，同一端口 HTTP/TLS）。
第 4f 期软件已先行（默认仅内网可打开管理面）。
插件体系软件已先行（zip 安装/启停/卸载、侧栏 iframe、本机后端）。硬件支持表见 [hardware-support.md](./hardware-support.md)。

PVE lab（VM 113 / `vodoge.lab.lan`）见 [pve-lab-deploy.md](./pve-lab-deploy.md)。
有卡那根在原厂 RNDIS/CPE 下也是紧急呼叫、没互联网（基带 `UFI103_CT`、卡 `46011`）。
不是单 QMI 的问题。代理冒烟要换卡或换地点。切回 QMI 后有卡那根掉总线，需拔插。
```

P2–P4、`internal/api` 重构与响应结构统一均已收口
（见 [backend-api-refactor-plan.md](./backend-api-refactor-plan.md)、
[api-envelope-design.md](./api-envelope-design.md)）。

**仍在架构债清单上、且卡在硬件后面的**：`esim/manager.go`（4115 行）、
`device.Pool`（46 字段的 Worker）、API 45 处直接摸设备内部、
`internal/device` 剩余 8 处猴子补丁、彻底干掉 `db.DB` 全局。
它们都在设备启停路径上，改动的正确性判据在真实模组上。

**不卡硬件、可持续做的**：继续扩大覆盖率；当前 47 个 Go 包中 37 个目录有测试，
前端为 22 个测试文件 / 104 例。数量不是完成标准，设备失败恢复和跨层状态机仍应优先。

P0 完成意味着「装得上、跑得起来、说明书没错」。
P1 决定它是否真的可用——那部分只能在有模组的机器上做。
