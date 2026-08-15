# VoDoge 服务端：SQLite → PostgreSQL 完整改造计划

> **状态**：计划定稿（策略已确认，**代码尚未改**）  
> **仓库**：https://github.com/yuanshuai1122/VoDoge  
> **日期**：2026-08-11  
> **策略**：**直接切断 SQLite 运行时，仅使用 PostgreSQL**（见 `docs/backend-db-decisions.md`）

---

## 0. 一句话目标

把 VoDoge 后端的持久化从「单文件 SQLite」换成「独立 PostgreSQL」，**业务 API / 设备池 / 短信 / eSIM / 代理逻辑尽量不动**，只改：配置、连接、方言、迁移、测试、部署。

---

## 1. 背景与约束

### 1.1 为什么改

| 点 | SQLite 现状问题 | PG 收益 |
|----|-----------------|--------|
| 并发 | `MaxOpenConns(1)`，写锁瓶颈 | 多连接、适合多模组+API 并发 |
| 部署 | 文件库，难共享/难 HA | 标准服务，易备份/监控 |
| 运维 | 拷贝 `.db` | `pg_dump` / 复制 / 云托管 |
| 与前后端分离 | 单机文件感强 | 与独立前端、多实例后端一致 |

### 1.2 已确认决策

| ID | 决策 | 说明 |
|----|------|------|
| D1 | **仅 PostgreSQL** | 不双引擎，不运行时回退 SQLite |
| D2 | 时区 **UTC 存库** | DSN `TimeZone=UTC` 或 timestamptz |
| D3 | 内网 SSL | compose 可用 `sslmode=disable` |
| D4 | 迁移工具 | 独立 `cmd/dbmigrate`（可选，读旧 `.db` → 写 PG） |
| D5 | 范围 | **只动服务端数据层**；不改前端业务、不改模组协议 |

### 1.3 非目标

- 不拆微服务、不分库分表  
- 不重写短信/eSIM/代理业务  
- 不在本计划内实现 React  
- 不做双写（SQLite+PG 同时写）  

---

## 2. 现状代码锚点（必改清单）

### 2.1 核心入口

| 文件 | 现状 | 改造动作 |
|------|------|----------|
| `internal/db/db.go` → `Init` | 文件路径 + glebarez + PRAGMA + `MaxOpen=1` | 改为 PG DSN；删 PRAGMA；调连接池 |
| `internal/db/sqlite_dialector.go` | 仅 SQLite | **删除**或整文件移除 |
| `cmd/vohive/main.go` | `dbPath := "data/vohive.db"` | 读 `config.Database` / 环境变量 |
| `go.mod` | `glebarez/sqlite` | 加 `gorm.io/driver/postgres`（及 `jackc/pgx` 传递依赖）；移除 sqlite 主依赖 |
| `internal/config/*` | 无 `database` 段 | 新增配置结构 + 默认值 + Viper 绑定 |

### 2.2 SQLite 专用逻辑（必须消掉）

| 位置 | 问题 | 处理 |
|------|------|------|
| `applySQLitePragmas` | `PRAGMA busy_timeout/journal_mode/...` | 删除；PG 不走 |
| `hasSQLiteTableColumn` | `PRAGMA table_info` | 改为 `information_schema.columns` 或 GORM Migrator |
| `migrateSIMCardIdentityColumnsOnly` | `ALTER TABLE ... DROP COLUMN` 手写 | 用 Migrator 或 PG 兼容 SQL，保证幂等 |
| `migrateSIMCardsToSubscriptions` | 数据迁移 + `OnConflict` | GORM OnConflict 在 PG 可用；全量测 |
| `RunICCIDReKeyMigration` | 业务数据迁移 | 在空 PG / 有数据 PG 各测一遍幂等 |

### 2.3 AutoMigrate 模型清单（表）

以 `db.Init` 中 `AutoMigrate(...)` 为准：

| 模型 | 典型表名（GORM 默认） | 备注 |
|------|----------------------|------|
| `Device` | `devices` | PK: IMEI |
| `CardPolicy` | `card_policies` | 跟 ICCID |
| `SIMCard` | `sim_cards` | 遗留+身份 |
| `SIMSubscription` | `sim_subscriptions` | PK: IMSI |
| `PendingPhoneNumber` | `pending_phone_numbers` | PK: ICCID |
| `ProxyInstance` | `proxy_instances` | 代理实例 |
| `UpstreamProxy` | `upstream_proxies` | 上游代理 |
| `UpstreamProxyCountryRule` | `upstream_proxy_country_rules` | 国家规则 |
| `SMS` | `sms` | 索引多，量大时注意 |
| `SMSContact` | `sms_contacts` | 复合主键 |
| `SMSDelivery` / `SMSDeliveryPart` | 投递状态 | |
| `TrafficMinute/Hour/Day/Week/Month` | 流量汇总 | 写入频繁 |

实现前用一次 `AutoMigrate` 到空 PG，导出 `\d+` 固化 schema 快照到 `docs/db-schema-pg.md`（可选）。

### 2.4 测试触点（必须改）

全库搜索 `db.Init(` / `filepath.Join(t.TempDir()` 等，分布在：

- `internal/db/*_test.go`
- `internal/api/*_test.go`
- `internal/device/*_test.go`
- `internal/proxy/traffic/*_test.go`
- 等

**统一改为**：`testdb.Open(t)` → 连 `TEST_DATABASE_URL` 或 compose 测试库；每测前后清理表或使用 transaction/schema。

### 2.5 部署触点

| 文件 | 改造 |
|------|------|
| `docker-compose.yml` / `docker-compose.windows.yml` / `docker-compose.hub.yml` | 增加 `postgres` 服务；vohive 注入 DSN；依赖 healthy |
| `Dockerfile*` | 无需内嵌 sqlite 文件；运行时需能连 PG |
| `README.md` / `DOCKERHUB.md` | 启动前置：必须有 PG |
| `packaging/openwrt/...` | 标注：需可达的 PG，不再「单文件库」 |
| 数据卷 | `./data` 可保留作日志/缓存；**库数据在 PG volume** |

---

## 3. 目标架构

```
                    ┌─────────────────┐
                    │  config.yaml /  │
                    │  VOHIVE_DB_DSN  │
                    └────────┬────────┘
                             │
┌────────────┐      ┌────────▼────────┐      ┌──────────────┐
│ cmd/vohive │─────▶│ db.Open(cfg)    │─────▶│ PostgreSQL   │
│ api/device │      │ gorm+postgres   │      │ (独立服务)   │
└────────────┘      └─────────────────┘      └──────────────┘
                             ▲
                             │ 可选一次性
                    ┌────────┴────────┐
                    │ cmd/dbmigrate   │◀── 旧 data/vohive.db
                    └─────────────────┘
```

- **运行时**：只认 PG。  
- **旧 SQLite 文件**：仅迁移工具输入，迁移后不再用于启动。

### 3.1 配置草案（实现时写入代码）

```yaml
# config/config.yaml
database:
  # 完整 DSN，优先于分散字段
  dsn: "host=127.0.0.1 user=vohive password=vohive dbname=vohive port=5432 sslmode=disable TimeZone=UTC"
  max_open_conns: 20
  max_idle_conns: 5
  conn_max_lifetime: 30m
  # 启动时是否 AutoMigrate（开发 true；生产可 true 首启，后续可关）
  auto_migrate: true
```

环境变量（优先级高于文件）：

| 变量 | 含义 |
|------|------|
| `VOHIVE_DB_DSN` | 主 DSN |
| `DATABASE_URL` | 别名（若设置且 `VOHIVE_DB_DSN` 空则用） |
| `VOHIVE_DB_AUTO_MIGRATE` | `true`/`false` |

**启动失败策略**：DSN 为空或连不上 → **直接 Fatal**，不再回退 `data/vohive.db`。

### 3.2 连接池建议

| 参数 | 建议初值 | 说明 |
|------|----------|------|
| MaxOpenConns | 20 | 可按模组数×2 + API 调 |
| MaxIdleConns | 5 | |
| ConnMaxLifetime | 30m | 避免云 PG 断闲置 |
| ConnMaxIdleTime | 10m | 可选 |

---

## 4. 方言与兼容风险

| 风险 | 处理 |
|------|------|
| 布尔 / 时间类型 | 交给 GORM + timestamptz；统一 UTC |
| `clause.OnConflict` | PG 支持；回归短信/订阅 upsert |
| 列探测 PRAGMA | 改为 information_schema 或 Migrator.HasColumn |
| 复合主键 / 索引名 | AutoMigrate 后人工核对长索引名 |
| 大小写 | 全 snake_case，勿混用引号标识符 |
| 大批量 traffic/sms | 迁移工具分批 INSERT（如 1000 行） |
| 并发 | 去掉「单连接」隐含假设；注意长事务 |

**预期**：绝大多数 `DB.Where/Create/Updates` 业务代码 **零改或极少改**。

---

## 5. 实施阶段（可执行任务板）

### 阶段 A — 配置与打开路径（先让空库跑起来）

| ID | 任务 | 验收 |
|----|------|------|
| A1 | `config` 增加 `Database` 结构与 Viper 加载 | 配置能解析 |
| A2 | `db.Open` / 改写 `Init`：仅 `postgres` dialector | 空库连接成功 |
| A3 | 连接池按配置设置；删除 PRAGMA | 无 PRAGMA 调用 |
| A4 | `main.go` 使用配置 DSN；失败即退出 | 无 DSN 无法启动 |
| A5 | `go get gorm.io/driver/postgres`；移除 glebarez 主依赖 | `go.mod` 干净 |
| A6 | 删除 `sqlite_dialector.go` | 编译通过 |
| A7 | 本地 compose 起 postgres + vohive 能启动到「API 服务器」日志 | 冒烟 OK |

**建议工期**：0.5–1 天

---

### 阶段 B — 迁移逻辑 PG 化

| ID | 任务 | 验收 |
|----|------|------|
| B1 | `hasTableColumn` PG 实现 | 单元测 |
| B2 | 手写迁移在空 PG 幂等 | 启动两次无报错 |
| B3 | ICCID rekey 在 PG 验证 | 有/无数据各一次 |
| B4 | AutoMigrate 全表成功 | `\dt` 表齐全 |
| B5 | 记录 schema 快照（可选文档） | `docs/db-schema-pg.md` |

**建议工期**：0.5–1 天

---

### 阶段 C — 测试体系

| ID | 任务 | 验收 |
|----|------|------|
| C1 | `internal/db/testdb` 或 `internal/testutil/dbtest` helper | 统一入口 |
| C2 | 所有 `Init(temp.db)` 改为 PG helper | 无文件库测试 |
| C3 | CI：`services: postgres` + `TEST_DATABASE_URL` | Actions 绿 |
| C4 | `go test ./internal/db/... ./internal/api/...` 等核心包 | 通过 |
| C5 | 并发写 SMS 小测 | 多 goroutine 不崩 |

**建议工期**：1–2 天

---

### 阶段 D — 数据迁移工具（有旧数据才必需）

| ID | 任务 | 验收 |
|----|------|------|
| D1 | `cmd/dbmigrate`：`--sqlite path --postgres dsn` | 可 dry-run |
| D2 | 按表顺序复制 + 分批 | 大表不 OOM |
| D3 | count 校验 + 抽样 | 行数一致 |
| D4 | `docs/db-migrate-runbook.md` 运维手册 | 可照做 |

**表复制建议顺序**：

1. `devices`  
2. `sim_cards` / `sim_subscriptions` / `pending_phone_numbers`  
3. `card_policies`  
4. `proxy_instances` / `upstream_*`  
5. `sms` / `sms_contacts` / delivery*  
6. `traffic_*`  

**建议工期**：1 天（无存量可延后）

---

### 阶段 E — 部署与文档

| ID | 任务 | 验收 |
|----|------|------|
| E1 | compose 增加 postgres（用户/密码/库/volume/healthcheck） | `docker compose up` 可用 |
| E2 | vohive 服务 `depends_on: condition: service_healthy` | 启动顺序正确 |
| E3 | README / DOCKERHUB / packaging 说明 | 新人能起 |
| E4 | 备份说明：`pg_dump` 替代拷贝 db 文件 | 文档有 |
| E5 | 进度日志 `docs/backend-db-progress.md` 逐步更新 | 可追溯 |

**compose 草案（实现时落地）：**

```yaml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: vohive
      POSTGRES_PASSWORD: vohive
      POSTGRES_DB: vohive
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U vohive -d vohive"]
      interval: 5s
      timeout: 5s
      retries: 10
    ports:
      - "5432:5432"

  vohive:
    # ...
    environment:
      VOHIVE_DB_DSN: "host=postgres user=vohive password=vohive dbname=vohive port=5432 sslmode=disable TimeZone=UTC"
    depends_on:
      postgres:
        condition: service_healthy

volumes:
  pgdata:
```

**建议工期**：0.5 天

---

### 阶段 F — 收尾与验收

| ID | 任务 | 验收 |
|----|------|------|
| F1 | 全链路手工：登录 → 设备列表 → 短信收发 → 策略 | 功能正常 |
| F2 | 确认无 sqlite 启动路径残留 | grep 无 glebarez 运行路径 |
| F3 | 默认配置示例全部指向 PG | 文档一致 |
| F4 | 打 tag / 发说明（可选） | breaking change 告知 |

---

## 6. 推荐实施顺序（日历视角）

```
Day 1     A 配置+PG 连接+删 sqlite 启动
Day 1–2   B 迁移兼容 + AutoMigrate
Day 2–3   C 测试/CI 全绿
Day 3     E compose + 文档
Day 4     D 迁移工具（有旧数据时）
Day 4–5   F 全链路验收
```

单人紧凑可 **2–3 天** 到「空库可上线」；含迁移工具与全测约 **3–5 天**。

---

## 7. 验收标准（Done Definition）

全部满足才算服务端 PG 改造完成：

1. **无 SQLite 运行时**：启动只连 PG；无 DSN 则失败退出。  
2. **空库 AutoMigrate** 成功，API 能监听（如 `:7575`）。  
3. **主路径可用**：登录、设备、短信、卡策略、代理配置读写。  
4. **核心自动化测试在 CI PostgreSQL 上通过**。  
5. **Compose 一键起 postgres + vohive** 有文档。  
6. （若有存量）**dbmigrate 校验通过**。  
7. README/部署文档与代码一致。  

---

## 8. 风险与回滚

| 风险 | 缓解 |
|------|------|
| 隐式 SQL 方言漏网 | 全量测试 + 手工主路径 |
| AutoMigrate 误伤生产列 | 生产首次迁移前备份；重大变更用迁移脚本评审 |
| 连接池打满 | 默认保守 20；监控 `pg_stat_activity` |
| 迁移丢数 | 先备份旧 `.db`；dry-run count |
| 测试环境无 PG | CI 强制 service；本地 docker 文档 |

**回滚（切断策略下）**：

- 不能靠配置切回 SQLite 运行  
- 只能：**回退旧版本二进制** + 保留的 `vohive.db`，或 **PG 从 dump 恢复** 后继续用新版本  

---

## 9. 文件级改动预览（实现时对照）

**新增（预计）**

- `internal/config` 中 database 字段  
- `internal/db/postgres.go` 或合并进 `db.go`  
- `internal/db/migrate_columns.go`（方言中立列探测）  
- `internal/testutil/dbtest` 或 `internal/db/testdb`  
- `cmd/dbmigrate/main.go`（可选）  
- `docs/backend-db-progress.md`  
- `docs/db-migrate-runbook.md`（可选）  
- compose 中 postgres 服务  

**修改**

- `internal/db/db.go`、`cmd/vohive/main.go`、`go.mod`/`go.sum`  
- 全部相关 `*_test.go`  
- `docker-compose*.yml`、`README.md`、`DOCKERHUB.md`  

**删除**

- `internal/db/sqlite_dialector.go`  
- `applySQLitePragmas`、sqlite 专用 env  

---

## 10. 与其它工作的关系

| 工作 | 关系 |
|------|------|
| 前端 React / 前后端分离 | **并行不阻塞**；API 契约不变，只换存储 |
| 设备 USB / eSIM | 不依赖库引擎，验收时顺带点检即可 |

---

## 11. 当前状态与下一步

| 项 | 状态 |
|----|------|
| 完整计划 | **本文 v1.0** |
| 决策 ADR | `docs/backend-db-decisions.md` |
| 代码实现 | **阶段 A/B/C/E 主体已落地**（见 `docs/backend-db-progress.md`） |
| 本地验证 | 需 Docker/Go：`go mod tidy`、`compose up postgres`、测试 |
| **下一步** | 环境可用时跑通编译与 `internal/db` 测试；可选 `cmd/dbmigrate` |

---

## 12. 版本记录

| 版本 | 日期 | 说明 |
|------|------|------|
| v0.1–v0.2 | 2026-08-11 | 初稿与「切断 SQLite」策略 |
| **v1.0** | 2026-08-11 | **完整可执行计划**：阶段/工期/配置/验收/文件清单；重写乱码文档 |
