// Command dbmigrate 把旧的单文件 SQLite 数据一次性导入 PostgreSQL。
//
// 运行时已经不支持 SQLite（见 docs/backend-db-decisions.md）：服务只连 PG，
// 没有 DSN 就直接退出。这个工具只为一件事存在——让升级前就在跑的部署把存量
// 数据搬过去。搬完之后旧 .db 文件不再有任何用处。
//
// 用法：
//
//	dbmigrate --sqlite ./data/vohive.db --dry-run
//	dbmigrate --sqlite ./data/vohive.db --postgres "host=... dbname=..."
//
// 默认是**只读演练**之外的一切都不做：目标库非空时直接拒绝，除非显式指定
// --allow-nonempty（追加，冲突行跳过）或 --truncate（清空后导入）。
//
// 依赖说明：这里用 modernc.org/sqlite（纯 Go，无需 CGO）。它只被本命令导入，
// ./cmd/vohive 的二进制里不含任何 SQLite 代码。
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yuanshuai1122/vohive/internal/db"
	"gorm.io/gorm"
	_ "modernc.org/sqlite"
)

// 复制顺序：先被引用的表，后引用它们的表。
//
// 目前的模型之间没有数据库层外键（GORM 未声明），所以顺序不影响成败；
// 但保持这个顺序能让中途失败时留下的状态更好理解——不会出现"有短信没有卡"。
var tableOrder = []string{
	"devices",
	"sim_cards",
	"sim_subscriptions",
	"pending_phone_numbers",
	"card_policies",
	"proxy_instances",
	"upstream_proxies",
	"upstream_proxy_country_rules",
	"sms",
	"sms_contacts",
	"sms_deliveries",
	"sms_delivery_parts",
	"traffic_minutes",
	"traffic_hours",
	"traffic_days",
	"traffic_weeks",
	"traffic_months",
}

type options struct {
	sqlitePath    string
	postgresDSN   string
	dryRun        bool
	batchSize     int
	allowNonEmpty bool
	truncate      bool
	only          string
}

func main() {
	var opts options
	flag.StringVar(&opts.sqlitePath, "sqlite", "", "旧 SQLite 文件路径（必填）")
	flag.StringVar(&opts.postgresDSN, "postgres", "", "目标 PostgreSQL DSN；留空则取 VOHIVE_DB_DSN / DATABASE_URL")
	flag.BoolVar(&opts.dryRun, "dry-run", false, "只报告将要迁移什么，不写入目标库")
	flag.IntVar(&opts.batchSize, "batch", 500, "每批插入行数")
	flag.BoolVar(&opts.allowNonEmpty, "allow-nonempty", false, "目标表非空时仍然导入（主键冲突的行跳过）")
	flag.BoolVar(&opts.truncate, "truncate", false, "导入前清空目标表——会删除目标库现有数据")
	flag.StringVar(&opts.only, "tables", "", "只迁移这些表，逗号分隔；留空为全部")
	flag.Parse()

	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "dbmigrate: %v\n", err)
		os.Exit(1)
	}
}

func run(opts options) error {
	if strings.TrimSpace(opts.sqlitePath) == "" {
		return errors.New("--sqlite 为必填项")
	}
	if opts.batchSize <= 0 {
		opts.batchSize = 500
	}
	if opts.truncate && opts.allowNonEmpty {
		return errors.New("--truncate 与 --allow-nonempty 互斥：前者清空后者追加，同时给出说明意图不明确")
	}
	if _, err := os.Stat(opts.sqlitePath); err != nil {
		return fmt.Errorf("读取 SQLite 文件: %w", err)
	}

	// mode=ro 保证绝不写旧库：迁移失败时旧数据仍然是可回退的那一份。
	src, err := sql.Open("sqlite", "file:"+opts.sqlitePath+"?mode=ro")
	if err != nil {
		return fmt.Errorf("打开 SQLite: %w", err)
	}
	defer src.Close()
	if err := src.Ping(); err != nil {
		return fmt.Errorf("打开 SQLite: %w", err)
	}

	dsn := db.ResolveDSN(opts.postgresDSN)
	if strings.TrimSpace(dsn) == "" {
		return errors.New("目标 DSN 为空：用 --postgres 指定，或设置 VOHIVE_DB_DSN / DATABASE_URL")
	}

	// AutoMigrate 建表：目标库通常是全新的，schema 以 Go 模型为准。
	// 演练也要建表——否则无从知道列能不能对上。
	if err := db.Open(db.Options{DSN: dsn, AutoMigrate: true}); err != nil {
		return fmt.Errorf("打开 PostgreSQL: %w", err)
	}
	dst := db.DB

	tables := tableOrder
	if strings.TrimSpace(opts.only) != "" {
		tables, err = selectTables(opts.only)
		if err != nil {
			return err
		}
	}

	if opts.dryRun {
		fmt.Println("== 演练模式：不会写入目标库 ==")
	}
	fmt.Printf("源: %s\n目标: %s\n\n", opts.sqlitePath, redactDSN(dsn))

	var (
		totalCopied int64
		skipped     []string
		problems    []string
	)

	for _, table := range tables {
		report, err := migrateTable(src, dst, table, opts)
		if err != nil {
			return fmt.Errorf("迁移 %s: %w", table, err)
		}
		switch {
		case report.skippedReason != "":
			skipped = append(skipped, fmt.Sprintf("%s（%s）", table, report.skippedReason))
			continue
		default:
			totalCopied += report.copied
		}
		fmt.Printf("%-28s 源 %6d 行 → 目标 %6d 行", table, report.sourceRows, report.destRows)
		if len(report.droppedColumns) > 0 {
			fmt.Printf("  [忽略源列: %s]", strings.Join(report.droppedColumns, ","))
		}
		if len(report.missingColumns) > 0 {
			fmt.Printf("  [目标列无源数据: %s]", strings.Join(report.missingColumns, ","))
		}
		fmt.Println()

		if !opts.dryRun && report.destRows < report.sourceRows {
			problems = append(problems, fmt.Sprintf(
				"%s: 源 %d 行，目标只有 %d 行", table, report.sourceRows, report.destRows))
		}
	}

	if len(skipped) > 0 {
		fmt.Printf("\n跳过: %s\n", strings.Join(skipped, "; "))
	}

	if opts.dryRun {
		fmt.Printf("\n演练结束，未写入任何数据。移除 --dry-run 执行实际迁移。\n")
		return nil
	}

	// 自增主键是显式带过来的，序列还停在 1；不校正的话新写入立刻主键冲突。
	if err := resetSequences(dst, tables); err != nil {
		return fmt.Errorf("校正自增序列: %w", err)
	}

	fmt.Printf("\n共导入 %d 行。\n", totalCopied)
	if len(problems) > 0 {
		return fmt.Errorf("行数校验未通过:\n  %s", strings.Join(problems, "\n  "))
	}
	fmt.Println("行数校验通过。")
	return nil
}

func selectTables(list string) ([]string, error) {
	known := map[string]bool{}
	for _, t := range tableOrder {
		known[t] = true
	}
	var out []string
	for _, raw := range strings.Split(list, ",") {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		if !known[t] {
			return nil, fmt.Errorf("未知表 %q；可选: %s", t, strings.Join(tableOrder, ", "))
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, errors.New("--tables 未给出任何表名")
	}
	// 保持 tableOrder 的相对顺序，而不是命令行给出的顺序
	sort.SliceStable(out, func(i, j int) bool {
		return indexOf(tableOrder, out[i]) < indexOf(tableOrder, out[j])
	})
	return out, nil
}

func indexOf(list []string, v string) int {
	for i, item := range list {
		if item == v {
			return i
		}
	}
	return len(list)
}

type tableReport struct {
	sourceRows     int64
	destRows       int64
	copied         int64
	droppedColumns []string
	missingColumns []string
	skippedReason  string
}

func migrateTable(src *sql.DB, dst *gorm.DB, table string, opts options) (tableReport, error) {
	var report tableReport

	srcCols, err := sqliteColumns(src, table)
	if err != nil {
		return report, err
	}
	if len(srcCols) == 0 {
		report.skippedReason = "源库无此表"
		return report, nil
	}

	dstCols, err := postgresColumns(dst, table)
	if err != nil {
		return report, err
	}
	if len(dstCols) == 0 {
		return report, fmt.Errorf("目标库无此表（AutoMigrate 应已建好，请检查）")
	}

	var shared []string
	for _, col := range srcCols {
		if _, ok := dstCols[col]; ok {
			shared = append(shared, col)
		} else {
			// 例如 sim_cards 上早已删掉的 phone_number 系列列
			report.droppedColumns = append(report.droppedColumns, col)
		}
	}
	if len(shared) == 0 {
		return report, errors.New("源表与目标表没有同名列，无法映射")
	}
	srcSet := map[string]bool{}
	for _, c := range srcCols {
		srcSet[c] = true
	}
	for col := range dstCols {
		if !srcSet[col] {
			report.missingColumns = append(report.missingColumns, col)
		}
	}
	sort.Strings(report.missingColumns)

	if err := src.QueryRow(`SELECT COUNT(*) FROM "` + table + `"`).Scan(&report.sourceRows); err != nil {
		return report, fmt.Errorf("统计源行数: %w", err)
	}

	existing, err := countRows(dst, table)
	if err != nil {
		return report, err
	}
	if existing > 0 && !opts.dryRun {
		switch {
		case opts.truncate:
			// RESTART IDENTITY 顺带把序列归零，后面 resetSequences 再按实际数据校正
			if err := dst.Exec(`TRUNCATE TABLE "` + table + `" RESTART IDENTITY CASCADE`).Error; err != nil {
				return report, fmt.Errorf("清空目标表: %w", err)
			}
			existing = 0
		case opts.allowNonEmpty:
			// 继续，主键冲突的行会被跳过
		default:
			return report, fmt.Errorf(
				"目标表已有 %d 行。默认拒绝导入以免与现有数据混在一起——"+
					"确认要追加请加 --allow-nonempty，确认要覆盖请加 --truncate", existing)
		}
	}

	if report.sourceRows == 0 {
		report.destRows = existing
		return report, nil
	}
	if opts.dryRun {
		report.destRows = report.sourceRows
		return report, nil
	}

	copied, err := copyRows(src, dst, table, shared, dstCols, opts.batchSize)
	if err != nil {
		return report, err
	}
	report.copied = copied

	report.destRows, err = countRows(dst, table)
	if err != nil {
		return report, err
	}
	return report, nil
}

func copyRows(src *sql.DB, dst *gorm.DB, table string, cols []string, dstCols map[string]string, batchSize int) (int64, error) {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = `"` + c + `"`
	}
	rows, err := src.Query(`SELECT ` + strings.Join(quoted, ",") + ` FROM "` + table + `"`)
	if err != nil {
		return 0, fmt.Errorf("读取源数据: %w", err)
	}
	defer rows.Close()

	var (
		copied int64
		batch  [][]any
	)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := insertBatch(dst, table, quoted, batch); err != nil {
			return err
		}
		copied += int64(len(batch))
		batch = batch[:0]
		return nil
	}

	for rows.Next() {
		values := make([]any, len(cols))
		holders := make([]any, len(cols))
		for i := range values {
			holders[i] = &values[i]
		}
		if err := rows.Scan(holders...); err != nil {
			return copied, fmt.Errorf("扫描源行: %w", err)
		}
		converted := make([]any, len(cols))
		for i, col := range cols {
			converted[i], err = coerce(values[i], dstCols[col])
			if err != nil {
				return copied, fmt.Errorf("列 %s: %w", col, err)
			}
		}
		batch = append(batch, converted)
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return copied, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return copied, fmt.Errorf("遍历源数据: %w", err)
	}
	return copied, flush()
}

func insertBatch(dst *gorm.DB, table string, quotedCols []string, batch [][]any) error {
	var (
		sb   strings.Builder
		args []any
	)
	sb.WriteString(`INSERT INTO "`)
	sb.WriteString(table)
	sb.WriteString(`" (`)
	sb.WriteString(strings.Join(quotedCols, ","))
	sb.WriteString(") VALUES ")

	n := 1
	for i, row := range batch {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("(")
		for j, v := range row {
			if j > 0 {
				sb.WriteString(",")
			}
			sb.WriteString("$")
			sb.WriteString(strconv.Itoa(n))
			n++
			args = append(args, v)
		}
		sb.WriteString(")")
	}
	// 冲突即跳过：重跑迁移应当是安全的，而不是中途炸掉留下半个库
	sb.WriteString(" ON CONFLICT DO NOTHING")

	return dst.Exec(sb.String(), args...).Error
}

// coerce 把 SQLite 的松散取值对齐到目标列的类型。
//
// SQLite 没有真正的类型：布尔存 0/1，时间可能是 RFC3339 文本、也可能是 Unix 秒。
// 直接塞给 PostgreSQL 会因为类型不匹配失败，所以按目标列类型逐个转换。
func coerce(v any, pgType string) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch pgType {
	case "boolean":
		switch t := v.(type) {
		case bool:
			return t, nil
		case int64:
			return t != 0, nil
		case float64:
			return t != 0, nil
		case string:
			return parseBoolText(t)
		case []byte:
			return parseBoolText(string(t))
		}
	case "timestamp with time zone", "timestamp without time zone", "date":
		return coerceTime(v)
	case "text", "character varying", "character":
		switch t := v.(type) {
		case []byte:
			return string(t), nil
		case string:
			return t, nil
		case int64:
			return strconv.FormatInt(t, 10), nil
		case float64:
			return strconv.FormatFloat(t, 'f', -1, 64), nil
		case bool:
			if t {
				return "1", nil
			}
			return "0", nil
		case time.Time:
			return t.Format(time.RFC3339Nano), nil
		}
	}
	// 数值等其余类型交给 pgx 自己判断；它对 int64/float64/[]byte 都能处理
	if b, ok := v.([]byte); ok {
		return string(b), nil
	}
	return v, nil
}

func parseBoolText(s string) (bool, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(s)
	if err != nil {
		return false, fmt.Errorf("无法识别的布尔值 %q", s)
	}
	return b, nil
}

// SQLite 里时间的存法取决于当年写它的驱动，这几种都见得到。
var sqliteTimeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func coerceTime(v any) (any, error) {
	switch t := v.(type) {
	case time.Time:
		return t, nil
	case int64:
		// Unix 秒。毫秒/纳秒的量级在这里不会出现：这些列都是 GORM 写的
		// CreatedAt/UpdatedAt/LastSeen，旧驱动存数字时用的就是秒。
		return time.Unix(t, 0).UTC(), nil
	case float64:
		return time.Unix(int64(t), 0).UTC(), nil
	case []byte:
		return parseTimeText(string(t))
	case string:
		return parseTimeText(t)
	}
	return nil, fmt.Errorf("无法识别的时间值 %T", v)
}

func parseTimeText(s string) (any, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	for _, layout := range sqliteTimeLayouts {
		if ts, err := time.Parse(layout, s); err == nil {
			return ts, nil
		}
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.Unix(n, 0).UTC(), nil
	}
	return nil, fmt.Errorf("无法解析时间 %q", s)
}

func sqliteColumns(src *sql.DB, table string) ([]string, error) {
	rows, err := src.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, fmt.Errorf("读取源表结构: %w", err)
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
}

// postgresColumns 返回列名 → 数据类型，coerce 依赖它决定怎么转换。
func postgresColumns(dst *gorm.DB, table string) (map[string]string, error) {
	type row struct {
		ColumnName string `gorm:"column:column_name"`
		DataType   string `gorm:"column:data_type"`
	}
	var rows []row
	err := dst.Raw(`SELECT column_name, data_type FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = ?`, table).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("读取目标表结构: %w", err)
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.ColumnName] = r.DataType
	}
	return out, nil
}

func countRows(dst *gorm.DB, table string) (int64, error) {
	var n int64
	if err := dst.Raw(`SELECT COUNT(*) FROM "` + table + `"`).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("统计目标行数: %w", err)
	}
	return n, nil
}

// resetSequences 把自增列的序列推到当前最大值之后。
//
// 迁移是带着原始 id 写进去的，序列并不知道这件事，仍然从 1 开始发号——
// 不校正的话，迁移后第一条新短信就会撞上已存在的主键。
func resetSequences(dst *gorm.DB, tables []string) error {
	for _, table := range tables {
		type row struct {
			Column string `gorm:"column:column_name"`
			Seq    string `gorm:"column:seq"`
		}
		var rows []row
		err := dst.Raw(`SELECT column_name, pg_get_serial_sequence(?, column_name) AS seq
			FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = ?`, table, table).Scan(&rows).Error
		if err != nil {
			return err
		}
		for _, r := range rows {
			if strings.TrimSpace(r.Seq) == "" {
				continue
			}
			// COALESCE + is_called=false：空表时序列应从 1 开始发号，
			// 直接 setval(0) 是非法值。
			sql := fmt.Sprintf(
				`SELECT setval('%s', COALESCE((SELECT MAX("%s") FROM "%s"), 0) + 1, false)`,
				r.Seq, r.Column, table)
			if err := dst.Exec(sql).Error; err != nil {
				return fmt.Errorf("%s.%s: %w", table, r.Column, err)
			}
		}
	}
	return nil
}

// redactDSN 去掉 DSN 里的口令后再打印。迁移日志经常被贴进工单。
func redactDSN(dsn string) string {
	if u := strings.Index(dsn, "://"); u >= 0 {
		// URL 形态：postgres://user:pass@host/db
		rest := dsn[u+3:]
		if at := strings.Index(rest, "@"); at >= 0 {
			cred := rest[:at]
			if colon := strings.Index(cred, ":"); colon >= 0 {
				return dsn[:u+3] + cred[:colon] + ":***" + rest[at:]
			}
		}
		return dsn
	}
	// key=value 形态
	parts := strings.Fields(dsn)
	for i, p := range parts {
		if strings.HasPrefix(strings.ToLower(p), "password=") {
			parts[i] = "password=***"
		}
	}
	return strings.Join(parts, " ")
}
