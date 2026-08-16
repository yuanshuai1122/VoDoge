package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yuanshuai1122/vodoge/internal/db"
	_ "modernc.org/sqlite"
)

// 造一个"旧版"SQLite 库。列有意与当前模型不完全一致：
//   - sim_cards 带着早已删掉的 phone_number 三列（要转换到新号码模型）
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
			vowifi_phone_number TEXT,
			last_seen INTEGER, created_at INTEGER, updated_at INTEGER)`,
		`INSERT INTO sim_cards VALUES
			('8986001','460010000000001','CMCC','350000000000001',1,'registered','1A2B','00C1D2','cmnet',1,
			 '+8613800000001','+8613700000001','+8613900000001',1780000000,1779000000,1780000000)`,

		`CREATE TABLE sms (
			id INTEGER PRIMARY KEY AUTOINCREMENT, imsi TEXT, iccid TEXT, peer TEXT,
			local_phone TEXT, sender TEXT, recipient TEXT, content TEXT,
			type INTEGER, status INTEGER, timestamp TEXT, created_at TEXT)`,
		`INSERT INTO sms (id, imsi, iccid, peer, local_phone, sender, recipient, content, type, status, timestamp, created_at) VALUES
			(41,'460010000000001','8986001','10086','+8613800000001','10086','+8613800000001','余额提醒',1,0,
			 '2026-06-01T10:05:00Z','2026-06-01T10:05:00Z'),
			(42,'460010000000001','8986001','10086','+8613800000001','+8613800000001','10086','CXLL',2,2,
			 '2026-06-01T10:06:00Z','2026-06-01T10:06:00Z')`,

		// 这九张表曾因迁移清单漂移而被漏掉：两张短信投递表和五张流量表
		// 写成了不存在的复数名，另两张根本不在清单中。
		`CREATE TABLE sms_delivery (
			message_id TEXT PRIMARY KEY, imsi TEXT, iccid TEXT, device_id TEXT, peer TEXT,
			content TEXT, parts_total INTEGER, acks INTEGER, state TEXT, last_error TEXT,
			created_at TEXT, updated_at TEXT)`,
		`INSERT INTO sms_delivery VALUES
			('legacy-message-1','460010000000001','8986001','350000000000001','10086',
			 'delivery payload',2,1,'partial_ack','','2026-06-01T10:06:00Z','2026-06-01T10:07:00Z')`,
		`CREATE TABLE sms_delivery_part (
			id INTEGER PRIMARY KEY AUTOINCREMENT, message_id TEXT, part_no INTEGER, call_id TEXT,
			in_reply_to TEXT, rp_mr INTEGER, state TEXT, sip_code INTEGER, rp_cause INTEGER,
			error_text TEXT, sent_at TEXT, report_at TEXT, created_at TEXT, updated_at TEXT)`,
		`INSERT INTO sms_delivery_part VALUES
			(77,'legacy-message-1',1,'call-1','',9,'acked',200,0,'',
			 '2026-06-01T10:06:00Z','2026-06-01T10:07:00Z','2026-06-01T10:06:00Z','2026-06-01T10:07:00Z')`,
		`CREATE TABLE upstream_proxy_profile_bindings (
			iccid TEXT PRIMARY KEY, device_id TEXT, profile_name TEXT, upstream_proxy_id TEXT,
			created_at TEXT, updated_at TEXT)`,
		`INSERT INTO upstream_proxy_profile_bindings VALUES
			('8986001234567890123','350000000000001','legacy-profile','proxy-a',
			 '2026-06-01T09:00:00Z','2026-06-01T10:00:00Z')`,
		`CREATE TABLE sms_send_attempts (
			id INTEGER PRIMARY KEY AUTOINCREMENT, device_id TEXT, recipient TEXT, created_at TEXT)`,
		`INSERT INTO sms_send_attempts VALUES
			(88,'350000000000001','10086','2026-06-01T10:06:00Z')`,

		`CREATE TABLE traffic_minute (
			id INTEGER PRIMARY KEY AUTOINCREMENT, period_start TEXT NOT NULL, resource TEXT NOT NULL,
			tag TEXT NOT NULL, direction INTEGER NOT NULL, traffic_bytes INTEGER NOT NULL,
			created_at TEXT, updated_at TEXT)`,
		`INSERT INTO traffic_minute VALUES
			(101,'2026-06-01T10:01:00Z','device','350000000000001',1,101,
			 '2026-06-01T10:02:00Z','2026-06-01T10:02:00Z')`,
		`CREATE TABLE traffic_hour (
			id INTEGER PRIMARY KEY AUTOINCREMENT, period_start TEXT NOT NULL, resource TEXT NOT NULL,
			tag TEXT NOT NULL, direction INTEGER NOT NULL, traffic_bytes INTEGER NOT NULL,
			created_at TEXT, updated_at TEXT)`,
		`INSERT INTO traffic_hour VALUES
			(102,'2026-06-01T10:00:00Z','device','350000000000001',1,102,
			 '2026-06-01T11:00:00Z','2026-06-01T11:00:00Z')`,
		`CREATE TABLE traffic_day (
			id INTEGER PRIMARY KEY AUTOINCREMENT, period_start TEXT NOT NULL, resource TEXT NOT NULL,
			tag TEXT NOT NULL, direction INTEGER NOT NULL, traffic_bytes INTEGER NOT NULL,
			created_at TEXT, updated_at TEXT)`,
		`INSERT INTO traffic_day VALUES
			(103,'2026-06-01T00:00:00Z','device','350000000000001',1,103,
			 '2026-06-02T00:00:00Z','2026-06-02T00:00:00Z')`,
		`CREATE TABLE traffic_week (
			id INTEGER PRIMARY KEY AUTOINCREMENT, period_start TEXT NOT NULL, resource TEXT NOT NULL,
			tag TEXT NOT NULL, direction INTEGER NOT NULL, traffic_bytes INTEGER NOT NULL,
			created_at TEXT, updated_at TEXT)`,
		`INSERT INTO traffic_week VALUES
			(104,'2026-06-01T00:00:00Z','device','350000000000001',1,104,
			 '2026-06-08T00:00:00Z','2026-06-08T00:00:00Z')`,
		`CREATE TABLE traffic_month (
			id INTEGER PRIMARY KEY AUTOINCREMENT, period_start TEXT NOT NULL, resource TEXT NOT NULL,
			tag TEXT NOT NULL, direction INTEGER NOT NULL, traffic_bytes INTEGER NOT NULL,
			created_at TEXT, updated_at TEXT)`,
		`INSERT INTO traffic_month VALUES
			(105,'2026-06-01T00:00:00Z','device','350000000000001',1,105,
			 '2026-07-01T00:00:00Z','2026-07-01T00:00:00Z')`,
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
		t.Skip("set TEST_DATABASE_URL or VODOGE_DB_DSN to a PostgreSQL DSN for database tests")
	}
	return options{
		sqlitePath:  sqlitePath,
		postgresDSN: dsn,
		batchSize:   2, // 小到足以跨越多批，验证分批不丢行
		truncate:    true,
	}
}

func execLegacySQLite(t *testing.T, path, statement string, args ...any) {
	t.Helper()
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Exec(statement, args...); err != nil {
		t.Fatalf("update legacy db: %v\n%s", err, statement)
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

	// 源多出来的号码列已从 sim_cards 删除，但其值必须进入订阅模型。
	var operator string
	if err := db.DB.Raw(`SELECT operator FROM sim_cards WHERE iccid = ?`, "8986001").
		Scan(&operator).Error; err != nil {
		t.Fatal(err)
	}
	if operator != "CMCC" {
		t.Fatalf("operator=%q want CMCC", operator)
	}
	var subscription db.SIMSubscription
	if err := db.DB.Where("imsi = ?", "460010000000001").Take(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	if subscription.CurrentICCID != "8986001" || subscription.Operator != "CMCC" {
		t.Fatalf("subscription identity=%+v", subscription)
	}
	if subscription.PhoneNumber != "+8613800000001" ||
		subscription.ModemPhoneNumber != "+8613700000001" ||
		subscription.VowifiPhoneNumber != "+8613900000001" {
		t.Fatalf("legacy phone fields not migrated: %+v", subscription)
	}
	if want := time.Unix(1780000000, 0).UTC(); !subscription.LastSeen.UTC().Equal(want) {
		t.Fatalf("subscription.last_seen=%s want %s", subscription.LastSeen.UTC(), want)
	}

	for table, want := range map[string]int64{
		"sms_delivery":                    1,
		"sms_delivery_part":               1,
		"traffic_minute":                  1,
		"traffic_hour":                    1,
		"traffic_day":                     1,
		"traffic_week":                    1,
		"traffic_month":                   1,
		"upstream_proxy_profile_bindings": 1,
		"sms_send_attempts":               1,
	} {
		var got int64
		if err := db.DB.Raw(`SELECT COUNT(*) FROM ` + quoteIdentifier(table)).Scan(&got).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s=%d want %d", table, got, want)
		}
	}
}

func TestMigrateRoutesLegacyPhoneWithoutIMSIToPending(t *testing.T) {
	path := newLegacySQLite(t)
	execLegacySQLite(t, path, `
		INSERT INTO sim_cards
			(iccid, imsi, operator, phone_number, modem_phone_number, vowifi_phone_number,
			 last_seen, created_at, updated_at)
		VALUES (?, '', ?, '', ?, ?, ?, ?, ?)
	`, "8986002", "CUCC", "+8613700000002", "+8613900000002", 1781000000, 1780000000, 1781000000)

	if err := run(testOptions(t, path)); err != nil {
		t.Fatalf("run() error: %v", err)
	}
	var pending db.PendingPhoneNumber
	if err := db.DB.Where("iccid = ?", "8986002").Take(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if pending.PhoneNumber != "+8613900000002" ||
		pending.ModemPhoneNumber != "+8613700000002" ||
		pending.VowifiPhoneNumber != "+8613900000002" {
		t.Fatalf("legacy pending phone fields not migrated: %+v", pending)
	}
}

func TestLegacyPhoneAggregationUsesLatestRowAndRealICCID(t *testing.T) {
	path := newLegacySQLite(t)
	execLegacySQLite(t, path, `
		INSERT INTO sim_cards
			(iccid, imsi, operator, phone_number, modem_phone_number, vowifi_phone_number,
			 last_seen, created_at, updated_at)
		VALUES ('reader-imsi-460010000000001', '460010000000001', '', '',
			'+8613700000099', '+8613900000099', 1781000000, 1780000000, 1781000000)
	`)

	if err := run(testOptions(t, path)); err != nil {
		t.Fatalf("run() error: %v", err)
	}
	var subscription db.SIMSubscription
	if err := db.DB.Where("imsi = ?", "460010000000001").Take(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	if subscription.CurrentICCID != "8986001" {
		t.Fatalf("CurrentICCID=%q want real ICCID 8986001", subscription.CurrentICCID)
	}
	if subscription.PhoneNumber != "+8613900000099" ||
		subscription.ModemPhoneNumber != "+8613700000099" ||
		subscription.VowifiPhoneNumber != "+8613900000099" {
		t.Fatalf("latest legacy phone fields not selected: %+v", subscription)
	}
}

func TestLegacyPhoneBackfillKeepsSourceNewModelFields(t *testing.T) {
	path := newLegacySQLite(t)
	execLegacySQLite(t, path, `
		CREATE TABLE sim_subscriptions (
			imsi TEXT PRIMARY KEY, current_iccid TEXT, phone_number TEXT,
			modem_phone_number TEXT, vowifi_phone_number TEXT, operator TEXT,
			last_seen INTEGER, created_at INTEGER, updated_at INTEGER)
	`)
	execLegacySQLite(t, path, `
		INSERT INTO sim_subscriptions VALUES
			('460010000000001', 'source-current-iccid', '+8613600000001', '',
			 '+8613500000001', 'SOURCE-OP', 1782000000, 1780000000, 1782000000)
	`)
	execLegacySQLite(t, path, `
		INSERT INTO sim_cards
			(iccid, imsi, phone_number, modem_phone_number, vowifi_phone_number,
			 last_seen, created_at, updated_at)
		VALUES ('8986002', '', '+8613800000002', '+8613700000002', '+8613900000002',
			1781000000, 1780000000, 1781000000)
	`)
	execLegacySQLite(t, path, `
		CREATE TABLE pending_phone_numbers (
			iccid TEXT PRIMARY KEY, phone_number TEXT, modem_phone_number TEXT,
			vowifi_phone_number TEXT, created_at INTEGER, updated_at INTEGER)
	`)
	execLegacySQLite(t, path, `
		INSERT INTO pending_phone_numbers VALUES
			('8986002', '+8613400000002', '', '+8613300000002', 1780000000, 1782000000)
	`)

	if err := run(testOptions(t, path)); err != nil {
		t.Fatalf("run() error: %v", err)
	}
	var subscription db.SIMSubscription
	if err := db.DB.Where("imsi = ?", "460010000000001").Take(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	if subscription.CurrentICCID != "source-current-iccid" ||
		subscription.PhoneNumber != "+8613600000001" ||
		subscription.ModemPhoneNumber != "+8613700000001" ||
		subscription.VowifiPhoneNumber != "+8613500000001" ||
		subscription.Operator != "SOURCE-OP" {
		t.Fatalf("source subscription authority/backfill mismatch: %+v", subscription)
	}
	var pending db.PendingPhoneNumber
	if err := db.DB.Where("iccid = ?", "8986002").Take(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if pending.PhoneNumber != "+8613400000002" ||
		pending.ModemPhoneNumber != "+8613700000002" ||
		pending.VowifiPhoneNumber != "+8613300000002" {
		t.Fatalf("source pending authority/backfill mismatch: %+v", pending)
	}
}

func TestLegacyPhoneMigrationRequiresSelectedDerivedTargets(t *testing.T) {
	path := newLegacySQLite(t)
	opts := testOptions(t, path)
	opts.only = "sim_cards"
	err := run(opts)
	if err == nil || !strings.Contains(err.Error(), "sim_subscriptions") || !strings.Contains(err.Error(), "--tables") {
		t.Fatalf("run() err=%v want missing sim_subscriptions selection error", err)
	}

	execLegacySQLite(t, path, `
		INSERT INTO sim_cards (iccid, imsi, phone_number, created_at, updated_at)
		VALUES ('8986002', '', '+8613800000002', 1780000000, 1780000000)
	`)
	opts.only = "sim_cards,sim_subscriptions"
	err = run(opts)
	if err == nil || !strings.Contains(err.Error(), "pending_phone_numbers") || !strings.Contains(err.Error(), "--tables") {
		t.Fatalf("run() err=%v want missing pending_phone_numbers selection error", err)
	}
}

func TestSelectingSubscriptionAloneDoesNotReadLegacySIMCards(t *testing.T) {
	opts := testOptions(t, newLegacySQLite(t))
	opts.only = "sim_subscriptions"
	if err := run(opts); err != nil {
		t.Fatalf("run() error: %v", err)
	}
	var subscriptions int64
	if err := db.DB.Model(&db.SIMSubscription{}).Count(&subscriptions).Error; err != nil {
		t.Fatal(err)
	}
	if subscriptions != 0 {
		t.Fatalf("sim_subscriptions=%d want 0（未选择 sim_cards 时不得隐式派生）", subscriptions)
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
	opts.only = "devices,sms"
	if err := run(opts); err != nil {
		t.Fatalf("首次迁移失败: %v", err)
	}
	if err := db.DB.Exec(`TRUNCATE TABLE devices RESTART IDENTITY`).Error; err != nil {
		t.Fatal(err)
	}

	opts.truncate = false
	err := run(opts)
	if err == nil {
		t.Fatal("目标库非空时 run() 应当报错")
	}
	if !strings.Contains(err.Error(), "--allow-nonempty") {
		t.Fatalf("err=%v want 提示 --allow-nonempty / --truncate", err)
	}
	var devices int64
	if err := db.DB.Raw(`SELECT COUNT(*) FROM devices`).Scan(&devices).Error; err != nil {
		t.Fatal(err)
	}
	if devices != 0 {
		t.Fatalf("devices=%d want 0（后面的 sms 非空时，预检失败前不得先写 devices）", devices)
	}
}

// 重跑迁移应当是安全的：冲突行跳过，而不是中途炸掉留下半个库。
func TestMigrateIsIdempotentWithAllowNonEmpty(t *testing.T) {
	path := newLegacySQLite(t)
	opts := testOptions(t, path)
	if err := run(opts); err != nil {
		t.Fatalf("首次迁移失败: %v", err)
	}
	if err := db.DB.Model(&db.SIMSubscription{}).
		Where("imsi = ?", "460010000000001").
		Updates(map[string]any{
			"phone_number":        "+8613200000001",
			"modem_phone_number":  "",
			"vowifi_phone_number": "+8613100000001",
		}).Error; err != nil {
		t.Fatal(err)
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
	var subscriptions int64
	if err := db.DB.Model(&db.SIMSubscription{}).Count(&subscriptions).Error; err != nil {
		t.Fatal(err)
	}
	if subscriptions != 1 {
		t.Fatalf("sim_subscriptions=%d want 1（派生迁移重跑不应重复）", subscriptions)
	}
	var subscription db.SIMSubscription
	if err := db.DB.Where("imsi = ?", "460010000000001").Take(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	if subscription.PhoneNumber != "+8613200000001" ||
		subscription.ModemPhoneNumber != "+8613700000001" ||
		subscription.VowifiPhoneNumber != "+8613100000001" {
		t.Fatalf("target authority/backfill mismatch after rerun: %+v", subscription)
	}
	inserted, err := insertBatch(db.DB, "devices", []string{`"imei"`}, [][]any{{"350000000000001"}})
	if err != nil {
		t.Fatal(err)
	}
	if inserted != 0 {
		t.Fatalf("RowsAffected=%d want 0（主键冲突必须按实际跳过数计数）", inserted)
	}
}

func TestMigrationRollsBackEarlierTablesWhenLaterTableFails(t *testing.T) {
	path := newLegacySQLite(t)
	opts := testOptions(t, path)
	opts.only = "devices,sms_send_attempts,traffic_minute"
	if err := run(opts); err != nil {
		t.Fatalf("准备目标表: %v", err)
	}

	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`UPDATE traffic_minute SET direction = 'not-a-bool' WHERE id = 101`); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	if err := db.DB.Exec(`TRUNCATE TABLE devices, traffic_minute RESTART IDENTITY`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Exec(`INSERT INTO devices (imei, alias) VALUES ('sentinel-device', 'keep-me')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Exec(`INSERT INTO traffic_minute
		(id, period_start, resource, tag, direction, traffic_bytes)
		VALUES (900, '2026-05-01T00:00:00Z', 'device', 'sentinel-device', true, 900)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Exec(`DROP TABLE sms_send_attempts`).Error; err != nil {
		t.Fatal(err)
	}

	err = run(opts)
	if err == nil || !strings.Contains(err.Error(), "traffic_minute") {
		t.Fatalf("run() err=%v want traffic_minute conversion failure", err)
	}
	var aliases []string
	if err := db.DB.Raw(`SELECT alias FROM devices ORDER BY imei`).Scan(&aliases).Error; err != nil {
		t.Fatal(err)
	}
	if len(aliases) != 1 || aliases[0] != "keep-me" {
		t.Fatalf("device aliases=%v want [keep-me]（晚表失败必须回滚早表 truncate/insert）", aliases)
	}
	var trafficIDs []int64
	if err := db.DB.Raw(`SELECT id FROM traffic_minute ORDER BY id`).Scan(&trafficIDs).Error; err != nil {
		t.Fatal(err)
	}
	if len(trafficIDs) != 1 || trafficIDs[0] != 900 {
		t.Fatalf("traffic ids=%v want [900]（失败事务必须恢复原数据）", trafficIDs)
	}
	exists, err := postgresTableExists(db.DB, "sms_send_attempts")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("sms_send_attempts 仍存在：失败事务必须回滚刚执行的 AutoMigrate")
	}
}

func TestAllowNonEmptyRejectsUniqueConflictThatHidesSourcePrimaryKey(t *testing.T) {
	opts := testOptions(t, newLegacySQLite(t))
	opts.only = "traffic_minute"
	if err := run(opts); err != nil {
		t.Fatalf("准备目标表: %v", err)
	}
	if err := db.DB.Exec(`UPDATE traffic_minute SET id = 999 WHERE id = 101`).Error; err != nil {
		t.Fatal(err)
	}

	opts.truncate = false
	opts.allowNonEmpty = true
	err := run(opts)
	if err == nil || !strings.Contains(err.Error(), "源主键落库校验失败") {
		t.Fatalf("run() err=%v want source-primary-key validation failure", err)
	}
	var ids []int64
	if err := db.DB.Raw(`SELECT id FROM traffic_minute ORDER BY id`).Scan(&ids).Error; err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != 999 {
		t.Fatalf("traffic ids=%v want [999]（冲突失败不得改变目标）", ids)
	}
}

func TestTruncateDoesNotCascadeIntoUnselectedTables(t *testing.T) {
	opts := testOptions(t, newLegacySQLite(t))
	opts.only = "devices"
	if err := run(opts); err != nil {
		t.Fatalf("准备目标表: %v", err)
	}

	const refTable = "dbmigrate_unselected_refs"
	if err := db.DB.Exec(`DROP TABLE IF EXISTS ` + refTable).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DB.Exec(`DROP TABLE IF EXISTS ` + refTable).Error })
	if err := db.DB.Exec(`CREATE TABLE ` + refTable + ` (
		device_imei text PRIMARY KEY REFERENCES devices(imei))`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Exec(`INSERT INTO ` + refTable + ` VALUES ('350000000000001')`).Error; err != nil {
		t.Fatal(err)
	}

	err := run(opts)
	if err == nil || !strings.Contains(err.Error(), "清空选中目标表") {
		t.Fatalf("run() err=%v want RESTRICT truncate failure", err)
	}
	var refs int64
	if err := db.DB.Raw(`SELECT COUNT(*) FROM ` + refTable).Scan(&refs).Error; err != nil {
		t.Fatal(err)
	}
	if refs != 1 {
		t.Fatalf("unselected refs=%d want 1（--truncate 不得 CASCADE 清除未选表）", refs)
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
	if err := db.DB.Exec(`TRUNCATE TABLE devices RESTART IDENTITY`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Exec(`DROP TABLE IF EXISTS sms_send_attempts`).Error; err != nil {
		t.Fatal(err)
	}

	opts.dryRun = true
	opts.truncate = false
	opts.only = "devices,sms_send_attempts"
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
	exists, err := postgresTableExists(db.DB, "sms_send_attempts")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("sms_send_attempts 存在：dry-run 的 AutoMigrate 必须回滚")
	}
}

func TestRedactDSNHidesPassword(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		secrets []string
	}{
		{
			name:    "keyword plain",
			in:      "host=db user=vohive password=s3cret dbname=vohive",
			want:    "host=db user=vohive password=*** dbname=vohive",
			secrets: []string{"s3cret"},
		},
		{
			name:    "keyword quoted space",
			in:      "host=db password='secret phrase' user=vohive",
			want:    "host=db password=*** user=vohive",
			secrets: []string{"secret", "phrase"},
		},
		{
			name:    "keyword quoted escapes",
			in:      `host=db password='secret \'quoted\' \\ path' dbname=vohive`,
			want:    "host=db password=*** dbname=vohive",
			secrets: []string{"secret", "quoted", "path"},
		},
		{
			name:    "keyword unquoted escaped space",
			in:      `host=db password=secret\ phrase dbname=vohive`,
			want:    "host=db password=*** dbname=vohive",
			secrets: []string{"secret", "phrase"},
		},
		{
			name:    "keyword spacing and case",
			in:      "host=db  PASSWORD = 'top secret'  dbname=vohive",
			want:    "host=db  PASSWORD = ***  dbname=vohive",
			secrets: []string{"top secret"},
		},
		{
			name:    "keyword ssl password",
			in:      `host=db sslpassword='key\ password' dbname=vohive`,
			want:    "host=db sslpassword=*** dbname=vohive",
			secrets: []string{"key", "password'"},
		},
		{
			name:    "keyword repeated passwords",
			in:      `password='first secret' host=db password=second\ secret`,
			want:    "password=*** host=db password=***",
			secrets: []string{"first", "second", "secret'"},
		},
		{
			name:    "URL userinfo",
			in:      "postgres://vohive:s3cret@db:5432/vohive",
			want:    "postgres://vohive:***@db:5432/vohive",
			secrets: []string{"s3cret"},
		},
		{
			name:    "URL encoded password",
			in:      "postgresql://vohive:secret%20phrase%5Ctail@db/vohive",
			want:    "postgresql://vohive:***@db/vohive",
			secrets: []string{"secret", "phrase", "tail"},
		},
		{
			name:    "URL query password",
			in:      "postgres://vohive@db/vohive?sslmode=disable&password=secret%20phrase",
			want:    "postgres://vohive@db/vohive?password=***&sslmode=disable",
			secrets: []string{"secret", "phrase"},
		},
		{
			name: "without password",
			in:   "host=db user=vohive dbname=vohive",
			want: "host=db user=vohive dbname=vohive",
		},
		{
			name:    "invalid fails closed",
			in:      `host=db password='never closed`,
			want:    "<invalid PostgreSQL DSN; redacted>",
			secrets: []string{"never closed"},
		},
		{
			name:    "invalid key cannot hide password token",
			in:      `garbage password='still secret'`,
			want:    "<invalid PostgreSQL DSN; redacted>",
			secrets: []string{"still secret"},
		},
		{
			name:    "URL fragment fails closed",
			in:      `postgres://vohive@db/vohive#password=fragment-secret`,
			want:    "<invalid PostgreSQL DSN; redacted>",
			secrets: []string{"fragment-secret"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactDSN(tc.in)
			if got != tc.want {
				t.Fatalf("redactDSN(%q)=%q want %q", tc.in, got, tc.want)
			}
			for _, secret := range tc.secrets {
				if strings.Contains(got, secret) {
					t.Fatalf("redactDSN(%q) leaked %q in %q", tc.in, secret, got)
				}
			}
		})
	}
}

func TestSelectTablesKeepsCanonicalOrder(t *testing.T) {
	catalog := []migrationTarget{{name: "devices"}, {name: "sim_cards"}, {name: "sms"}}
	got, err := selectTables("sms,devices,devices", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].name != "devices" || got[1].name != "sms" {
		t.Fatalf("selectTables=%v want [devices sms]", got)
	}
	if _, err := selectTables("not_a_table", catalog); err == nil {
		t.Fatal("selectTables() 对未知表名应报错")
	}
}

func TestMigrationCatalogMatchesAutoMigrateModels(t *testing.T) {
	dsn := db.TestDSN()
	if strings.TrimSpace(dsn) == "" {
		t.Skip("set TEST_DATABASE_URL or VODOGE_DB_DSN to a PostgreSQL DSN for database tests")
	}
	if err := db.Open(db.Options{DSN: dsn, AutoMigrate: false}); err != nil {
		t.Fatal(err)
	}
	catalog, err := migrationCatalog(db.DB)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != len(db.AutoMigrateModels()) {
		t.Fatalf("catalog=%d models=%d", len(catalog), len(db.AutoMigrateModels()))
	}
	got := make([]string, 0, len(catalog))
	for _, table := range catalog {
		got = append(got, table.name)
	}
	want := []string{
		"devices", "card_policies", "sim_cards", "sim_subscriptions", "pending_phone_numbers",
		"proxy_instances", "upstream_proxies", "upstream_proxy_country_rules",
		"upstream_proxy_profile_bindings", "sms", "sms_contacts", "sms_delivery",
		"sms_delivery_part", "sms_send_attempts", "traffic_minute", "traffic_hour",
		"traffic_day", "traffic_week", "traffic_month",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("migration catalog=%v\nwant=%v", got, want)
	}
}
