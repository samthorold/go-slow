package editor_test

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// briefer is an Editor over store that writes the Briefing with writer.
func briefer(store *sqlite.Store, writer editor.Model) (*editor.Editor, *bytes.Buffer) {
	var logs bytes.Buffer
	return editor.New(editor.Deps{
		Store:         store,
		Briefings:     store,
		BriefingModel: writer,
		ModelFor:      modelFor,
		Log:           slog.New(slog.NewTextHandler(&logs, nil)),
	}), &logs
}

// spokeInMotherwell tells the speech's first Span, S1 "It’s good to be back
// in Scotland, to be here in Motherwell.", which has no relations.
const spokeInMotherwell = `{"statements": [{"spans": ["S1"], "stories": [], "text": "The Prime Minister spoke in Motherwell."}]}`

// speechSpan is the Span n (from 1) of the speech.
func speechSpan(n int) editor.Address {
	return editor.Address{SourceID: speech.SourceID, EntryID: speech.EntryID, Span: cut.Sentences(speech.Content)[n-1]}
}

func TestTheBriefingIsWrittenOverSpansCutButNotYetReadAndKeptWithItsJudge(t *testing.T) {
	store := record(t)
	writer := &fakeModel{output: spokeInMotherwell}
	ed, _ := briefer(store, writer)
	at := saturday.Add(time.Hour)

	if err := ed.Brief(t.Context(), at); err != nil {
		t.Fatal(err)
	}

	if len(writer.sent) != 1 {
		t.Fatalf("model called %d times, want once", len(writer.sent))
	}
	in := writer.sent[0].Input
	for _, want := range []string{"Item: " + speech.Title + " (not yet read)", "S1: It’s good to be back in Scotland, to be here in Motherwell."} {
		if !strings.Contains(in, want) {
			t.Errorf("the model is not shown %q:\n%s", want, in)
		}
	}
	b, found, err := store.Briefing(t.Context(), "2026-10-24")
	if err != nil || !found {
		t.Fatalf("Briefing: %v %v", found, err)
	}
	if !b.WrittenAt.Equal(at) || b.Judge.Model != "fake" || b.Judge.Version != editor.BriefingVersion ||
		b.Judge.RequestHash != editor.RequestHash(writer.sent[0], "fake-briefing") || b.Call == 0 {
		t.Errorf("written at %v by %+v, call %d", b.WrittenAt, b.Judge, b.Call)
	}
	if len(b.Statements) != 1 || b.Statements[0].Text != "The Prime Minister spoke in Motherwell." ||
		len(b.Statements[0].Cites) != 1 || b.Statements[0].Cites[0].At != speechSpan(1) ||
		b.Statements[0].Cites[0].Item.Title != speech.Title {
		t.Fatalf("statements = %+v", b.Statements)
	}
	if len(b.Gathered) != 1 || b.Gathered[0].From != speechSpan(1) || len(b.Gathered[0].Story) != 0 || b.Gathered[0].ID == 0 {
		t.Errorf("gathered = %+v, want S1's empty Story", b.Gathered)
	}
}

func TestEachBriefingCoversTheSpansCutSinceTheLastAndOneWithNoneIsWrittenEmptyWithNoCall(t *testing.T) {
	store := record(t)
	writer := &fakeModel{output: spokeInMotherwell}
	ed, _ := briefer(store, writer)
	if err := ed.Brief(t.Context(), saturday.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	writer.err = errors.New("a call was made")
	sunday := saturday.AddDate(0, 0, 1)
	if err := ed.Brief(t.Context(), sunday); err != nil {
		t.Fatal(err)
	}

	b, found, err := store.Briefing(t.Context(), "2026-10-25")
	if err != nil || !found || len(b.Statements) != 0 || len(writer.sent) != 1 || b.Call != 0 {
		t.Errorf("Sunday's Briefing = %+v, %v, %v, after %d calls; want one written empty with no call", b, found, err, len(writer.sent))
	}
}

func TestABriefingThatFailsIsNotWrittenAndTheFailureIsKept(t *testing.T) {
	store := record(t)
	ed, logs := briefer(store, &fakeModel{output: `{"statements": [{"spans": [], "text": "Scotland is the true north."}]}`})
	at := saturday.Add(time.Hour)

	if err := ed.Brief(t.Context(), at); err != nil {
		t.Fatal(err)
	}

	if _, found, _ := store.Briefing(t.Context(), "2026-10-24"); found {
		t.Error("a refused Briefing was written")
	}
	f, failed, err := store.BriefingFailure(t.Context(), "2026-10-24")
	if err != nil || !failed || !f.At.Equal(at) || !strings.Contains(f.Reason, "untraced statement") {
		t.Errorf("failure = %+v, %v, %v", f, failed, err)
	}
	if !strings.Contains(logs.String(), "writing the Briefing failed") {
		t.Errorf("not logged: %s", logs.String())
	}
	calls, err := store.CallsOn(t.Context(), "briefing", "2026-10-24")
	if err != nil || len(calls) != 1 || calls[0].Outcome != editor.Refused {
		t.Errorf("calls = %+v, %v; want the refused one kept", calls, err)
	}
}
