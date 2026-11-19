package editor_test

import (
	"errors"
	"log/slog"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// weavingEditor is the Editor weavingBatches runs weaving with.
func weavingEditor(store *sqlite.Store) *editor.Editor {
	return editor.New(editor.Deps{Store: store, Weavings: store, ModelFor: modelFor, Log: slog.New(slog.DiscardHandler)})
}

// beforeWeavingAt is saturday before weaving's time of day.
var beforeWeavingAt = saturday.Add(-3 * time.Hour)

// The Owner asks for today's run before its time of day: it is submitted at
// once, keyed by today, asking what the timetabled run would ask, and its
// result is applied by the batch runner as a timetabled run's is.
func TestTodaysWeavingRunAskedForBeforeItsTimeIsSubmittedAndApplied(t *testing.T) {
	ctx := t.Context()
	store := record(t)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)

	run, err := weavingEditor(store).WeaveToday(ctx, beforeWeavingAt)
	if err != nil {
		t.Fatal(err)
	}
	if run.Key != "2026-10-24" || len(run.Stories) != 1 || len(run.Made) != 0 {
		t.Errorf("run keyed %q shown %d Stories cut and %d made, want today's shown the speech", run.Key,
			len(run.Stories), len(run.Made))
	}
	if _, err := b.Submit(ctx, run.Job, beforeWeavingAt); err != nil {
		t.Fatal(err)
	}
	if len(batcher.submitted) != 1 || len(batcher.submitted[0]) != 1 {
		t.Fatalf("submitted %v, want one weaving request", batcher.submitted)
	}
	timetabled := &fakeBatcher{}
	runAt(t, weavingBatches(record(t), timetabled), saturday)
	if got, want := batcher.submitted[0][0], timetabled.submitted[0][0]; got.ID != want.ID ||
		editor.RequestHash(got.Request, "") != editor.RequestHash(want.Request, "") {
		t.Errorf("asked %s:\n%s\nwant what the timetabled run asks, %s:\n%s", got.ID, got.Request.Input, want.ID, want.Request.Input)
	}

	runAt(t, b, beforeWeavingAt.Add(time.Hour))
	if len(batcher.submitted) != 1 {
		t.Errorf("submitted %d batches while today's run is pending, want 1", len(batcher.submitted))
	}
	answerLast(batcher, oneOpinion)
	runAt(t, b, beforeWeavingAt.Add(2*time.Hour))

	if got := madeStories(t, store); len(got) != 1 {
		t.Errorf("made %d Stories, want the run's one", len(got))
	}
	calls, err := store.CallsOn(ctx, "weaving", "2026-10-24")
	if err != nil || len(calls) != 1 || calls[0].Outcome != editor.Applied {
		t.Errorf("calls %+v (%v), want the one applied call", calls, err)
	}
	runAt(t, b, saturday)
	if len(batcher.submitted) != 1 {
		t.Errorf("submitted %d batches after today's run, want no second run today", len(batcher.submitted))
	}
}

// A run in a batch is priced at batch rates, half the real-time price, from
// the size of its request.
func TestABatchRunIsPricedAtHalfTheRealTimeRate(t *testing.T) {
	req := editor.Request{Act: "weaving", Instructions: "Weave.", Input: strings.Repeat("x", 2990)}
	price := editor.Price{InputPerMTok: 4, OutputPerMTok: 20}

	est, err := editor.EstimateOf("weaving", "claude-opus-5-5", []editor.Request{req}, editor.PastUsage{},
		price.AtBatchRates(), editor.BatchRates)
	if err != nil {
		t.Fatal(err)
	}

	// 3000 bytes with the schema's "null": 1000 tokens in, at 3 bytes a token,
	// and 4000 out: $0.004 + $0.08 at real-time rates.
	if est.Requests != 1 || est.InputTokens != 1000 || est.OutputTokens != 4000 || est.Rates != "batch" ||
		math.Abs(est.USD-0.042) > 1e-9 {
		t.Errorf("estimate %+v, want 1000 in, 4000 out, $0.042 at batch rates", est)
	}
}

// A weaving run is estimated from the runs written before it, as hydration is
// from its hydrations.
func TestWeavingsPastUsageIsItsWrittenRunsCalls(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	answerLast(batcher, oneOpinion)
	runAt(t, b, saturday.Add(time.Hour))

	past, err := store.PastUsage(t.Context(), "weaving")
	if err != nil {
		t.Fatal(err)
	}

	if past.Calls != 1 || past.InputTokens != 1200 || past.OutputTokens != 340 || past.RequestBytes == 0 {
		t.Errorf("past usage %+v, want the one applied call's", past)
	}
}

// Today's run is refused where the timetable would submit none: a run is
// pending, today's has been written or failed, or nothing is cut to weave.
func TestTodaysWeavingRunIsRefusedWhereTheTimetableWouldSubmitNone(t *testing.T) {
	for name, tc := range map[string]struct {
		before func(t *testing.T, store *sqlite.Store, b *editor.Batches, batcher *fakeBatcher)
		want   error
	}{
		"a run pending": {func(t *testing.T, _ *sqlite.Store, b *editor.Batches, _ *fakeBatcher) {
			runAt(t, b, saturday.AddDate(0, 0, -1))
		}, editor.ErrWeavingPending},
		"today's run written": {func(t *testing.T, store *sqlite.Store, b *editor.Batches, batcher *fakeBatcher) {
			runAt(t, b, saturday)
			answerLast(batcher, `{"stories": []}`)
			runAt(t, b, saturday.Add(time.Hour))
			cutAt(t, store, saturday.Add(2*time.Hour))
		}, editor.ErrWovenToday},
		"today's run failed": {func(t *testing.T, store *sqlite.Store, _ *editor.Batches, _ *fakeBatcher) {
			if err := store.EndBatch(t.Context(), "batch-0", []editor.WorkFailure{{Act: "weaving", Key: "2026-10-24",
				At: saturday, Why: "failed", Failed: true}}, nil); err != nil {
				t.Fatal(err)
			}
		}, editor.ErrWovenToday},
		"nothing cut since the last run": {func(t *testing.T, _ *sqlite.Store, b *editor.Batches, batcher *fakeBatcher) {
			runAt(t, b, saturday.AddDate(0, 0, -1).Add(time.Hour))
			answerLast(batcher, `{"stories": []}`)
			runAt(t, b, saturday.AddDate(0, 0, -1).Add(2*time.Hour))
		}, editor.ErrNothingToWeave},
	} {
		t.Run(name, func(t *testing.T) {
			store := record(t)
			batcher := &fakeBatcher{}
			b := weavingBatches(store, batcher)
			tc.before(t, store, b, batcher)
			submitted := len(batcher.submitted)

			_, err := weavingEditor(store).WeaveToday(t.Context(), saturday)

			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
			if len(batcher.submitted) != submitted {
				t.Errorf("submitted %d batches, want none more", len(batcher.submitted)-submitted)
			}
		})
	}
}
