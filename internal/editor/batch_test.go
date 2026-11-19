package editor_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// fakeBatcher stands in for a provider's batch interface: it keeps every
// batch submitted, and a batch ends only when the test answers it.
type fakeBatcher struct {
	submitted [][]editor.BatchRequest
	ended     map[string][]editor.BatchResult
}

func (f *fakeBatcher) Submit(_ context.Context, rs []editor.BatchRequest) (string, error) {
	f.submitted = append(f.submitted, rs)
	return fmt.Sprintf("batch-%d", len(f.submitted)), nil
}

func (f *fakeBatcher) Poll(_ context.Context, id string) (editor.BatchPoll, error) {
	rs, ok := f.ended[id]
	return editor.BatchPoll{Ended: ok, Results: rs}, nil
}

// end ends the nth batch submitted (from 1), answering each of its requests
// with what answer gives, in the reverse of the order they were submitted.
func (f *fakeBatcher) end(n int, answer func(editor.BatchRequest) editor.BatchResult) {
	if f.ended == nil {
		f.ended = map[string][]editor.BatchResult{}
	}
	rs := f.submitted[n-1]
	var out []editor.BatchResult
	for i := len(rs) - 1; i >= 0; i-- {
		res := answer(rs[i])
		res.ID = rs[i].ID
		out = append(out, res)
	}
	f.ended[fmt.Sprintf("batch-%d", n)] = out
}

// answering answers every request with output.
func answering(output string) func(editor.BatchRequest) editor.BatchResult {
	return func(editor.BatchRequest) editor.BatchResult {
		return editor.BatchResult{Response: editor.Response{Output: json.RawMessage(output), Model: "fake"}}
	}
}

func newBatches(store *sqlite.Store, b editor.Batcher) (*editor.Batches, *bytes.Buffer) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	ed := editor.New(editor.Deps{Store: store, Log: log})
	return editor.NewBatches(editor.BatchDeps{
		Store:   store,
		Batcher: b,
		Acts:    []editor.BatchAct{ed.Hydration()},
		Log:     log,
	}), &logs
}

// ferries is a second Item awaiting hydration.
var ferries = collect.Item{
	SourceID: number10.ID, EntryID: "ferries", Title: "Ferries to the islands",
	Link: "https://www.gov.uk/ferries", Summary: "Ferries to Scotland's islands will run more often.",
	Content:   "<p>Ferries to Scotland's islands will run more often.</p>",
	Published: saturday, Attribution: collect.OGL, CollectedAt: saturday,
}

const ferriesHydration = `{"names": [{"at": {"span": 1, "words": "Scotland"}, "kind": "place", "name": "Scotland", "office": ""}],
  "refers_to": [], "rejected": [], "speakers": []}`

// withFerries adds the ferries Item to the Record, cut and awaiting hydration.
func withFerries(t *testing.T, store *sqlite.Store) {
	t.Helper()
	if _, err := store.AddItems(t.Context(), []collect.Item{ferries}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(store).CutNew(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
}

// newestHydration is the Item's newest hydration, if it has one.
func newestHydration(t *testing.T, store *sqlite.Store, sourceID, entryID string) (editor.Hydration, bool) {
	t.Helper()
	r, found, err := store.NewestHydration(t.Context(), sourceID, entryID)
	if err != nil {
		t.Fatal(err)
	}
	return r, found
}

// byItem answers each request with the hydration of the Item it is about.
func byItem(r editor.BatchRequest) editor.BatchResult {
	out := goodHydration
	if strings.Contains(r.Request.Input, ferries.Title) {
		out = ferriesHydration
	}
	return answering(out)(r)
}

func TestABatchsResultsAreMatchedToTheirItemsByIdentifierNotPosition(t *testing.T) {
	store := record(t)
	withFerries(t, store)
	batcher := &fakeBatcher{}
	b, _ := newBatches(store, batcher)
	if err := b.RunWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	if len(batcher.submitted) != 1 || len(batcher.submitted[0]) != 2 {
		t.Fatalf("submitted %v, want one batch of both Items", batcher.submitted)
	}

	batcher.end(1, byItem) // answered in the reverse order
	if err := b.RunWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	for entry, want := range map[string]int{speech.EntryID: 2, ferries.EntryID: 1} {
		got, hydrated := newestHydration(t, store, number10.ID, entry)
		if !hydrated || len(got.Names) != want {
			t.Errorf("%s: hydrated %v, names %+v, want %d", entry, hydrated, got.Names, want)
		}
	}
}

func TestABatchStillProcessingIsWaitedForAcrossARestartWithoutBlocking(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b, _ := newBatches(store, batcher)
	if err := b.RunWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	// A restart: a new runner over the same Record, while the batch runs.
	b, _ = newBatches(store, batcher)
	withFerries(t, store)
	if err := b.RunWaiting(t.Context(), saturday.Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}

	if len(batcher.submitted) != 2 || len(batcher.submitted[1]) != 1 ||
		!strings.Contains(batcher.submitted[1][0].Request.Input, ferries.Title) {
		t.Fatalf("submitted %v, want the speech's batch, then one of the ferries alone: the speech is already in a batch", batcher.submitted)
	}

	batcher.end(1, byItem)
	if err := b.RunWaiting(t.Context(), saturday.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, hydrated := newestHydration(t, store, speech.SourceID, speech.EntryID); !hydrated {
		t.Error("the speech was not hydrated when its batch ended after the restart")
	}
	if _, hydrated := newestHydration(t, store, ferries.SourceID, ferries.EntryID); hydrated {
		t.Error("the ferries were hydrated before their batch ended")
	}
}

// A batch submitted before the act was called hydration identifies its
// request by the act's old name; the Record's migration names the request's
// act hydration. Its work is in a batch still, so it is not submitted again,
// and it is hydrated when the batch ends.
func TestABatchPendingSinceTheActWasCalledReadingIsWaitedForAndApplied(t *testing.T) {
	store := record(t)
	key := speech.SourceID + "\n" + speech.EntryID
	sum := sha256.Sum256([]byte(key))
	oldID := "reading-" + hex.EncodeToString(sum[:16])
	if err := store.SaveBatch(t.Context(), editor.PendingBatch{ID: "batch-old", SubmittedAt: saturday,
		Requests: []editor.PendingRequest{{ID: oldID, Act: "hydration", Key: key}}}); err != nil {
		t.Fatal(err)
	}
	batcher := &fakeBatcher{}
	b, _ := newBatches(store, batcher)

	if err := b.RunWaiting(t.Context(), saturday.Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(batcher.submitted) != 0 {
		t.Fatalf("submitted %v, want nothing: the speech is in a batch already", batcher.submitted)
	}

	batcher.ended = map[string][]editor.BatchResult{"batch-old": {{ID: oldID,
		Response: editor.Response{Output: json.RawMessage(goodHydration), Model: "fake"}}}}
	if err := b.RunWaiting(t.Context(), saturday.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, hydrated := newestHydration(t, store, speech.SourceID, speech.EntryID); !hydrated {
		t.Error("the speech was not hydrated when its batch ended")
	}
	if len(batcher.submitted) != 0 {
		t.Errorf("submitted %v, want nothing more", batcher.submitted)
	}
}

func TestAFailedRequestInABatchIsResubmittedAloneAndTheRestAreApplied(t *testing.T) {
	for name, failure := range map[string]editor.BatchResult{
		"errored":            {Err: errors.New("overloaded")},
		"expired":            {Err: errors.New("expired")},
		"failing the checks": {Response: editor.Response{Output: json.RawMessage(`{"facts": [`)}},
	} {
		t.Run(name, func(t *testing.T) {
			store := record(t)
			withFerries(t, store)
			batcher := &fakeBatcher{}
			b, logs := newBatches(store, batcher)
			if err := b.RunWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}

			batcher.end(1, func(r editor.BatchRequest) editor.BatchResult {
				if strings.Contains(r.Request.Input, ferries.Title) {
					return failure
				}
				return byItem(r)
			})
			if err := b.RunWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}

			if _, hydrated := newestHydration(t, store, speech.SourceID, speech.EntryID); !hydrated {
				t.Error("the speech was held up by the ferries' failure")
			}
			if got, hydrated := newestHydration(t, store, ferries.SourceID, ferries.EntryID); hydrated {
				t.Errorf("the failed hydration was written: %+v", got)
			}
			if len(batcher.submitted) != 2 || len(batcher.submitted[1]) != 1 ||
				!strings.Contains(batcher.submitted[1][0].Request.Input, ferries.Title) {
				t.Fatalf("submitted %v, want the ferries resubmitted alone", batcher.submitted)
			}
			if !strings.Contains(logs.String(), "failed") {
				t.Error("the failure was not logged")
			}

			// The resubmission is hydrated when it ends, and nothing more is sent.
			batcher.end(2, byItem)
			if err := b.RunWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}
			if _, hydrated := newestHydration(t, store, ferries.SourceID, ferries.EntryID); !hydrated {
				t.Error("the resubmitted hydration was not applied")
			}
			if len(batcher.submitted) != 2 {
				t.Errorf("%d batches submitted, want 2", len(batcher.submitted))
			}
		})
	}
}

// downBatcher is a batch interface that fails every call.
type downBatcher struct{}

func (downBatcher) Submit(context.Context, []editor.BatchRequest) (string, error) {
	return "", errors.New("unauthorised")
}

func (downBatcher) Poll(context.Context, string) (editor.BatchPoll, error) {
	return editor.BatchPoll{}, errors.New("unauthorised")
}

func TestABatchNotSubmittedIsLoggedAndSubmittedOnTheNextRun(t *testing.T) {
	store := record(t)
	b, logs := newBatches(store, downBatcher{})

	if err := b.RunWaiting(t.Context(), saturday); err != nil {
		t.Fatalf("a failing batch interface stopped the run: %v", err)
	}
	if !strings.Contains(logs.String(), "unauthorised") {
		t.Error("the failure was not logged")
	}

	batcher := &fakeBatcher{}
	b, _ = newBatches(store, batcher)
	if err := b.RunWaiting(t.Context(), saturday.Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(batcher.submitted) != 1 {
		t.Errorf("submitted %v, want the speech submitted on the next run", batcher.submitted)
	}
}

func TestTheItemsAwaitingHydrationAreSubmittedAsOneBatchAndHydratedWhenItEnds(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b, _ := newBatches(store, batcher)

	if err := b.RunWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if len(batcher.submitted) != 1 || len(batcher.submitted[0]) != 1 {
		t.Fatalf("submitted %v, want one batch of one request: only the speech has text to cut", batcher.submitted)
	}
	if r := batcher.submitted[0][0].Request; r.Act != "hydration" || r.Instructions == "" {
		t.Errorf("the request is not hydration's: %+v", r)
	}
	if _, hydrated := newestHydration(t, store, speech.SourceID, speech.EntryID); hydrated {
		t.Fatal("the speech is hydrated before its batch has ended")
	}

	batcher.end(1, answering(goodHydration))
	if err := b.RunWaiting(t.Context(), saturday.Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}

	if got, hydrated := newestHydration(t, store, speech.SourceID, speech.EntryID); !hydrated || len(got.Names) != 2 || !got.At.Equal(saturday.Add(15*time.Minute)) {
		t.Errorf("the speech's hydration is not in the Record: %+v", got)
	}
	if len(batcher.submitted) != 1 {
		t.Errorf("%d batches submitted, want one: nothing more awaits hydration", len(batcher.submitted))
	}
}
