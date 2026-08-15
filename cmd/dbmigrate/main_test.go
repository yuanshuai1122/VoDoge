package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yuanshuai1122/vodog/internal/db"
	_ "modernc.org/sqlite"
)

// 造一个"旧版"SQLite 库。列有意与当前模型不完全一致：
//   - sim_cards 带着早已删掉的 phone_number 列（源多出来的列要被忽略）
//   - 时间用三种存法（RFC3339 文本、空格分隔文本、Unix 秒），旧驱动这几种都写过
//   - 布尔存 0/1
func newLegacySQLite(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "legacy.db")
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	stmts := []string{
		`CREATE TABLE devices (
			imei TEXT PRIMARY KEY, alias TEXT, model TEXT, firmware TEXT, port TEXT,
			public_ip TEXT, private_ip TEXT, public_ipv6 TEXT, private_ipv6 TEXT,
			iccid TEXT, sim_inserted INTEGER, signal_dbm INTEGER, signal_rsrq INTEGER,
			signal_rsrp INTEGER, last_seen TEXT, created_at TEXT, updated_at TEXT)`,
		`INSERT INTO devices VALUES
			('350000000000001','dev-a','RM500','1.0','/dev/ttyUSB0','1.2.3.4','192.168.1.9','','',
			 '8986001',1,-71,-11,-95,'2026-06-01T10:00:00Z','2026-06-01T09:00:00Z','2026-06-01T10:00:00Z'),
			('350000000000002','dev-b','RM520','2.0','/dev/ttyUSB2','5.6.7.8','192.168.1.10','','',
			 NULL,0,-99,-15,-115,'2026-06-02 11:30:00','2026-06-02 11:00:00','2026-06-02 11:30:00')`,

		`CREATE TABLE sim_cards (
			iccid TEXT PRIMARY KEY, imsi TEXT, operator TEXT, current_imei TEXT,
			reg_status INTEGER, reg_status_text TEXT, lac TEXT, cell_id TEXT, apn TEXT,
			ims_status INTEGER, phone_number TEXT, modem_phone_number TEXT,
			last_seen INTEGER, created_at INTEGER, updated_at INTEGER)`,
		`INSERT INTO sim_cards VALUES
			('8986001','460010000000001','CMCC','350000000000001',1,'registered','1A2B','00C1D2','cmnet',1,
			 '+8613800000001','+8613800000001',1780000000,1779000000,1780000000)`,

		`CREATE TABLE sms (
			id INTEGER PRIMARY KEY AUTOINCREMENT, imsi TEXT, iccid TEXT, peer TEXT,
			local_phone TEXT, sender TEXT, recipient TEXT, content TEXT,
			type INTEGER, status INTEGER, timestamp TEXT, created_at TEXT)`,
		`INSERT INTO sms (id, imsi, iccid, peer, local_phone, sender, recipient, content, type, status, timestamp, created_at) VALUES
			(41,'460010000000001','8986001','10086','+8613800000001','10086','+8613800000001','余额提醒',1,0,
			 '2026-06-01T10:05:00Z','2026-06-01T10:05:00Z'),
			(42,'460010000000001','8986001','10086','+8613800000001','+8613800000001','10086','CXLL',2,2,
			 '2026-06-01T10:06:00Z','2026-06-01T10:06:00Z')`,
	}
	for _, stmt := range stmts {
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("prepare legacy db: %v\n%s", err, stmt)
		}
	}
	return path
}

func testOptions(t *testing.T, sqlitePath string) options {
	t.Helper()
	dsn := db.TestDSN()
	if strings.TrimSpace(dsn) == "" {
		t.Skip("set TEST_DATABASE_URL or VOHIVE_DB_DSN to a PostgreSQL DSN for database tests")
	}
	return options{
		sqlitePath:  sqlitePath,
		postgresDSN: dsn,
		batchSize:   2, // 小到足以跨越多批，验证分批不丢行
		truncate:    true,
	}
}

func TestMigrateCopiesRowsAndCoercesLegacyValueShapes(t *testing.T) {
	opts := testOptions(t, newLegacySQLite(t))
	if err := run(opts); err != nil {
		t.Fatalf("run() error: %v", err)
	}

	var devices int64
	if err := db.DB.Raw(`SELECT COUNT(*) FROM devices`).Scan(&devices).Error; err != nil {
		t.Fatal(err)
	}
	if devices != 2 {
		t.Fatalf("devices=%d want 2", devices)
	}

	// 0/1 必须变成真正的布尔，否则 PostgreSQL 根本不接受这次插入
	var inserted bool
	if err := db.DB.Raw(`SELECT sim_inserted FROM devices WHERE imei = ?`, "350000000000001").
		Scan(&inserted).Error; err != nil {
		t.Fatal(err)
	}
	if !inserted {
		t.Fatalf("sim_inserted=false want true (0/1 未被转成布尔)")
	}

	// 三种时间存法都要落成同一个时刻
	var lastSeen time.Time
	if err := db.DB.Raw(`SELECT last_seen FROM devices WHERE imei = ?`, "350000000000001").
		Scan(&lastSeen).Error; err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC); !lastSeen.UTC().Equal(want) {
		t.Fatalf("last_seen=%s want %s", lastSeen.UTC(), want)
	}

	var simLastSeen time.Time
	if err := db.DB.Raw(`SELECT last_seen FROM sim_cards WHERE iccid = ?`, "8986001").
		Scan(&simLastSeen).Error; err != nil {
		t.Fatal(err)
	}
	if want := time.Unix(1780000000, 0).UTC(); !simLastSeen.UTC().Equal(want) {
		t.Fatalf("sim_cards.last_seen=%s want %s (Unix 秒未被识别)", simLastSeen.UTC(), want)
	}

	// 源多出来的列（phone_number 早已从模型删除）不应让整表失败
	var operator string
	if err := db.DB.Raw(`SELECT operator FROM sim_cards WHERE iccid = ?`, "8986001").
		Scan(&operator).Error; err != nil {
		t.Fatal(err)
	}
	if operator != "CMCC" {
		t.Fatalf("operator=%q want CMCC", operator)
	}
}

// 迁移是带着原始 id 写进去的，序列并不知情。不校正的话，迁移完第一条新短信
// 就会撞上已存在的主键——这是最容易在上线当天才发现的问题。
func TestMigrateAdvancesSequencesPastImportedIDs(t *testing.T) {
	opts := testOptions(t, newLegacySQLite(t))
	if err := run(opts); err != nil {
		t.Fatalf("run() error: %v", err)
	}

	if err := db.DB.Create(&db.SMS{
		IMSI:      "460010000000001",
		ICCID:     "8986001",
		Peer:      "10086",
		Sender:    "10086",
		Recipient: "+8613800000001",
		Content:   "迁移之后写入的新短信",
		Type:      1,
		Status:    0,
		Timestamp: time.Now().Truncate(time.Second),
	}).Error; err != nil {
		t.Fatalf("插入新短信失败（序列未推进到导入的最大 id 之后）: %v", err)
	}

	var maxID int64
	if err := db.DB.Raw(`SELECT MAX(id) FROM sms`).Scan(&maxID).Error; err != nil {
		t.Fatal(err)
	}
	if maxID <= 42 {
		t.Fatalf("max(id)=%d want > 42", maxID)
	}
}

// 默认必须拒绝往非空库里导：把两份数据混在一起，事后分不开。
func TestMigrateRefusesNonEmptyDestinationByDefault(t *testing.T) {
	path := newLegacySQLite(t)
	opts := testOptions(t, path)
	if err := run(opts); err != nil {
		t.Fatalf("首次迁移失败: %v", err)
	}

	opts.truncate = false
	err := run(opts)
	if err == nil {
		t.Fatal("目标库非空时 run() 应当报错")
	}
	if !strings.Contains(err.Error(), "--allow-nonempty") {
		t.Fatalf("err=%v want 提示 --allow-nonempty / --truncate", err)
	}
}

// 重跑迁移应当是安全的：冲突行跳过，而不是中途炸掉留下半个库。
func TestMigrateIsIdempotentWithAllowNonEmpty(t *testing.T) {
	path := newLegacySQLite(t)
	opts := testOptions(t, path)
	if err := run(opts); err != nil {
		t.Fatalf("首次迁移失败: %v", err)
	}

	opts.truncate = false
	opts.allowNonEmpty = true
	if err := run(opts); err != nil {
		t.Fatalf("重跑迁移失败: %v", err)
	}

	var devices int64
	if err := db.DB.Raw(`SELECT COUNT(*) FROM devices`).Scan(&devices).Error; err != nil {
		t.Fatal(err)
	}
	if devices != 2 {
		t.Fatalf("devices=%d want 2（重跑不应产生重复行）", devices)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	opts := testOptions(t, newLegacySQLite(t))
	opts.truncate = true
	// 先清空目标库
	if err := run(options{
		sqlitePath: opts.sqlitePath, postgresDSN: opts.postgresDSN,
		batchSize: 2, truncate: true, only: "devices",
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Exec(`TRUNCATE TABLE devices RESTART IDENTITY CASCADE`).Error; err != nil {
		t.Fatal(err)
	}

	opts.dryRun = true
	opts.truncate = false
	if err := run(opts); err != nil {
		t.Fatalf("dry-run error: %v", err)
	}

	var devices int64
	if err := db.DB.Raw(`SELECT COUNT(*) FROM devices`).Scan(&devices).Error; err != nil {
		t.Fatal(err)
	}
	if devices != 0 {
		t.Fatalf("devices=%d want 0（演练不应写入）", devices)
	}
}

func TestRedactDSNHidesPassword(t *testing.T) {
	cases := []struct{ in, want string }{
		{"host=db user=vohive password=s3cret dbname=vohive", "host=db user=vohive password=*** dbname=vohive"},
		{"postgres://vohive:s3cret@db:5432/vohive", "postgres://vohive:***@db:5432/vohive"},
		{"host=db user=vohive dbname=vohive", "host=db user=vohive dbname=vohive"},
	}
	for _, tc := range cases {
		if got := redactDSN(tc.in); got != tc.want {
			t.Fatalf("redactDSN(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestSelectTablesKeepsCanonicalOrder(t *testing.T) {
	got, err := selectTables("sms,devices")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "devices" || got[1] != "sms" {
		t.Fatalf("selectTables=%v want [devices sms]", got)
	}
	if _, err := selectTables("not_a_table"); err == nil {
		t.Fatal("selectTables() 对未知表名应报错")
	}
}
