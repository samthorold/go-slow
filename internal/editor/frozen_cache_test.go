package editor_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// A frozen Record is built from an act's frozen copies alone, and several
// recorded tests build one from byte-identical copies (the Briefing's, the
// chain replay's, the Sunday read's and its replay over its own directory).
// Building one replays every fixture through reading and relating, which
// under the race detector is most of the package's time, so each is built
// once per test binary for the bytes of what it is built from, and each
// test is given a copy of its own. A test still reads every file of its own
// directory, to key the build: one missing there misses the build.

// frozenBuild is one frozen Record built, or the failure to build it.
type frozenBuild struct {
	once  sync.Once
	bytes []byte
	err   string
}

var (
	frozenMu     sync.Mutex
	frozenBuilds = map[string]*frozenBuild{}
)

// frozenOnce is a file in t's own temporary directory holding the closed
// Record build makes at the path it is given, built once for kind and the
// bytes of every file under dirs (missing directories hash as empty).
func frozenOnce(t *testing.T, kind string, dirs []string, build func(path string)) string {
	t.Helper()
	h := sha256.New()
	h.Write([]byte(kind))
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := os.ReadFile(path) //nolint:gosec // a test's own recordings
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(dir, path)
			h.Write([]byte(rel))
			h.Write(b)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	key := hex.EncodeToString(h.Sum(nil))
	frozenMu.Lock()
	b, ok := frozenBuilds[key]
	if !ok {
		b = &frozenBuild{}
		frozenBuilds[key] = b
	}
	frozenMu.Unlock()
	b.once.Do(func() {
		built := false
		defer func() {
			if !built {
				b.err = "building the frozen Record failed in another test"
			}
		}()
		path := filepath.Join(t.TempDir(), "frozen.db")
		build(path)
		bytes, err := os.ReadFile(path) //nolint:gosec // a test's own temporary file
		if err != nil {
			t.Fatal(err)
		}
		b.bytes, built = bytes, true
	})
	if b.err != "" {
		t.Fatal(b.err)
	}
	path := filepath.Join(t.TempDir(), "frozen.db")
	if err := os.WriteFile(path, b.bytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
