package editor_test

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// weavingAt is the time of day weaving runs from in these tests; saturday
// is an hour after it.
const weavingAt = 6 * time.Hour

// weavingBatches is the batch runner over weaving alone, keeping every call.
func weavingBatches(store *sqlite.Store, b editor.Batcher) *editor.Batches {
	log := slog.New(slog.DiscardHandler)
	ed := editor.New(editor.Deps{Store: store, Weavings: store, ModelFor: modelFor, Log: log})
	return editor.NewBatches(editor.BatchDeps{
		Store: store, Batcher: b, Acts: []editor.BatchAct{ed.Weaving(weavingAt)}, ModelFor: modelFor, Log: log,
	})
}

// runAt is the batch runner's turn after a Collection run ending at now.
func runAt(t *testing.T, b *editor.Batches, now time.Time) {
	t.Helper()
	if err := b.ReadWaiting(t.Context(), now); err != nil {
		t.Fatal(err)
	}
}

// oneOpinion is an answer to a weaving run shown the speech, S1 "It’s good
// to be back in Scotland, to be here in Motherwell." and S2 "Scotland will
// get £2 billion.": one Story of two Editor's Spans.
const oneOpinion = `{"stories": [{"editor_spans": [
  {"body": "Money follows the Prime Minister to Scotland.", "cites": [1, 2]},
  {"body": "Motherwell is where it lands first.", "cites": [1]}
]}]}`

// madeStories are the Stories the Editor made, newest first.
func madeStories(t *testing.T, store *sqlite.Store) []editor.Story {
	t.Helper()
	got, err := store.StoriesMade(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestAWeavingRunMakesAStoryOfEachOpinionWithItsJudgeAndItsCall(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	if len(batcher.submitted) != 1 || len(batcher.submitted[0]) != 1 {
		t.Fatalf("submitted %v, want one weaving request", batcher.submitted)
	}
	sent := batcher.submitted[0][0].Request
	if sent.Act != "weaving" || sent.Effort != editor.EffortMedium ||
		!strings.Contains(sent.Input, "S1: It’s good to be back in Scotland, to be here in Motherwell.\nS2: Scotland will get £2 billion.") {
		t.Errorf("sent %s at %q effort:\n%s", sent.Act, sent.Effort, sent.Input)
	}

	batcher.end(1, func(editor.BatchRequest) editor.BatchResult {
		return editor.BatchResult{Response: answered(oneOpinion)}
	})
	runAt(t, b, saturday.Add(time.Hour))

	stories := madeStories(t, store)
	if len(stories) != 1 {
		t.Fatalf("made %d Stories, want 1", len(stories))
	}
	s := stories[0]
	if len(s.Spans) != 2 || s.Spans[0].Body != "Money follows the Prime Minister to Scotland." ||
		len(s.Spans[0].Cites) != 2 || s.Spans[0].Cites[1].Words != "Scotland will get £2 billion." ||
		s.Spans[1].Cites[0].Words != "It’s good to be back in Scotland, to be here in Motherwell." {
		t.Errorf("Story holds %+v", s.Spans)
	}
	if c := s.Spans[0].Cites[0].Address; c.SourceID != speech.SourceID || c.EntryID != speech.EntryID {
		t.Errorf("cites %+v, want the speech", c)
	}
	if s.Judge.Act != "weaving" || s.Judge.Version != editor.WeavingVersion || s.Judge.Model != "fake-reported" ||
		s.Judge.RequestHash != editor.RequestHash(sent, "fake-weaving") || !s.At.Equal(saturday.Add(time.Hour)) {
		t.Errorf("made by %+v at %v", s.Judge, s.At)
	}
	calls, err := store.CallsOn(t.Context(), "weaving", "2026-10-24")
	if err != nil || len(calls) != 1 || calls[0].Outcome != editor.Applied || s.Call != calls[0].ID {
		t.Errorf("calls %+v (%v), Story's call %d, want the one applied call", calls, err, s.Call)
	}
}

func TestAWeavingAnswerFailingItsChecksMakesNothingAndIsAskedAgain(t *testing.T) {
	for name, tc := range map[string]struct{ answer, why string }{
		"a citation of a Span not shown": {
			`{"stories": [{"editor_spans": [{"body": "Words.", "cites": [3]}]}]}`, "cites S3, which was not shown"},
		"an Editor's Span with no words": {
			`{"stories": [{"editor_spans": [{"body": " ", "cites": [1]}]}]}`, "has no words"},
		"an Editor's Span with no citation": {
			`{"stories": [{"editor_spans": [{"body": "Words.", "cites": []}]}]}`, "cites no Span"},
		"a Story with no Editor's Spans": {
			`{"stories": [{"editor_spans": []}]}`, "has no editor_spans"},
	} {
		t.Run(name, func(t *testing.T) {
			store := record(t)
			batcher := &fakeBatcher{}
			b := weavingBatches(store, batcher)
			runAt(t, b, saturday)
			batcher.end(1, func(editor.BatchRequest) editor.BatchResult {
				return editor.BatchResult{Response: answered(tc.answer)}
			})
			runAt(t, b, saturday.Add(time.Hour))

			if got := madeStories(t, store); len(got) != 0 {
				t.Errorf("made %+v from an answer failing the checks", got)
			}
			calls, err := store.CallsOn(t.Context(), "weaving", "2026-10-24")
			if err != nil || len(calls) != 1 || calls[0].Outcome != editor.Refused || !strings.Contains(calls[0].Why, tc.why) {
				t.Errorf("calls %+v (%v), want one refused saying %q", calls, err, tc.why)
			}
			if len(batcher.submitted) != 2 || batcher.submitted[1][0].Request.Act != "weaving" {
				t.Errorf("submitted %d batches, want the run asked again in a second", len(batcher.submitted))
			}
		})
	}
}

// cutAt adds the ferries Item to the Record, collected and cut at at.
func cutAt(t *testing.T, store *sqlite.Store, at time.Time) {
	t.Helper()
	it := ferries
	it.CollectedAt = at
	if _, err := store.AddItems(t.Context(), []collect.Item{it}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(store).CutNew(t.Context(), at); err != nil {
		t.Fatal(err)
	}
}

// answerLast answers the last batch submitted with output.
func answerLast(batcher *fakeBatcher, output string) {
	batcher.end(len(batcher.submitted), func(editor.BatchRequest) editor.BatchResult {
		return editor.BatchResult{Response: answered(output)}
	})
}

func TestARunWithNothingToSaySucceedsAndTheNextIsShownOnlyWhatWasCutSince(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	answerLast(batcher, `{"stories": []}`)
	runAt(t, b, saturday.Add(time.Hour))

	if got := madeStories(t, store); len(got) != 0 {
		t.Errorf("made %+v from an answer with no stories", got)
	}
	calls, err := store.CallsOn(t.Context(), "weaving", "2026-10-24")
	if err != nil || len(calls) != 1 || calls[0].Outcome != editor.Applied {
		t.Errorf("calls %+v (%v), want one applied", calls, err)
	}
	// The next day, nothing new has been cut: no run.
	sunday := saturday.Add(24 * time.Hour)
	runAt(t, b, sunday)
	if len(batcher.submitted) != 1 {
		t.Fatalf("submitted %d batches with nothing new cut, want none after the first", len(batcher.submitted))
	}
	// Once something is cut, the next day's run is shown it alone.
	cutAt(t, store, sunday)
	runAt(t, b, sunday.Add(24*time.Hour))
	if len(batcher.submitted) != 2 {
		t.Fatalf("submitted %d batches, want a second run", len(batcher.submitted))
	}
	in := batcher.submitted[1][0].Request.Input
	if strings.Contains(in, "Motherwell") || !strings.Contains(in, "S1: Ferries to Scotland's islands will run more often.") {
		t.Errorf("the second run was shown:\n%s", in)
	}
}

func TestARunFailingAllItsAttemptsIsListedAndItsStoriesAreShownToTheNext(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	for i := range editor.MaxAttempts {
		answerLast(batcher, `{"stories": [{"editor_spans": []}]}`)
		runAt(t, b, saturday.Add(time.Duration(i+1)*time.Hour))
	}
	if len(batcher.submitted) != editor.MaxAttempts {
		t.Fatalf("submitted %d batches, want one per attempt", len(batcher.submitted))
	}

	failed, err := store.FailedWork(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 || failed[0].Act != "weaving" || failed[0].Attempts != editor.MaxAttempts ||
		!strings.Contains(failed[0].Why, "has no editor_spans") || failed[0].LastCall == 0 {
		t.Errorf("failed work %+v, want the weaving run", failed)
	}
	// No other run that day; the next day's is shown what the failed run was.
	runAt(t, b, saturday.Add(12*time.Hour))
	if len(batcher.submitted) != editor.MaxAttempts {
		t.Fatalf("submitted %d batches, want no other run the day one failed", len(batcher.submitted))
	}
	runAt(t, b, saturday.Add(24*time.Hour))
	if len(batcher.submitted) != editor.MaxAttempts+1 ||
		!strings.Contains(batcher.submitted[editor.MaxAttempts][0].Request.Input, "Motherwell") {
		t.Errorf("the next day's run was not shown the failed run's Stories")
	}
}

func TestWeavingRunsOnTheFirstRunAtOrAfterItsTimeOfDay(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	sixAM := time.Date(2026, time.October, 24, 6, 0, 0, 0, time.UTC)

	runAt(t, b, sixAM.Add(-time.Minute))
	if len(batcher.submitted) != 0 {
		t.Fatalf("submitted %d batches before the time of day", len(batcher.submitted))
	}
	runAt(t, b, sixAM)
	if len(batcher.submitted) != 1 {
		t.Fatalf("submitted %d batches at the time of day, want one", len(batcher.submitted))
	}
}

func TestWeavingRunsAtMostOnceADay(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	answerLast(batcher, oneOpinion)
	runAt(t, b, saturday.Add(time.Hour))

	cutAt(t, store, saturday.Add(2*time.Hour))
	runAt(t, b, saturday.Add(3*time.Hour))
	if len(batcher.submitted) != 1 {
		t.Errorf("submitted %d batches, want no second run the same day", len(batcher.submitted))
	}
	runAt(t, b, saturday.Add(24*time.Hour))
	if len(batcher.submitted) != 2 {
		t.Errorf("submitted %d batches, want the next day's run", len(batcher.submitted))
	}
}

func TestNoRunIsSubmittedWhileARunsBatchIsPending(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	sunday := saturday.Add(24 * time.Hour)
	cutAt(t, store, sunday)

	runAt(t, b, sunday)
	if len(batcher.submitted) != 1 {
		t.Fatalf("submitted %d batches, want none while Saturday's run is pending", len(batcher.submitted))
	}
	// Once Saturday's ends and is applied, Sunday's run is shown what was
	// cut since.
	answerLast(batcher, oneOpinion)
	runAt(t, b, sunday.Add(time.Hour))
	if len(madeStories(t, store)) != 1 {
		t.Errorf("Saturday's run was not applied")
	}
	if len(batcher.submitted) != 2 || strings.Contains(batcher.submitted[1][0].Request.Input, "Motherwell") {
		t.Errorf("submitted %d batches, want Sunday's run shown only what was cut since", len(batcher.submitted))
	}
}

func TestAResultArrivingAfterTheRecordHasMovedOnIsAppliedToWhatTheRunWasShown(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	// Cut after the request went: a run built now would be shown it as S3.
	cutAt(t, store, saturday.Add(30*time.Minute))
	answerLast(batcher, oneOpinion)
	runAt(t, b, saturday.Add(time.Hour))

	if got := madeStories(t, store); len(got) != 1 {
		t.Fatalf("made %d Stories, want the late result applied", len(got))
	}
	calls, err := store.CallsOn(t.Context(), "weaving", "2026-10-24")
	if err != nil || len(calls) != 1 || calls[0].Outcome != editor.Applied {
		t.Errorf("calls %+v (%v), want one applied", calls, err)
	}
	// What was cut after the request went is the next run's.
	runAt(t, b, saturday.Add(24*time.Hour))
	if len(batcher.submitted) != 2 || !strings.Contains(batcher.submitted[1][0].Request.Input, "S1: Ferries") {
		t.Errorf("the next run was not shown what was cut after the last was sent")
	}
}

func TestALateResultCitingASpanCutAfterItsRequestWentIsRefused(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	cutAt(t, store, saturday.Add(30*time.Minute))
	answerLast(batcher, `{"stories": [{"editor_spans": [{"body": "Ferries.", "cites": [3]}]}]}`)
	runAt(t, b, saturday.Add(time.Hour))

	calls, err := store.CallsOn(t.Context(), "weaving", "2026-10-24")
	if err != nil || len(calls) != 1 || calls[0].Outcome != editor.Refused || !strings.Contains(calls[0].Why, "S1 to S2") {
		t.Errorf("calls %+v (%v), want one refused for citing a Span not shown", calls, err)
	}
}
