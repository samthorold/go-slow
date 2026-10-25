package editor_test

import (
	"bytes"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/claude"
	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// The recorded run of placing with a precedent (system design: Feedback
// fixes the Record and becomes precedent). The precedent is a fixture, not
// a model's output: a reaction drawn by hand from the readout of the Prime
// Minister's meeting with Chancellor Merz (testdata/precedent), made a
// Development in a Story about support for Ukraine, which the Owner said was
// unwanted while following a Topic on Ukraine. The government's response to
// the US-Russia agreement, a reaction tied to the same Topic and sharing the
// Entity Ukraine, is then placed by the real model, shown that precedent at
// the Depth the Owner still follows the Topic at. Re-recording costs one
// call:
//
//	go test -count=1 ./internal/editor -run TestRecordedPlacingPrecedent -record
//
// with GOSLOW_ANTHROPIC_API_KEY and PLACING_MODEL set in the environment.
// Re-record it after the reaction's reading.
func TestRecordedPlacingPrecedent(t *testing.T) {
	ctx := t.Context()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "go-slow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SyncSources(ctx, []collect.Source{number10}, saturday); err != nil {
		t.Fatal(err)
	}
	topics := interest.New(store)
	if _, errs, err := topics.Add(ctx, interest.Draft{Name: "Ukraine", Depth: "following", Terms: "Ukraine"}); err != nil || errs != nil {
		t.Fatalf("add Topic: %v %v", errs, err)
	}
	addItem := func(it editor.Item) {
		t.Helper()
		if _, err := store.AddItems(ctx, []collect.Item{{
			SourceID: it.SourceID, EntryID: it.EntryID, Title: it.Title, Link: it.Link, Summary: it.Summary,
			Content: it.Content, Published: it.Published, Attribution: collect.OGL, CollectedAt: saturday,
		}}); err != nil {
			t.Fatal(err)
		}
		if err := topics.CheckNew(ctx); err != nil {
			t.Fatal(err)
		}
	}

	// The precedent: the readout's reaffirmed support for Ukraine, a
	// Development the Owner did not want.
	merz, body := fixture(t, filepath.Join("testdata", "precedent", "pm-meeting-with-chancellor-merz-of-germany-8-october-2026.json"))
	addItem(merz)
	reaction := editor.Fact{
		Statement: "The Prime Minister and Chancellor Friedrich Merz reaffirmed their support for Ukraine.",
		AsAt:      time.Date(2026, time.October, 8, 0, 0, 0, 0, time.UTC),
		Quote:     "The leaders reaffirmed their unwavering support for Ukraine",
		Entities:  []string{"Friedrich Merz", "Ukraine"},
	}
	if !strings.Contains(normal(stripTags(body)), normal(reaction.Quote)) {
		t.Fatalf("the precedent's quote is not in the readout: %q", reaction.Quote)
	}
	if err := store.SaveReading(ctx, editor.Reading{
		SourceID: merz.SourceID, EntryID: merz.EntryID, Model: "fixture",
		Entities: []editor.Entity{{Name: "Friedrich Merz", Kind: "person"}, {Name: "Ukraine", Kind: "place"}},
		Facts:    []editor.Fact{reaction},
	}, saturday); err != nil {
		t.Fatal(err)
	}
	quiet := slog.New(slog.DiscardHandler)
	starts := &fakeModel{output: `{"new_stories": [{"ref": "N1", "title": "UK support for Ukraine"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}]}`}
	if err := editor.New(editor.Deps{Store: store, PlacingModel: starts, Log: quiet}).PlaceWaiting(ctx, saturday); err != nil {
		t.Fatal(err)
	}
	story, fact := storyTitled(t, store, "UK support for Ukraine")
	unwanted := give(t, store, "unwanted", story, fact)
	if unwanted.Depth != interest.Following {
		t.Fatalf("the precedent was given at %v, want following", unwanted.Depth)
	}

	// The reaction to place, read as recorded.
	name := "uk-government-response-to-agreement-between-us-and-russia-9-october"
	it, _ := fixture(t, filepath.Join("testdata", "govuk", name+".json"))
	addItem(it)
	reading, err := editor.Read(ctx, replay{kept(t, filepath.Join("testdata", "recorded", "reading", name+".json"))}, it)
	if err != nil {
		t.Fatalf("the recorded reading fails its checks: %v", err)
	}
	if err := store.SaveReading(ctx, reading, saturday); err != nil {
		t.Fatal(err)
	}
	waiting, err := store.FactsToPlace(ctx)
	if err != nil || len(waiting) != 1 || waiting[0].Depth != interest.Following {
		t.Fatalf("awaiting placing: %+v %v; want the reaction, followed at following", waiting, err)
	}

	path := filepath.Join("testdata", "recorded", "precedent", name+".json")
	var model editor.Model
	var rec recording
	if *rerecordFlag {
		key, m := os.Getenv("GOSLOW_ANTHROPIC_API_KEY"), os.Getenv("PLACING_MODEL")
		if key == "" || m == "" {
			t.Fatal("recording needs GOSLOW_ANTHROPIC_API_KEY and PLACING_MODEL")
		}
		model = recorder{claude.New(claude.Options{APIKey: key, Model: m}), &rec}
	} else {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			t.Skipf("%s has not been recorded yet; record it with: "+
				"go test -count=1 ./internal/editor -run TestRecordedPlacingPrecedent -record "+
				"(GOSLOW_ANTHROPIC_API_KEY and PLACING_MODEL set)", name)
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
	t.Logf("recorded placing usage with the precedent: %d input, %d output tokens", rec.Usage.InputTokens, rec.Usage.OutputTokens)
	if strings.Contains(logs.String(), "placing failed") {
		t.Fatalf("the recorded placing fails the act's checks:\n%s", logs.String())
	}

	shown, err := store.PrecedentsShown(ctx, "placing", it.SourceID, it.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	if len(shown) != 1 || shown[0] != unwanted.ID {
		t.Errorf("the placing was shown precedents %v, want the unwanted reaction %d", shown, unwanted.ID)
	}
	// A reaction like one marked unwanted at the Owner's Depth is not made a
	// Development.
	s, _, err := store.Story(ctx, story)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range s.Timeline {
		if d.EntryID == it.EntryID {
			t.Errorf("the reaction's Fact %q is a Development in %q", d.Statement, s.Title)
		}
	}
	stories, err := store.ListStories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stories) != 1 {
		t.Errorf("placing started a Story for the reaction: %+v", stories)
	}
}
