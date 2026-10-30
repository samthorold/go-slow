package editor_test

import (
	"errors"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// factBatches runs, in batches over store, the acts the Briefing is written
// from: placing and connecting the Facts in the Record.
func factBatches(store *sqlite.Store, b editor.Batcher) *editor.Batches {
	log := slog.New(slog.DiscardHandler)
	ed := editor.New(editor.Deps{Store: store, Wikidata: &fakeWikidata{}, Log: log})
	return editor.NewBatches(editor.BatchDeps{Store: store, Batcher: b,
		Acts: []editor.BatchAct{ed.Placing(), ed.Connecting()}, Log: log})
}

// lateStart is a start past the set time: the speech's Fact, drawn before
// it, awaits placing.
var lateStart = seven.Add(30 * time.Minute)

func readWaiting(t *testing.T, b *editor.Batches, at time.Time) {
	t.Helper()
	if err := b.ReadWaiting(t.Context(), at); err != nil {
		t.Fatal(err)
	}
}

// factsAwaiting is the Record with the speech's Fact drawn, awaiting
// placing, as the Briefing begins waiting.
func factsAwaiting(t *testing.T) *sqlite.Store {
	t.Helper()
	store := record(t)
	drawSpeechFacts(t, store, seven.Add(-time.Hour))
	return store
}

func TestPastTheSetTimeTheBriefingWaitsForTheWorkCollectedBeforeIt(t *testing.T) {
	store := factsAwaiting(t)
	batcher := &fakeBatcher{}
	b := factBatches(store, batcher)
	s, happened := schedule(store, &fakeModel{output: spokeInMotherwell})

	// The start's Collection run submits the Fact's placing.
	readWaiting(t, b, lateStart)
	s.Tick(t.Context(), lateStart)
	// Each act's batch ends, and the next act's is submitted, on later runs.
	for i, output := range []string{startsAStory, speechRoles} {
		if len(*happened) != 0 {
			t.Fatalf("written while the speech's Fact awaits act %d: %v", i+1, *happened)
		}
		at := lateStart.Add(time.Duration(i+1) * 15 * time.Minute)
		batcher.end(i+1, answering(output))
		readWaiting(t, b, at)
		s.Tick(t.Context(), at)
	}

	if want := []string{"brief"}; !slices.Equal(*happened, want) {
		t.Errorf("happened = %v, want %v once the speech's Fact is placed and connected", *happened, want)
	}
	if _, found, err := store.Briefing(t.Context(), "2026-10-24"); err != nil || !found {
		t.Errorf("no Briefing written once the work was done: %v %v", found, err)
	}
}

func TestTheBriefingDoesNotWaitForReading(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{} // the reading's batch never ends
	b, _ := newBatches(store, batcher)
	s, happened := schedule(store, &fakeModel{output: spokeInMotherwell})

	readWaiting(t, b, lateStart)
	if len(batcher.submitted) != 1 {
		t.Fatalf("submitted %v, want the speech's reading", batcher.submitted)
	}
	s.Tick(t.Context(), lateStart)

	if want := []string{"brief"}; !slices.Equal(*happened, want) {
		t.Errorf("happened = %v, want %v at once: the Briefing is written from Facts, which reading no longer draws", *happened, want)
	}
}

func TestABatchThatDoesNotEndHoldsTheBriefingOnlyUntilTheBound(t *testing.T) {
	store := factsAwaiting(t)
	batcher := &fakeBatcher{} // the placing's batch never ends
	b := factBatches(store, batcher)
	s, happened := schedule(store, &fakeModel{output: spokeInMotherwell})

	readWaiting(t, b, lateStart)
	s.Tick(t.Context(), lateStart)
	s.Tick(t.Context(), lateStart.Add(editor.BriefingWait-time.Minute))
	if len(*happened) != 0 {
		t.Fatalf("written before the bound: %v", *happened)
	}
	s.Tick(t.Context(), lateStart.Add(editor.BriefingWait))

	if want := []string{"brief"}; !slices.Equal(*happened, want) {
		t.Errorf("happened = %v, want %v once the bound passed", *happened, want)
	}
}

func TestOnSundayTheSundayReadWaitsWithTheBriefingAndFollowsIt(t *testing.T) {
	store := factsAwaiting(t)
	batcher := &fakeBatcher{}
	b := factBatches(store, batcher)
	s, happened := sundaySchedule(t, store, &fakeAgent{turns: []editor.Turn{submits(citesTheSpeech)}})
	start := lateStart.AddDate(0, 0, 1)

	readWaiting(t, b, start)
	s.Tick(t.Context(), start)
	at := start
	for i, output := range []string{startsAStory, speechRoles} {
		if len(*happened) != 0 {
			t.Fatalf("written while the speech's Fact awaits act %d: %v", i+1, *happened)
		}
		batcher.end(i+1, answering(output))
		at = at.Add(15 * time.Minute)
		readWaiting(t, b, at)
		s.Tick(t.Context(), at)
	}

	if want := []string{"brief", "sunday read"}; !slices.Equal(*happened, want) {
		t.Errorf("happened = %v, want %v", *happened, want)
	}
}

func TestWorkThatFailedEveryAttemptNoLongerHoldsTheBriefing(t *testing.T) {
	store := factsAwaiting(t)
	batcher := &fakeBatcher{}
	b := factBatches(store, batcher)
	s, happened := schedule(store, &fakeModel{output: spokeInMotherwell})
	readWaiting(t, b, lateStart)
	s.Tick(t.Context(), lateStart)

	at := lateStart
	for n := 1; n <= editor.MaxAttempts; n++ {
		if len(*happened) != 0 {
			t.Fatalf("written after %d failed attempts, before the speech's placing is marked failed: %v", n-1, *happened)
		}
		batcher.end(n, func(editor.BatchRequest) editor.BatchResult {
			return editor.BatchResult{Err: errors.New("overloaded")}
		})
		at = at.Add(15 * time.Minute)
		readWaiting(t, b, at)
		s.Tick(t.Context(), at)
	}

	if want := []string{"brief"}; !slices.Equal(*happened, want) {
		t.Errorf("happened = %v, want %v once the placing failed its last attempt", *happened, want)
	}
}

func TestWorkCollectedAfterTheBriefingBeganWaitingDoesNotHoldIt(t *testing.T) {
	store := factsAwaiting(t)
	batcher := &fakeBatcher{}
	b := factBatches(store, batcher)
	s, happened := schedule(store, &fakeModel{output: spokeInMotherwell})
	readWaiting(t, b, lateStart)
	s.Tick(t.Context(), lateStart)
	at := lateStart
	for i, output := range []string{startsAStory, speechRoles} {
		batcher.end(i+1, answering(output))
		at = at.Add(15 * time.Minute)
		readWaiting(t, b, at)
	}

	// The ferries came in on a later run, while the Briefing waited, and
	// their Fact awaits placing.
	later := ferries
	later.CollectedAt = lateStart.Add(15 * time.Minute)
	if _, err := store.AddItems(t.Context(), []collect.Item{later}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveFactReading(t.Context(), editor.FactReading{SourceID: later.SourceID, EntryID: later.EntryID,
		Model: "fake", Facts: []editor.Fact{{Statement: "Ferries will run more often.", AsAt: oct16,
			Quote: "will run more often"}}}, at); err != nil {
		t.Fatal(err)
	}
	if waiting, err := store.FactsToPlace(t.Context()); err != nil || len(waiting) != 1 {
		t.Fatalf("awaiting placing: %v %v, want the ferries' Fact", waiting, err)
	}
	s.Tick(t.Context(), at)

	if want := []string{"brief"}; !slices.Equal(*happened, want) {
		t.Errorf("happened = %v, want %v once the work collected before the wait is done", *happened, want)
	}
}
