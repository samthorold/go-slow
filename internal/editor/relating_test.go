package editor_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// The press release a speech follows, and the speech: S1 "The PM will argue
// for more powers for the regions." and S2 "Scotland is one of the most
// centralised nations in Europe." in the release; S1 "Today I argue for more
// powers for the regions." and S2 "Scotland is one of the most centralised
// nations in Europe." in the speech.
var (
	pressRelease = editor.Item{
		SourceID: "govuk-number-10", EntryID: "release", Title: "PM to set out offer to Holyrood",
		Content:   "<p>The PM will argue for more powers for the regions. Scotland is one of the most centralised nations in Europe.</p>",
		Published: time.Date(2026, time.October, 14, 9, 0, 0, 0, time.UTC),
	}
	holyrood = editor.Item{
		SourceID: "govuk-number-10", EntryID: "speech", Title: "PM speech at Holyrood",
		Content:   "<p>Today I argue for more powers for the regions. Scotland is one of the most centralised nations in Europe.</p>",
		Published: time.Date(2026, time.October, 16, 9, 0, 0, 0, time.UTC),
	}
	scotlandEntity = editor.NamedEntity{WikidataID: "Q22", Name: "Scotland", Kind: "place"}
)

// spansOf are it's Spans by the sentence cutter.
func spansOf(it editor.Item) []cut.Span { return cut.Sentences(it.Content) }

// other is the Span n (from 1) of it as relating is shown it, from another
// Item.
func other(it editor.Item, n int) editor.OtherSpan {
	s := spansOf(it)[n-1]
	return editor.OtherSpan{
		At:   editor.Address{SourceID: it.SourceID, EntryID: it.EntryID, Span: s},
		N:    n,
		Item: it, Words: cut.Words(it.Content, s), Entities: []editor.NamedEntity{scotlandEntity},
	}
}

// toRelate is it as relating is given it: each of its Spans, with the
// candidates given for each, by Span number.
func toRelate(it editor.Item, candidates map[int][]editor.OtherSpan) editor.ToRelate {
	tr := editor.ToRelate{Item: it, Reading: editor.Reading{ID: 3, SourceID: it.SourceID, EntryID: it.EntryID}}
	for i, s := range spansOf(it) {
		tr.Spans = append(tr.Spans, editor.SpanToRelate{N: i + 1, Span: s, Candidates: candidates[i+1]})
	}
	return tr
}

func TestRelatingShowsTheItemsSpansAndTheCandidatesFoundForThem(t *testing.T) {
	model := &fakeModel{output: `{"relations": []}`}
	tr := toRelate(holyrood, map[int][]editor.OtherSpan{1: {other(pressRelease, 1)}, 2: {other(pressRelease, 2)}})

	if _, err := editor.Relate(t.Context(), model, "claude-test", tr); err != nil {
		t.Fatal(err)
	}

	in := model.sent[0].Input
	for _, want := range []string{
		"Item: PM speech at Holyrood\n",
		"Published: 2026-10-16\n",
		"S1: Today I argue for more powers for the regions.\n",
		"Item: PM to set out offer to Holyrood\n",
		"Published: 2026-10-14\n",
		"C1: The PM will argue for more powers for the regions.\n",
		"C2: Scotland is one of the most centralised nations in Europe.\n",
	} {
		if !strings.Contains(in, want) {
			t.Errorf("the input does not show %q:\n%s", want, in)
		}
	}
}

func TestFollowsRunsFromTheLaterPublishedSpanToTheEarlier(t *testing.T) {
	// The release arrived last, though published first: relating it, the
	// speech it moves on from is the later.
	tr := toRelate(pressRelease, map[int][]editor.OtherSpan{1: {other(holyrood, 1)}, 2: {other(holyrood, 2)}})
	model := &fakeModel{output: `{"relations": [
		{"span": "S1", "to": "C1", "verdict": "follows"},
		{"span": "S2", "to": "C2", "verdict": "same_as"}
	]}`}

	r, err := editor.Relate(t.Context(), model, "claude-test", tr)
	if err != nil {
		t.Fatal(err)
	}

	if len(r.Relations) != 2 {
		t.Fatalf("relations = %+v", r.Relations)
	}
	follows := r.Relations[0]
	if follows.Kind != editor.Follows || follows.From.EntryID != "speech" || follows.To.EntryID != "release" {
		t.Errorf("follows = %+v, want the speech following the release", follows)
	}
	if words := cut.Words(holyrood.Content, follows.From.Span); words != "Today I argue for more powers for the regions." {
		t.Errorf("follows from %q", words)
	}
	if same := r.Relations[1]; same.Kind != editor.SameAs || same.From.EntryID != "speech" || same.To.EntryID != "release" {
		t.Errorf("same as = %+v", same)
	}
	if r.Judge.Act != "relating" || r.Judge.Version != editor.RelatingVersion || r.Judge.Model != "fake" || r.Judge.RequestHash == "" {
		t.Errorf("judge = %+v", r.Judge)
	}
	if r.ReadingID != 3 || r.SourceID != pressRelease.SourceID || r.EntryID != "release" {
		t.Errorf("the relating is of %s %s on reading %d", r.SourceID, r.EntryID, r.ReadingID)
	}
}

func TestARelationToASpanNotShownIsRefused(t *testing.T) {
	tr := toRelate(holyrood, map[int][]editor.OtherSpan{1: {other(pressRelease, 1)}, 2: {other(pressRelease, 2)}})
	for name, output := range map[string]string{
		"no candidate at all":     `{"relations": [{"span": "S1", "to": "C9", "verdict": "follows"}]}`,
		"a Span of the same Item": `{"relations": [{"span": "S1", "to": "S2", "verdict": "same_as"}]}`,
		"a Span not to relate":    `{"relations": [{"span": "S7", "to": "C1", "verdict": "follows"}]}`,
		"no such verdict":         `{"relations": [{"span": "S1", "to": "C1", "verdict": "updates"}]}`,
		"two verdicts on one pair": `{"relations": [{"span": "S1", "to": "C1", "verdict": "follows"},
			{"span": "S1", "to": "C1", "verdict": "contradicts"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := editor.Relate(t.Context(), &fakeModel{output: output}, "claude-test", tr)
			if !errors.Is(err, editor.ErrFailedChecks) {
				t.Errorf("err = %v, want ErrFailedChecks", err)
			}
		})
	}
}

func TestSameAsAcrossIndependentSourcesIsCorroborationAndWithinOnePublisherIsNot(t *testing.T) {
	speech := editor.Address{SourceID: "govuk-number-10", EntryID: "speech"}
	release := editor.Address{SourceID: "govuk-number-10", EntryID: "release"}
	poll := editor.Address{SourceID: "pollster", EntryID: "poll"}
	for _, tc := range []struct {
		rel  editor.Relation
		want bool
	}{
		{editor.Relation{Kind: editor.SameAs, From: speech, To: release}, false},
		{editor.Relation{Kind: editor.SameAs, From: poll, To: release}, true},
		{editor.Relation{Kind: editor.Follows, From: poll, To: release}, false},
		{editor.Relation{Kind: editor.Contradicts, From: poll, To: release}, false},
	} {
		if got := tc.rel.Corroborates(); got != tc.want {
			t.Errorf("%+v corroborates: %v, want %v", tc.rel, got, tc.want)
		}
	}
}

func TestACandidateOfTheSameItemIsRefused(t *testing.T) {
	// A Store that gave one would be wrong: Spans of one Item are not
	// related to each other.
	same := other(holyrood, 2)
	tr := toRelate(holyrood, map[int][]editor.OtherSpan{1: {same}})
	_, err := editor.Relate(t.Context(), &fakeModel{output: `{"relations": [{"span": "S1", "to": "C1", "verdict": "follows"}]}`}, "claude-test", tr)
	if !errors.Is(err, editor.ErrFailedChecks) {
		t.Errorf("err = %v, want ErrFailedChecks", err)
	}
}
