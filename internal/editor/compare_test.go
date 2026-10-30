package editor_test

import (
	"testing"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

func TestComparingReadingsCountsEachKindOfJudgementEachMadeAndBothMade(t *testing.T) {
	addr := func(start, end int) editor.Address {
		return editor.Address{SourceID: "s", EntryID: "e", Span: cut.Span{Start: start, End: end}}
	}
	derby := editor.Name{At: addr(0, 5), Name: "Derby", Kind: "place"}
	live := editor.Reading{
		Names:    []editor.Name{derby, {At: addr(10, 15), Name: "Leeds", Kind: "place"}},
		RefersTo: []editor.RefersTo{{From: addr(20, 28), To: addr(0, 5)}},
		Rejected: []editor.Address{addr(30, 40)},
	}
	candidate := editor.Reading{
		Names:    []editor.Name{derby, {At: addr(10, 15), Name: "Leeds", Kind: "organisation"}},
		Speakers: []editor.Speaker{{Of: addr(30, 40), By: addr(0, 5)}},
		Rejected: []editor.Address{addr(30, 40)},
	}

	got := editor.CompareReadings(live, candidate)

	want := []editor.KindCompared{
		{Kind: "names", Live: 2, Candidate: 2, Both: 1},
		{Kind: "refers_to", Live: 1, Candidate: 0, Both: 0},
		{Kind: "speakers", Live: 0, Candidate: 1, Both: 0},
		{Kind: "rejected", Live: 1, Candidate: 1, Both: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("compared %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: %+v, want %+v", want[i].Kind, got[i], want[i])
		}
	}
}
