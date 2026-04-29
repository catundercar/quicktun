package dao

import (
	"fmt"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open establishes a GORM connection to a SQLite database.
//
// Recommended DSN flags:
//
//	file:/path/to/quicktun.db?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on
//
// WAL gives better read/write concurrency. busy_timeout avoids "database is locked"
// errors under contention. foreign_keys enforces FK constraints (off by default in
// SQLite).
func Open(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("dao: open %q: %w", dsn, err)
	}
	return db, nil
}
