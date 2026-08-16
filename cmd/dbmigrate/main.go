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
// ./cmd/vodoge 的二进制里不含任何 SQLite 代码。
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yuanshuai1122/vodoge/internal/db"
	"gorm.io/gorm"
	_ "modernc.org/sqlite"
)

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
	flag.StringVar(&opts.postgresDSN, "postgres", "", "目标 PostgreSQL DSN；留空则取 VODOGE_DB_DSN / DATABASE_URL")
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
		return errors.New("目标 DSN 为空：用 --postgres 指定，或设置 VODOGE_DB_DSN / DATABASE_URL")
	}

	// 先只连接。默认模式必须在任何持久化副作用（包括 AutoMigrate）之前
	// 检查完所有已存在的目标表，确保目标非空时能真正零副作用退出。
	if err := db.Open(db.Options{DSN: dsn, AutoMigrate: false}); err != nil {
		return fmt.Errorf("打开 PostgreSQL: %w", err)
	}
	dst := db.DB

	catalog, err := migrationCatalog(dst)
	if err != nil {
		return err
	}
	tables := catalog
	if strings.TrimSpace(opts.only) != "" {
		tables, err = selectTables(opts.only, catalog)
		if err != nil {
			return err
		}
	}
	legacyPhones, err := prepareLegacySIMPhoneMigration(src, tables)
	if err != nil {
		return err
	}
	if !opts.dryRun && !opts.allowNonEmpty && !opts.truncate {
		if err := refuseNonEmptyDestination(dst, tables); err != nil {
			return err
		}
	}

	// 目标库通常是全新的。只为本次选中的模型建表，避免 --tables 在
	// schema 层面触碰未选中的表；表名仍由同一份 AutoMigrate 模型清单派生。
	models := make([]any, 0, len(tables))
	for _, table := range tables {
		models = append(models, table.model)
	}
	if opts.dryRun {
		fmt.Println("== 演练模式：不会写入目标库 ==")
	}
	fmt.Printf("源: %s\n目标: %s\n\n", opts.sqlitePath, redactDSN(dsn))

	var plans []tablePlan
	if opts.dryRun {
		dryRunComplete := errors.New("dry-run schema inspection complete")
		err = dst.Transaction(func(tx *gorm.DB) error {
			// AutoMigrate is needed to derive the exact destination schema, but a
			// dry run must not leave those DDL changes behind.
			if err := tx.AutoMigrate(models...); err != nil {
				return fmt.Errorf("AutoMigrate 目标表: %w", err)
			}
			var err error
			plans, err = prepareTables(src, tx, tables)
			if err != nil {
				return err
			}
			for i := range plans {
				if plans[i].report.skippedReason == "" {
					plans[i].report.destRows = plans[i].report.sourceRows
				}
			}
			return dryRunComplete
		})
		if !errors.Is(err, dryRunComplete) {
			return err
		}
		err = nil
	} else {
		err = dst.Transaction(func(tx *gorm.DB) error {
			// PostgreSQL DDL 也参与事务。二次预检或正式迁移失败时，新建/调整的
			// schema 与数据一起回滚，保持默认拒绝路径真正零副作用。
			if err := tx.AutoMigrate(models...); err != nil {
				return fmt.Errorf("AutoMigrate 目标表: %w", err)
			}
			if err := lockTables(tx, tables); err != nil {
				return err
			}

			// 锁表后重新做完整预检，消除初次检查与正式写入之间的竞争窗口。
			var err error
			plans, err = prepareTables(src, tx, tables)
			if err != nil {
				return err
			}
			if !opts.allowNonEmpty && !opts.truncate {
				if err := refusePreparedNonEmpty(plans); err != nil {
					return err
				}
			}
			if opts.truncate {
				if err := truncateTables(tx, tables); err != nil {
					return err
				}
			}
			for i := range plans {
				if err := migratePreparedTable(src, tx, &plans[i], opts); err != nil {
					return fmt.Errorf("迁移 %s: %w", plans[i].target.name, err)
				}
			}
			// 先复制源中已经符合新模型的 subscription / pending 行，再仅用
			// sim_cards 的旧号码列回填空字段。新模型数据始终具有更高优先级。
			if err := migrateLegacySIMPhones(tx, legacyPhones); err != nil {
				return fmt.Errorf("迁移 sim_cards 旧号码列: %w", err)
			}
			// 自增主键是显式带过来的，序列还停在 1；与数据复制放在同一
			// 事务中，任一序列失败都会回滚全部表。
			if err := resetSequences(tx, tableNames(tables)); err != nil {
				return fmt.Errorf("校正自增序列: %w", err)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	var (
		totalCopied int64
		skipped     []string
	)
	for _, plan := range plans {
		table := plan.target.name
		report := plan.report
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
		if len(report.convertedColumns) > 0 {
			fmt.Printf("  [转换源列: %s]", strings.Join(report.convertedColumns, ","))
		}
		if len(report.missingColumns) > 0 {
			fmt.Printf("  [目标列无源数据: %s]", strings.Join(report.missingColumns, ","))
		}
		if opts.allowNonEmpty && !opts.dryRun {
			fmt.Printf("  [新增: %d, 冲突跳过: %d]", report.copied, report.sourceRows-report.copied)
		}
		fmt.Println()
	}
	if len(legacyPhones.subscriptions) > 0 || len(legacyPhones.pending) > 0 {
		verb := "已派生/回填"
		if opts.dryRun {
			verb = "将派生/回填"
		}
		fmt.Printf("sim_cards 旧号码列       %s sim_subscriptions %d 行，pending_phone_numbers %d 行\n",
			verb, len(legacyPhones.subscriptions), len(legacyPhones.pending))
	}

	if len(skipped) > 0 {
		fmt.Printf("\n跳过: %s\n", strings.Join(skipped, "; "))
	}

	if opts.dryRun {
		fmt.Printf("\n演练结束，未写入任何数据。移除 --dry-run 执行实际迁移。\n")
		return nil
	}

	fmt.Printf("\n共导入 %d 行。\n", totalCopied)
	fmt.Println("源主键落库校验通过。")
	return nil
}

type migrationTarget struct {
	name  string
	model any
}

func migrationCatalog(dst *gorm.DB) ([]migrationTarget, error) {
	models := db.AutoMigrateModels()
	out := make([]migrationTarget, 0, len(models))
	seen := make(map[string]bool, len(models))
	for _, model := range models {
		stmt := &gorm.Statement{DB: dst}
		if err := stmt.Parse(model); err != nil {
			return nil, fmt.Errorf("解析 AutoMigrate 模型 %T: %w", model, err)
		}
		name := strings.TrimSpace(stmt.Schema.Table)
		if name == "" {
			return nil, fmt.Errorf("AutoMigrate 模型 %T 未解析出表名", model)
		}
		if seen[name] {
			return nil, fmt.Errorf("AutoMigrate 模型清单包含重复表 %q", name)
		}
		seen[name] = true
		out = append(out, migrationTarget{name: name, model: model})
	}
	return out, nil
}

func selectTables(list string, catalog []migrationTarget) ([]migrationTarget, error) {
	known := make(map[string]bool, len(catalog))
	allNames := make([]string, 0, len(catalog))
	for _, table := range catalog {
		known[table.name] = true
		allNames = append(allNames, table.name)
	}
	selected := map[string]bool{}
	for _, raw := range strings.Split(list, ",") {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		if !known[t] {
			return nil, fmt.Errorf("未知表 %q；可选: %s", t, strings.Join(allNames, ", "))
		}
		selected[t] = true
	}
	if len(selected) == 0 {
		return nil, errors.New("--tables 未给出任何表名")
	}
	out := make([]migrationTarget, 0, len(selected))
	for _, table := range catalog {
		if selected[table.name] {
			out = append(out, table)
		}
	}
	return out, nil
}

type tableReport struct {
	sourceRows       int64
	destRows         int64
	copied           int64
	droppedColumns   []string
	convertedColumns []string
	missingColumns   []string
	skippedReason    string
}

type sqliteTableInfo struct {
	columns     []string
	primaryKeys []string
}

type tablePlan struct {
	target      migrationTarget
	report      tableReport
	shared      []string
	dstCols     map[string]string
	primaryKeys []string
}

func prepareTables(src *sql.DB, dst *gorm.DB, tables []migrationTarget) ([]tablePlan, error) {
	plans := make([]tablePlan, 0, len(tables))
	for _, target := range tables {
		plan, err := prepareTable(src, dst, target)
		if err != nil {
			return nil, fmt.Errorf("预检 %s: %w", target.name, err)
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func prepareTable(src *sql.DB, dst *gorm.DB, target migrationTarget) (tablePlan, error) {
	plan := tablePlan{target: target}
	var err error
	plan.dstCols, err = postgresColumns(dst, target.name)
	if err != nil {
		return plan, err
	}
	if len(plan.dstCols) == 0 {
		return plan, errors.New("目标库无此表（AutoMigrate 应已建好，请检查）")
	}
	plan.report.destRows, err = countRows(dst, target.name)
	if err != nil {
		return plan, err
	}

	srcInfo, err := sqliteTable(src, target.name)
	if err != nil {
		return plan, err
	}
	if len(srcInfo.columns) == 0 {
		plan.report.skippedReason = "源库无此表"
		return plan, nil
	}

	srcSet := make(map[string]bool, len(srcInfo.columns))
	for _, col := range srcInfo.columns {
		srcSet[col] = true
		if _, ok := plan.dstCols[col]; ok {
			plan.shared = append(plan.shared, col)
		} else {
			if target.name == "sim_cards" && isLegacySIMPhoneColumn(col) {
				plan.report.convertedColumns = append(plan.report.convertedColumns, col)
			} else {
				plan.report.droppedColumns = append(plan.report.droppedColumns, col)
			}
		}
	}
	if len(plan.shared) == 0 {
		return plan, errors.New("源表与目标表没有同名列，无法映射")
	}
	for col := range plan.dstCols {
		if !srcSet[col] {
			plan.report.missingColumns = append(plan.report.missingColumns, col)
		}
	}
	sort.Strings(plan.report.missingColumns)

	if err := src.QueryRow(`SELECT COUNT(*) FROM ` + quoteIdentifier(target.name)).Scan(&plan.report.sourceRows); err != nil {
		return plan, fmt.Errorf("统计源行数: %w", err)
	}
	if plan.report.sourceRows > 0 {
		if len(srcInfo.primaryKeys) == 0 {
			return plan, errors.New("源表没有主键，无法证明每一行均已落库")
		}
		for _, key := range srcInfo.primaryKeys {
			if _, ok := plan.dstCols[key]; !ok {
				return plan, fmt.Errorf("源主键列 %q 在目标表中不存在", key)
			}
		}
		plan.primaryKeys = srcInfo.primaryKeys
	}
	return plan, nil
}

func migratePreparedTable(src *sql.DB, dst *gorm.DB, plan *tablePlan, opts options) error {
	if plan.report.skippedReason != "" {
		if opts.truncate {
			plan.report.destRows = 0
		}
		return nil
	}
	if plan.report.sourceRows == 0 {
		if opts.truncate {
			plan.report.destRows = 0
		}
		return nil
	}

	copied, err := copyRows(src, dst, plan.target.name, plan.shared, plan.dstCols, opts.batchSize)
	if err != nil {
		return err
	}
	plan.report.copied = copied
	if err := verifySourcePrimaryKeys(src, dst, plan, opts.batchSize); err != nil {
		return err
	}
	plan.report.destRows, err = countRows(dst, plan.target.name)
	return err
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
		inserted, err := insertBatch(dst, table, quoted, batch)
		if err != nil {
			return err
		}
		copied += inserted
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

func insertBatch(dst *gorm.DB, table string, quotedCols []string, batch [][]any) (int64, error) {
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

	result := dst.Exec(sb.String(), args...)
	return result.RowsAffected, result.Error
}

type legacySIMPhoneMigration struct {
	subscriptions []db.SIMSubscription
	pending       []db.PendingPhoneNumber
}

type legacySIMPhoneSourceRow struct {
	ICCID             string
	IMSI              string
	Operator          string
	PhoneNumber       string
	ModemPhoneNumber  string
	VowifiPhoneNumber string
	LastSeen          time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func isLegacySIMPhoneColumn(column string) bool {
	switch column {
	case "phone_number", "modem_phone_number", "vowifi_phone_number":
		return true
	default:
		return false
	}
}

// prepareLegacySIMPhoneMigration reads the three columns that used to live on
// sim_cards and deterministically maps them to their current owners. A row with
// an IMSI belongs to sim_subscriptions; without one it is staged by ICCID.
func prepareLegacySIMPhoneMigration(src *sql.DB, tables []migrationTarget) (legacySIMPhoneMigration, error) {
	var plan legacySIMPhoneMigration
	selected := make(map[string]bool, len(tables))
	for _, table := range tables {
		selected[table.name] = true
	}
	// --tables names source tables. Legacy columns are transformed only when
	// sim_cards itself was selected; selecting a new-model table alone must not
	// implicitly read or migrate an otherwise out-of-scope source table.
	if !selected["sim_cards"] {
		return plan, nil
	}

	info, err := sqliteTable(src, "sim_cards")
	if err != nil {
		return plan, fmt.Errorf("检查 sim_cards 旧号码列: %w", err)
	}
	if len(info.columns) == 0 {
		return plan, nil
	}
	columnSet := make(map[string]bool, len(info.columns))
	hasPhoneColumn := false
	for _, column := range info.columns {
		columnSet[column] = true
		hasPhoneColumn = hasPhoneColumn || isLegacySIMPhoneColumn(column)
	}
	if !hasPhoneColumn {
		return plan, nil
	}
	if !columnSet["iccid"] {
		return plan, errors.New("sim_cards 有旧号码列但没有 iccid，无法安全迁移")
	}

	wanted := []string{
		"iccid", "imsi", "operator",
		"phone_number", "modem_phone_number", "vowifi_phone_number",
		"last_seen", "created_at", "updated_at",
	}
	columns := make([]string, 0, len(wanted))
	columnIndex := make(map[string]int, len(wanted))
	for _, column := range wanted {
		if columnSet[column] {
			columnIndex[column] = len(columns)
			columns = append(columns, column)
		}
	}
	quoted := make([]string, len(columns))
	for i, column := range columns {
		quoted[i] = quoteIdentifier(column)
	}
	rows, err := src.Query(
		`SELECT ` + strings.Join(quoted, ",") + ` FROM "sim_cards" ORDER BY "iccid"`)
	if err != nil {
		return plan, fmt.Errorf("读取 sim_cards 旧号码: %w", err)
	}
	defer rows.Close()

	var sourceRows []legacySIMPhoneSourceRow
	for rows.Next() {
		values := make([]any, len(columns))
		holders := make([]any, len(columns))
		for i := range values {
			holders[i] = &values[i]
		}
		if err := rows.Scan(holders...); err != nil {
			return plan, fmt.Errorf("扫描 sim_cards 旧号码: %w", err)
		}
		value := func(column string) any {
			if index, ok := columnIndex[column]; ok {
				return values[index]
			}
			return nil
		}
		text := func(column string) (string, error) {
			converted, err := coerce(value(column), "text")
			if err != nil || converted == nil {
				return "", err
			}
			s, ok := converted.(string)
			if !ok {
				return "", fmt.Errorf("列 %s 的值类型为 %T", column, converted)
			}
			return strings.TrimSpace(s), nil
		}

		var row legacySIMPhoneSourceRow
		stringFields := []struct {
			column string
			target *string
		}{
			{"iccid", &row.ICCID},
			{"imsi", &row.IMSI},
			{"operator", &row.Operator},
			{"phone_number", &row.PhoneNumber},
			{"modem_phone_number", &row.ModemPhoneNumber},
			{"vowifi_phone_number", &row.VowifiPhoneNumber},
		}
		for _, field := range stringFields {
			*field.target, err = text(field.column)
			if err != nil {
				return plan, fmt.Errorf("读取 sim_cards.%s: %w", field.column, err)
			}
		}
		timeFields := []struct {
			column string
			target *time.Time
		}{
			{"last_seen", &row.LastSeen},
			{"created_at", &row.CreatedAt},
			{"updated_at", &row.UpdatedAt},
		}
		for _, field := range timeFields {
			*field.target, err = legacySIMTime(value(field.column))
			if err != nil {
				return plan, fmt.Errorf("读取 sim_cards[%s].%s: %w", row.ICCID, field.column, err)
			}
		}
		if row.PhoneNumber == "" && row.ModemPhoneNumber == "" && row.VowifiPhoneNumber == "" {
			continue
		}

		targetTable := "sim_subscriptions"
		if row.IMSI == "" {
			targetTable = "pending_phone_numbers"
			if row.ICCID == "" {
				return plan, errors.New("sim_cards 旧号码行同时缺少 IMSI 和 ICCID，无法安全迁移")
			}
		}
		if !selected[targetTable] {
			return plan, fmt.Errorf(
				"sim_cards 旧号码行（iccid=%q imsi=%q）需要派生到 %s；"+
					"请把该表加入 --tables，迁移不会越过显式选择边界",
				row.ICCID, row.IMSI, targetTable)
		}
		sourceRows = append(sourceRows, row)
	}
	if err := rows.Err(); err != nil {
		return plan, fmt.Errorf("遍历 sim_cards 旧号码: %w", err)
	}

	// Old databases can contain a synthetic reader row and a real ICCID row for
	// one IMSI. Oldest-to-newest ordering makes non-empty newer values win; ICCID
	// provides a stable tie-breaker when timestamps are equal or missing.
	sort.SliceStable(sourceRows, func(i, j int) bool {
		left, right := legacySIMRecency(sourceRows[i]), legacySIMRecency(sourceRows[j])
		if !left.Equal(right) {
			return left.Before(right)
		}
		if sourceRows[i].ICCID != sourceRows[j].ICCID {
			return sourceRows[i].ICCID < sourceRows[j].ICCID
		}
		return sourceRows[i].IMSI < sourceRows[j].IMSI
	})

	subscriptions := make(map[string]db.SIMSubscription)
	pending := make(map[string]db.PendingPhoneNumber)
	for _, row := range sourceRows {
		resolvedPhone := row.PhoneNumber
		if resolvedPhone == "" {
			if row.VowifiPhoneNumber != "" {
				resolvedPhone = row.VowifiPhoneNumber
			} else {
				resolvedPhone = row.ModemPhoneNumber
			}
		}
		if row.IMSI != "" {
			sub := subscriptions[row.IMSI]
			sub.IMSI = row.IMSI
			if row.ICCID != "" && !strings.HasPrefix(row.ICCID, "reader-imsi-") {
				sub.CurrentICCID = row.ICCID
			}
			if resolvedPhone != "" {
				sub.PhoneNumber = resolvedPhone
			}
			if row.ModemPhoneNumber != "" {
				sub.ModemPhoneNumber = row.ModemPhoneNumber
			}
			if row.VowifiPhoneNumber != "" {
				sub.VowifiPhoneNumber = row.VowifiPhoneNumber
			}
			if row.Operator != "" {
				sub.Operator = row.Operator
			}
			sub.LastSeen = laterTime(sub.LastSeen, row.LastSeen)
			sub.CreatedAt = earlierTime(sub.CreatedAt, row.CreatedAt)
			sub.UpdatedAt = laterTime(sub.UpdatedAt, row.UpdatedAt)
			subscriptions[row.IMSI] = sub
			continue
		}

		entry := pending[row.ICCID]
		entry.ICCID = row.ICCID
		if resolvedPhone != "" {
			entry.PhoneNumber = resolvedPhone
		}
		if row.ModemPhoneNumber != "" {
			entry.ModemPhoneNumber = row.ModemPhoneNumber
		}
		if row.VowifiPhoneNumber != "" {
			entry.VowifiPhoneNumber = row.VowifiPhoneNumber
		}
		entry.CreatedAt = earlierTime(entry.CreatedAt, row.CreatedAt)
		entry.UpdatedAt = laterTime(entry.UpdatedAt, row.UpdatedAt)
		pending[row.ICCID] = entry
	}

	fallbackTime := time.Unix(0, 0).UTC()
	for _, sub := range subscriptions {
		if sub.PhoneNumber == "" {
			if sub.VowifiPhoneNumber != "" {
				sub.PhoneNumber = sub.VowifiPhoneNumber
			} else {
				sub.PhoneNumber = sub.ModemPhoneNumber
			}
		}
		if sub.CreatedAt.IsZero() {
			sub.CreatedAt = fallbackTime
		}
		if sub.LastSeen.IsZero() {
			sub.LastSeen = laterTime(sub.UpdatedAt, sub.CreatedAt)
		}
		if sub.UpdatedAt.IsZero() {
			sub.UpdatedAt = laterTime(sub.LastSeen, sub.CreatedAt)
		}
		plan.subscriptions = append(plan.subscriptions, sub)
	}
	for _, entry := range pending {
		if entry.PhoneNumber == "" {
			if entry.VowifiPhoneNumber != "" {
				entry.PhoneNumber = entry.VowifiPhoneNumber
			} else {
				entry.PhoneNumber = entry.ModemPhoneNumber
			}
		}
		if entry.CreatedAt.IsZero() {
			entry.CreatedAt = fallbackTime
		}
		if entry.UpdatedAt.IsZero() {
			entry.UpdatedAt = entry.CreatedAt
		}
		plan.pending = append(plan.pending, entry)
	}
	sort.Slice(plan.subscriptions, func(i, j int) bool {
		return plan.subscriptions[i].IMSI < plan.subscriptions[j].IMSI
	})
	sort.Slice(plan.pending, func(i, j int) bool {
		return plan.pending[i].ICCID < plan.pending[j].ICCID
	})
	return plan, nil
}

func legacySIMTime(value any) (time.Time, error) {
	if value == nil {
		return time.Time{}, nil
	}
	converted, err := coerceTime(value)
	if err != nil || converted == nil {
		return time.Time{}, err
	}
	timestamp, ok := converted.(time.Time)
	if !ok {
		return time.Time{}, fmt.Errorf("无法识别的时间值 %T", converted)
	}
	return timestamp, nil
}

func legacySIMRecency(row legacySIMPhoneSourceRow) time.Time {
	return laterTime(row.LastSeen, laterTime(row.UpdatedAt, row.CreatedAt))
}

func laterTime(left, right time.Time) time.Time {
	if left.IsZero() || right.After(left) {
		return right
	}
	return left
}

func earlierTime(left, right time.Time) time.Time {
	if right.IsZero() {
		return left
	}
	if left.IsZero() || right.Before(left) {
		return right
	}
	return left
}

func migrateLegacySIMPhones(dst *gorm.DB, plan legacySIMPhoneMigration) error {
	const upsertSubscription = `
		INSERT INTO sim_subscriptions
			(imsi, current_iccid, phone_number, modem_phone_number, vowifi_phone_number,
			 operator, last_seen, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (imsi) DO UPDATE SET
			current_iccid = CASE WHEN BTRIM(COALESCE(sim_subscriptions.current_iccid, '')) = ''
				THEN EXCLUDED.current_iccid ELSE sim_subscriptions.current_iccid END,
			phone_number = CASE WHEN BTRIM(COALESCE(sim_subscriptions.phone_number, '')) = ''
				THEN EXCLUDED.phone_number ELSE sim_subscriptions.phone_number END,
			modem_phone_number = CASE WHEN BTRIM(COALESCE(sim_subscriptions.modem_phone_number, '')) = ''
				THEN EXCLUDED.modem_phone_number ELSE sim_subscriptions.modem_phone_number END,
			vowifi_phone_number = CASE WHEN BTRIM(COALESCE(sim_subscriptions.vowifi_phone_number, '')) = ''
				THEN EXCLUDED.vowifi_phone_number ELSE sim_subscriptions.vowifi_phone_number END,
			operator = CASE WHEN BTRIM(COALESCE(sim_subscriptions.operator, '')) = ''
				THEN EXCLUDED.operator ELSE sim_subscriptions.operator END`
	for _, derived := range plan.subscriptions {
		expected := derived
		var existing db.SIMSubscription
		err := dst.Where("imsi = ?", derived.IMSI).Take(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("读取 sim_subscriptions[%s]: %w", derived.IMSI, err)
		}
		if err == nil {
			mergeAuthoritativeSubscription(&expected, existing)
		}
		if err := dst.Exec(upsertSubscription,
			derived.IMSI, derived.CurrentICCID, derived.PhoneNumber,
			derived.ModemPhoneNumber, derived.VowifiPhoneNumber, derived.Operator,
			derived.LastSeen, derived.CreatedAt, derived.UpdatedAt,
		).Error; err != nil {
			return fmt.Errorf("写入 sim_subscriptions[%s]: %w", derived.IMSI, err)
		}
		var actual db.SIMSubscription
		if err := dst.Where("imsi = ?", derived.IMSI).Take(&actual).Error; err != nil {
			return fmt.Errorf("验证 sim_subscriptions[%s]: %w", derived.IMSI, err)
		}
		if err := verifySubscriptionStrings(expected, actual); err != nil {
			return fmt.Errorf("验证 sim_subscriptions[%s]: %w", derived.IMSI, err)
		}
	}

	const upsertPending = `
		INSERT INTO pending_phone_numbers
			(iccid, phone_number, modem_phone_number, vowifi_phone_number, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (iccid) DO UPDATE SET
			phone_number = CASE WHEN BTRIM(COALESCE(pending_phone_numbers.phone_number, '')) = ''
				THEN EXCLUDED.phone_number ELSE pending_phone_numbers.phone_number END,
			modem_phone_number = CASE WHEN BTRIM(COALESCE(pending_phone_numbers.modem_phone_number, '')) = ''
				THEN EXCLUDED.modem_phone_number ELSE pending_phone_numbers.modem_phone_number END,
			vowifi_phone_number = CASE WHEN BTRIM(COALESCE(pending_phone_numbers.vowifi_phone_number, '')) = ''
				THEN EXCLUDED.vowifi_phone_number ELSE pending_phone_numbers.vowifi_phone_number END`
	for _, derived := range plan.pending {
		expected := derived
		var existing db.PendingPhoneNumber
		err := dst.Where("iccid = ?", derived.ICCID).Take(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("读取 pending_phone_numbers[%s]: %w", derived.ICCID, err)
		}
		if err == nil {
			mergeAuthoritativePending(&expected, existing)
		}
		if err := dst.Exec(upsertPending,
			derived.ICCID, derived.PhoneNumber, derived.ModemPhoneNumber,
			derived.VowifiPhoneNumber, derived.CreatedAt, derived.UpdatedAt,
		).Error; err != nil {
			return fmt.Errorf("写入 pending_phone_numbers[%s]: %w", derived.ICCID, err)
		}
		var actual db.PendingPhoneNumber
		if err := dst.Where("iccid = ?", derived.ICCID).Take(&actual).Error; err != nil {
			return fmt.Errorf("验证 pending_phone_numbers[%s]: %w", derived.ICCID, err)
		}
		if err := verifyPendingStrings(expected, actual); err != nil {
			return fmt.Errorf("验证 pending_phone_numbers[%s]: %w", derived.ICCID, err)
		}
	}
	return nil
}

func mergeAuthoritativeSubscription(target *db.SIMSubscription, existing db.SIMSubscription) {
	keepNonEmpty(&target.CurrentICCID, existing.CurrentICCID)
	keepNonEmpty(&target.PhoneNumber, existing.PhoneNumber)
	keepNonEmpty(&target.ModemPhoneNumber, existing.ModemPhoneNumber)
	keepNonEmpty(&target.VowifiPhoneNumber, existing.VowifiPhoneNumber)
	keepNonEmpty(&target.Operator, existing.Operator)
}

func mergeAuthoritativePending(target *db.PendingPhoneNumber, existing db.PendingPhoneNumber) {
	keepNonEmpty(&target.PhoneNumber, existing.PhoneNumber)
	keepNonEmpty(&target.ModemPhoneNumber, existing.ModemPhoneNumber)
	keepNonEmpty(&target.VowifiPhoneNumber, existing.VowifiPhoneNumber)
}

func keepNonEmpty(target *string, existing string) {
	if strings.TrimSpace(existing) != "" {
		*target = existing
	}
}

func verifySubscriptionStrings(expected, actual db.SIMSubscription) error {
	return verifyStringFields(
		stringFieldValue{"current_iccid", expected.CurrentICCID, actual.CurrentICCID},
		stringFieldValue{"phone_number", expected.PhoneNumber, actual.PhoneNumber},
		stringFieldValue{"modem_phone_number", expected.ModemPhoneNumber, actual.ModemPhoneNumber},
		stringFieldValue{"vowifi_phone_number", expected.VowifiPhoneNumber, actual.VowifiPhoneNumber},
		stringFieldValue{"operator", expected.Operator, actual.Operator},
	)
}

func verifyPendingStrings(expected, actual db.PendingPhoneNumber) error {
	return verifyStringFields(
		stringFieldValue{"phone_number", expected.PhoneNumber, actual.PhoneNumber},
		stringFieldValue{"modem_phone_number", expected.ModemPhoneNumber, actual.ModemPhoneNumber},
		stringFieldValue{"vowifi_phone_number", expected.VowifiPhoneNumber, actual.VowifiPhoneNumber},
	)
}

type stringFieldValue struct {
	column   string
	expected string
	actual   string
}

func verifyStringFields(fields ...stringFieldValue) error {
	for _, field := range fields {
		if field.actual != field.expected {
			return fmt.Errorf("字段 %s=%q，期望 %q", field.column, field.actual, field.expected)
		}
	}
	return nil
}

func refuseNonEmptyDestination(dst *gorm.DB, tables []migrationTarget) error {
	nonEmpty := make([]string, 0)
	for _, table := range tables {
		exists, err := postgresTableExists(dst, table.name)
		if err != nil {
			return fmt.Errorf("预检目标表 %s: %w", table.name, err)
		}
		if !exists {
			continue
		}
		rows, err := countRows(dst, table.name)
		if err != nil {
			return fmt.Errorf("预检目标表 %s: %w", table.name, err)
		}
		if rows > 0 {
			nonEmpty = append(nonEmpty, fmt.Sprintf("%s=%d", table.name, rows))
		}
	}
	return nonEmptyDestinationError(nonEmpty)
}

func refusePreparedNonEmpty(plans []tablePlan) error {
	nonEmpty := make([]string, 0)
	for _, plan := range plans {
		if plan.report.destRows > 0 {
			nonEmpty = append(nonEmpty, fmt.Sprintf("%s=%d", plan.target.name, plan.report.destRows))
		}
	}
	return nonEmptyDestinationError(nonEmpty)
}

func nonEmptyDestinationError(nonEmpty []string) error {
	if len(nonEmpty) == 0 {
		return nil
	}
	return fmt.Errorf(
		"目标表非空（%s）。默认拒绝导入且尚未执行任何变更；"+
			"确认要追加请加 --allow-nonempty，确认要覆盖请加 --truncate",
		strings.Join(nonEmpty, ", "))
}

func postgresTableExists(dst *gorm.DB, table string) (bool, error) {
	var exists bool
	err := dst.Raw(`SELECT EXISTS (
		SELECT 1 FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_name = ?
	)`, table).Scan(&exists).Error
	return exists, err
}

func lockTables(dst *gorm.DB, tables []migrationTarget) error {
	quoted := make([]string, 0, len(tables))
	for _, table := range tables {
		quoted = append(quoted, quoteIdentifier(table.name))
	}
	if err := dst.Exec("LOCK TABLE " + strings.Join(quoted, ", ") + " IN ACCESS EXCLUSIVE MODE").Error; err != nil {
		return fmt.Errorf("锁定目标表: %w", err)
	}
	return nil
}

func truncateTables(dst *gorm.DB, tables []migrationTarget) error {
	quoted := make([]string, 0, len(tables))
	for _, table := range tables {
		quoted = append(quoted, quoteIdentifier(table.name))
	}
	// 一个语句、默认 RESTRICT：任何未选中表的外键引用都会让整个事务失败，
	// 绝不通过 CASCADE 越过 --tables 的明确边界。
	if err := dst.Exec("TRUNCATE TABLE " + strings.Join(quoted, ", ") + " RESTART IDENTITY").Error; err != nil {
		return fmt.Errorf("清空选中目标表: %w", err)
	}
	return nil
}

func tableNames(tables []migrationTarget) []string {
	out := make([]string, 0, len(tables))
	for _, table := range tables {
		out = append(out, table.name)
	}
	return out
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func verifySourcePrimaryKeys(src *sql.DB, dst *gorm.DB, plan *tablePlan, batchSize int) error {
	quoted := make([]string, len(plan.primaryKeys))
	for i, key := range plan.primaryKeys {
		quoted[i] = quoteIdentifier(key)
	}
	rows, err := src.Query(
		`SELECT ` + strings.Join(quoted, ",") + ` FROM ` + quoteIdentifier(plan.target.name))
	if err != nil {
		return fmt.Errorf("读取源主键: %w", err)
	}
	defer rows.Close()

	// PostgreSQL 单条语句最多 65535 个参数，给其它表达式留出余量。
	if maxRows := 60000 / len(plan.primaryKeys); batchSize > maxRows {
		batchSize = maxRows
	}
	var (
		batch    [][]any
		verified int64
	)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		var (
			predicate strings.Builder
			args      []any
		)
		if len(plan.primaryKeys) == 1 {
			predicate.WriteString(quoted[0])
			predicate.WriteString(" IN (")
			for i, values := range batch {
				if i > 0 {
					predicate.WriteString(",")
				}
				predicate.WriteString("?")
				args = append(args, values[0])
			}
			predicate.WriteString(")")
		} else {
			predicate.WriteString("(")
			predicate.WriteString(strings.Join(quoted, ","))
			predicate.WriteString(") IN (")
			for i, values := range batch {
				if i > 0 {
					predicate.WriteString(",")
				}
				predicate.WriteString("(")
				for j, value := range values {
					if j > 0 {
						predicate.WriteString(",")
					}
					predicate.WriteString("?")
					args = append(args, value)
				}
				predicate.WriteString(")")
			}
			predicate.WriteString(")")
		}

		var found int64
		query := `SELECT COUNT(*) FROM ` + quoteIdentifier(plan.target.name) + ` WHERE ` + predicate.String()
		if err := dst.Raw(query, args...).Scan(&found).Error; err != nil {
			return fmt.Errorf("查询目标主键: %w", err)
		}
		if found != int64(len(batch)) {
			return fmt.Errorf(
				"源主键落库校验失败：本批 %d 个主键仅找到 %d 个；"+
					"可能被目标库其它唯一约束冲突跳过", len(batch), found)
		}
		verified += found
		batch = batch[:0]
		return nil
	}

	for rows.Next() {
		values := make([]any, len(plan.primaryKeys))
		holders := make([]any, len(values))
		for i := range values {
			holders[i] = &values[i]
		}
		if err := rows.Scan(holders...); err != nil {
			return fmt.Errorf("扫描源主键: %w", err)
		}
		for i, key := range plan.primaryKeys {
			values[i], err = coerce(values[i], plan.dstCols[key])
			if err != nil {
				return fmt.Errorf("主键列 %s: %w", key, err)
			}
		}
		batch = append(batch, values)
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("遍历源主键: %w", err)
	}
	if err := flush(); err != nil {
		return err
	}
	if verified != plan.report.sourceRows {
		return fmt.Errorf("源主键落库校验失败：源 %d 个，验证 %d 个", plan.report.sourceRows, verified)
	}
	return nil
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

func sqliteTable(src *sql.DB, table string) (sqliteTableInfo, error) {
	rows, err := src.Query(`SELECT name, pk FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		return sqliteTableInfo{}, fmt.Errorf("读取源表结构: %w", err)
	}
	defer rows.Close()

	type primaryKey struct {
		name  string
		order int
	}
	var (
		out  sqliteTableInfo
		keys []primaryKey
	)
	for rows.Next() {
		var (
			name string
			pk   int
		)
		if err := rows.Scan(&name, &pk); err != nil {
			return sqliteTableInfo{}, err
		}
		out.columns = append(out.columns, name)
		if pk > 0 {
			keys = append(keys, primaryKey{name: name, order: pk})
		}
	}
	if err := rows.Err(); err != nil {
		return sqliteTableInfo{}, err
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].order < keys[j].order })
	for _, key := range keys {
		out.primaryKeys = append(out.primaryKeys, key.name)
	}
	return out, nil
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
	trimmed := strings.TrimSpace(dsn)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://") {
		return redactURLDSN(trimmed)
	}
	redacted, ok := redactKeywordValueDSN(dsn)
	if !ok {
		// Parsing failures must fail closed: returning the input would put the
		// very credential this helper protects into migration logs.
		return "<invalid PostgreSQL DSN; redacted>"
	}
	return redacted
}

func redactURLDSN(dsn string) string {
	parsed, err := url.Parse(dsn)
	if err != nil ||
		!strings.EqualFold(parsed.Scheme, "postgres") && !strings.EqualFold(parsed.Scheme, "postgresql") ||
		parsed.Opaque != "" || parsed.Fragment != "" {
		return "<invalid PostgreSQL DSN; redacted>"
	}
	changed := false
	if parsed.User != nil {
		if _, present := parsed.User.Password(); present {
			parsed.User = url.UserPassword(parsed.User.Username(), "***")
			changed = true
		}
	}
	if parsed.RawQuery != "" {
		query, err := url.ParseQuery(parsed.RawQuery)
		if err != nil {
			return "<invalid PostgreSQL DSN; redacted>"
		}
		for key := range query {
			if isDSNSecretKey(key) {
				query[key] = []string{"***"}
				changed = true
			}
		}
		if changed {
			parsed.RawQuery = query.Encode()
		}
	}
	if !changed {
		return dsn
	}
	// net/url correctly percent-escapes userinfo and query values. Keep the
	// long-standing human-readable marker in logs after serialization.
	return strings.ReplaceAll(parsed.String(), "%2A%2A%2A", "***")
}

// redactKeywordValueDSN follows libpq's keyword/value lexer: whitespace ends
// an unquoted value unless escaped, while quoted values accept escaped quotes
// and backslashes. It preserves the original non-secret spelling and spacing.
func redactKeywordValueDSN(dsn string) (string, bool) {
	var out strings.Builder
	lastWritten := 0
	for cursor := 0; cursor < len(dsn); {
		for cursor < len(dsn) && isLibpqSpace(dsn[cursor]) {
			cursor++
		}
		if cursor == len(dsn) {
			break
		}

		relativeEquals := strings.IndexByte(dsn[cursor:], '=')
		if relativeEquals < 0 {
			return "", false
		}
		equals := cursor + relativeEquals
		key := strings.TrimSpace(dsn[cursor:equals])
		if !isLibpqKeyword(key) {
			return "", false
		}
		valueStart := equals + 1
		for valueStart < len(dsn) && isLibpqSpace(dsn[valueStart]) {
			valueStart++
		}

		valueEnd := valueStart
		if valueStart < len(dsn) && dsn[valueStart] == '\'' {
			valueEnd++
			closed := false
			for valueEnd < len(dsn) {
				switch dsn[valueEnd] {
				case '\\':
					valueEnd += 2
				case '\'':
					valueEnd++
					closed = true
				default:
					valueEnd++
				}
				if closed {
					break
				}
			}
			if !closed || valueEnd > len(dsn) {
				return "", false
			}
		} else {
			for valueEnd < len(dsn) && !isLibpqSpace(dsn[valueEnd]) {
				if dsn[valueEnd] == '\\' {
					valueEnd += 2
					if valueEnd > len(dsn) {
						return "", false
					}
					continue
				}
				valueEnd++
			}
		}

		if isDSNSecretKey(key) {
			out.WriteString(dsn[lastWritten:valueStart])
			out.WriteString("***")
			lastWritten = valueEnd
		}
		cursor = valueEnd
	}
	out.WriteString(dsn[lastWritten:])
	return out.String(), true
}

func isLibpqSpace(value byte) bool {
	switch value {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	default:
		return false
	}
}

func isLibpqKeyword(value string) bool {
	if value == "" || !isASCIIAlpha(value[0]) && value[0] != '_' {
		return false
	}
	for i := 1; i < len(value); i++ {
		if !isASCIIAlpha(value[i]) && (value[i] < '0' || value[i] > '9') && value[i] != '_' {
			return false
		}
	}
	return true
}

func isASCIIAlpha(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func isDSNSecretKey(key string) bool {
	return strings.EqualFold(strings.TrimSpace(key), "password") ||
		strings.EqualFold(strings.TrimSpace(key), "sslpassword")
}
