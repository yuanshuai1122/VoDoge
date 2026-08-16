# SQLite → PostgreSQL 数据迁移手册

适用对象：**升级前就在运行、且 `data/` 下有旧 `.db` 文件**的部署。
全新部署不需要这一步——启动时 AutoMigrate 会建好空库。

工具：`cmd/dbmigrate`。它只做一次性导入，跑完之后旧 `.db` 文件不再有任何用处。

---

## 0. 先决条件

- 目标 PostgreSQL 已就绪，且**是空的**（工具默认拒绝往非空库里导，见 §3）
- 旧 `.db` 文件可读。工具以 `mode=ro` 打开，**不会修改它**——迁移失败时旧数据仍是可回退的那一份
- VoDoge 服务**已停止**。边写边迁会漏掉迁移开始之后产生的短信与流量

---

## 1. 先演练

```bash
go run ./cmd/dbmigrate --sqlite ./data/vohive.db --postgres "host=127.0.0.1 user=vodoge password=... dbname=vodoge port=5432 sslmode=disable" --dry-run
```

演练会在 PostgreSQL 事务中执行 AutoMigrate 和 schema 映射检查，随后强制回滚；
**不会留下表、列、索引或业务数据**。输出形如：

```
== 演练模式：不会写入目标库 ==
源: ./data/vohive.db
目标: host=127.0.0.1 user=vodoge password=*** dbname=vodoge port=5432 sslmode=disable

devices                          源      2 行 → 目标      2 行
sim_cards                        源      1 行 → 目标      1 行  [转换源列: phone_number,modem_phone_number,vowifi_phone_number]
sim_cards 旧号码列       将派生/回填 sim_subscriptions 1 行，pending_phone_numbers 0 行
sms                              源   8134 行 → 目标   8134 行
sms_delivery                     源     27 行 → 目标     27 行
traffic_minute                   源  22031 行 → 目标  22031 行
...
跳过: traffic_week（源库无此表）
```

两类提示需要看一眼，但通常都是正常的：

- `[转换源列: ...]` —— 旧 `sim_cards` 号码列会按 IMSI 派生到 `sim_subscriptions`；
  没有 IMSI 但有 ICCID 的行会进入 `pending_phone_numbers`。已有新模型非空字段优先，
  旧值只回填空字段，派生结果还会逐字段读回验证
- `[忽略源列: ...]` —— 只有没有已知业务映射的旧列才会忽略
- `[目标列无源数据: ...]` —— 当前模型新增的列，导入后为该列的零值/默认值

`跳过（源库无此表）` 说明旧版本还没有这张表，正常。

---

## 2. 正式导入

去掉 `--dry-run`：

```bash
go run ./cmd/dbmigrate --sqlite ./data/vohive.db --postgres "host=127.0.0.1 user=vodoge password=... dbname=vodoge port=5432 sslmode=disable"
```

正式模式下的目标表 AutoMigrate、数据导入、所有自增序列校正和源主键落库校验位于
**同一个 PostgreSQL 事务**中。任意表建模、复制、序列或校验失败，schema 变更和所有
选中表数据一起回滚，不会留下半迁移状态。

校验不是简单比较总行数：工具会逐批确认源库的每个主键都存在于目标表中，目标库已有的
额外行不能掩盖漏行。校验通过时打印 `源主键落库校验通过。`

`--postgres` 可省略，此时按 `VODOGE_DB_DSN` → `DATABASE_URL` 的顺序取，与服务本身一致。

---

## 3. 目标库非空时

默认**拒绝**。把新旧两份数据混在一起，事后分不开，所以必须显式表态：

| 参数 | 行为 | 用在什么时候 |
|------|------|-------------|
| `--allow-nonempty` | 追加，冲突行跳过并按实际 `RowsAffected` 计数 | 明确要把旧库合入已有目标库 |
| `--truncate` | **一次性清空所有选中表**后导入 | 目标库里只有试跑残留，确认可丢 |

`--truncate` 会删除目标库现有数据，且不可撤销。两个参数互斥。清理语句不带
`CASCADE`：如果未选中的表通过外键引用选中表，命令会失败并整体回滚，而不会顺带删除
未选表。配合 `--tables` 时，只有明确列出的表属于清理范围。

重跑是安全的：插入语句带 `ON CONFLICT DO NOTHING`，不会产生重复行。但如果某行因
非主键唯一约束冲突被跳过、导致该行的源主键没有真正落库，主键校验会报错并回滚，而不
会被目标库的其它行数掩盖。

不带这两个参数时，工具会在 AutoMigrate 和任何数据写入之前检查完全部选中目标表；只要
任意一张非空，就以零副作用失败。正式事务锁表后还会再检查一次，防止预检后的并发写入。

---

## 4. 其它参数

| 参数 | 默认 | 说明 |
|------|------|------|
| `--batch` | 500 | 每批插入行数。大表内存吃紧时调小 |
| `--tables` | 全部 | 逗号分隔，只迁指定表；执行顺序取自 `internal/db` 的 AutoMigrate 模型清单 |

迁移工具不再维护独立表名清单：所有表名均从服务启动使用的同一份 AutoMigrate 模型解析，
因此 `sms_delivery`、`traffic_minute` 这类自定义单数表名不会因手写复数再次漂移。
如果显式选择 `sim_cards` 且其中存在旧号码，必须同时选择实际需要的
`sim_subscriptions` / `pending_phone_numbers`；缺少目标表会在写入前明确报错，工具不会
越过 `--tables` 边界，也不会静默丢弃号码。

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
它**只被这个命令导入**，`./cmd/vodoge` 的依赖图里没有任何 SQLite 代码——
可以用 `go list -deps ./cmd/vodoge | grep -i sqlite` 自行确认，输出为空。

运行时不支持 SQLite 是明确决策（`docs/backend-db-decisions.md`），
这个工具的存在不改变那一点。
