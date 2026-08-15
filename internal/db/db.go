// 数据库连接与全局句柄。
//
// **仅 PostgreSQL**：没有 DSN 就启动失败，不会回退到任何文件型数据库
// （见 docs/backend-db-decisions.md）。旧 SQLite 数据用 cmd/dbmigrate 一次性导入。
//
// 模型在 models.go，迁移在 migrate.go，各域的读写在 device/sim/sms/traffic/… 里。
package db

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// Options configures PostgreSQL connection (SQLite is not supported).
type Options struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	AutoMigrate     bool
}

// DefaultOptions returns production-ish defaults for the given DSN.
func DefaultOptions(dsn string) Options {
	return Options{
		DSN:             strings.TrimSpace(dsn),
		MaxOpenConns:    20,
		MaxIdleConns:    5,
		ConnMaxLifetime: 30 * time.Minute,
		AutoMigrate:     true,
	}
}

// ResolveDSN returns the first non-empty DSN from env then fallback.
// Order: VODOGE_DB_DSN, VODOG_DB_DSN, VOHIVE_DB_DSN, DATABASE_URL, fallback.
func ResolveDSN(fallback string) string {
	for _, key := range []string{"VODOGE_DB_DSN", "VODOG_DB_DSN", "VOHIVE_DB_DSN", "DATABASE_URL"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return strings.TrimSpace(fallback)
}

// Init opens PostgreSQL with default pool settings. dsn must be non-empty.
// For tests, pass TEST_DATABASE_URL / VOHIVE_DB_DSN via ResolveDSN.
func Init(dsn string) error {
	return Open(DefaultOptions(dsn))
}

// Open initializes the global DB handle with the given options.
func Open(opts Options) error {
	dsn := strings.TrimSpace(opts.DSN)
	if dsn == "" {
		return fmt.Errorf("database dsn is empty: set database.dsn or VODOGE_DB_DSN / VODOG_DB_DSN / VOHIVE_DB_DSN / DATABASE_URL")
	}
	if opts.MaxOpenConns <= 0 {
		opts.MaxOpenConns = 20
	}
	if opts.MaxIdleConns <= 0 {
		opts.MaxIdleConns = 5
	}
	if opts.ConnMaxLifetime <= 0 {
		opts.ConnMaxLifetime = 30 * time.Minute
	}

	gdb, err := gorm.Open(postgres.New(postgres.Config{
		DSN: dsn,
		// 启动流程会执行 AutoMigrate 与若干自定义迁移（ALTER TABLE 加列等）。
		// 若沿用隐式 prepared statement 缓存，DDL 之后此前缓存的执行计划
		// 结果类型不再匹配，PostgreSQL 会报
		// "cached plan must not change result type"(SQLSTATE 0A000)。
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}

	sqlDB, err := gdb.DB()
	if err != nil || sqlDB == nil {
		return fmt.Errorf("open db pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(opts.MaxOpenConns)
	sqlDB.SetMaxIdleConns(opts.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(opts.ConnMaxLifetime)

	DB = gdb

	if opts.AutoMigrate {
		if err := runMigrations(DB); err != nil {
			return err
		}
	}
	return nil
}
