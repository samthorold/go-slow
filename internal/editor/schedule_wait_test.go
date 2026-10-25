package editor_test

import (
	"errors"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// dailyBatches runs every daily act in batches over store, as main does.
func dailyBatches(store *sqlite.Store, b editor.Batcher) *editor.Batches {
	log := slog.New(slog.DiscardHandler)
	ed := editor.New(editor.Deps{Store: store, Wikidata: &fakeWikidata{}, Log: log})
	return editor.NewBatches(editor.BatchDeps{Store: store, Batcher: b,
		Acts: []editor.BatchAct{ed.Reading(), ed.Placing(), ed.Connecting()}, Log: log})
}

// lateStart is a start past the set time: the speech, collected before it,
// awaits reading.
var lateStart = seven.Add(30 * time.Minute)

func readWaiting(t *testing.T, b *editor.Batches, at time.Time) {
	t.Helper()
	if err := b.ReadWaiting(t.Context(), at); err != nil {
		t.Fatal(err)
	}
}

func TestPastTheSetTimeTheBriefingWaitsForTheWorkCollectedBeforeIt(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := dailyBatches(store, batcher)
	s, happened := schedule(store, &fakeModel{output: spokeInMotherwell})

	// The start's Collection run submits the speech's reading.
	readWaiting(t, b, lateStart)
	s.Tick(t.Context(), lateStart)
	// Each act's batch ends, and the next act's is submitted, on later runs.
	for i, output := range []string{goodReading, startsAStory, speechRoles} {
		if len(*happened) != 0 {
			t.Fatalf("written while the speech awaits act %d: %v", i+1, *happened)
		}
		at := lateStart.Add(time.Duration(i+1) * 15 * time.Minute)
		batcher.end(i+1, answering(output))
		readWaiting(t, b, at)
		s.Tick(t.Context(), at)
	}

	if want := []string{"brief"}; !slices.Equal(*happened, want) {
		t.Errorf("happened = %v, want %v once the speech is read, placed and connected", *happened, want)
	}
	if _, found, err := store.Briefing(t.Context(), "2026-10-24"); err != nil || !found {
		t.Errorf("no Briefing written once the work was done: %v %v", found, err)
	}
}

func TestABatchThatDoesNotEndHoldsTheBriefingOnlyUntilTheBound(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{} // the reading's batch never ends
	b := dailyBatches(store, batcher)
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
	store := record(t)
	batcher := &fakeBatcher{}
	b := dailyBatches(store, batcher)
	s, happened := sundaySchedule(t, store, &fakeAgent{turns: []editor.Turn{submits(citesTheSpeech)}})
	start := lateStart.AddDate(0, 0, 1)

	readWaiting(t, b, start)
	s.Tick(t.Context(), start)
	at := start
	for i, output := range []string{goodReading, startsAStory, speechRoles} {
		if len(*happened) != 0 {
			t.Fatalf("written while the speech awaits act %d: %v", i+1, *happened)
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
	store := record(t)
	batcher := &fakeBatcher{}
	b := dailyBatches(store, batcher)
	s, happened := schedule(store, &fakeModel{output: spokeInMotherwell})
	readWaiting(t, b, lateStart)
	s.Tick(t.Context(), lateStart)

	at := lateStart
	for n := 1; n <= editor.MaxAttempts; n++ {
		if len(*happened) != 0 {
			t.Fatalf("written after %d failed attempts, before the speech's reading is marked failed: %v", n-1, *happened)
		}
		batcher.end(n, func(editor.BatchRequest) editor.BatchResult {
			return editor.BatchResult{Err: errors.New("overloaded")}
		})
		at = at.Add(15 * time.Minute)
		readWaiting(t, b, at)
		s.Tick(t.Context(), at)
	}

	if want := []string{"brief"}; !slices.Equal(*happened, want) {
		t.Errorf("happened = %v, want %v once the reading failed its last attempt", *happened, want)
	}
}

func TestWorkCollectedAfterTheBriefingBeganWaitingDoesNotHoldIt(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := dailyBatches(store, batcher)
	s, happened := schedule(store, &fakeModel{output: spokeInMotherwell})
	readWaiting(t, b, lateStart)
	s.Tick(t.Context(), lateStart)
	at := lateStart
	for i, output := range []string{goodReading, startsAStory, speechRoles} {
		batcher.end(i+1, answering(output))
		at = at.Add(15 * time.Minute)
		readWaiting(t, b, at)
	}

	// The ferries came in on a later run, while the Briefing waited.
	later := ferries
	later.CollectedAt = lateStart.Add(15 * time.Minute)
	if _, err := store.AddItems(t.Context(), []collect.Item{later}); err != nil {
		t.Fatal(err)
	}
	if err := interest.New(store).CheckNew(t.Context()); err != nil {
		t.Fatal(err)
	}
	if waiting, err := store.ItemsToRead(t.Context()); err != nil || len(waiting) != 1 {
		t.Fatalf("awaiting reading: %v %v, want the ferries", waiting, err)
	}
	s.Tick(t.Context(), at)

	if want := []string{"brief"}; !slices.Equal(*happened, want) {
		t.Errorf("happened = %v, want %v once the work collected before the wait is done", *happened, want)
	}
}
