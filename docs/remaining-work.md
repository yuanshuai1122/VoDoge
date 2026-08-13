# VoHive 剩余工作

> 更新：2026-08-13（P0 完成后）
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

> **注意**：以上全部在本机 Docker 中验证。GitHub Actions 上的实际结果仍需推送后确认。

---

## P1 — 需要真实模组才能验证（代码已写，未经现场）

必须在**接有 Quectel 模组的 Linux 主机**上做（Windows/Docker Desktop 无 USB 透传，
日志里的 `未发现调制解调器` 属预期）。

| 项 | 验证什么 |
|----|----------|
| 设备发现与添加 | 发现列表字段、degraded（无 IMEI）判定、`started=false + warning` 的提示 |
| 状态灯 | 9 个 `lifecycle_phase` × 8 个布尔位的组合展示是否可解释 |
| 概览 SSE | `overview` / `traffic` / `ussd` 三种事件、断线重连、切页不泄漏连接 |
| eSIM | Profile 列表、下载进度流、切换、改名、删除的 warning/space_delta |
| **ESIM_BUSY 并发** | 真实 APDU 仲裁下 409 + `retryAfterMs` 的调度是否正确 |
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
| 设备配置 Tab 可编辑（现只读） | ⬜ | 先梳理 `PUT /devices/:id` 接受的字段与校验规则 |
| 代理实例新增/编辑 | ⬜ | 后端是整体 `PUT /proxy-instances/config`，需确认替换语义与必填项 |
| 短信投递状态明细 UI | ⬜ 端点与类型已备（`parts_total`/`acks`/`last_error`） | 仅 VoWiFi 通道有 `message_id`，需真实短信才有意义 |
| weixin 通知渠道表单 | ⬜ | 其 QR 接口在 OpenAPI 中声明但**后端未实现** |
| E911 websheet 入口 | ⬜ | 需代理运营商页面，先定用新窗口还是 iframe |

---

## P3 — 后端遗留（api-matrix §8）

| # | 问题 | 影响 |
|---|------|------|
| 1 | eSIM 激活码经 **GET query** 传输 | `confirmation_code` 进浏览器历史与 Referer；改 POST body 需前后端同步改 |
| 2 | `/api/docs` 免鉴权，但它要拉的 `/api/openapi.yaml` 需鉴权 | Swagger UI 页面必然空白 |
| 3 | `/api/health` 需鉴权，注释却写「外部监控用」 | 监控接不上 |
| 4 | Next dev 的 rewrite 代理**缓冲 SSE** | 仅开发期：`next dev` 下流式端点收不到数据；生产同源无此问题。若要修，需为其余 3 个 SSE 端点也放开 CORS |
| 5 | OpenAPI 已显著滞后（缺 17 条、含 3 条不存在） | 见 api-matrix §7；不要据其生成类型 |

---

## P4 — 交付与运维

| 项 | 状态 | 说明 |
|----|------|------|
| 备份说明改 `pg_dump` | ✅ 已做 | README 与 DOCKERHUB 均已更新 |
| GitHub Actions 实际转绿 | ⬜ **下一步** | 本机已全绿，需推送后在 Actions 上确认 |
| GHCR 镜像发布 | ⬜ | `docker-publish.yml` 存在但未验证；依赖 CI 先绿 |
| 多架构构建 | ⬜ | `Dockerfile.github` 的 arm64/armv7 路径未验证 |
| `cmd/dbmigrate` | ⬜ | PG 计划阶段 D，从未实现。仅在有存量 SQLite 数据要迁移时才必要 |

---

## 下一步

```
P4 确认 Actions 转绿 ──► GHCR 发布 ──► 多架构验证
P1 现场验证（需硬件，完全并行）──► 发现的问题回流 P2
```

P0 完成意味着「装得上、跑得起来、说明书没错」。
P1 决定它是否真的可用——那部分只能在有模组的机器上做。
