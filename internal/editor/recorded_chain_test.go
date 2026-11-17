package editor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
	"github.com/samthorold/go-slow/internal/sqlite/sqlitetest"
)

// What a recorded act whose fixtures build on each other runs over: the
// fixture Items in the order they were published, each sample of a fixture
// over a copy of the Record the fixtures before it left.

// recordedDir holds the recordings of each act, one directory per act.
var recordedDir = filepath.Join("testdata", "recorded")

// namedItem is a fixture Item with the name its recordings are kept under.
type namedItem struct {
	name string
	item editor.Item
}

// fixtureItems are the fixture Items in the order they were published, the
// order the recorded acts run over them.
func fixtureItems(t *testing.T) []namedItem {
	t.Helper()
	paths, err := filepath.Glob("testdata/govuk/*.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	var out []namedItem
	for _, path := range paths {
		it, _ := fixture(t, path)
		out = append(out, namedItem{strings.TrimSuffix(filepath.Base(path), ".json"), it})
	}
	slices.SortFunc(out, func(a, b namedItem) int { return a.item.Published.Compare(b.item.Published) })
	return out
}

// newRecordAt is an empty Record with the fixtures' Source and the Topics
// given, kept at path, a file not yet there.
func newRecordAt(t *testing.T, path string, topics []interest.Draft) (*sqlite.Store, *interest.Service) {
	t.Helper()
	ctx := t.Context()
	sqlitetest.At(t, path)
	store, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SyncSources(ctx, []collect.Source{number10}, saturday); err != nil {
		t.Fatal(err)
	}
	svc := interest.New(store)
	for _, d := range topics {
		if _, errs, err := svc.Add(ctx, d); err != nil || errs != nil {
			t.Fatalf("add Topic %s: %v %v", d.Name, errs, err)
		}
	}
	return store, svc
}

// ownDirectory is the recordings root an act's own recorded test replays
// over: a copy holding the act's own directory alone, so a test that reads
// another act's recordings fails; recording, the recordings themselves.
func ownDirectory(t *testing.T, act string) string {
	t.Helper()
	if *rerecordFlag {
		return recordedDir
	}
	root := t.TempDir()
	if err := os.CopyFS(filepath.Join(root, act), os.DirFS(filepath.Join(recordedDir, act))); err != nil {
		t.Fatal(err)
	}
	return root
}

// copyOfRecord opens a copy of the closed Record at path, in a directory of
// its own.
func copyOfRecord(t *testing.T, path string) *sqlite.Store {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // a test's own temporary file
	if err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(t.TempDir(), "go-slow.db")
	if err := os.WriteFile(copied, b, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(t.Context(), copied)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// sampleNumber is the k of the sample kept at path.
func sampleNumber(path string) int {
	var k int
	_, _ = fmt.Sscanf(filepath.Base(path), "%d.json", &k)
	return k
}
