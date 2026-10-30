package editor_test

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
// the Depth the Owner still follows the Topic at. It is sampled five times,
// each over a fresh Record, and kept under testdata/recorded/precedent/<the
// reaction>/. Recording costs five calls, and more for attempts the checks
// refuse:
//
//	go test -count=1 ./internal/editor -run TestRecordedPlacingPrecedent -record
//
// with GOSLOW_ANTHROPIC_API_KEY and PLACING_MODEL set in the environment.
// The reaction's reading is its own frozen copy
// (testdata/recorded/precedent/inputs/reading), so re-recording reading
// leaves it valid; a refresh copies the current one over it, then
// re-records this.
func TestRecordedPlacingPrecedent(t *testing.T) {
	t.Parallel()
	recordedPlacingPrecedent(t, ownDirectory(t, "precedent"))
}

// recordedPlacingPrecedent replays (or records) placing with a precedent
// from the recordings under root.
func recordedPlacingPrecedent(t *testing.T, root string) {
	name := "uk-government-response-to-agreement-between-us-and-russia-9-october"
	for _, err := range sampled(t, filepath.Join(root, "precedent"), name, dailyRule, placingPrecedentCommand, func(m *sampleModel) sampleResult {
		return placingPrecedentSample(t, root, name, m)
	}) {
		t.Error(err)
	}
}

// placingPrecedentSample is one sample of placing the reaction name with the
// precedent, on m, over a fresh Record.
func placingPrecedentSample(t *testing.T, root, name string, m *sampleModel) sampleResult {
	t.Helper()
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
	}
	if !strings.Contains(normal(stripTags(body)), normal(reaction.Quote)) {
		t.Fatalf("the precedent's quote is not in the readout: %q", reaction.Quote)
	}
	if err := store.SaveFactReading(ctx, editor.FactReading{
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
	it, _ := fixture(t, filepath.Join("testdata", "govuk", name+".json"))
	addItem(it)
	reading, err := replayFactReading(ctx, replay{kept(t, filepath.Join(inputsOf(root, "precedent", "reading"), name+".json"))}, it)
	if err != nil {
		t.Fatalf("the frozen reading is not one of Facts: %v", err)
	}
	// The reaction's own Fact: the UK government's response to the
	// agreement. The reading may draw other Facts, such as the agreement
	// itself, which may start a Story of its own; the property below rests on
	// this one, so it must be there.
	i := slices.IndexFunc(reading.Facts, func(f editor.Fact) bool {
		return strings.Contains(strings.ToLower(f.Statement), "respon")
	})
	if i < 0 {
		t.Fatalf("the reading draws no Fact of the government's response, so placing is not tested on the reaction: %s", factStatements(reading))
	}
	response := reading.Facts[i].Statement
	if err := store.SaveFactReading(ctx, reading, saturday); err != nil {
		t.Fatal(err)
	}
	waiting, err := store.FactsToPlace(ctx)
	if err != nil || len(waiting) != 1 || waiting[0].Depth != interest.Following {
		t.Fatalf("awaiting placing: %+v %v; want the reaction, followed at following", waiting, err)
	}

	passed, err := runSample(m, func(model editor.Model) (bool, error) {
		var logs bytes.Buffer
		ed := editor.New(editor.Deps{Store: store, PlacingModel: model, Log: slog.New(slog.NewTextHandler(&logs, nil))})
		err := ed.PlaceWaiting(ctx, saturday)
		return !strings.Contains(logs.String(), "placing failed"), err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !passed {
		return sampleResult{}
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
	p := properties{}
	notMade := p.property("a reaction like one marked unwanted is not made a Development")
	s, _, err := store.Story(ctx, story)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range s.Timeline {
		if d.EntryID == it.EntryID {
			notMade("the reaction's Fact %q is a Development in %q", d.Statement, s.Title)
		}
	}
	// No Story placing starts holds the reaction's Fact.
	started := p.property("no Story placing starts holds the reaction's Fact")
	stories, err := store.ListStories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range stories {
		if l.ID == story {
			continue
		}
		s, _, err := store.Story(ctx, l.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range s.Timeline {
			if d.EntryID == it.EntryID && d.Statement == response {
				started("placing started %q for the reaction's Fact %q", s.Title, response)
			}
		}
	}
	return sampleResult{passed: true, held: p}
}
