package db

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"
)

var (
	testDBMu   sync.Mutex
	testTables = []string{
		"traffic_months",
		"traffic_weeks",
		"traffic_days",
		"traffic_hours",
		"traffic_minutes",
		"sms_delivery_parts",
		"sms_deliveries",
		"sms_contacts",
		"sms",
		"upstream_proxy_country_rules",
		"upstream_proxies",
		"proxy_instances",
		"pending_phone_numbers",
		"sim_subscriptions",
		"sim_cards",
		"card_policies",
		"devices",
	}
)

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
		t.Skip("set TEST_DATABASE_URL or VOHIVE_DB_DSN to a PostgreSQL DSN for database tests")
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
		t.Skip("set TEST_DATABASE_URL or VOHIVE_DB_DSN to a PostgreSQL DSN for database tests")
	}
	testDBMu.Lock()
	defer testDBMu.Unlock()
	if err := Init(dsn); err != nil {
		t.Fatalf("db.Init(postgres) reopen error: %v", err)
	}
}

func truncateAll(gdb *gorm.DB) error {
	if gdb == nil {
		return fmt.Errorf("DB is nil")
	}
	quoted := make([]string, 0, len(testTables))
	for _, t := range testTables {
		if gdb.Migrator().HasTable(t) {
			quoted = append(quoted, t)
		}
	}
	if len(quoted) == 0 {
		return nil
	}
	sql := "TRUNCATE TABLE " + strings.Join(quoted, ", ") + " RESTART IDENTITY CASCADE"
	return gdb.Exec(sql).Error
}
