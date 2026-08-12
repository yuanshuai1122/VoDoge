# 后端 PostgreSQL 改造进度

## 2026-08-11 — 阶段 A/B/C 实施中

### 已完成

- [x] `config.DatabaseConfig`（dsn / 连接池 / auto_migrate）
- [x] `db.Open` / `db.Init` **仅 PostgreSQL**（`gorm.io/driver/postgres`）
- [x] 删除 `sqlite_dialector.go` 与 PRAGMA
- [x] 列探测改为 GORM Migrator；`DROP COLUMN IF EXISTS`（PG）
- [x] `main.go` 通过 `VOHIVE_DB_DSN` / `DATABASE_URL` / `database.dsn` 连接，失败即退出
- [x] 测试：`db.OpenTestDB` / `ReopenTestDB`，去掉文件型 SQLite Init
- [x] compose：`docker-compose.yml` / `windows` / `hub` 增加 postgres 服务
- [x] `config/config.yaml` 增加 database 段

### 待本地验证

- [ ] `go mod tidy`（依赖已写入 go.mod；需在有 Go/Docker 环境确认 sum 完整）
- [ ] `docker compose up postgres` + 启动 vohive 冒烟
- [ ] `TEST_DATABASE_URL=... go test ./internal/db/...`

## 2026-08-12 — 阶段 C（CI）补齐

此前 `.github/workflows/ci.yml` 没有 postgres service，`TEST_DATABASE_URL` 也未设置，
`db.OpenTestDB` 因而整体 `t.Skip` —— PG 改造实际上**没有任何自动化验证覆盖**。
同时 `scripts/ci.sh` 的测试包列表里没有 `./internal/db` 与 `./internal/api`。

- [x] C3：`ci.yml` 增加 `postgres:16-alpine` service（含 `pg_isready` healthcheck）与 job 级 `TEST_DATABASE_URL`
- [x] C4：`scripts/ci.sh` 测试包列表补 `./internal/db ./internal/api`
- [ ] 推送后在 GitHub Actions 上确认实际转绿（本机无 Go 工具链，未能本地预跑）

另：`cmd/vohive/main.go` 中 SQLite 遗留函数 `migrateLegacyServerDB`（处理 `-wal` / `-shm` 边车文件）
已无调用点，本次删除，对应 F2「确认无 sqlite 启动路径残留」。

### 环境变量

| 变量 | 说明 |
|------|------|
| `VOHIVE_DB_DSN` | 主 DSN（优先） |
| `DATABASE_URL` | 备选 DSN |
| `TEST_DATABASE_URL` | 测试专用 |

示例：

```text
host=127.0.0.1 user=vohive password=vohive dbname=vohive port=5432 sslmode=disable TimeZone=UTC
```
