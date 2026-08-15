# SQLite → PostgreSQL 数据迁移手册

适用对象：**升级前就在运行、且 `data/` 下有旧 `.db` 文件**的部署。
全新部署不需要这一步——启动时 AutoMigrate 会建好空库。

工具：`cmd/dbmigrate`。它只做一次性导入，跑完之后旧 `.db` 文件不再有任何用处。

---

## 0. 先决条件

- 目标 PostgreSQL 已就绪，且**是空的**（工具默认拒绝往非空库里导，见 §3）
- 旧 `.db` 文件可读。工具以 `mode=ro` 打开，**不会修改它**——迁移失败时旧数据仍是可回退的那一份
- VoDog 服务**已停止**。边写边迁会漏掉迁移开始之后产生的短信与流量

---

## 1. 先演练

```bash
go run ./cmd/dbmigrate --sqlite ./data/vohive.db --postgres "host=127.0.0.1 user=vohive password=... dbname=vohive port=5432 sslmode=disable" --dry-run
```

演练会连上目标库并执行 AutoMigrate（建表），但**不写入任何数据**。输出形如：

```
== 演练模式：不会写入目标库 ==
源: ./data/vohive.db
目标: host=127.0.0.1 user=vohive password=*** dbname=vohive port=5432 sslmode=disable

devices                          源      2 行 → 目标      2 行
sim_cards                        源      1 行 → 目标      1 行  [忽略源列: phone_number,modem_phone_number]
sms                              源   8134 行 → 目标   8134 行
...
跳过: traffic_weeks（源库无此表）
```

两类提示需要看一眼，但通常都是正常的：

- `[忽略源列: ...]` —— 源表有、当前模型已经没有的列。例如 `sim_cards` 上的
  `phone_number` 系列早已迁往 `sim_subscriptions` 并被删除，这些列的数据**不会**被带过来
- `[目标列无源数据: ...]` —— 当前模型新增的列，导入后为该列的零值/默认值

`跳过（源库无此表）` 说明旧版本还没有这张表，正常。

---

## 2. 正式导入

去掉 `--dry-run`：

```bash
go run ./cmd/dbmigrate --sqlite ./data/vohive.db --postgres "host=127.0.0.1 user=vohive password=... dbname=vohive port=5432 sslmode=disable"
```

结束时会做**行数校验**：任何一张表在目标库的行数少于源库，命令以非零码退出并列出差异。
校验通过时打印 `行数校验通过。`

`--postgres` 可省略，此时按 `VOHIVE_DB_DSN` → `DATABASE_URL` 的顺序取，与服务本身一致。

---

## 3. 目标库非空时

默认**拒绝**。把新旧两份数据混在一起，事后分不开，所以必须显式表态：

| 参数 | 行为 | 用在什么时候 |
|------|------|-------------|
| `--allow-nonempty` | 追加，主键冲突的行跳过 | 上次迁移中途失败，想接着导 |
| `--truncate` | **清空目标表**后导入 | 目标库里只有试跑残留，确认可丢 |

`--truncate` 会删除目标库现有数据，且不可撤销。两个参数互斥。

重跑是安全的：插入语句带 `ON CONFLICT DO NOTHING`，不会产生重复行。

---

## 4. 其它参数

| 参数 | 默认 | 说明 |
|------|------|------|
| `--batch` | 500 | 每批插入行数。大表内存吃紧时调小 |
| `--tables` | 全部 | 逗号分隔，只迁指定表；执行顺序仍按内置顺序 |

---

## 5. 迁移完成后

1. 启动服务，确认能连上 PG（无 DSN 会直接退出，不会退回 SQLite）
2. 抽查：设备列表、某个会话的短信、流量曲线
3. **发一条新短信**。这一步专门验自增序列——迁移是带着原始 id 写进去的，
   工具会把序列推到最大 id 之后；若这一步主键冲突，说明序列没校正成功
4. 确认无误后再归档旧 `.db` 文件

---

## 6. 迁移工具为什么不影响主程序

`cmd/dbmigrate` 用 `modernc.org/sqlite`（纯 Go，无需 CGO）读旧库。
它**只被这个命令导入**，`./cmd/vodog` 的依赖图里没有任何 SQLite 代码——
可以用 `go list -deps ./cmd/vodog | grep -i sqlite` 自行确认，输出为空。

运行时不支持 SQLite 是明确决策（`docs/backend-db-decisions.md`），
这个工具的存在不改变那一点。
