package editor_test

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// placingBatches runs placing in batches over store.
func placingBatches(store *sqlite.Store, b editor.Batcher) *editor.Batches {
	log := slog.New(slog.DiscardHandler)
	ed := editor.New(editor.Deps{Store: store, Log: log})
	return editor.NewBatches(editor.BatchDeps{Store: store, Batcher: b, Acts: []editor.BatchAct{ed.Placing()}, Log: log})
}

// tyroneNotice is an Item whose Fact shares no Entity with the speech's.
var tyroneNotice = collect.Item{
	SourceID: number10.ID, EntryID: "tyrone", Title: "Lord-Lieutenant of County Tyrone",
	Link: "https://www.gov.uk/tyrone", Published: speech.Published.Add(time.Hour),
	Attribution: collect.OGL, CollectedAt: saturday,
}

func readTyroneNotice(t *testing.T, store *sqlite.Store) {
	t.Helper()
	if _, err := store.AddItems(t.Context(), []collect.Item{tyroneNotice}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveReading(t.Context(), editor.Reading{
		SourceID: tyroneNotice.SourceID, EntryID: tyroneNotice.EntryID, Model: "fake",
		Entities: []editor.Entity{{Name: "County Tyrone", Kind: "place"}},
		Facts: []editor.Fact{{Statement: "A Lord-Lieutenant of County Tyrone was appointed.", AsAt: oct16,
			Quote: "Lord-Lieutenant"}},
	}, saturday); err != nil {
		t.Fatal(err)
	}
}

func TestPlacingRunsInBatchesAndAFactWaitsForTheStoriesAnEarlierItemMayStart(t *testing.T) {
	store := record(t)
	ed, _ := placer(store, &fakeModel{})
	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	readTyroneNotice(t, store)
	readLater(t, store)
	batcher := &fakeBatcher{}
	b := placingBatches(store, batcher)

	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	// The readout mentions Andy Burnham, as the speech does, so it waits
	// until the speech is placed; the Tyrone notice mentions nothing the
	// speech does, so it goes too.
	if len(batcher.submitted) != 1 || len(batcher.submitted[0]) != 2 {
		t.Fatalf("submitted %d batches, first %v; want one of the speech and the Tyrone notice", len(batcher.submitted), batcher.submitted)
	}
	for _, r := range batcher.submitted[0] {
		if r.Request.Act != "placing" || strings.Contains(r.Request.Input, laterItem.Title) {
			t.Errorf("request %s is not placing the speech or the notice:\n%s", r.ID, r.Request.Input)
		}
	}

	batcher.end(1, answering(`{"new_stories": [{"ref": "N1", "title": "A new Story"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}]}`))
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	stories, err := store.ListStories(t.Context())
	if err != nil || len(stories) != 2 {
		t.Fatalf("Stories = %+v %v, want one started by each of the first batch's Items", stories, err)
	}
	if len(batcher.submitted) != 2 || len(batcher.submitted[1]) != 1 {
		t.Fatalf("submitted %v, want a second batch placing the readout", batcher.submitted)
	}
	in := batcher.submitted[1][0].Request.Input
	if !strings.Contains(in, laterItem.Title) || !strings.Contains(in, "Candidate Stories, for every Fact:\n\nS") {
		t.Errorf("the readout is not weighed against the Story the speech started:\n%s", in)
	}
}
