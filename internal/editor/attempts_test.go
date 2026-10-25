package editor_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

// failingChecks answers every request with output that fails its checks.
func failingChecks(editor.BatchRequest) editor.BatchResult {
	return editor.BatchResult{Response: editor.Response{Output: json.RawMessage(`{"facts": [`), Model: "fake"}}
}

func TestWorkThatKeepsFailingIsAttemptedThreeTimesThenMarkedFailed(t *testing.T) {
	for name, tc := range map[string]struct {
		answer func(editor.BatchRequest) editor.BatchResult
		why    string
	}{
		"failing its checks": {failingChecks, "checks"},
		"refused": {func(editor.BatchRequest) editor.BatchResult {
			return editor.BatchResult{Err: errors.New("the model refused")}
		}, "refused"},
	} {
		t.Run(name, func(t *testing.T) { keepsFailing(t, tc.answer, tc.why) })
	}
}

func keepsFailing(t *testing.T, answer func(editor.BatchRequest) editor.BatchResult, why string) {
	t.Helper()
	store := record(t)
	batcher := &fakeBatcher{}
	b, _ := newBatches(store, batcher)
	at := saturday
	if err := b.ReadWaiting(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	// Each batch the speech is in ends failing its checks: the first with
	// the rest of the work, the others resubmitted alone.
	for n := 1; n <= editor.MaxAttempts; n++ {
		batcher.end(n, answer)
		at = at.Add(15 * time.Minute)
		if err := b.ReadWaiting(t.Context(), at); err != nil {
			t.Fatal(err)
		}
	}
	// Later runs submit nothing more.
	if err := b.ReadWaiting(t.Context(), at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if len(batcher.submitted) != editor.MaxAttempts {
		t.Fatalf("%d batches submitted, want %d: one per attempt", len(batcher.submitted), editor.MaxAttempts)
	}
	waiting, err := store.ItemsToRead(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(waiting) != 0 {
		t.Errorf("the speech still awaits reading after %d failures: %+v", editor.MaxAttempts, waiting)
	}
	failed, err := store.FailedWork(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 {
		t.Fatalf("failed work %+v, want the speech's reading", failed)
	}
	f := failed[0]
	if f.Act != "reading" || f.EntryID != speech.EntryID || f.Title != speech.Title ||
		f.Attempts != editor.MaxAttempts || !f.LastFailedAt.Equal(at) || !strings.Contains(f.Why, why) {
		t.Errorf("failed work %+v, want the speech's reading, failed %d times, last at %v, %s", f, editor.MaxAttempts, at, why)
	}
}

func TestAnItemHeldBackBehindAnItemWhosePlacingFailedGoesAhead(t *testing.T) {
	store := record(t)
	ed, _ := placer(store, &fakeModel{})
	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	// The readout shares Andy Burnham with the speech, so waits behind it.
	readLater(t, store)
	batcher := &fakeBatcher{}
	b := placingBatches(store, batcher)
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= editor.MaxAttempts; n++ {
		if got := batcher.submitted[n-1]; len(got) != 1 || strings.Contains(got[0].Request.Input, laterItem.Title) {
			t.Fatalf("batch %d is %v, want the speech's placing alone", n, got)
		}
		batcher.end(n, failingChecks)
		if err := b.ReadWaiting(t.Context(), saturday); err != nil {
			t.Fatal(err)
		}
	}

	if len(batcher.submitted) != editor.MaxAttempts+1 {
		t.Fatalf("%d batches submitted, want the readout's after the speech's %d", len(batcher.submitted), editor.MaxAttempts)
	}
	if got := batcher.submitted[editor.MaxAttempts]; len(got) != 1 || !strings.Contains(got[0].Request.Input, laterItem.Title) {
		t.Errorf("last batch %v, want the readout placed once the speech's placing failed", got)
	}
}

func TestConnectingThatKeepsFailingIsNoLongerSubmitted(t *testing.T) {
	store := record(t)
	ed, _ := placer(store, &fakeModel{output: startsAStory})
	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	batcher := &fakeBatcher{}
	b := editor.NewBatches(editor.BatchDeps{Store: store, Batcher: batcher,
		Acts: []editor.BatchAct{ed.Connecting()}, Log: slog.New(slog.DiscardHandler)})
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= editor.MaxAttempts; n++ {
		batcher.end(n, failingChecks)
		if err := b.ReadWaiting(t.Context(), saturday); err != nil {
			t.Fatal(err)
		}
	}

	if len(batcher.submitted) != editor.MaxAttempts {
		t.Errorf("%d batches submitted, want %d", len(batcher.submitted), editor.MaxAttempts)
	}
	failed, err := store.FailedWork(t.Context())
	if err != nil || len(failed) != 1 || failed[0].Act != "connecting" {
		t.Errorf("failed work %+v %v, want the speech's connecting", failed, err)
	}
}

func TestRetryingAnActsFailedWorkResetsItsAttempts(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b, _ := newBatches(store, batcher)
	fail := func() {
		t.Helper()
		if err := b.ReadWaiting(t.Context(), saturday); err != nil {
			t.Fatal(err)
		}
		for range editor.MaxAttempts {
			batcher.end(len(batcher.submitted), failingChecks)
			if err := b.ReadWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}
		}
	}
	fail()

	// Another act's retry leaves reading's failed work failed.
	if n, err := store.RetryFailed(t.Context(), "placing"); err != nil || n != 0 {
		t.Fatalf("retrying placing cleared %d %v, want none", n, err)
	}
	if waiting, _ := store.ItemsToRead(t.Context()); len(waiting) != 0 {
		t.Fatalf("retrying placing took reading's failed work up again: %+v", waiting)
	}
	if n, err := store.RetryFailed(t.Context(), "reading"); err != nil || n != 1 {
		t.Fatalf("retrying reading cleared %d %v, want the speech's", n, err)
	}
	if failed, _ := store.FailedWork(t.Context()); len(failed) != 0 {
		t.Errorf("failed work %+v once retried, want none", failed)
	}

	// The next run takes it up again, with every attempt afresh.
	before := len(batcher.submitted)
	fail()
	if got := len(batcher.submitted) - before; got != editor.MaxAttempts {
		t.Errorf("%d batches submitted after the retry, want %d attempts afresh", got, editor.MaxAttempts)
	}
}

// batches is how many batches have been submitted.
func (f *fakeBatcher) batches() int { return len(f.submitted) }

// lostBatcher is a batch interface whose batches disappear once submitted:
// every poll fails.
type lostBatcher struct{ fakeBatcher }

func (*lostBatcher) Poll(context.Context, string) (editor.BatchPoll, error) {
	return editor.BatchPoll{}, errors.New("batch not found")
}

func TestABatchThatDisappearsOrNeverEndsCountsAsAFailedAttemptForItsWork(t *testing.T) {
	for name, batcher := range map[string]interface {
		editor.Batcher
		batches() int
	}{
		"disappeared": &lostBatcher{},
		"never ends":  &fakeBatcher{},
	} {
		t.Run(name, func(t *testing.T) {
			store := record(t)
			b, _ := newBatches(store, batcher)
			at := saturday
			if err := b.ReadWaiting(t.Context(), at); err != nil {
				t.Fatal(err)
			}
			// Short of the deadline, the batch is still waited for.
			if err := b.ReadWaiting(t.Context(), at.Add(editor.BatchDeadline-time.Minute)); err != nil {
				t.Fatal(err)
			}
			if n := batcher.batches(); n != 1 {
				t.Fatalf("%d batches submitted before the deadline, want 1", n)
			}
			for range editor.MaxAttempts {
				at = at.Add(editor.BatchDeadline)
				if err := b.ReadWaiting(t.Context(), at); err != nil {
					t.Fatal(err)
				}
			}
			if n := batcher.batches(); n != editor.MaxAttempts {
				t.Errorf("%d batches submitted, want %d: the speech resubmitted alone after each lost batch, until its last", n, editor.MaxAttempts)
			}
			failed, err := store.FailedWork(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if len(failed) != 1 || failed[0].EntryID != speech.EntryID || !failed[0].LastFailedAt.Equal(at) {
				t.Errorf("failed work %+v, want the speech's reading, last failed at %v", failed, at)
			}
			pending, err := store.PendingBatches(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if len(pending) != 0 {
				t.Errorf("batches %+v still pending, want the lost ones given up", pending)
			}
		})
	}
}
