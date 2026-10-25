package editor_test

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// keepingBriefer is an Editor over store that writes the Briefing with m,
// keeping its calls.
func keepingBriefer(store *sqlite.Store, m editor.Model) *editor.Editor {
	return editor.New(editor.Deps{
		Store: store, Briefings: store, BriefingModel: m, ModelFor: modelFor,
		Log: slog.New(slog.DiscardHandler),
	})
}

// briefingCalls are the calls kept on writing Saturday's Briefing.
func briefingCalls(t *testing.T, store *sqlite.Store) []editor.Call {
	t.Helper()
	calls, err := store.CallsOn(t.Context(), "briefing", "2026-10-24")
	if err != nil {
		t.Fatal(err)
	}
	return calls
}

func TestAWrittenBriefingLeavesAKeptCall(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	m := &answeringModel{res: answered(spokeInMotherwell)}
	at := saturday.Add(time.Hour)

	if err := keepingBriefer(store, m).Brief(t.Context(), at); err != nil {
		t.Fatal(err)
	}

	calls := briefingCalls(t, store)
	if len(calls) != 1 {
		t.Fatalf("kept %d calls, want 1", len(calls))
	}
	c := calls[0]
	if c.BatchID != "" || c.Model != "fake-briefing" || !c.At.Equal(at) {
		t.Errorf("call in %q on %q at %v", c.BatchID, c.Model, c.At)
	}
	if c.Request.Input != m.sent[0].Input || c.RequestHash != editor.RequestHash(m.sent[0], "fake-briefing") {
		t.Errorf("request kept is not the request sent")
	}
	if c.Response == nil || c.Response.Model != "fake-reported" || string(c.Response.Raw) != string(answered("").Raw) {
		t.Errorf("response kept %+v", c.Response)
	}
	if c.Outcome != editor.Applied {
		t.Errorf("outcome %q (%q), want applied", c.Outcome, c.Why)
	}
}

func TestARefusedBriefingAndEachRetryLeaveAKeptCall(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	m := &answeringModel{res: answered(`{"stories": [{"story": "S1", "statements": [{"text": "Scotland is the true north.", "developments": []}]}]}`)}
	ed := keepingBriefer(store, m)
	at := saturday.Add(time.Hour)

	if err := ed.Brief(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	m.res, m.err = editor.Response{}, editor.ErrRefused
	if err := ed.Brief(t.Context(), at.Add(editor.RetryBriefing)); err != nil {
		t.Fatal(err)
	}
	m.res, m.err = answered(spokeInMotherwell), nil
	if err := ed.Brief(t.Context(), at.Add(2*editor.RetryBriefing)); err != nil {
		t.Fatal(err)
	}

	calls := briefingCalls(t, store)
	if len(calls) != 3 {
		t.Fatalf("kept %d calls, want 3: %+v", len(calls), calls)
	}
	if c := calls[0]; c.Outcome != editor.Refused || !strings.Contains(c.Why, "untraced") || c.Response == nil {
		t.Errorf("first call %q (%q), want refused for its untraced statement", c.Outcome, c.Why)
	}
	if c := calls[1]; c.Outcome != editor.Failed || !strings.Contains(c.Why, "refused") || c.Response != nil {
		t.Errorf("second call %q (%q), want failed with no response", c.Outcome, c.Why)
	}
	if c := calls[2]; c.Outcome != editor.Applied || !c.At.Equal(at.Add(2*editor.RetryBriefing)) {
		t.Errorf("third call %q at %v, want applied", c.Outcome, c.At)
	}
}

func TestABriefingWrittenEmptyKeepsNoCall(t *testing.T) {
	store := record(t)
	m := &answeringModel{res: answered(spokeInMotherwell)}

	if err := keepingBriefer(store, m).Brief(t.Context(), saturday.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if _, found, _ := store.Briefing(t.Context(), "2026-10-24"); !found {
		t.Fatal("no Briefing written")
	}
	if calls := briefingCalls(t, store); len(m.sent) != 0 || len(calls) != 0 {
		t.Errorf("%d calls sent and %d kept, want none", len(m.sent), len(calls))
	}
}
