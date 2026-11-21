package editor_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// citedThenCitedAgain is a Record whose Saturday run made oneOpinion's
// Story, citing the speech; whose Sunday run, shown the ferries, made a
// Story citing them and the Saturday Story's second Editor's Span; with
// ferries again cut on Monday, and the runner whose Monday run is next.
func citedThenCitedAgain(t *testing.T) (*sqlite.Store, *fakeBatcher, *editor.Batches, time.Time) {
	t.Helper()
	store, batcher, b, sunday := wovenThenFerries(t)
	runAt(t, b, sunday)
	respondLast(batcher, asking("find", `{"words": "motherwell"}`))
	runAt(t, b, sunday.Add(time.Hour))
	answerLast(batcher, `{"stories": [{"editor_spans": [
	  {"body": "The money, Motherwell and the ferries are one promise.", "cites": [1, 2]}
	]}]}`)
	runAt(t, b, sunday.Add(2*time.Hour))
	if len(madeStories(t, store)) != 2 {
		t.Fatal("Sunday's run made no Story")
	}
	monday := sunday.Add(24 * time.Hour)
	it := ferries
	it.EntryID, it.CollectedAt = "ferries-again", monday
	if _, err := store.AddItems(t.Context(), []collect.Item{it}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(store).CutNew(t.Context(), monday); err != nil {
		t.Fatal(err)
	}
	return store, batcher, b, monday
}

// cutOf is the id of the Story the newest Cut of the Item made.
func cutOf(t *testing.T, store *sqlite.Store, sourceID, entryID string) int64 {
	t.Helper()
	c, _, _, err := store.NewestCut(t.Context(), sourceID, entryID)
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}

// A run asks what cites a Story it was shown, and is answered with a
// catalogue of the Stories holding an Editor's Span citing one of its Spans:
// a Story a Cut made is cited by the Editor's Spans citing its Spans, and
// one the Editor made by those citing its Editor's Spans (system design: A
// run asks by words, Entities and citations, and is answered with a
// catalogue).
func TestARunAsksWhatCitesAStoryItWasShownAndFindsTheStoriesCitingIt(t *testing.T) {
	store, batcher, b, monday := citedThenCitedAgain(t)
	made := madeStories(t, store)
	sundays, saturdays := made[0], made[1]
	speechCut := cutOf(t, store, speech.SourceID, speech.EntryID)
	runAt(t, b, monday)
	respondLast(batcher, asking("find", `{"words": "motherwell"}`))
	runAt(t, b, monday.Add(time.Hour))
	respondLast(batcher, asking(
		"find_citing", fmt.Sprintf(`{"story": %d}`, speechCut),
		"find_citing", fmt.Sprintf(`{"story": %d}`, saturdays.ID),
		"find_citing", fmt.Sprintf(`{"story": %d}`, sundays.ID),
	))
	runAt(t, b, monday.Add(2*time.Hour))
	next := lastSent(t, batcher)

	speechCiting := resultOf(t, next, "call-1")
	for _, want := range []string{
		fmt.Sprintf("1 earlier Story has Spans citing a Span of Story %d.\n", speechCut),
		fmt.Sprintf("Story %d, the Editor's, made 2026-10-24: \"Money follows the Prime Minister to Scotland.\"", saturdays.ID),
		"(the Editor's): Money follows the Prime Minister to Scotland.\n",
		"(the Editor's): Motherwell is where it lands first.\n",
	} {
		if speechCiting.IsError || !strings.Contains(speechCiting.Content, want) {
			t.Errorf("what cites the speech:\n%s\nwant it to hold %q", speechCiting.Content, want)
		}
	}
	saturdayCiting := resultOf(t, next, "call-2")
	for _, want := range []string{
		fmt.Sprintf("1 earlier Story has Spans citing a Span of Story %d.\n", saturdays.ID),
		fmt.Sprintf("Story %d, the Editor's, made 2026-10-25: \"The money, Motherwell and the ferries are one promise.\"", sundays.ID),
		"(the Editor's): The money, Motherwell and the ferries are one promise.\n",
	} {
		if saturdayCiting.IsError || !strings.Contains(saturdayCiting.Content, want) {
			t.Errorf("what cites Saturday's Story:\n%s\nwant it to hold %q", saturdayCiting.Content, want)
		}
	}
	if strings.Contains(saturdayCiting.Content, "Money follows") {
		t.Errorf("what cites Saturday's Story lists it:\n%s", saturdayCiting.Content)
	}
	if got := resultOf(t, next, "call-3"); got.IsError ||
		got.Content != fmt.Sprintf("No earlier Story has a Span citing a Span of Story %d.", sundays.ID) {
		t.Errorf("what cites Sunday's Story, which nothing cites, is answered %+v, want an empty catalogue", got)
	}
}

// Asking what cites a Story no answer named is an error the run reads, as
// an ask by an Entity it was never shown is, and naming the number in the
// error never counts as showing the Story.
func TestAskingWhatCitesAStoryTheRunWasNeverShownIsAnErrorItReads(t *testing.T) {
	store, batcher, b, monday := citedThenCitedAgain(t)
	speechCut := cutOf(t, store, speech.SourceID, speech.EntryID)
	runAt(t, b, monday)
	ask := asking("find_citing", fmt.Sprintf(`{"story": %d}`, speechCut))
	respondLast(batcher, ask)
	runAt(t, b, monday.Add(time.Hour))
	if got := resultOf(t, lastSent(t, batcher), "call-1"); !got.IsError ||
		!strings.Contains(got.Content, fmt.Sprintf("No Story shown to you is numbered %d", speechCut)) {
		t.Errorf("asking what cites the speech, never shown, is answered %+v, want an error", got)
	}
	respondLast(batcher, ask)
	runAt(t, b, monday.Add(2*time.Hour))
	if got := resultOf(t, lastSent(t, batcher), "call-1"); !got.IsError {
		t.Errorf("asking again, after an error named it, is answered %+v, want an error", got)
	}
	// Opened from a catalogue, or listed in one, a Story is shown.
	respondLast(batcher, asking("open_story", fmt.Sprintf(`{"story": %d}`, speechCut)))
	runAt(t, b, monday.Add(3*time.Hour))
	respondLast(batcher, ask)
	runAt(t, b, monday.Add(4*time.Hour))
	if got := resultOf(t, lastSent(t, batcher), "call-1"); got.IsError {
		t.Errorf("asking what cites the speech once opened is answered %+v, want a catalogue", got)
	}
}
