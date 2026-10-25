package editor_test

import (
	"bytes"
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

// The recorded runs of connecting. The fixtures' recorded readings and
// placings are replayed in the order the Items were published, and after
// each Item is placed its Facts are connected, so later Facts are weighed
// against earlier ones. Each connecting call is kept under
// testdata/recorded/connecting and replayed. Re-recording costs one call per
// fixture:
//
//	go test -count=1 ./internal/editor -run TestRecordedConnectings -record
//
// with GOSLOW_ANTHROPIC_API_KEY and CONNECTING_MODEL set in the environment.
// Re-recording a reading or a placing makes the connectings after it stale,
// so re-record connecting after either.
func TestRecordedConnectings(t *testing.T) {
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
		var logs bytes.Buffer
		log := slog.New(slog.NewTextHandler(&logs, nil))
		placing := replay{kept(t, filepath.Join("testdata", "recorded", "placing", f.name+".json"))}
		if err := editor.New(editor.Deps{Store: store, PlacingModel: placing, Log: log}).PlaceWaiting(ctx, saturday); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(logs.String(), "placing failed") {
			t.Fatalf("%s: the recorded placing fails the act's checks:\n%s", f.name, logs.String())
		}

		path := filepath.Join("testdata", "recorded", "connecting", f.name+".json")
		var model editor.Model
		var rec recording
		if *rerecordFlag {
			key, name := os.Getenv("GOSLOW_ANTHROPIC_API_KEY"), os.Getenv("CONNECTING_MODEL")
			if key == "" || name == "" {
				t.Fatal("recording needs GOSLOW_ANTHROPIC_API_KEY and CONNECTING_MODEL")
			}
			model = recorder{claude.New(claude.Options{APIKey: key, Model: name}), &rec}
		} else {
			if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
				// Each connecting depends on the ones before it, so the rest
				// can't be replayed either.
				t.Skipf("%s has not been recorded yet; record connecting with: "+
					"go test -count=1 ./internal/editor -run TestRecordedConnectings -record "+
					"(GOSLOW_ANTHROPIC_API_KEY and CONNECTING_MODEL set)", f.name)
			}
			rec = kept(t, path)
			model = replay{rec}
		}
		if err := editor.New(editor.Deps{Store: store, ConnectingModel: model, Log: log}).ConnectWaiting(ctx, saturday); err != nil {
			t.Fatal(err)
		}
		if *rerecordFlag && rec.Output != nil {
			keep(t, path, rec)
		}
		total.InputTokens += rec.Usage.InputTokens
		total.OutputTokens += rec.Usage.OutputTokens
		// A failed connecting leaves its Facts waiting, to be connected with
		// the next fixture's in a call no recording holds, so it stops the
		// run.
		if strings.Contains(logs.String(), "connecting failed") {
			t.Fatalf("%s: the recorded connecting fails the act's checks:\n%s", f.name, logs.String())
		}
	}
	t.Logf("recorded connecting usage over %d fixtures: %d input, %d output tokens", len(fixtures), total.InputTokens, total.OutputTokens)

	// Every cause Connection traces to Items through its Facts.
	causes, err := store.Causes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range causes {
		for _, d := range []editor.Development{c.Cause, c.Effect} {
			if d.ID == 0 || d.SourceID != number10.ID || d.EntryID == "" || d.ItemTitle == "" {
				t.Errorf("a cause that does not trace to an Item through its Fact: %+v", c)
			}
		}
		t.Logf("cause: %q (%s) led to %q (%s)", c.Cause.Statement, c.Cause.ItemTitle, c.Effect.Statement, c.Effect.ItemTitle)
	}
	// No Connection is between Stories: two Stories are connected only
	// through the Facts they share, which each connection points at.
	stories, err := store.ListStories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stories {
		connected, err := store.ConnectedStories(ctx, s.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range connected {
			if len(c.Shared)+len(c.Entities) == 0 {
				t.Errorf("Story %q is connected to %q with no shared Fact behind it", s.Title, c.Title)
			}
			for _, e := range c.Entities {
				if e.Here.ID == 0 || e.There.ID == 0 || e.Here.ID == e.There.ID {
					t.Errorf("Story %q is connected to %q by %s as %s without a Fact on each side: %+v", s.Title, c.Title, e.Entity, e.Role, e)
				}
			}
			t.Logf("Story %q is connected to %q: %d shared Developments, %d shared Entities", s.Title, c.Title, len(c.Shared), len(c.Entities))
		}
	}
}
