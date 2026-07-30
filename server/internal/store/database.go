package store

import (
	"database/sql"
	"fmt"
	"io/fs"
	"time"

	"breckr-server/internal/config"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

// Open connects to the SQLite file and applies the pragmas the app relies on.
//
// modernc.org/sqlite is a pure-Go driver, so the binary builds with
// CGO_ENABLED=0 and ships as a single static file.
func Open(cfg *config.Config) (*sql.DB, error) {
	// WAL lets the dashboard read run history while a task is mid-write.
	// busy_timeout covers the moment a cron tick and an HTTP request want the
	// writer at once.
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)",
		cfg.Database.Path,
	)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	// SQLite takes one writer at a time. Capping the pool at one connection
	// turns what would be SQLITE_BUSY into ordinary queueing, which is the
	// right trade for a low-volume monitor -- and it matches the single-writer
	// behavior the synchronous better-sqlite3 server had by construction.
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("could not open the database at %s: %w", cfg.Database.Path, err)
	}

	return db, nil
}

func MigrateFS(db *sql.DB, migrationFS fs.FS, dir string) error {
	goose.SetBaseFS(migrationFS)
	defer func() {
		goose.SetBaseFS(nil)
	}()

	return Migrate(db, dir)
}

func Migrate(db *sql.DB, dir string) error {
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	goose.SetLogger(goose.NopLogger())

	if err := goose.Up(db, dir); err != nil {
		return fmt.Errorf("goose Up: %w", err)
	}

	return nil
}

// withTx runs fn inside a transaction, rolling back on error or panic.
//
// Writes that span tables need it -- a task saved without its channel links
// would silently notify nowhere. Keep the body short and never hold one across
// a network call: SetMaxOpenConns(1) means an open transaction blocks every
// other query in the process.
func withTx(db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	defer func() {
		// A no-op once Commit has run, and the safety net if fn panics.
		_ = tx.Rollback()
	}()

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit()
}

// now is the single source of timestamps. All of them are ISO-8601 UTC, written
// from one place so they agree.
func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// SQLite has no boolean type; rows come back as 0/1.
func fromBool(value bool) int {
	if value {
		return 1
	}
	return 0
}

// nullString unwraps a nullable TEXT column into a *string, so the JSON layer
// can emit a real null rather than an empty string.
func nullString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
