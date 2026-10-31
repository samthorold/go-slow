package editor_test

import (
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

// lateStart is a start past the set time.
var lateStart = seven.Add(30 * time.Minute)

func TestStartingLateWithBatchesPendingTheBriefingIsWrittenAtOnceOverSpansCutButUnread(t *testing.T) {
	store := record(t)
	// The release, read and its names looked up, awaits relating; the speech
	// awaits reading. Neither batch ever ends.
	readNaming(t, store, pressRelease, "Scotland")
	batcher := &fakeBatcher{}
	log := slog.New(slog.DiscardHandler)
	ed := editor.New(editor.Deps{Store: store, Wikidata: &fakeWikidata{}, Log: log})
	b := editor.NewBatches(editor.BatchDeps{Store: store, Batcher: batcher,
		Acts: []editor.BatchAct{ed.Reading(), ed.Relating()}, Log: log})
	// The release, published first, is S1 and S2; the speech S3 and S4.
	s, happened := schedule(store, &fakeModel{output: `{"statements": [{"spans": ["S3"], "stories": [],
		"text": "The Prime Minister spoke in Motherwell."}]}`})

	if err := b.ReadWaiting(t.Context(), lateStart); err != nil {
		t.Fatal(err)
	}
	if len(batcher.submitted) != 1 || len(batcher.submitted[0]) != 1 {
		t.Fatalf("submitted %v, want the speech's reading", batcher.submitted)
	}
	s.Tick(t.Context(), lateStart)

	if want := []string{"brief"}; !slices.Equal(*happened, want) {
		t.Errorf("happened = %v, want %v at once: the Briefing is written over what the Record holds at its time", *happened, want)
	}
	brief, found, err := store.Briefing(t.Context(), "2026-10-24")
	if err != nil || !found || len(brief.Statements) != 1 || brief.Statements[0].Cites[0].At != speechSpan(1) {
		t.Errorf("Briefing = %+v, %v, %v; want it telling the speech, cut and not yet read", brief, found, err)
	}
}

func TestOnSundayTheSundayReadFollowsTheBriefing(t *testing.T) {
	store := record(t)
	s, happened := sundaySchedule(t, store, &fakeAgent{turns: []editor.Turn{submits(citesTheTold)}})

	s.Tick(t.Context(), lateStart.AddDate(0, 0, 1))

	if want := []string{"brief", "sunday read"}; !slices.Equal(*happened, want) {
		t.Errorf("happened = %v, want %v", *happened, want)
	}
}
