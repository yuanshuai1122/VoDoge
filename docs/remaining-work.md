# VoHive 剩余工作

> 更新：2026-08-13，紧接 Docker 部署与首轮联调之后。
> 前端计划（阶段 1–10）已全部实现，本文件列的是**在此之后**仍未完成的事。
> 相关：[known-issues.md](./known-issues.md)、[frontend-api-matrix.md](./frontend-api-matrix.md)、
> [frontend-react-progress.md](./frontend-react-progress.md)

---

## P0 — 阻塞项（当前是坏的）

### 0-1 CI 是红的：65 处 UTF-8 损坏 · 0.5–1 天

`go build` / `go test` 在 5 个 `_test.go` 上直接失败，详见 [KI-001](./known-issues.md)。
镜像已通过排除测试文件绕开，但 CI 用完整仓库，仍然过不了。

- 需要**逐处补回被吞掉的两个字符**（被毁的汉字 + 其后一个字符）
- 重复模式居多（`"已启用"` / `"未启用"` / `"中国联通"`），可高置信还原
- 少数断言文案（如 `t.Fatalf("应取卡策【?】 %+v"`）需原作者确认是 `策略: ` 还是 `策略，`
- 必须 65 处**全部**修完，Go 才接受这些文件
- 修完后建议把 `known-issues.md` 里的检测脚本接进 `scripts/ci.sh`，防止再次引入

### 0-2 部署文档会把人带沟里 · 0.5 天

`DOCKERHUB.md` 中 **postgres / VOHIVE_DB_DSN 出现 0 次**——它仍是 SQLite 时代的说明。
照着它的 compose 示例部署，服务会因为拿不到 DSN 直接 Fatal 退出。
该文件同时存在编码损坏（多处 `?` 乱码）。

- 补 postgres 服务与 `VOHIVE_DB_DSN`，对齐 `docker-compose.hub.yml`
- 修掉乱码
- 说明「升级到本版本需要 PG，旧 `data/vohive.db` 不会自动迁移」

### 0-3 CI 从未真正跑绿过 · 0.5 天

`ci.yml` 的 postgres service 与 `TEST_DATABASE_URL` 是本轮加的，
但因 0-1 一直没跑通，**整套 CI 配置尚未被验证**。0-1 修完后需确认：
`./internal/db` 与 `./internal/api` 的测试在 CI 的 PG 上真的能过。

---

## P1 — 需要真实模组才能验证（代码已写，未经现场）

必须在**接有 Quectel 模组的 Linux 主机**上做（Windows/Docker Desktop 无 USB 透传，
现在日志里的 `未发现调制解调器` 属预期）。

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
| 卡策略 | 读写、APN/IP 版本下次连接生效 |

建议顺序：设备发现 → 状态灯 → 概览 SSE → 短信 → eSIM（最复杂放后面）。
优先跑 `node scripts/smoke-api.mjs`，它能在 UI 之前先暴露契约偏差。

---

## P2 — 有意留下的功能缺口

| 项 | 为什么没做 | 前置条件 |
|----|-----------|---------|
| 设备配置 Tab 可编辑（现只读） | `PUT /devices/:id` 的完整字段集未核对，给表单有写坏配置的风险 | 先梳理该端点接受的字段与校验规则 |
| 代理实例新增/编辑 | 后端是整体 `PUT /proxy-instances/config`，字段语义未确认 | 确认整体替换的语义与必填项 |
| weixin 通知渠道表单 | 其 QR 接口在 OpenAPI 中声明但**后端未实现**（3 个幽灵端点） | 后端先实现，或从 spec 删除 |
| VoWiFi 重连 / E911 websheet 入口 | websheet 需代理运营商页面，交互形态待定 | 决定用新窗口还是 iframe |
| 短信投递状态 UI | endpoint 已封装，未接界面 | 需要真实短信才有意义 |

---

## P3 — 后端遗留（api-matrix §8，均不阻塞前端）

| # | 问题 | 影响 |
|---|------|------|
| 1 | eSIM 激活码经 **GET query** 传输 | `confirmation_code` 进浏览器历史与 Referer；改 POST body 需前后端同步改 |
| 2 | `/api/docs` 免鉴权，但它要拉的 `/api/openapi.yaml` 需鉴权 | Swagger UI 页面必然空白 |
| 3 | `/api/health` 需鉴权，注释却写「外部监控用」 | 监控接不上 |
| 4 | Next dev 的 rewrite 代理**缓冲 SSE** | 仅开发期：`next dev` 下 4 个流式端点收不到数据；生产同源无此问题。若要修，需为其余 3 个 SSE 端点也放开 CORS（目前只有 `/logs/stream` 有） |

---

## P4 — 交付与运维

| 项 | 说明 | 工期 |
|----|------|------|
| GHCR 镜像发布 | `docker-publish.yml` 存在但本轮未验证；依赖 CI 先绿 | 0.5 天 |
| `cmd/dbmigrate` | PG 计划的阶段 D，**从未实现**。仅在有存量 SQLite 数据需要迁移时才必要 | 1 天 |
| 备份说明 | 从「拷 `.db` 文件」改为 `pg_dump`，README/DOCKERHUB 都要提 | 0.5 天 |
| 多架构构建 | `Dockerfile.github` 的 arm64/armv7 路径本轮未验证 | 0.5 天 |

---

## 建议顺序

```
P0-1 修 UTF-8 ──► P0-3 CI 转绿 ──► P4 GHCR 发布
      │
      └──► P0-2 部署文档（可并行，与代码无关）

P1 现场验证（需硬件，与上面完全并行）
      └──► 期间发现的问题回流到 P2
```

**最小可交付**：P0 三项做完，CI 绿 + 文档正确 + 镜像可发布，
此时「装得上、跑得起来、说明书没错」。P1 决定它是否真的可用。
