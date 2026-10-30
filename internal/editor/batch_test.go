package editor_test

import (
	"bytes"
	"context"
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
	"github.com/samthorold/go-slow/internal/interest"
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
	ed := editor.New(editor.Deps{Store: store, Wikidata: &fakeWikidata{}, Log: log})
	return editor.NewBatches(editor.BatchDeps{
		Store:   store,
		Batcher: b,
		Acts:    []editor.BatchAct{ed.Reading()},
		Log:     log,
	}), &logs
}

// ferries is a second Item awaiting reading.
var ferries = collect.Item{
	SourceID: number10.ID, EntryID: "ferries", Title: "Ferries to the islands",
	Link: "https://www.gov.uk/ferries", Summary: "Ferries to Scotland's islands will run more often.",
	Content:   "<p>Ferries to Scotland's islands will run more often.</p>",
	Published: saturday, Attribution: collect.OGL, CollectedAt: saturday,
}

const ferriesReading = `{"names": [{"at": {"span": 1, "words": "Scotland"}, "kind": "place", "name": "Scotland", "office": ""}],
  "refers_to": [], "rejected": [], "speakers": []}`

// withFerries adds the ferries Item to the Record, cut and awaiting reading.
func withFerries(t *testing.T, store *sqlite.Store) {
	t.Helper()
	if _, err := store.AddItems(t.Context(), []collect.Item{ferries}); err != nil {
		t.Fatal(err)
	}
	if err := interest.New(store).CheckNew(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(store).CutNew(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
}

// newestReading is the Item's newest reading, if it has been read.
func newestReading(t *testing.T, store *sqlite.Store, sourceID, entryID string) (editor.Reading, bool) {
	t.Helper()
	r, found, err := store.NewestReading(t.Context(), sourceID, entryID)
	if err != nil {
		t.Fatal(err)
	}
	return r, found
}

// byItem answers each request with the reading of the Item it is about.
func byItem(r editor.BatchRequest) editor.BatchResult {
	out := goodReading
	if strings.Contains(r.Request.Input, ferries.Title) {
		out = ferriesReading
	}
	return answering(out)(r)
}

func TestABatchsResultsAreMatchedToTheirItemsByIdentifierNotPosition(t *testing.T) {
	store := record(t)
	withFerries(t, store)
	batcher := &fakeBatcher{}
	b, _ := newBatches(store, batcher)
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	if len(batcher.submitted) != 1 || len(batcher.submitted[0]) != 2 {
		t.Fatalf("submitted %v, want one batch of both Items", batcher.submitted)
	}

	batcher.end(1, byItem) // answered in the reverse order
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	for entry, want := range map[string]int{speech.EntryID: 2, ferries.EntryID: 1} {
		got, read := newestReading(t, store, number10.ID, entry)
		if !read || len(got.Names) != want {
			t.Errorf("%s: read %v, names %+v, want %d", entry, read, got.Names, want)
		}
	}
}

func TestABatchStillProcessingIsWaitedForAcrossARestartWithoutBlocking(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b, _ := newBatches(store, batcher)
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	// A restart: a new runner over the same Record, while the batch runs.
	b, _ = newBatches(store, batcher)
	withFerries(t, store)
	if err := b.ReadWaiting(t.Context(), saturday.Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}

	if len(batcher.submitted) != 2 || len(batcher.submitted[1]) != 1 ||
		!strings.Contains(batcher.submitted[1][0].Request.Input, ferries.Title) {
		t.Fatalf("submitted %v, want the speech's batch, then one of the ferries alone: the speech is already in a batch", batcher.submitted)
	}

	batcher.end(1, byItem)
	if err := b.ReadWaiting(t.Context(), saturday.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, read := newestReading(t, store, speech.SourceID, speech.EntryID); !read {
		t.Error("the speech was not read when its batch ended after the restart")
	}
	if _, read := newestReading(t, store, ferries.SourceID, ferries.EntryID); read {
		t.Error("the ferries were read before their batch ended")
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
			if err := b.ReadWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}

			batcher.end(1, func(r editor.BatchRequest) editor.BatchResult {
				if strings.Contains(r.Request.Input, ferries.Title) {
					return failure
				}
				return byItem(r)
			})
			if err := b.ReadWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}

			if _, read := newestReading(t, store, speech.SourceID, speech.EntryID); !read {
				t.Error("the speech was held up by the ferries' failure")
			}
			if got, read := newestReading(t, store, ferries.SourceID, ferries.EntryID); read {
				t.Errorf("the failed reading was written: %+v", got)
			}
			if len(batcher.submitted) != 2 || len(batcher.submitted[1]) != 1 ||
				!strings.Contains(batcher.submitted[1][0].Request.Input, ferries.Title) {
				t.Fatalf("submitted %v, want the ferries resubmitted alone", batcher.submitted)
			}
			if !strings.Contains(logs.String(), "failed") {
				t.Error("the failure was not logged")
			}

			// The resubmission is read when it ends, and nothing more is sent.
			batcher.end(2, byItem)
			if err := b.ReadWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}
			if _, read := newestReading(t, store, ferries.SourceID, ferries.EntryID); !read {
				t.Error("the resubmitted reading was not applied")
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

	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatalf("a failing batch interface stopped the run: %v", err)
	}
	if !strings.Contains(logs.String(), "unauthorised") {
		t.Error("the failure was not logged")
	}

	batcher := &fakeBatcher{}
	b, _ = newBatches(store, batcher)
	if err := b.ReadWaiting(t.Context(), saturday.Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(batcher.submitted) != 1 {
		t.Errorf("submitted %v, want the speech submitted on the next run", batcher.submitted)
	}
}

func TestTheItemsAwaitingReadingAreSubmittedAsOneBatchAndReadWhenItEnds(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b, _ := newBatches(store, batcher)

	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if len(batcher.submitted) != 1 || len(batcher.submitted[0]) != 1 {
		t.Fatalf("submitted %v, want one batch of one request: only the speech has text to cut", batcher.submitted)
	}
	if r := batcher.submitted[0][0].Request; r.Act != "reading" || r.Instructions == "" {
		t.Errorf("the request is not reading's: %+v", r)
	}
	if _, read := newestReading(t, store, speech.SourceID, speech.EntryID); read {
		t.Fatal("the speech is read before its batch has ended")
	}

	batcher.end(1, answering(goodReading))
	if err := b.ReadWaiting(t.Context(), saturday.Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}

	if got, read := newestReading(t, store, speech.SourceID, speech.EntryID); !read || len(got.Names) != 2 || !got.At.Equal(saturday.Add(15*time.Minute)) {
		t.Errorf("the speech's reading is not in the Record: %+v", got)
	}
	if len(batcher.submitted) != 1 {
		t.Errorf("%d batches submitted, want one: nothing more awaits reading", len(batcher.submitted))
	}
}
