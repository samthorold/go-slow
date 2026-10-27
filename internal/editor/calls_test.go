package editor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// modelFor names the model each act asks for, as the adapter's config does.
func modelFor(act string) string { return "fake-" + act }

// keepingBatches is the batch runner over reading, keeping every call.
func keepingBatches(store *sqlite.Store, b editor.Batcher) *editor.Batches {
	log := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	ed := editor.New(editor.Deps{Store: store, Wikidata: &fakeWikidata{}, ModelFor: modelFor, Log: log})
	return editor.NewBatches(editor.BatchDeps{
		Store:    store,
		Batcher:  b,
		Acts:     []editor.BatchAct{ed.Reading()},
		ModelFor: modelFor,
		Log:      log,
	})
}

// speechKey is the reading work key of the speech.
var speechKey = speech.SourceID + "\n" + speech.EntryID

// answered is a provider's answer as the adapter reports it, with its raw
// form, usage and stop reason.
func answered(output string) editor.Response {
	return editor.Response{
		Output:     json.RawMessage(output),
		Raw:        json.RawMessage(`{"id":"msg_1","content":[{"type":"thinking","thinking":"hmm"}]}`),
		Model:      "fake-reported",
		StopReason: "end_turn",
		Usage:      editor.Usage{InputTokens: 1200, OutputTokens: 340},
	}
}

func TestAReadingAppliedFromABatchLeavesAKeptCall(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := keepingBatches(store, batcher)
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	sent := batcher.submitted[0][0].Request
	batcher.end(1, func(editor.BatchRequest) editor.BatchResult {
		return editor.BatchResult{Response: answered(goodReading)}
	})
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	calls, err := store.CallsOn(t.Context(), "reading", speechKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("kept %d calls, want 1: %+v", len(calls), calls)
	}
	c := calls[0]
	if c.Act != "reading" || c.Key != speechKey || !c.At.Equal(saturday) || c.BatchID != "batch-1" {
		t.Errorf("call is for %s %q at %v in %q", c.Act, c.Key, c.At, c.BatchID)
	}
	if c.Request.Instructions != sent.Instructions || c.Request.Input != sent.Input {
		t.Errorf("request kept is not the request sent: %+v", c.Request)
	}
	if s, _ := json.Marshal(c.Request.Schema); string(s) != mustJSON(t, sent.Schema) {
		t.Errorf("schema kept %s, sent %s", s, mustJSON(t, sent.Schema))
	}
	if c.Model != "fake-reading" {
		t.Errorf("model asked for %q, want fake-reading", c.Model)
	}
	if c.RequestHash != editor.RequestHash(sent, "fake-reading") {
		t.Errorf("request hash %q, want the request's own", c.RequestHash)
	}
	if c.Response == nil {
		t.Fatal("kept with no response")
	}
	want := answered(goodReading)
	if string(c.Response.Raw) != string(want.Raw) || string(c.Response.Output) != goodReading ||
		c.Response.Model != "fake-reported" || c.Response.StopReason != "end_turn" || c.Response.Usage != want.Usage {
		t.Errorf("response kept %+v", c.Response)
	}
	if c.Outcome != editor.Applied || c.Why != "" {
		t.Errorf("outcome %q (%q), want applied", c.Outcome, c.Why)
	}
}

func TestAReadingRefusedByItsChecksInABatchIsKeptWithItsReason(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := keepingBatches(store, batcher)
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	bad := `{"facts": [{"statement": "x", "as_at": "2026-10-22", "quote": "not in the Item"}], "claims": [], "mentions": []}`
	batcher.end(1, func(editor.BatchRequest) editor.BatchResult {
		return editor.BatchResult{Response: answered(bad)}
	})
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	calls, err := store.CallsOn(t.Context(), "reading", speechKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("kept %d calls, want 1", len(calls))
	}
	c := calls[0]
	if c.Outcome != editor.Refused || !strings.Contains(c.Why, "quote is not in the Item") {
		t.Errorf("outcome %q (%q), want refused with the check's reason", c.Outcome, c.Why)
	}
	if c.Response == nil || string(c.Response.Output) != bad {
		t.Errorf("the refused response is not kept: %+v", c.Response)
	}
}

func TestABatchRequestWithNoResultIsKeptWithNoResponse(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := keepingBatches(store, batcher)
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	batcher.ended = map[string][]editor.BatchResult{"batch-1": nil}
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	calls, err := store.CallsOn(t.Context(), "reading", speechKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("kept %d calls, want 1", len(calls))
	}
	if c := calls[0]; c.Response != nil || c.Outcome != editor.Failed || c.Why == "" || c.Request.Input == "" {
		t.Errorf("kept %+v, want the request with no response, failed with why", c)
	}
}

func TestABatchCallKeepsTheRequestAsSentNotAsTheWorkStandsWhenItEnds(t *testing.T) {
	store := record(t)
	fact := readGlasgow(t, store)
	wrongAt(t, store, fact, saturday)
	batcher := &fakeBatcher{}
	b := keepingBatches(store, batcher)
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	sent := batcher.submitted[0][0].Request
	// More Feedback after the request went: had the request been built
	// again when the batch ended, it would show two precedents.
	wrongAt(t, store, readTied(t, store, ferries, editor.Fact{Statement: "Ferries run.", AsAt: oct16,
		Quote: "will run more often"}, "Arran"), saturday)
	batcher.end(1, func(editor.BatchRequest) editor.BatchResult {
		return editor.BatchResult{Response: answered(goodReading)}
	})
	if err := b.ReadWaiting(t.Context(), saturday.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	c := onlyCall(t, store)
	if c.Request.Input != sent.Input || c.RequestHash != editor.RequestHash(sent, "fake-reading") {
		t.Errorf("kept input\n%s\nwant as sent\n%s", c.Request.Input, sent.Input)
	}
	if len(c.Precedents) != 1 || c.Precedents[0] == 0 {
		t.Errorf("precedents kept %v, want the one shown", c.Precedents)
	}
}

func TestARequestsHashCoversItsInstructionsSchemaInputAndModel(t *testing.T) {
	req := editor.Request{Act: "reading", Instructions: "Read.", Input: "Title: x",
		Schema: map[string]any{"type": "object", "properties": map[string]any{"b": 1, "a": 2}}}
	h := editor.RequestHash(req, "m")
	same := req
	same.Act = "another act"
	same.Schema = map[string]any{"properties": map[string]any{"a": 2, "b": 1}, "type": "object"}
	if editor.RequestHash(same, "m") != h {
		t.Error("the same request hashes differently")
	}
	if len(h) != 64 {
		t.Errorf("hash %q is not a hex SHA-256", h)
	}
	for name, other := range map[string]func() (editor.Request, string){
		"instructions": func() (editor.Request, string) { r := req; r.Instructions = "Read!"; return r, "m" },
		"input":        func() (editor.Request, string) { r := req; r.Input = "Title: y"; return r, "m" },
		"schema":       func() (editor.Request, string) { r := req; r.Schema = map[string]any{"type": "array"}; return r, "m" },
		"model":        func() (editor.Request, string) { return req, "n" },
	} {
		if editor.RequestHash(other()) == h {
			t.Errorf("a request with other %s hashes the same", name)
		}
	}
}

// answering is a model answering every request with res and err, keeping
// the requests it was sent.
type answeringModel struct {
	res  editor.Response
	err  error
	sent []editor.Request
}

func (m *answeringModel) Complete(_ context.Context, r editor.Request) (editor.Response, error) {
	m.sent = append(m.sent, r)
	return m.res, m.err
}

// readInRealTime reads the Items awaiting reading on the synchronous path.
func readInRealTime(t *testing.T, store *sqlite.Store, m editor.Model) {
	t.Helper()
	ed := editor.New(editor.Deps{
		Store: store, Model: m, Wikidata: &fakeWikidata{}, ModelFor: modelFor,
		Log: slog.New(slog.DiscardHandler),
	})
	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
}

// onlyCall is the one call kept on reading the speech.
func onlyCall(t *testing.T, store *sqlite.Store) editor.Call {
	t.Helper()
	calls, err := store.CallsOn(t.Context(), "reading", speechKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("kept %d calls, want 1: %+v", len(calls), calls)
	}
	return calls[0]
}

func TestAReadingInRealTimeLeavesAKeptCall(t *testing.T) {
	store := record(t)
	m := &answeringModel{res: answered(goodReading)}

	readInRealTime(t, store, m)

	c := onlyCall(t, store)
	if c.BatchID != "" || c.Model != "fake-reading" || !c.At.Equal(saturday) {
		t.Errorf("call in %q on %q at %v", c.BatchID, c.Model, c.At)
	}
	if c.Request.Input != m.sent[0].Input || c.RequestHash != editor.RequestHash(m.sent[0], "fake-reading") {
		t.Errorf("request kept is not the request sent")
	}
	if c.Response == nil || c.Response.Model != "fake-reported" || c.Response.Usage.InputTokens != 1200 ||
		string(c.Response.Raw) != string(answered("").Raw) {
		t.Errorf("response kept %+v", c.Response)
	}
	if c.Outcome != editor.Applied {
		t.Errorf("outcome %q (%q), want applied", c.Outcome, c.Why)
	}
}

func TestAReadingInRealTimeRefusedByItsChecksIsKeptWithItsReason(t *testing.T) {
	store := record(t)

	readInRealTime(t, store, &answeringModel{res: answered(`{"facts": [], "claims": [], "extra": 1}`)})

	if c := onlyCall(t, store); c.Outcome != editor.Refused || !strings.Contains(c.Why, "schema") || c.Response == nil {
		t.Errorf("kept %+v, want refused with why", c)
	}
}

func TestAModelsRefusalInRealTimeIsKeptWithWhatCameBack(t *testing.T) {
	store := record(t)
	refusal := answered("")
	refusal.Output, refusal.StopReason = nil, "refusal"

	readInRealTime(t, store, &answeringModel{res: refusal, err: editor.ErrRefused})

	c := onlyCall(t, store)
	if c.Outcome != editor.Failed || !strings.Contains(c.Why, "refused") {
		t.Errorf("outcome %q (%q), want failed: refused", c.Outcome, c.Why)
	}
	if c.Response == nil || c.Response.StopReason != "refusal" || string(c.Response.Raw) != string(refusal.Raw) {
		t.Errorf("the refusal is not kept: %+v", c.Response)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
