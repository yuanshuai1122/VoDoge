# 后端 PostgreSQL 改造进度

## 2026-08-11 — 阶段 A/B/C 实施中

### 已完成

- [x] `config.DatabaseConfig`（dsn / 连接池 / auto_migrate）
- [x] `db.Open` / `db.Init` **仅 PostgreSQL**（`gorm.io/driver/postgres`）
- [x] 删除 `sqlite_dialector.go` 与 PRAGMA
- [x] 列探测改为 GORM Migrator；`DROP COLUMN IF EXISTS`（PG）
- [x] `main.go` 通过 `VODOGE_DB_DSN` / `DATABASE_URL` / `database.dsn` 连接，失败即退出
- [x] 测试：`db.OpenTestDB` / `ReopenTestDB`，去掉文件型 SQLite Init
- [x] compose：`docker-compose.yml` / `windows` / `hub` 增加 postgres 服务
- [x] `config/config.yaml` 增加 database 段

### 本地验证（2026-08-16）

- [x] Linux 容器内 `go mod tidy -diff` 无差异
- [x] PostgreSQL 16 + Linux 发布参数二进制启动冒烟：AutoMigrate、`/ping`、SIGTERM 退出码 0
- [x] `TEST_DATABASE_URL=... go test -count=1 -p 1 ./internal/db ./cmd/dbmigrate`

## 2026-08-12 — 阶段 C（CI）补齐

此前 `.github/workflows/ci.yml` 没有 postgres service，`TEST_DATABASE_URL` 也未设置，
`db.OpenTestDB` 因而整体 `t.Skip` —— PG 改造实际上**没有任何自动化验证覆盖**。
同时 `scripts/ci.sh` 的测试包列表里没有 `./internal/db` 与 `./internal/api`。

- [x] C3：`ci.yml` 增加 `postgres:16-alpine` service（含 `pg_isready` healthcheck）与 job 级 `TEST_DATABASE_URL`
- [x] C4：`scripts/ci.sh` 测试包列表补 `./internal/db ./internal/api`
- [ ] 推送后在 GitHub Actions 上确认实际转绿；本地等价 Linux/PostgreSQL、前端和安装器门禁已通过

另：`cmd/vohive/main.go` 中 SQLite 遗留函数 `migrateLegacyServerDB`（处理 `-wal` / `-shm` 边车文件）
已无调用点，本次删除，对应 F2「确认无 sqlite 启动路径残留」。

### 环境变量

| 变量 | 说明 |
|------|------|
| `VODOGE_DB_DSN` | 主 DSN（优先） |
| `DATABASE_URL` | 备选 DSN |
| `TEST_DATABASE_URL` | 测试专用 |

示例：

```text
host=127.0.0.1 user=vodoge password=vodoge dbname=vodoge port=5432 sslmode=disable TimeZone=UTC
```

## 2026-08-14 — 阶段 D（数据迁移工具）完成

`cmd/dbmigrate` 已实现，对应计划里的 D1–D4。

- [x] D1：`--sqlite path --postgres dsn`，支持 `--dry-run`
- [x] D2：按 `internal/db` 的 AutoMigrate 模型清单复制 + `--batch` 分批（默认 500）
- [x] D3：逐批验证源主键均已落库；任一表漏主键即整体回滚并以非零码退出
- [x] D4：`docs/db-migrate-runbook.md`

几个实现上值得记下来的点：

- **普通列按名取交集**，目标表新增列留默认值；有业务语义的旧列不能直接忽略。
  例如旧 `sim_cards` 上的 `phone_number` 系列会显式转换为 `sim_subscriptions`，
  并由迁移测试断言结果，防止手机号在归档旧库后静默丢失。
- **按目标列类型做值转换**。SQLite 没有真正的类型：布尔存 0/1，时间可能是
  RFC3339 文本、空格分隔文本，也可能是 Unix 秒。直接塞给 PG 会因类型不匹配失败。
- **必须校正自增序列**。迁移带着原始 id 写入，序列仍从 1 发号，
  不 `setval` 的话迁移后第一条新短信就撞主键——上线当天才会发现的那种问题。
  测试 `TestMigrateAdvancesSequencesPastImportedIDs` 专门盯这一条。
- **目标库非空时默认拒绝**，而且在 AutoMigrate 和数据写入前完成全部选中表预检；要
  `--allow-nonempty`（追加）或 `--truncate`（清空）必须显式表态。
- **正式模式的 AutoMigrate、复制、源主键校验和序列校正在同一个 PostgreSQL 事务中**。
  `--truncate` 对所有选中表只发一个不带 `CASCADE` 的语句，失败整体回滚，不会清掉未选表。
- 插入带 `ON CONFLICT DO NOTHING`，并以 `RowsAffected` 计数；追加模式仍会验证每个源
  主键确实存在，目标额外行不能掩盖被其它唯一约束跳过的源行。
- **旧库以 `mode=ro` 打开**，迁移失败时它仍是可回退的那一份。

依赖：`modernc.org/sqlite`（纯 Go，无需 CGO），**只被 `cmd/dbmigrate` 导入**。
`go list -deps ./cmd/vodoge | grep -i sqlite` 输出为空，生产二进制不含 SQLite 代码。
