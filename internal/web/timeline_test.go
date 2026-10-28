package web

import (
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

// development is a Development of the Fact id, drawn from the Item entry
// and holding as at day d of October 2026.
func development(id int64, entry string, d int) editor.Development {
	return editor.Development{
		Fact: editor.Fact{ID: id, SourceID: "number-10", EntryID: entry, Statement: "Fact " + entry,
			AsAt: time.Date(2026, time.October, d, 0, 0, 0, 0, time.UTC)},
		ItemTitle: "Item " + entry,
	}
}

// shape is each entry's Item and its Developments' Fact ids.
func shape(entries []timelineEntry) [][]any {
	var out [][]any
	for _, e := range entries {
		row := []any{e.Item.Title}
		for _, d := range e.Developments {
			row = append(row, d.Anchor)
		}
		out = append(out, row)
	}
	return out
}

func TestATimelineIsOneEntryPerRunOfDevelopmentsFromOneItem(t *testing.T) {
	ds := []editor.Development{
		development(1, "a", 20), development(2, "a", 20), development(3, "b", 21),
		development(4, "a", 22), development(5, "a", 22),
	}

	got := timelineOf(ds, developmentView)

	want := [][]any{
		{"Item a", developmentAnchor(1), developmentAnchor(2)},
		{"Item b", developmentAnchor(3)},
		{"Item a", developmentAnchor(4), developmentAnchor(5)},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", shape(got), want)
	}
	for i := range want {
		g := shape(got)[i]
		if len(g) != len(want[i]) {
			t.Fatalf("entry %d: got %v, want %v", i, g, want[i])
		}
		for j := range g {
			if g[j] != want[i][j] {
				t.Errorf("entry %d: got %v, want %v", i, g, want[i])
			}
		}
	}
	if got[0].AsAt != "20 October 2026" || got[0].Item.Href != "/items/number-10/a" {
		t.Errorf("the first entry's head is %q, %q", got[0].AsAt, got[0].Item.Href)
	}
}

func TestADevelopmentShowsItsDateOnlyWhereItDiffersFromItsEntrys(t *testing.T) {
	ds := []editor.Development{development(1, "a", 20), development(2, "a", 21)}

	got := timelineOf(ds, developmentView)

	if len(got) != 1 {
		t.Fatalf("want one entry, got %v", shape(got))
	}
	if same, differs := got[0].Developments[0].AsAt, got[0].Developments[1].AsAt; same != "" || differs != "21 October 2026" {
		t.Errorf("the Developments' own dates are %q and %q, want none and 21 October 2026", same, differs)
	}
}
