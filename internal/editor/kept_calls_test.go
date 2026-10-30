package editor_test

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// callsOn are the calls kept on act's work on the speech.
func callsOn(t *testing.T, store *sqlite.Store, act string) []editor.Call {
	t.Helper()
	calls, err := store.CallsOn(t.Context(), act, speechKey)
	if err != nil {
		t.Fatal(err)
	}
	return calls
}

func TestARelatingInABatchLeavesAKeptCall(t *testing.T) {
	store := record(t)
	readNaming(t, store, pressRelease, "Scotland")
	readNaming(t, store, holyrood, "Scotland")
	batcher := &fakeBatcher{}
	log := slog.New(slog.DiscardHandler)
	ed := editor.New(editor.Deps{Store: store, ModelFor: modelFor, Log: log})
	b := editor.NewBatches(editor.BatchDeps{Store: store, Batcher: batcher, Acts: []editor.BatchAct{ed.Relating()},
		ModelFor: modelFor, Log: log})
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	batcher.end(1, answering(`{"relations": [{"span": "S2", "to": "C1", "verdict": "same_as"}]}`))
	if err := b.ReadWaiting(t.Context(), saturday.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	calls, err := store.CallsOn(t.Context(), "relating", holyrood.SourceID+"\n"+holyrood.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("kept %d relating calls on the speech, want 1", len(calls))
	}
	c := calls[0]
	if c.Outcome != editor.Applied || c.BatchID != "batch-1" || c.Model != "fake-relating" ||
		c.RequestHash != editor.RequestHash(c.Request, "fake-relating") ||
		!strings.Contains(c.Request.Input, "C1: Scotland is one of the most centralised nations in Europe.") {
		t.Errorf("kept %+v, want the request with the candidate it weighed, applied", c)
	}
	if got := relationsOf(t, store, holyrood, 2); len(got) != 1 || got[0].Call != c.ID || got[0].Judge.RequestHash != c.RequestHash {
		t.Errorf("the relation %+v does not link the call %d that made it", got, c.ID)
	}
	if calls, err := store.CallsOn(t.Context(), "relating", pressRelease.SourceID+"\n"+pressRelease.EntryID); err != nil || len(calls) != 0 {
		t.Errorf("kept %+v on the release, related with no call", calls)
	}
}
