package db

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"
)

var testDBMu sync.Mutex

// TestDSN returns PostgreSQL DSN for tests.
func TestDSN() string {
	if v := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL")); v != "" {
		return v
	}
	return ResolveDSN("")
}

// OpenTestDB initializes the global DB for tests (PostgreSQL required).
// Skips the test when no DSN is configured. Truncates known tables before use.
func OpenTestDB(t testing.TB) {
	t.Helper()
	dsn := TestDSN()
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL or VODOGE_DB_DSN to a PostgreSQL DSN for database tests")
	}
	testDBMu.Lock()
	defer testDBMu.Unlock()
	if err := Init(dsn); err != nil {
		t.Fatalf("db.Init(postgres) error: %v", err)
	}
	if err := truncateAll(DB); err != nil {
		t.Fatalf("truncate test tables: %v", err)
	}
	t.Cleanup(func() {
		testDBMu.Lock()
		defer testDBMu.Unlock()
		if DB != nil {
			_ = truncateAll(DB)
		}
	})
}

// ReopenTestDB re-runs Init on the same PostgreSQL DSN without truncating data.
func ReopenTestDB(t testing.TB) {
	t.Helper()
	dsn := TestDSN()
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL or VODOGE_DB_DSN to a PostgreSQL DSN for database tests")
	}
	testDBMu.Lock()
	defer testDBMu.Unlock()
	if err := Init(dsn); err != nil {
		t.Fatalf("db.Init(postgres) reopen error: %v", err)
	}
}

// truncateAll 清空当前 schema 下的全部表。
//
// 刻意从 pg_tables 动态取表名，而不是维护一份硬编码清单：
// 旧实现列的是 traffic_hours / sms_deliveries 等复数名，而 GORM 建出来的是
// traffic_hour / sms_delivery，7 张表因此被 HasTable 静默跳过、从不清理，
// 测试间数据累积，表现为唯一键冲突和汇总断言对不上。
// 硬编码清单必然随模型漂移，而漂移的失败方式是"静默不清理"，很难发现。
func truncateAll(gdb *gorm.DB) error {
	if gdb == nil {
		return fmt.Errorf("DB is nil")
	}

	var tables []string
	if err := gdb.Raw(
		`SELECT tablename FROM pg_tables WHERE schemaname = current_schema()`,
	).Scan(&tables).Error; err != nil {
		return fmt.Errorf("list tables: %w", err)
	}
	if len(tables) == 0 {
		return nil
	}

	quoted := make([]string, 0, len(tables))
	for _, t := range tables {
		quoted = append(quoted, `"`+t+`"`)
	}
	// CASCADE 使外键依赖顺序无关；RESTART IDENTITY 复位自增序列
	sql := "TRUNCATE TABLE " + strings.Join(quoted, ", ") + " RESTART IDENTITY CASCADE"
	return gdb.Exec(sql).Error
}
