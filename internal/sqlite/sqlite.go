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
func migrate(ctx context.Context, db *sql.DB) error {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)

	var applied int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&applied); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if applied > len(names) {
		return errors.New("database schema is newer than this build")
	}
	for i := applied; i < len(names); i++ {
		script, err := migrations.ReadFile(names[i])
		if err != nil {
			return err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(script)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply %s: %w", names[i], err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record %s: %w", names[i], err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", names[i], err)
		}
	}
	return nil
}
