// Package sqlite is the storage adapter: the Record kept in a SQLite file.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver (pure Go, no cgo)
)

//go:embed migrations/*.sql
var migrations embed.FS

// Store is the Record in a SQLite file.
type Store struct {
	db *sql.DB
}

// Open opens (creating if need be) the SQLite file at path and brings its
// schema up to date.
func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	dsn := "file:" + path +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// HasBriefing reports whether a Briefing has been written for the day that
// contains t (taken in t's location).
func (s *Store) HasBriefing(ctx context.Context, t time.Time) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM briefings WHERE day = ?`, t.Format(time.DateOnly),
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("query briefings: %w", err)
	}
	return n > 0, nil
}

// migrate applies, in name order, the embedded migrations the database has
// not yet seen. The count applied is kept in PRAGMA user_version.
func migrate(ctx context.Context, db *sql.DB) error { return migrateTo(ctx, db, -1) }

// migrateTo is migrate applying only the first n migrations, or all if n is
// negative.
func migrateTo(ctx context.Context, db *sql.DB, n int) error {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	if n >= 0 {
		names = names[:n]
	}

	var applied int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&applied); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if applied > len(names) {
		return errors.New("database schema is newer than this build")
	}
	if applied == len(names) {
		return nil
	}
	// Foreign keys are off while migrations run, on the one connection that
	// runs them, so a migration can rebuild a table others refer to (SQLite
	// can't change a constraint in place); each is checked before it
	// commits. The pragma can't change inside a transaction.
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	defer func() { _, _ = conn.ExecContext(context.WithoutCancel(ctx), `PRAGMA foreign_keys = ON`) }()
	for i := applied; i < len(names); i++ {
		if err := applyMigration(ctx, conn, names[i], i+1); err != nil {
			return err
		}
	}
	return nil
}

// applyMigration runs one migration, numbered n, all at once.
func applyMigration(ctx context.Context, conn *sql.Conn, name string, n int) error {
	script, err := migrations.ReadFile(name)
	if err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, string(script)); err != nil {
		return fmt.Errorf("apply %s: %w", name, err)
	}
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("check %s: %w", name, err)
	}
	broken := rows.Next()
	_ = rows.Close()
	if broken {
		return fmt.Errorf("check %s: it leaves a foreign key broken", name)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, n)); err != nil {
		return fmt.Errorf("record %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", name, err)
	}
	return nil
}
