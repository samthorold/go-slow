package editor_test

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// briefer is an Editor over store that writes the Briefing with writer.
func briefer(store *sqlite.Store, writer *fakeModel) (*editor.Editor, *bytes.Buffer) {
	var logs bytes.Buffer
	return editor.New(editor.Deps{
		Store:         store,
		Briefings:     store,
		BriefingModel: writer,
		Log:           slog.New(slog.NewTextHandler(&logs, nil)),
	}), &logs
}

const spokeInMotherwell = `{"stories": [{"story": "S1", "statements": [
	{"text": "The Prime Minister spoke in Motherwell.", "developments": ["D1"]}]}]}`

func TestTheBriefingIsWrittenFromTheDevelopmentsInTheRecordAndKept(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	writer := &fakeModel{output: spokeInMotherwell}
	ed, _ := briefer(store, writer)

	if err := ed.Brief(t.Context(), saturday.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if len(writer.sent) != 1 {
		t.Fatalf("model called %d times, want once", len(writer.sent))
	}
	in := writer.sent[0].Input
	for _, want := range []string{"Andy Burnham in Scotland", "Andy Burnham gave a speech in Motherwell.", "following", "Scotland"} {
		if !strings.Contains(in, want) {
			t.Errorf("the model is not shown %q:\n%s", want, in)
		}
	}
	b, found, err := store.Briefing(t.Context(), "2026-10-24")
	if err != nil || !found {
		t.Fatalf("Briefing: %v %v", found, err)
	}
	if !b.WrittenAt.Equal(saturday.Add(time.Hour)) || b.Model != "fake" {
		t.Errorf("written at %v by %q", b.WrittenAt, b.Model)
	}
	if len(b.Stories) != 1 || b.Stories[0].Story.Title != "Andy Burnham in Scotland" {
		t.Fatalf("Stories = %+v", b.Stories)
	}
	st := b.Stories[0].Statements
	if len(st) != 1 || st[0].Text != "The Prime Minister spoke in Motherwell." || len(st[0].Developments) != 1 {
		t.Fatalf("statements = %+v", st)
	}
	// The statement traces to its Development, Fact and Item.
	d := st[0].Developments[0]
	if d.ID == 0 || d.Statement != "Andy Burnham gave a speech in Motherwell." ||
		d.SourceID != speech.SourceID || d.EntryID != speech.EntryID || d.ItemTitle != speech.Title {
		t.Errorf("the statement does not trace: %+v", d)
	}
}

func TestEachBriefingCoversTheDevelopmentsPlacedSinceTheLast(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	writer := &fakeModel{output: spokeInMotherwell}
	ed, _ := briefer(store, writer)
	if err := ed.Brief(t.Context(), saturday.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	// Nothing has been placed since: Sunday's Briefing says so, with no
	// model call.
	sunday := saturday.AddDate(0, 0, 1)
	if err := ed.Brief(t.Context(), sunday); err != nil {
		t.Fatal(err)
	}

	if len(writer.sent) != 1 {
		t.Errorf("model called %d times, want once: Sunday has nothing to write", len(writer.sent))
	}
	b, found, err := store.Briefing(t.Context(), "2026-10-25")
	if err != nil || !found || len(b.Stories) != 0 {
		t.Errorf("Sunday's Briefing = %+v, %v, %v; want one written empty", b, found, err)
	}
}

func TestAStoryNoTopicSelectsIsNotInTheBriefing(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	// Not following Scotland any more: its Items' ties go with it.
	topics, err := store.Topics(t.Context())
	if err != nil || len(topics) != 1 {
		t.Fatal(topics, err)
	}
	if _, err := store.RemoveTopic(t.Context(), topics[0].ID); err != nil {
		t.Fatal(err)
	}
	writer := &fakeModel{output: spokeInMotherwell}
	ed, _ := briefer(store, writer)

	if err := ed.Brief(t.Context(), saturday.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if len(writer.sent) != 0 {
		t.Errorf("the model was shown a Story no Topic selects:\n%s", writer.sent[0].Input)
	}
}

func TestABriefingThatFailsIsNotWrittenAndTheFailureIsKept(t *testing.T) {
	for name, writer := range map[string]*fakeModel{
		"an untraced statement": {output: `{"stories": [{"story": "S1", "statements": [{"text": "Scotland is the true north.", "developments": []}]}]}`},
		"a failed call":         {err: errors.New("overloaded")},
	} {
		t.Run(name, func(t *testing.T) {
			store := record(t)
			readAndPlace(t, store, &fakeModel{output: startsAStory})
			ed, logs := briefer(store, writer)
			at := saturday.Add(time.Hour)

			if err := ed.Brief(t.Context(), at); err != nil {
				t.Fatal(err)
			}

			if _, found, _ := store.Briefing(t.Context(), "2026-10-24"); found {
				t.Error("a failed Briefing was written")
			}
			f, found, err := store.BriefingFailure(t.Context(), "2026-10-24")
			if err != nil || !found || !f.At.Equal(at) || f.Reason == "" {
				t.Errorf("failure = %+v, %v, %v; want it kept with its time and reason", f, found, err)
			}
			if !strings.Contains(logs.String(), "level=ERROR") {
				t.Errorf("the failure was not logged:\n%s", logs)
			}
		})
	}
}
