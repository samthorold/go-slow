package editor_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

var briefingDay = time.Date(2026, time.October, 24, 7, 0, 0, 0, time.UTC)

// toTell is it as the Briefing is shown it, read: each of its Spans, the
// Spans numbered in spoken spoken by who, with the relations given for each
// Span by number.
func toTell(it editor.Item, spoken map[int]string, relations map[int][]editor.SpanRelation) editor.ItemToTell {
	tt := editor.ItemToTell{Item: it, Read: true}
	for i := range spansOf(it) {
		s := other(it, i+1)
		s.Speaker = spoken[i+1]
		tt.Spans = append(tt.Spans, editor.SpanToTell{OtherSpan: s, Relations: relations[i+1]})
	}
	return tt
}

// related is a relation of kind from the Span n of from to the Span m of
// to, as a Span's relations give it, seen from the Span of from.
func related(kind string, from editor.Item, n int, to editor.Item, m int) editor.SpanRelation {
	return editor.SpanRelation{
		Relation: editor.Relation{Kind: kind, From: other(from, n).At, To: other(to, m).At},
		Other:    other(to, m),
	}
}

func TestTheBriefingTellsTheSpansItWasShownInStatementsCitingThem(t *testing.T) {
	model := &fakeModel{output: `{"statements": [{"spans": ["S2"], "stories": [],
		"text": "Scotland is among Europe's most centralised nations, the PM said."}]}`}

	b, err := editor.WriteBriefing(t.Context(), model, "claude-test", briefingDay, []editor.ItemToTell{toTell(holyrood, nil, nil)})
	if err != nil {
		t.Fatal(err)
	}

	in := model.sent[0].Input
	for _, want := range []string{"Item: " + holyrood.Title, "Published: 2026-10-16",
		"S1: Today I argue for more powers for the regions.", "S2: Scotland is one of the most centralised nations in Europe."} {
		if !strings.Contains(in, want) {
			t.Errorf("the input lacks %q:\n%s", want, in)
		}
	}
	if len(b.Statements) != 1 || len(b.Statements[0].Cites) != 1 || b.Statements[0].Cites[0].At != other(holyrood, 2).At {
		t.Fatalf("statements = %+v, want one citing S2", b.Statements)
	}
	if b.Day != "2026-10-24" || b.Judge.Act != "briefing" || b.Judge.Version != editor.BriefingVersion ||
		b.Judge.Model != "fake" || b.Judge.RequestHash != editor.RequestHash(model.sent[0], "claude-test") {
		t.Errorf("Briefing for %s by %+v", b.Day, b.Judge)
	}
}

func TestABriefingWithAStatementCitingNothingOrASpanNotShownIsRefusedWhole(t *testing.T) {
	for name, output := range map[string]string{
		"citing nothing": `{"statements": [
			{"spans": ["S1"], "text": "The PM argued for the regions."},
			{"spans": [], "text": "Scotland is the true north."}]}`,
		"a Span not shown": `{"statements": [
			{"spans": ["S1"], "text": "The PM argued for the regions."},
			{"spans": ["S9"], "text": "Scotland is the true north."}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := editor.WriteBriefing(t.Context(), &fakeModel{output: output}, "", briefingDay,
				[]editor.ItemToTell{toTell(holyrood, nil, nil)})
			if !errors.Is(err, editor.ErrFailedChecks) {
				t.Errorf("err = %v, want the Briefing refused", err)
			}
		})
	}
}

func TestWithNothingToTellTheBriefingIsWrittenEmptyWithNoCall(t *testing.T) {
	model := &fakeModel{err: errors.New("a call was made")}

	b, err := editor.WriteBriefing(t.Context(), model, "", briefingDay, nil)

	if err != nil || len(b.Statements) != 0 || len(model.sent) != 0 || b.Day != "2026-10-24" {
		t.Errorf("Briefing %+v, %v, with %d calls; want one written empty, with none", b, err, len(model.sent))
	}
}

func TestEachSpanIsShownWithItsSpeakerAndRelationsAndTheEarlierSpansTheyReach(t *testing.T) {
	model := &fakeModel{output: `{"statements": [{"spans": ["S1", "E1"], "stories": [{"span": "S1", "story": ["E1"]}],
		"text": "The PM argued for the regions, as he said he would."}]}`}
	// The speech's S1 follows the release's S1, which was told before; its S2
	// is the same as the release's S2.
	item := toTell(holyrood, map[int]string{1: "Andy Burnham"}, map[int][]editor.SpanRelation{
		1: {related(editor.Follows, holyrood, 1, pressRelease, 1)},
		2: {related(editor.SameAs, holyrood, 2, pressRelease, 2)},
	})

	b, err := editor.WriteBriefing(t.Context(), model, "", briefingDay, []editor.ItemToTell{item})
	if err != nil {
		t.Fatal(err)
	}

	in := model.sent[0].Input
	for _, want := range []string{
		"S1 (spoken by Andy Burnham): Today I argue for more powers for the regions.\n  follows E1\n",
		"S2: Scotland is one of the most centralised nations in Europe.\n  same as E2\n",
		"Item: " + pressRelease.Title + "\nPublished: 2026-10-14\nE1: The PM will argue for more powers for the regions.\nE2: Scotland",
	} {
		if !strings.Contains(in, want) {
			t.Errorf("the input lacks %q:\n%s", want, in)
		}
	}
	if len(b.Gathered) != 1 || b.Gathered[0].From != other(holyrood, 1).At ||
		len(b.Gathered[0].Story) != 1 || b.Gathered[0].Story[0] != other(pressRelease, 1).At {
		t.Errorf("gathered = %+v, want S1's Story holding the release's S1", b.Gathered)
	}
	if cites := b.Statements[0].Cites; len(cites) != 2 || cites[1].At != other(pressRelease, 1).At {
		t.Errorf("cites = %+v, want an earlier Span shown citable", cites)
	}
}

func TestSameAsAcrossSourcesIsShownAsCorroborated(t *testing.T) {
	model := &fakeModel{output: `{"statements": []}`}
	elsewhere := pressRelease
	elsewhere.SourceID = "parliament"
	item := toTell(holyrood, nil, map[int][]editor.SpanRelation{2: {related(editor.SameAs, holyrood, 2, elsewhere, 2)}})

	if _, err := editor.WriteBriefing(t.Context(), model, "", briefingDay, []editor.ItemToTell{item}); err != nil {
		t.Fatal(err)
	}

	if in := model.sent[0].Input; !strings.Contains(in, "  same as E1, corroborated\n") {
		t.Errorf("corroboration not shown:\n%s", in)
	}
}

func TestAStoryIsGatheredForEachSpanToldOfTheSpansItsRelationsReachOrTheBriefingIsRefused(t *testing.T) {
	item := toTell(holyrood, nil, map[int][]editor.SpanRelation{1: {related(editor.Follows, holyrood, 1, pressRelease, 1)}})
	for name, output := range map[string]string{
		"a Span told, not gathered": `{"statements": [{"spans": ["S1"], "stories": [], "text": "The PM argued."}]}`,
		"a Story of a Span the statement does not tell": `{"statements": [{"spans": ["S1"],
			"stories": [{"span": "S1", "story": []}, {"span": "S2", "story": []}], "text": "The PM argued."}]}`,
		"a Story holding a Span no relation reaches": `{"statements": [{"spans": ["S1"],
			"stories": [{"span": "S1", "story": ["S2"]}], "text": "The PM argued."}]}`,
		"a Story gathered from an earlier Span": `{"statements": [{"spans": ["S1", "E1"],
			"stories": [{"span": "S1", "story": []}, {"span": "E1", "story": []}], "text": "The PM argued."}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := editor.WriteBriefing(t.Context(), &fakeModel{output: output}, "", briefingDay, []editor.ItemToTell{item})
			if !errors.Is(err, editor.ErrFailedChecks) {
				t.Errorf("err = %v, want the Briefing refused", err)
			}
		})
	}
}

func TestASpanToldWithNoRelationsHasAnEmptyStoryWithoutTheModelGatheringIt(t *testing.T) {
	model := &fakeModel{output: `{"statements": [{"spans": ["S2"], "text": "Scotland is centralised, the PM said."}]}`}

	b, err := editor.WriteBriefing(t.Context(), model, "", briefingDay, []editor.ItemToTell{toTell(holyrood, nil, nil)})
	if err != nil {
		t.Fatal(err)
	}

	if len(b.Gathered) != 1 || b.Gathered[0].From != other(holyrood, 2).At || len(b.Gathered[0].Story) != 0 {
		t.Errorf("gathered = %+v, want S2's Story, empty", b.Gathered)
	}
}

func TestAnItemNotYetReadIsShownAsSo(t *testing.T) {
	model := &fakeModel{output: `{"statements": []}`}
	item := toTell(holyrood, nil, nil)
	item.Read = false

	if _, err := editor.WriteBriefing(t.Context(), model, "", briefingDay, []editor.ItemToTell{item}); err != nil {
		t.Fatal(err)
	}

	if in := model.sent[0].Input; !strings.Contains(in, "Item: "+holyrood.Title+" (not yet read)\n") {
		t.Errorf("not shown as unread:\n%s", in)
	}
}
