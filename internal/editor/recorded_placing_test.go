package editor_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/claude"
	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// The recorded runs of placing. The fixtures' recorded readings are placed
// in the order the Items were published, each Item's Facts in one call, so
// later Facts meet the Stories earlier ones started. Each call is kept under
// testdata/recorded/placing and replayed; the Stories it builds are the
// same on every replay because each call is answered as it was when kept.
// Re-recording costs one call per fixture:
//
//	go test -count=1 ./internal/editor -run TestRecordedPlacings -record
//
// with GOSLOW_ANTHROPIC_API_KEY and PLACING_MODEL set in the environment.
// Re-recording a reading makes the placings after it stale, so re-record
// placing after any reading.
func TestRecordedPlacings(t *testing.T) {
	ctx := t.Context()
	paths, err := filepath.Glob("testdata/govuk/*.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	type fixtureItem struct {
		name string
		item editor.Item
	}
	var fixtures []fixtureItem
	for _, path := range paths {
		it, _ := fixture(t, path)
		fixtures = append(fixtures, fixtureItem{strings.TrimSuffix(filepath.Base(path), ".json"), it})
	}
	slices.SortFunc(fixtures, func(a, b fixtureItem) int { return a.item.Published.Compare(b.item.Published) })

	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "go-slow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SyncSources(ctx, []collect.Source{number10}, saturday); err != nil {
		t.Fatal(err)
	}

	var total editor.Usage
	for _, f := range fixtures {
		it := f.item
		if _, err := store.AddItems(ctx, []collect.Item{{
			SourceID: it.SourceID, EntryID: it.EntryID, Title: it.Title, Link: it.Link, Summary: it.Summary,
			Content: it.Content, Published: it.Published, Attribution: collect.OGL, CollectedAt: saturday,
		}}); err != nil {
			t.Fatal(err)
		}
		reading, err := editor.Read(ctx, replay{kept(t, filepath.Join("testdata", "recorded", "reading", f.name+".json"))}, it)
		if err != nil {
			t.Fatalf("%s: the recorded reading fails its checks: %v", f.name, err)
		}
		if err := store.SaveReading(ctx, reading, saturday); err != nil {
			t.Fatal(err)
		}

		path := filepath.Join("testdata", "recorded", "placing", f.name+".json")
		var model editor.Model
		var rec recording
		if *rerecordFlag {
			key, name := os.Getenv("GOSLOW_ANTHROPIC_API_KEY"), os.Getenv("PLACING_MODEL")
			if key == "" || name == "" {
				t.Fatal("recording needs GOSLOW_ANTHROPIC_API_KEY and PLACING_MODEL")
			}
			model = recorder{claude.New(claude.Options{APIKey: key, Model: name}), &rec}
		} else {
			if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
				// Each placing depends on the ones before it, so the rest
				// can't be replayed either.
				t.Skipf("%s has not been recorded yet; record placing with: "+
					"go test -count=1 ./internal/editor -run TestRecordedPlacings -record "+
					"(GOSLOW_ANTHROPIC_API_KEY and PLACING_MODEL set)", f.name)
			}
			rec = kept(t, path)
			model = replay{rec}
		}
		var logs bytes.Buffer
		ed := editor.New(editor.Deps{Store: store, PlacingModel: model, Log: slog.New(slog.NewTextHandler(&logs, nil))})
		if err := ed.PlaceWaiting(ctx, saturday); err != nil {
			t.Fatal(err)
		}
		if *rerecordFlag && rec.Output != nil {
			keep(t, path, rec)
		}
		total.InputTokens += rec.Usage.InputTokens
		total.OutputTokens += rec.Usage.OutputTokens
		// A failed placing leaves its Facts waiting, to be placed with the
		// next fixture's in a second call no recording holds, so it stops
		// the run.
		if strings.Contains(logs.String(), "placing failed") {
			t.Fatalf("%s: the recorded placing fails the act's checks:\n%s", f.name, logs.String())
		}
	}
	t.Logf("recorded placing usage over %d fixtures: %d input, %d output tokens", len(fixtures), total.InputTokens, total.OutputTokens)

	stories, err := store.ListStories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stories) == 0 {
		t.Fatal("placing started no Story")
	}
	developments := map[string][]string{} // Fact statement -> the Stories it is a Development in
	threaded := false
	for _, l := range stories {
		s, _, err := store.Story(ctx, l.ID)
		if err != nil {
			t.Fatal(err)
		}
		items := map[string]bool{}
		for _, d := range s.Timeline {
			// Every Development traces to its Fact and Item.
			if d.ID == 0 || d.SourceID != number10.ID || d.EntryID == "" || d.ItemTitle == "" {
				t.Errorf("Story %q: a Development that does not trace: %+v", s.Title, d)
			}
			items[d.EntryID] = true
			developments[d.Statement] = append(developments[d.Statement], s.Title)
		}
		if len(items) > 1 {
			threaded = true
		}
		t.Logf("Story %q: %d Developments from %d Items", s.Title, len(s.Timeline), len(items))
	}
	// Later Facts meet the Stories earlier ones started.
	if !threaded {
		t.Error("no Story has Developments from more than one Item")
	}
	// A reaction is not a Development: the East Midlands mayor welcoming
	// the Derby headquarters moves no Story on.
	isReaction := func(statement string) bool { return strings.Contains(statement, "Claire Ward") }
	if !slices.ContainsFunc(readFacts(t, store), isReaction) {
		t.Fatal("the readings hold no reaction to test placing on: Claire Ward's welcome is missing")
	}
	for statement, in := range developments {
		if isReaction(statement) {
			t.Errorf("the reaction %q is a Development in %v", statement, in)
		}
	}
}

// readFacts are the statements of every Fact in the Record.
func readFacts(t *testing.T, store *sqlite.Store) []string {
	t.Helper()
	items, err := store.ListItems(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, li := range items {
		it, _, err := store.ReadItem(t.Context(), li.SourceID, li.EntryID)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range it.Facts {
			out = append(out, f.Statement)
		}
	}
	return out
}

// kept is a kept response.
func kept(t *testing.T, path string) recording {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec recording
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return rec
}

// keep writes a response to be replayed.
func keep(t *testing.T, path string, rec recording) {
	t.Helper()
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
