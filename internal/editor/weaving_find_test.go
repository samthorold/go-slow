package editor_test

import (
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
)

// findAnswer is the catalogue a Sunday run's find, asked for words, is
// answered with, over a Record holding the speech and the Editor's Story of
// Saturday ("Money follows the Prime Minister to Scotland." / "Motherwell is
// where it lands first.").
func findAnswer(t *testing.T, words string) string {
	t.Helper()
	_, batcher, b, sunday := wovenThenFerries(t)
	runAt(t, b, sunday)
	respondLast(batcher, asking("find", `{"words": "`+words+`"}`))
	runAt(t, b, sunday.Add(time.Hour))
	return answerOf(t, lastSent(t, batcher), "call-1")
}

// An ask's words are found in a Span in any order, and apart (system design:
// A run asks by words, Entities and citations…).
func TestAnAskFindsASpanHoldingItsWordsInAnotherOrderOrApart(t *testing.T) {
	got := findAnswer(t, "Motherwell Scotland")

	if want := "It’s good to be back in Scotland, to be here in Motherwell.\n"; !strings.Contains(got, want) {
		t.Errorf("find answered:\n%s\nwant it to hold %q", got, want)
	}
}

// A Span holding only some of the words matches, and the Stories whose
// Spans hold the most of them are listed first, newest first among equals:
// the speech, older, holds both words in one Span, where the Editor's Story
// holds one in each.
func TestTheStoriesWhoseSpansHoldTheMostOfTheWordsAreListedFirst(t *testing.T) {
	got := findAnswer(t, "Motherwell Scotland")

	speech := strings.Index(got, `from "PM speech in Scotland: 16 October 2026"`)
	editors := strings.Index(got, "the Editor's, made 2026-10-24")
	if speech < 0 || editors < 0 || speech > editors {
		t.Errorf("find answered:\n%s\nwant the speech listed, then the Editor's Story", got)
	}
	if !strings.Contains(got, "(the Editor's): Motherwell is where it lands first.\n") ||
		!strings.Contains(got, "(the Editor's): Money follows the Prime Minister to Scotland.\n") {
		t.Errorf("find answered:\n%s\nwant each Editor's Span holding one of the words", got)
	}
}

// Each matched Span says which of the words it holds, and the catalogue says
// how it is ordered.
func TestEachMatchedSpanSaysWhichOfTheWordsItHolds(t *testing.T) {
	got := findAnswer(t, "Motherwell Scotland")

	want := `2 earlier Stories have Spans holding any of the words "Motherwell Scotland", those holding the most first, newest first among equals.

Story 1, from "PM speech in Scotland: 16 October 2026", published 2026-10-16, from govuk-number-10; cited by 2 later Editor's Spans
  S2: It’s good to be back in Scotland, to be here in Motherwell.
    holding every word: "Motherwell", "Scotland"
  S3: Scotland will get £2 billion.
    holding 1 of the 2 words: "Scotland"

Story 3, the Editor's, made 2026-10-24: "Money follows the Prime Minister to Scotland."; cited by no later Editor's Span
  S4 (the Editor's): Money follows the Prime Minister to Scotland.
    holding 1 of the 2 words: "Scotland"
  S5 (the Editor's): Motherwell is where it lands first.
    holding 1 of the 2 words: "Motherwell"
`
	if got != want {
		t.Errorf("find answered:\n%s\nwant:\n%s", got, want)
	}
	if one := findAnswer(t, "scotland"); !strings.Contains(one, "\n    holding the word \"scotland\"\n") {
		t.Errorf("find answered:\n%s\nwant each Span to say it holds the one word asked", one)
	}
}

// Of a Story with more matched Spans than a catalogue shows, those shown are
// the ones holding the most of the words, in their order in the Story.
func TestACatalogueShowsTheSpansOfAStoryHoldingTheMostOfTheWords(t *testing.T) {
	store := record(t)
	it := ferries
	it.EntryID = "many-ferries"
	it.Content = strings.Repeat("<p>A ferry sails.</p><p>Gulls follow.</p>", 7) + "<p>A ferry sails to Arran.</p>"
	if _, err := store.AddItems(t.Context(), []collect.Item{it}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(store).CutNew(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	answerLast(batcher, `{"stories": []}`)
	runAt(t, b, saturday.Add(time.Hour))
	sunday := saturday.Add(24 * time.Hour)
	cutAt(t, store, sunday)
	runAt(t, b, sunday)
	respondLast(batcher, asking("find", `{"words": "ferry Arran"}`))
	runAt(t, b, sunday.Add(time.Hour))

	got := answerOf(t, lastSent(t, batcher), "call-1")
	if !strings.Contains(got, ": A ferry sails to Arran.\n    holding every word") ||
		strings.Count(got, ": A ferry sails.\n") != 4 || !strings.Contains(got, "… and 3 more Spans of it hold any of the words") {
		t.Errorf("find answered:\n%s\nwant the Span holding both words shown, then 4 holding one, and 3 more said", got)
	}
}

// Words match whole words only: "he" is not found inside "The".
func TestAWordIsNotFoundInsideALongerOne(t *testing.T) {
	got := findAnswer(t, "he lands")

	if strings.Contains(got, "(the Editor's): Money follows") || strings.Contains(got, "speech") ||
		!strings.Contains(got, "(the Editor's): Motherwell is where it lands first.") {
		t.Errorf("find answered:\n%s\nwant only the Span holding \"lands\"", got)
	}
}

// find's description states the rule its words are matched by, and points
// the model at its own Stories as its memory of a subject.
func TestFindsDescriptionStatesHowItsWordsMatch(t *testing.T) {
	_, batcher, b, sunday := wovenThenFerries(t)
	runAt(t, b, sunday)

	rule := "Words are matched as whole words, whatever their case, in Spans as they read with the words between " +
		"⟦ and ⟧, so a name finds the Spans that refer to it in other words. A Span holding any of the words " +
		"matches; those holding the most come first, each saying which of the words it holds. Your own Stories " +
		"are in your words, so asking as you would put the subject finds them."
	for _, tool := range lastSent(t, batcher).Tools {
		if tool.Name == "find" && !strings.HasSuffix(tool.Description, rule) {
			t.Errorf("find's description is:\n%s\nwant it to end:\n%s", tool.Description, rule)
		}
	}
}

// Where the Spans hold as many of the words, the newest Story comes first.
func TestStoriesWhoseSpansHoldAsManyOfTheWordsAreListedNewestFirst(t *testing.T) {
	got := findAnswer(t, "Scotland")

	speech := strings.Index(got, `from "PM speech in Scotland: 16 October 2026"`)
	editors := strings.Index(got, "the Editor's, made 2026-10-24")
	if speech < 0 || editors < 0 || editors > speech {
		t.Errorf("find answered:\n%s\nwant the Editor's Story listed, then the speech", got)
	}
}
