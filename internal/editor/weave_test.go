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
	if run.Key != "2026-10-24" || len(run.Stories) != 1 {
		t.Errorf("run keyed %q shown %d Stories cut, want today's shown the speech", run.Key, len(run.Stories))
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

// cappedWeaving is the batch runner over weaving whose Editor prices calls
// on claude-opus-5-5's rates, $4 and $20 a million tokens, and the run keyed
// key capped at usd.
func cappedWeaving(t *testing.T, store *sqlite.Store, b editor.Batcher, key string, usd float64) *editor.Batches {
	t.Helper()
	if err := store.KeepWeavingCap(t.Context(), key, usd); err != nil {
		t.Fatal(err)
	}
	return editor.NewBatches(editor.BatchDeps{
		Store: store, Batcher: b, Acts: []editor.BatchAct{cappedEditor(store).Weaving(weavingAt)}, ModelFor: modelFor,
		Log: slog.New(slog.DiscardHandler),
	})
}

// The Owner's --max-usd caps a run's whole conversation: a turn that asked,
// whose next turn would take what the run has spent over its cap, gives the
// run up, marked failed with why, and nothing more is submitted. A run under
// its cap goes on.
func TestARunWhoseNextTurnWouldTakeItOverItsCapIsGivenUp(t *testing.T) {
	for name, tc := range map[string]struct {
		usd       float64
		submitted int
	}{"over": {usd: 0.02, submitted: 1}, "under": {usd: 1, submitted: 2}} {
		t.Run(name, func(t *testing.T) {
			store := record(t)
			batcher := &fakeBatcher{}
			b := cappedWeaving(t, store, batcher, "2026-10-24", tc.usd)
			runAt(t, b, saturday)
			// 1200 tokens in and 340 out at batch rates: $0.0058; the next turn
			// is some 1300 tokens in and 4000 out by the fallback: $0.04.
			respondLast(batcher, asking("find", `{"words": "Motherwell"}`))
			runAt(t, b, saturday.Add(time.Hour))

			if len(batcher.submitted) != tc.submitted {
				t.Fatalf("submitted %d batches, want %d", len(batcher.submitted), tc.submitted)
			}
			failed, err := store.FailedWork(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if over := len(failed) == 1 && failed[0].Act == "weaving" && strings.Contains(failed[0].Why, "over its cap of $0.02"); over != (name == "over") {
				t.Errorf("failed work %+v", failed)
			}
		})
	}
}

// cappedEditor is the Editor cappedWeaving weaves with.
func cappedEditor(store *sqlite.Store) *editor.Editor {
	return editor.New(editor.Deps{Store: store, Weavings: store, ModelFor: modelFor, Log: slog.New(slog.DiscardHandler),
		PriceOf: func(string) (editor.Price, bool) { return editor.Price{InputPerMTok: 4, OutputPerMTok: 20}, true }})
}

// givenUpOverCap is a Record whose run keyed saturday's day, capped at
// $0.02, was given up over its cap on its first turn, which asked: the batch
// runner and its batch interface.
func givenUpOverCap(t *testing.T, store *sqlite.Store) (*editor.Batches, *fakeBatcher) {
	t.Helper()
	batcher := &fakeBatcher{}
	b := cappedWeaving(t, store, batcher, "2026-10-24", 0.02)
	runAt(t, b, saturday)
	respondLast(batcher, asking("find", `{"words": "Motherwell"}`))
	runAt(t, b, saturday.Add(time.Hour))
	return b, batcher
}

// A run given up over its cap is shown to the Owner as it would resume: what
// it has spent, its cap, and its next turn with that turn's estimate; resumed
// under a higher cap, its next turn is submitted and applied as any turn is.
func TestARunGivenUpOverItsCapIsPricedAndResumedWithAHigherCap(t *testing.T) {
	ctx := t.Context()
	store := record(t)
	b, batcher := givenUpOverCap(t, store)
	first := batcher.submitted[0][0].Request

	g, err := cappedEditor(store).GivenUpWeaving(ctx, "2026-10-24")
	if err != nil {
		t.Fatal(err)
	}

	// 1200 tokens in and 340 out at batch rates: $0.0058.
	if !g.Capped || g.MaxUSD != 0.02 || math.Abs(g.SpentUSD-0.0058) > 1e-9 {
		t.Errorf("given up capped %v at $%v having spent $%v, want $0.0058 spent of $0.02", g.Capped, g.MaxUSD, g.SpentUSD)
	}
	if g.Next.USD <= 0 || g.Next.Rates != editor.BatchRates || !g.OverCap() {
		t.Errorf("next turn %+v, over its cap %v, want it priced at batch rates and over", g.Next, g.OverCap())
	}
	if g.Run.Key != "2026-10-24" || editor.RequestHash(g.Run.Job.Request, "") != editor.RequestHash(first, "") {
		t.Errorf("resumes %q asking:\n%s\nwant its first turn again, nothing of it kept", g.Run.Key, g.Run.Job.Request.Input)
	}

	if err := store.KeepWeavingCap(ctx, "2026-10-24", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RetryFailed(ctx, "weaving"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Submit(ctx, g.Run.Job, saturday.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	answerLast(batcher, oneOpinion)
	runAt(t, b, saturday.Add(3*time.Hour))
	if got := madeStories(t, store); len(got) != 1 {
		t.Errorf("made %d Stories, want the resumed run's one", len(got))
	}
}

// A run given up over its cap after turns it kept resumes from the last of
// them, its spending every turn's, the one given up included.
func TestARunGivenUpAfterKeptTurnsResumesFromTheLast(t *testing.T) {
	ctx := t.Context()
	store := record(t)
	batcher := &fakeBatcher{}
	b := cappedWeaving(t, store, batcher, "2026-10-24", 0.05)
	runAt(t, b, saturday)
	respondLast(batcher, asking("find", `{"words": "Motherwell"}`))
	runAt(t, b, saturday.Add(time.Hour))
	if len(batcher.submitted) != 2 {
		t.Fatalf("submitted %d batches, want the run's second turn", len(batcher.submitted))
	}
	second := batcher.submitted[1][0].Request
	respondLast(batcher, asking("find", `{"words": "Scotland"}`))
	runAt(t, b, saturday.Add(2*time.Hour))
	if failed, err := store.FailedWork(ctx); err != nil || len(failed) != 1 {
		t.Fatalf("failed work %+v (%v), want the run given up", failed, err)
	}

	g, err := cappedEditor(store).GivenUpWeaving(ctx, "2026-10-24")
	if err != nil {
		t.Fatal(err)
	}

	if math.Abs(g.SpentUSD-2*0.0058) > 1e-9 {
		t.Errorf("spent $%v, want both turns' $0.0116", g.SpentUSD)
	}
	if editor.RequestHash(g.Run.Job.Request, "") != editor.RequestHash(second, "") {
		t.Errorf("resumes asking %d messages, want its second turn, the last kept", len(g.Run.Job.Request.Messages))
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
