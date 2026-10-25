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
	"time"

	"github.com/samthorold/go-slow/internal/claude"
	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// briefingTopics are the fixed Topics and Depths the recorded Briefing is
// written for. No Topic follows the Lord-Lieutenancy of County Tyrone, so
// its Story is not in the Briefing.
var briefingTopics = []interest.Draft{
	{Name: "Ukraine", Depth: "close", Terms: "Ukraine\nZelenskyy\nRussia"},
	{Name: "Railways", Depth: "following", Terms: "Great British Railways"},
	{Name: "Scotland", Depth: "passing", Terms: "Scotland\nScottish\nHolyrood"},
}

// The recorded run of writing the Briefing. The fixtures' recorded readings
// and placings are replayed into a Record with the Topics above, and the
// Briefing for the day is written from it in one call, kept under
// testdata/recorded/briefing and replayed. Re-recording costs one call:
//
//	go test -count=1 ./internal/editor -run TestRecordedBriefing -record
//
// with GOSLOW_ANTHROPIC_API_KEY and BRIEFING_MODEL set in the environment.
// It rests on the recorded placings, so re-record it after them.
func TestRecordedBriefing(t *testing.T) {
	ctx := t.Context()
	store := placedFixtures(t)
	at := saturday.Add(time.Hour)

	given, err := store.ToBrief(ctx, at.Add(-24*time.Hour), at)
	if err != nil {
		t.Fatal(err)
	}
	if len(given) == 0 {
		t.Fatal("the Topics select no Story")
	}
	for _, tb := range given {
		// The Topics select the Stories: none follows County Tyrone.
		if strings.Contains(tb.Story.Title, "Tyrone") {
			t.Errorf("a Story no Topic follows was given: %q", tb.Story.Title)
		}
		t.Logf("given %q at %s Depth (%s): %d Developments", tb.Story.Title, tb.Depth, strings.Join(tb.Topics, ", "), len(tb.Today))
	}
	path := filepath.Join("testdata", "recorded", "briefing", "saturday.json")
	var model editor.Model
	var rec recording
	if *rerecordFlag {
		key, name := os.Getenv("GOSLOW_ANTHROPIC_API_KEY"), os.Getenv("BRIEFING_MODEL")
		if key == "" || name == "" {
			t.Fatal("recording needs GOSLOW_ANTHROPIC_API_KEY and BRIEFING_MODEL")
		}
		model = recorder{claude.New(claude.Options{APIKey: key, Model: name}), &rec}
	} else {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			t.Skip("the Briefing has not been recorded yet; record it with: " +
				"go test -count=1 ./internal/editor -run TestRecordedBriefing -record " +
				"(GOSLOW_ANTHROPIC_API_KEY and BRIEFING_MODEL set)")
		}
		rec = kept(t, path)
		model = replay{rec}
	}
	var logs bytes.Buffer
	ed := editor.New(editor.Deps{Store: store, Briefings: store, BriefingModel: model, Log: slog.New(slog.NewTextHandler(&logs, nil))})
	if err := ed.Brief(ctx, at); err != nil {
		t.Fatal(err)
	}
	if *rerecordFlag && rec.Output != nil {
		keep(t, path, rec)
	}
	t.Logf("recorded briefing usage: %d input, %d output tokens", rec.Usage.InputTokens, rec.Usage.OutputTokens)
	if strings.Contains(logs.String(), "writing the Briefing failed") {
		t.Fatalf("the recorded Briefing fails the act's checks:\n%s", logs.String())
	}

	b, found, err := store.Briefing(ctx, at.Format(time.DateOnly))
	if err != nil || !found {
		t.Fatalf("no Briefing written: %v %v", found, err)
	}
	if len(b.Stories) == 0 {
		t.Fatal("the Briefing tells no Story")
	}
	cited := map[int64]bool{}
	for _, bs := range b.Stories {
		i := slices.IndexFunc(given, func(tb editor.ToBrief) bool { return tb.Story.ID == bs.Story.ID })
		if i < 0 {
			t.Errorf("the Briefing tells %q, which it was not given", bs.Story.Title)
			continue
		}
		for _, st := range bs.Statements {
			// Every statement cites Developments that exist in the input.
			if len(st.Developments) == 0 {
				t.Errorf("%q: an untraced statement: %q", bs.Story.Title, st.Text)
			}
			for _, d := range st.Developments {
				if !slices.ContainsFunc(given[i].Today, func(x editor.Development) bool { return x.ID == d.ID }) {
					t.Errorf("%q: %q cites a Development not given: %+v", bs.Story.Title, st.Text, d)
				}
				if d.EntryID == "" || d.ItemTitle == "" {
					t.Errorf("%q: a cited Development that does not trace to its Item: %+v", bs.Story.Title, d)
				}
				cited[d.ID] = true
			}
			t.Logf("%s: %s", bs.Story.Title, st.Text)
		}
	}
	for _, tb := range given {
		// At following Depth or closer, every Development is told.
		if tb.Depth >= interest.Following {
			for _, d := range tb.Today {
				if !cited[d.ID] {
					t.Errorf("%q, at %s Depth: %q is left out", tb.Story.Title, tb.Depth, d.Statement)
				}
			}
		}
	}

	// The next day nothing has moved on: the Briefing says so, with no call.
	if err := ed.Brief(ctx, at.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	next, found, err := store.Briefing(ctx, at.AddDate(0, 0, 1).Format(time.DateOnly))
	if err != nil || !found || len(next.Stories) != 0 {
		t.Errorf("the next day's Briefing = %+v, %v, %v; want one written empty", next, found, err)
	}
}

// placedFixtures is a Record holding the fixture Items, tied to the
// briefingTopics, with their recorded readings and placings replayed in the
// order the Items were published, at saturday. Each placing rests on those
// before it, so the first that fails stops the run.
func placedFixtures(t *testing.T) *sqlite.Store {
	t.Helper()
	return fixturesRecord(t, recordedDir, false)
}

// connectedFixtures is placedFixtures with each fixture's recorded
// connecting replayed after its placing, as the recorded connectings were
// made, from the recordings under dir.
func connectedFixtures(t *testing.T, dir string) *sqlite.Store {
	t.Helper()
	return fixturesRecord(t, dir, true)
}

// recordedDir holds the recordings of each act, one directory per act.
var recordedDir = filepath.Join("testdata", "recorded")

func fixturesRecord(t *testing.T, dir string, connect bool) *sqlite.Store {
	t.Helper()
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
	topics := interest.New(store)
	for _, d := range briefingTopics {
		if _, errs, err := topics.Add(ctx, d); err != nil || errs != nil {
			t.Fatalf("add Topic %s: %v %v", d.Name, errs, err)
		}
	}
	for _, f := range fixtures {
		it := f.item
		if _, err := store.AddItems(ctx, []collect.Item{{
			SourceID: it.SourceID, EntryID: it.EntryID, Title: it.Title, Link: it.Link, Summary: it.Summary,
			Content: it.Content, Published: it.Published, Attribution: collect.OGL, CollectedAt: saturday,
		}}); err != nil {
			t.Fatal(err)
		}
		if err := topics.CheckNew(ctx); err != nil {
			t.Fatal(err)
		}
		reading, err := editor.Read(ctx, replay{kept(t, filepath.Join(dir, "reading", f.name+".json"))}, it)
		if err != nil {
			t.Fatalf("%s: the recorded reading fails its checks: %v", f.name, err)
		}
		if err := store.SaveReading(ctx, reading, saturday); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "placing", f.name+".json")
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			t.Skipf("%s: placing has not been recorded, so the Briefing can't be written from it", f.name)
		}
		var logs bytes.Buffer
		ed := editor.New(editor.Deps{Store: store, PlacingModel: replay{kept(t, path)}, Log: slog.New(slog.NewTextHandler(&logs, nil))})
		if err := ed.PlaceWaiting(ctx, saturday); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(logs.String(), "placing failed") {
			t.Fatalf("%s: the recorded placing fails the act's checks:\n%s", f.name, logs.String())
		}
		if !connect {
			continue
		}
		path = filepath.Join(dir, "connecting", f.name+".json")
		ed = editor.New(editor.Deps{Store: store, ConnectingModel: replay{kept(t, path)}, Log: slog.New(slog.NewTextHandler(&logs, nil))})
		if err := ed.ConnectWaiting(ctx, saturday); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(logs.String(), "connecting failed") {
			t.Fatalf("%s: the recorded connecting fails the act's checks:\n%s", f.name, logs.String())
		}
	}
	return store
}
