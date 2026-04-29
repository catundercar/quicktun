package dao

import (
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	gormzap "moul.io/zapgorm2"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open establishes a GORM connection to a SQLite database.
//
// dsn flags worth setting:
//
//	?_journal_mode=WAL          better read/write concurrency
//	?_busy_timeout=5000         retry on lock instead of failing
//	?_foreign_keys=on           enforce FK constraints (SQLite default is off)
//
// If lg is nil a no-op logger is used (suitable for tests that don't care
// about query output).
func Open(dsn string, lg *zap.Logger) (*gorm.DB, error) {
	gormLog := newGormLogger(lg)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormLog,
	})
	if err != nil {
		return nil, fmt.Errorf("dao: open %q: %w", dsn, err)
	}
	return db, nil
}

func newGormLogger(lg *zap.Logger) logger.Interface {
	if lg == nil {
		return logger.Default.LogMode(logger.Silent)
	}
	z := gormzap.New(lg)
	z.SlowThreshold = 200 * time.Millisecond
	z.IgnoreRecordNotFoundError = true
	return z.LogMode(logger.Warn)
}

// IsNotFound returns true if err is gorm's record-not-found error.
// Wrap callers' DAO errors with this so service code doesn't import gorm.
func IsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
