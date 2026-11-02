// Package sqlitetest gives tests a Record of their own to open, with every
// migration applied, without each test applying them again: under the race
// detector applying them takes about a second, and a test binary opens
// hundreds of Records. It is for tests only.
package sqlitetest

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/samthorold/go-slow/internal/sqlite"
)

var (
	once     sync.Once
	migrated []byte
	failed   error
)

// Path is a file in t's temporary directory holding an empty Record with
// every migration applied, for sqlite.Open: a copy of one migrated once per
// test binary.
func Path(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "go-slow.db")
	At(t, path)
	return path
}

// At writes the empty Record Path copies at path, a file not yet there.
func At(t testing.TB, path string) {
	t.Helper()
	once.Do(func() { migrated, failed = migrate() })
	if failed != nil {
		t.Fatalf("migrate the Record tests copy: %v", failed)
	}
	if err := os.WriteFile(path, migrated, 0o600); err != nil {
		t.Fatal(err)
	}
}

// migrate is the bytes of an empty Record with every migration applied,
// closed, so its write-ahead log is in the file.
func migrate() ([]byte, error) {
	dir, err := os.MkdirTemp("", "sqlitetest")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "go-slow.db")
	s, err := sqlite.Open(context.Background(), path)
	if err != nil {
		return nil, err
	}
	if err := s.Close(); err != nil {
		return nil, err
	}
	if _, err := os.Stat(path + "-wal"); !errors.Is(err, fs.ErrNotExist) {
		return nil, errors.New("the write-ahead log is still there after closing")
	}
	return os.ReadFile(path) //nolint:gosec // our own temporary file
}
