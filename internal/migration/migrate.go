// Package migration runs SQL schema migrations against the control plane DB.
//
// Migration files live at /migrations/*.sql in the repo root. They are mirrored
// into internal/migration/files/ at build time so the binary can embed them
// (//go:embed cannot reach outside the package directory).
package migration

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Up applies every pending migration against the SQLite database at dsn.
// Idempotent: returns nil if no migrations are pending.
//
// dsn is the sqlite filename DSN from config (it may include pragma query
// params like ?_journal_mode=WAL&_foreign_keys=on; golang-migrate's sqlite3
// driver passes them through to the underlying driver). The "sqlite3://"
// scheme prefix is added internally.
func Up(dsn string) error {
	m, err := newMigrator(dsn)
	if err != nil {
		return err
	}
	defer closeMigrator(m)

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migration: up: %w", err)
	}
	return nil
}

// Down rolls back every applied migration, leaving an empty database.
// Intended for tests and disaster recovery; never called in normal operation.
func Down(dsn string) error {
	m, err := newMigrator(dsn)
	if err != nil {
		return err
	}
	defer closeMigrator(m)

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migration: down: %w", err)
	}
	return nil
}

func newMigrator(dsn string) (*migrate.Migrate, error) {
	src, err := iofs.New(migrationFS, "files")
	if err != nil {
		return nil, fmt.Errorf("migration: load source: %w", err)
	}
	// golang-migrate expects the sqlite3:// URL scheme.
	target := "sqlite3://" + dsn
	m, err := migrate.NewWithSourceInstance("iofs", src, target)
	if err != nil {
		return nil, fmt.Errorf("migration: connect %q: %w", target, err)
	}
	return m, nil
}

func closeMigrator(m *migrate.Migrate) {
	srcErr, dbErr := m.Close()
	_ = srcErr
	_ = dbErr
}
