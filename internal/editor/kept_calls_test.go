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

// keepingPlacer is an Editor over store that reads with goodReading and
// places with placing, naming each act's model.
func keepingPlacer(store *sqlite.Store, placing editor.Model) *editor.Editor {
	return editor.New(editor.Deps{
		Store: store, Model: &fakeModel{output: goodReading}, PlacingModel: placing,
		Wikidata: &fakeWikidata{}, ModelFor: modelFor, Log: slog.New(slog.DiscardHandler),
	})
}

func TestAPlacingInRealTimeLeavesAKeptCall(t *testing.T) {
	store := record(t)
	m := &answeringModel{res: answered(startsAStory)}
	ed := keepingPlacer(store, m)
	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	calls := callsOn(t, store, "placing")
	if len(calls) != 1 {
		t.Fatalf("kept %d placing calls, want 1", len(calls))
	}
	c := calls[0]
	if c.BatchID != "" || c.Model != "fake-placing" || !c.At.Equal(saturday) {
		t.Errorf("call in %q on %q at %v", c.BatchID, c.Model, c.At)
	}
	if c.Request.Input != m.sent[0].Input || c.RequestHash != editor.RequestHash(m.sent[0], "fake-placing") {
		t.Errorf("request kept is not the request sent")
	}
	if c.Response == nil || string(c.Response.Output) != startsAStory || c.Response.Usage.InputTokens != 1200 {
		t.Errorf("response kept %+v", c.Response)
	}
	if c.Outcome != editor.Applied {
		t.Errorf("outcome %q (%q), want applied", c.Outcome, c.Why)
	}
}

func TestAPlacingInRealTimeRefusedByItsChecksIsKeptWithItsReason(t *testing.T) {
	store := record(t)
	ed := keepingPlacer(store, &answeringModel{res: answered(`{"new_stories": [], "facts": []}`)})
	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	calls := callsOn(t, store, "placing")
	if len(calls) != 1 || calls[0].Outcome != editor.Refused || !strings.Contains(calls[0].Why, "left unplaced") {
		t.Errorf("kept %+v, want one refused with the check's reason", calls)
	}
}

func TestAConnectingInRealTimeLeavesAKeptCall(t *testing.T) {
	store, _ := speechAndReadout(t)
	m := &answeringModel{res: answered(speechRoles)}
	ed := editor.New(editor.Deps{Store: store, ConnectingModel: m, ModelFor: modelFor, Log: slog.New(slog.DiscardHandler)})
	// Only the speech awaits connecting: the readout's answer would fail.
	if err := ed.ConnectWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	calls := callsOn(t, store, "connecting")
	if len(calls) != 1 {
		t.Fatalf("kept %d connecting calls on the speech, want 1", len(calls))
	}
	c := calls[0]
	if c.BatchID != "" || c.Model != "fake-connecting" || !c.At.Equal(saturday) ||
		c.Request.Input != m.sent[0].Input || c.RequestHash != editor.RequestHash(m.sent[0], "fake-connecting") {
		t.Errorf("kept %+v, not the call sent", c)
	}
	if c.Outcome != editor.Applied {
		t.Errorf("outcome %q (%q), want applied", c.Outcome, c.Why)
	}
	readout, err := store.CallsOn(t.Context(), "connecting", laterItem.SourceID+"\n"+laterItem.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	if len(readout) != 1 || readout[0].Outcome != editor.Refused || readout[0].Why == "" {
		t.Errorf("kept on the readout %+v, want one refused with why", readout)
	}
}

func TestConnectingAgainBeforeTheBriefingLeavesKeptCalls(t *testing.T) {
	store, _ := speechAndReadout(t)
	ed := editor.New(editor.Deps{Store: store, ConnectingModel: &inTurn{outputs: []string{speechRoles, readoutCausedBySpeech}},
		ModelFor: modelFor, Log: slog.New(slog.DiscardHandler)})

	if err := ed.Reconnect(t.Context(), saturday.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{speechKey, laterItem.SourceID + "\n" + laterItem.EntryID} {
		calls, err := store.CallsOn(t.Context(), "connecting", key)
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) != 1 || calls[0].Outcome != editor.Applied || !calls[0].At.Equal(saturday.Add(time.Hour)) {
			t.Errorf("kept on %q: %+v, want the re-run's call, applied", key, calls)
		}
	}
}

// readoutKey is the work key of the readout.
var readoutKey = laterItem.SourceID + "\n" + laterItem.EntryID

func TestAPlacingsKeptCallCarriesThePrecedentsItShowed(t *testing.T) {
	placedNowhere := `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": []}]}`
	for name, place := range map[string]func(t *testing.T, store *sqlite.Store) []editor.Request{
		"in real time": func(t *testing.T, store *sqlite.Store) []editor.Request {
			m := &answeringModel{res: answered(placedNowhere)}
			if err := keepingPlacer(store, m).PlaceWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}
			return m.sent
		},
		"in a batch": func(t *testing.T, store *sqlite.Store) []editor.Request {
			batcher := &fakeBatcher{}
			ed := keepingPlacer(store, nil)
			b := editor.NewBatches(editor.BatchDeps{Store: store, Batcher: batcher,
				Acts: []editor.BatchAct{ed.Placing()}, ModelFor: modelFor, Log: slog.New(slog.DiscardHandler)})
			if err := b.ReadWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}
			batcher.end(1, func(editor.BatchRequest) editor.BatchResult {
				return editor.BatchResult{Response: answered(placedNowhere)}
			})
			if err := b.ReadWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}
			return []editor.Request{batcher.submitted[0][0].Request}
		},
	} {
		t.Run(name, func(t *testing.T) {
			store := record(t)
			story, fact := speechStory(t, store)
			unwanted := give(t, store, "unwanted", story, fact)
			readLater(t, store)

			sent := place(t, store)

			calls, err := store.CallsOn(t.Context(), "placing", readoutKey)
			if err != nil {
				t.Fatal(err)
			}
			if len(calls) != 1 {
				t.Fatalf("kept %d placing calls on the readout, want 1", len(calls))
			}
			c := calls[0]
			if len(c.Precedents) != 1 || c.Precedents[0] != unwanted.ID {
				t.Errorf("precedents kept %v, want the one shown (%d)", c.Precedents, unwanted.ID)
			}
			if c.Outcome != editor.Applied || c.RequestHash != editor.RequestHash(sent[0], "fake-placing") ||
				!strings.Contains(c.Request.Input, "Andy Burnham in Scotland") {
				t.Errorf("kept %+v, want the request sent, with the candidate Story it weighed, applied", c)
			}
		})
	}
}

func TestAConnectingInABatchLeavesAKeptCall(t *testing.T) {
	store, _ := speechAndReadout(t)
	batcher := &fakeBatcher{}
	ed := editor.New(editor.Deps{Store: store, ModelFor: modelFor, Log: slog.New(slog.DiscardHandler)})
	b := editor.NewBatches(editor.BatchDeps{Store: store, Batcher: batcher,
		Acts: []editor.BatchAct{ed.Connecting()}, ModelFor: modelFor, Log: slog.New(slog.DiscardHandler)})
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	batcher.end(1, func(r editor.BatchRequest) editor.BatchResult {
		if strings.Contains(r.Request.Input, laterItem.Title) {
			return editor.BatchResult{Response: answered(readoutCausedBySpeech)}
		}
		return editor.BatchResult{Response: answered(speechRoles)}
	})
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	calls, err := store.CallsOn(t.Context(), "connecting", readoutKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("kept %d connecting calls on the readout, want 1", len(calls))
	}
	c := calls[0]
	if c.Outcome != editor.Applied || c.BatchID != "batch-1" || c.Model != "fake-connecting" ||
		c.RequestHash != editor.RequestHash(c.Request, "fake-connecting") ||
		!strings.Contains(c.Request.Input, "Andy Burnham gave a speech in Motherwell.") {
		t.Errorf("kept %+v, want the request with the neighbouring Fact it weighed, applied", c)
	}
}
