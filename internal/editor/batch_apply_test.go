package editor_test

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// stubAct is an act over the pieces of work keys, each awaiting it until it
// is applied, whose applying the test controls: a key in fail fails to
// apply, for a reason other than the act's checks, until it is taken out.
type stubAct struct {
	keys    []string
	applied []string
	fail    map[string]error
}

func (s *stubAct) act() editor.BatchAct {
	return editor.BatchAct{
		Name: "stub",
		Waiting: func(context.Context) ([]editor.Job, error) {
			var jobs []editor.Job
			for _, k := range s.keys {
				if slices.Contains(s.applied, k) {
					continue
				}
				jobs = append(jobs, editor.Job{
					Key:     k,
					Request: editor.Request{Act: "stub", Input: k},
					Apply: func(context.Context, editor.Response, time.Time) error {
						if err := s.fail[k]; err != nil {
							return err
						}
						s.applied = append(s.applied, k)
						return nil
					},
				})
			}
			return jobs, nil
		},
	}
}

func stubBatches(store *sqlite.Store, b editor.Batcher, acts ...editor.BatchAct) *editor.Batches {
	return editor.NewBatches(editor.BatchDeps{
		Store: store, Batcher: b, Acts: acts,
		Log: slog.New(slog.DiscardHandler),
	})
}

var answerAll = answering(`{}`)

// errTooBusy is a reading's Entity lookup that Wikidata's search refused.
var errTooBusy = errors.New("wikidata wbsearchentities: API error: cirrussearch-too-busy-error")

func attempts(t *testing.T, store *sqlite.Store, key string) int {
	t.Helper()
	n, err := store.Attempts(t.Context(), "stub", key)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestOneResultFailingToApplyHoldsUpNeitherTheRestOfItsBatchNorItsOwnLaterApplying(t *testing.T) {
	store := record(t)
	stub := &stubAct{keys: []string{"a", "b", "c"}, fail: map[string]error{"b": errTooBusy}}
	batcher := &fakeBatcher{}
	b := stubBatches(store, batcher, stub.act())
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	batcher.end(1, answerAll)

	if err := b.ReadWaiting(t.Context(), saturday.Add(15*time.Minute)); err != nil {
		t.Fatalf("one result failing to apply stopped the run: %v", err)
	}
	if !slices.Equal(stub.applied, []string{"a", "c"}) && !slices.Equal(stub.applied, []string{"c", "a"}) {
		t.Fatalf("applied %v, want a and c", stub.applied)
	}
	if len(batcher.submitted) != 1 {
		t.Fatalf("submitted %v, want no new request: b's result is kept to apply again", batcher.submitted)
	}
	if n := attempts(t, store, "b"); n != 0 {
		t.Errorf("b counted %d attempts, want none while it fails to apply", n)
	}

	// Applying succeeds again: b is applied from the batch's result.
	delete(stub.fail, "b")
	if err := b.ReadWaiting(t.Context(), saturday.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(stub.applied, "b") {
		t.Errorf("applied %v, want b applied on the later run", stub.applied)
	}
	if len(batcher.submitted) != 1 {
		t.Errorf("submitted %v, want no new request", batcher.submitted)
	}
	if n := attempts(t, store, "b"); n != 0 {
		t.Errorf("b counted %d attempts, want none", n)
	}
	pending, err := store.PendingBatches(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending %+v, want the batch ended once every result is applied", pending)
	}
}

func TestNewWorkIsSubmittedInTheRunAResultFailsToApply(t *testing.T) {
	store := record(t)
	stub := &stubAct{keys: []string{"a", "b"}, fail: map[string]error{"b": errTooBusy}}
	batcher := &fakeBatcher{}
	b := stubBatches(store, batcher, stub.act())
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	batcher.end(1, answerAll)
	stub.keys = append(stub.keys, "c") // new work since the batch was submitted

	if err := b.ReadWaiting(t.Context(), saturday.Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(batcher.submitted) != 2 || len(batcher.submitted[1]) != 1 || batcher.submitted[1][0].Request.Input != "c" {
		t.Fatalf("submitted %v, want c alone in a new batch: b is still in its batch", batcher.submitted)
	}
}

func TestAResultStillFailingToApplyAtTheBatchsDeadlineCountsAnAttempt(t *testing.T) {
	store := record(t)
	stub := &stubAct{keys: []string{"a", "b"}, fail: map[string]error{"b": errTooBusy}}
	batcher := &fakeBatcher{}
	b := stubBatches(store, batcher, stub.act())
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	batcher.end(1, answerAll)
	if err := b.ReadWaiting(t.Context(), saturday.Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if n := attempts(t, store, "b"); n != 0 {
		t.Fatalf("b counted %d attempts before the deadline, want none", n)
	}

	if err := b.ReadWaiting(t.Context(), saturday.Add(editor.BatchDeadline)); err != nil {
		t.Fatal(err)
	}
	if n := attempts(t, store, "b"); n != 1 {
		t.Errorf("b counted %d attempts at the deadline, want 1", n)
	}
	if len(batcher.submitted) != 2 || len(batcher.submitted[1]) != 1 || batcher.submitted[1][0].Request.Input != "b" {
		t.Errorf("submitted %v, want b resubmitted alone, as any failed attempt short of the last is", batcher.submitted)
	}
}
