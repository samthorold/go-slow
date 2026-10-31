package editor_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

func TestTheStoryToolShowsASpansLatestGatheringAndTheRelationsSinceWithTheirRefs(t *testing.T) {
	store := toldWeek(t)
	agent := &fakeAgent{turns: []editor.Turn{calls("story", `{"span": "P1"}`, "story", `{"span": "P7"}`), submits(citesTheTold)}}
	ed, logs := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatalf("%v\n%s", err, logs)
	}

	story := toolResult(t, agent, 1, 0)
	for _, want := range []string{"P1's Story, as the Briefing for 2026-10-24 gathered it:",
		"P2 (from \"PM to set out offer to Holyrood\"; published 2026-10-14; names Scotland): Scotland is one of the most centralised nations in Europe."} {
		if story.IsError || !strings.Contains(story.Content, want) {
			t.Errorf("the story tool does not show %q:\n%s", want, story.Content)
		}
	}
	// A ref it was not shown is told back as an error, to try again.
	if bad := toolResult(t, agent, 1, 1); !bad.IsError || !strings.Contains(bad.Content, "P7") {
		t.Errorf("a ref not shown = %+v", bad)
	}
	// The Span the gathering holds was shown, so the read may cite it.
	if _, found, err := store.SundayRead(t.Context(), "2026-10-25"); err != nil || !found {
		t.Errorf("SundayRead: %v %v\n%s", found, err, logs)
	}
}

func TestTheStoryToolShowsTheRelationsOfASpanNotYetGathered(t *testing.T) {
	store := toldWeek(t)
	// P2, the offer's Span, was gathered by nothing: its Story is its
	// relations, worded from its side.
	agent := &fakeAgent{turns: []editor.Turn{calls("story", `{"span": "P1"}`), calls("story", `{"span": "P2"}`), submits(citesTheTold)}}
	ed, logs := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatalf("%v\n%s", err, logs)
	}

	got := toolResult(t, agent, 2, 0).Content
	for _, want := range []string{"P2's Story has not been gathered. Its relations:", "followed by P1 "} {
		if !strings.Contains(got, want) {
			t.Errorf("the story tool does not show %q:\n%s", want, got)
		}
	}
}

func TestSpansNamingAnEntityAreFoundByTheMentionViewBackThroughTime(t *testing.T) {
	store := toldWeek(t)
	agent := &fakeAgent{turns: []editor.Turn{
		calls("spans_naming", `{"entity": "scotland", "before": ""}`,
			"spans_naming", `{"entity": "Scotland", "before": "2026-10-15"}`,
			"spans_naming", `{"entity": "Finland", "before": ""}`),
		submits(`{"statements": [{"text": "Scotland has been the PM's theme all week.", "cites": ["P1", "P2"], "stories": []}],
			"assessments": []}`),
	}}
	ed, logs := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatalf("%v\n%s", err, logs)
	}

	all := toolResult(t, agent, 1, 0).Content
	// Every Span naming Scotland, the latest Items first: the speech's
	// first, then the offer's; each with its ref.
	speechAt := strings.Index(all, "back in Scotland")
	offerAt := strings.Index(all, "centralised nations")
	if speechAt < 0 || offerAt < speechAt || !strings.Contains(all, "P2 (") {
		t.Errorf("the Spans naming Scotland:\n%s", all)
	}
	if before := toolResult(t, agent, 1, 1).Content; strings.Contains(before, "Motherwell") || !strings.Contains(before, "centralised") {
		t.Errorf("the Spans naming Scotland before the speech:\n%s", before)
	}
	if unknown := toolResult(t, agent, 1, 2); !unknown.IsError {
		t.Errorf("an Entity no Span shown names = %+v", unknown)
	}
	r, found, err := store.SundayRead(t.Context(), "2026-10-25")
	if err != nil || !found || len(r.Statements[0].Cites) != 2 {
		t.Fatalf("SundayRead: %+v %v %v\n%s", r, found, err, logs)
	}
}

func TestTheAgentLooksThingsUpInWikidataAndCitesTheLookup(t *testing.T) {
	store := toldWeek(t)
	wd := &fakeWikidata{
		search: map[string][]string{"Andy Burnham": {"Q123"}},
		items: map[string]editor.WikidataItem{"Q123": {
			ID: "Q123", Label: "Andy Burnham", Aliases: []string{"Andrew Burnham"}, InstanceOf: []string{"Q5"}, Revision: 42,
			Positions: []editor.Tenure{{Holder: "Q123", Office: "Q14211", Start: time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC), StartToTheDay: true}},
		}},
		labels: map[string]string{"Q123": "Andy Burnham", "Q14211": "Prime Minister of the United Kingdom", "Q5": "human"},
	}
	agent := &fakeAgent{turns: []editor.Turn{
		calls("wikidata_search", `{"text": "Andy Burnham"}`),
		calls("wikidata_item", `{"id": "Q123"}`),
		submits(`{"statements": [{"text": "Andy Burnham has been Prime Minister since July.", "cites": ["L1", "P1"], "stories": []}],
			"assessments": []}`),
	}}
	ed, logs := readerWith(store, agent, wd)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	if got := toolResult(t, agent, 1, 0).Content; !strings.Contains(got, "Q123: Andy Burnham") {
		t.Errorf("the search shows:\n%s", got)
	}
	got := toolResult(t, agent, 2, 0).Content
	for _, want := range []string{"L1", "Q123", "revision 42", "human", "Prime Minister of the United Kingdom", "2026-07-20"} {
		if !strings.Contains(got, want) {
			t.Errorf("the item does not show %q:\n%s", want, got)
		}
	}
	r, found, err := store.SundayRead(t.Context(), "2026-10-25")
	if err != nil || !found {
		t.Fatalf("SundayRead: %v %v\n%s", found, err, logs)
	}
	// The lookup is kept, as the item and revision seen, under its licence.
	c := r.Statements[0].Cites[0]
	if c.Kind != editor.CitesLookup || c.Lookup.ID == 0 || c.Lookup.ItemID != "Q123" || c.Lookup.Revision != 42 ||
		c.Lookup.Reference != "Wikidata" || c.Lookup.Licence != editor.WikidataLicence || !c.Lookup.At.Equal(sundayMorning) {
		t.Errorf("cites %+v, want the lookup", c)
	}
}

func TestTheAgentsToolsOnlyRead(t *testing.T) {
	store := toldWeek(t)
	agent := &fakeAgent{turns: []editor.Turn{submits(citesTheTold)}}
	ed, _ := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, tool := range agent.sent[0].Tools {
		names = append(names, tool.Name)
	}
	want := []string{"story", "spans_naming", "assessments", "wikidata_search", "wikidata_item", "submit_read"}
	if !slices.Equal(names, want) {
		t.Errorf("tools = %v, want %v", names, want)
	}
}
