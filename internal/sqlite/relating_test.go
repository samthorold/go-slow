package sqlite

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// relatingRecord is a store with the Source the relating tests' Items are
// collected from.
func relatingRecord(t *testing.T) *Store {
	t.Helper()
	s := openTestStore(t)
	src := collect.Source{ID: "number-10", Name: "No 10", FeedURL: "https://example.org/feed", UsualGap: time.Hour}
	if err := s.SyncSources(t.Context(), []collect.Source{src}, cutAt); err != nil {
		t.Fatal(err)
	}
	return s
}

// collected is an Item with text, published on published, collected and
// cut.
func collected(t *testing.T, s *Store, entry, text string, published time.Time) cut.Cut {
	t.Helper()
	it := collect.Item{SourceID: "number-10", EntryID: entry, Title: "Title of " + entry, Content: text,
		Published: published, CollectedAt: cutAt}
	if _, err := s.AddItems(t.Context(), []collect.Item{it}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(s).CutNew(t.Context(), cutAt); err != nil {
		t.Fatal(err)
	}
	c, _, _, err := s.NewestCut(t.Context(), "number-10", entry)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// named is a name a reading judges: its words, wherever they first are in
// the Item, and the Wikidata item a look-up matches it to ("" for none
// looked up).
type named struct{ words, kind, item string }

// read keeps a reading of the Item entry, cut as c, naming each name in the
// first Span holding its words, rejecting the Spans numbered in rejected,
// and keeps a look-up of each name with an item.
func read(t *testing.T, s *Store, entry, text string, c cut.Cut, names []named, rejected ...int) editor.Reading {
	t.Helper()
	r := editor.Reading{SourceID: "number-10", EntryID: entry, CutID: c.ID, At: cutAt.Add(time.Hour),
		Judge: editor.Judge{Act: "reading", Version: 1, Model: "claude-test", RequestHash: "hash-" + entry}}
	for _, n := range names {
		found := false
		for _, sp := range c.Spans {
			if w, ok := cut.Within(text, sp, n.words); ok {
				r.Names = append(r.Names, editor.Name{At: editor.Address{SourceID: "number-10", EntryID: entry, Span: w}, Name: n.words, Kind: n.kind})
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%q is not in %s", n.words, entry)
		}
	}
	for _, n := range rejected {
		r.Rejected = append(r.Rejected, editor.Address{SourceID: "number-10", EntryID: entry, Span: c.Spans[n-1]})
	}
	if err := s.KeepReading(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if n.item == "" {
			continue
		}
		if err := s.KeepLookup(t.Context(), editor.NameLookup{Key: editor.LookupKey{Name: n.words, Kind: n.kind},
			Judge: editor.Looker, At: cutAt, WikidataID: n.item, Label: n.words}); err != nil {
			t.Fatal(err)
		}
	}
	got, _, err := s.NewestReading(t.Context(), "number-10", entry)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

var (
	monday    = time.Date(2026, time.October, 12, 9, 0, 0, 0, time.UTC)
	tuesday   = monday.AddDate(0, 0, 1)
	wednesday = monday.AddDate(0, 0, 2)
)

const (
	tuesdayText = "<p>The PM will argue for more powers for Scotland. Derby gets a headquarters.</p>"
	mondayText  = "<p>Scotland is one of the most centralised nations in Europe. The weather was fine.</p>"
)

var (
	scotland = named{"Scotland", "place", "Q22"}
	derby    = named{"Derby", "place", "Q43475"}
)

func entries(spans []editor.OtherSpan) []string {
	var out []string
	for _, o := range spans {
		out = append(out, o.At.EntryID)
	}
	return out
}

func TestAnItemAwaitsRelatingOnceReadAndWhatItNamesLookedUp(t *testing.T) {
	s := relatingRecord(t)
	c := collected(t, s, "tuesday", tuesdayText, tuesday)
	if waiting, err := s.ItemsToRelate(t.Context()); err != nil || len(waiting) != 0 {
		t.Fatalf("an unread Item awaits relating: %+v %v", waiting, err)
	}
	read(t, s, "tuesday", tuesdayText, c, []named{scotland, {"Derby", "place", ""}})
	if waiting, err := s.ItemsToRelate(t.Context()); err != nil || len(waiting) != 0 {
		t.Fatalf("an Item with a name not looked up awaits relating: %+v %v", waiting, err)
	}
	if err := s.KeepLookup(t.Context(), editor.NameLookup{Key: editor.LookupKey{Name: "Derby", Kind: "place"},
		Judge: editor.Looker, At: cutAt}); err != nil {
		t.Fatal(err)
	}

	waiting, err := s.ItemsToRelate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(waiting) != 1 || waiting[0].Item.EntryID != "tuesday" || waiting[0].Reading.ID == 0 || waiting[0].ReadOrder == 0 {
		t.Fatalf("awaiting relating: %+v", waiting)
	}
	tr := waiting[0]
	if len(tr.Spans) != 2 || tr.Spans[0].N != 1 || tr.Spans[1].N != 2 {
		t.Fatalf("Spans to relate: %+v", tr.Spans)
	}
	if !slices.ContainsFunc(tr.Spans[0].Entities, func(e editor.NamedEntity) bool { return e.WikidataID == "Q22" }) {
		t.Errorf("S1 mentions %+v, not Scotland", tr.Spans[0].Entities)
	}
	if !slices.ContainsFunc(tr.Spans[1].Entities, func(e editor.NamedEntity) bool { return e.Name == "Derby" && e.EditorID != 0 }) {
		t.Errorf("S2 mentions %+v, not the Editor's Derby", tr.Spans[1].Entities)
	}

	if err := s.KeepRelating(t.Context(), editor.Relating{SourceID: "number-10", EntryID: "tuesday", ReadingID: tr.Reading.ID,
		Judge: editor.Judge{Act: "relating", Version: 1}, At: cutAt}); err != nil {
		t.Fatal(err)
	}
	if waiting, err := s.ItemsToRelate(t.Context()); err != nil || len(waiting) != 0 {
		t.Errorf("an Item once related still awaits relating: %+v %v", waiting, err)
	}
}

func TestARejectedSpanIsNotRelatedNorACandidate(t *testing.T) {
	s := relatingRecord(t)
	read(t, s, "monday", mondayText, collected(t, s, "monday", mondayText, monday), []named{scotland}, 1)
	read(t, s, "tuesday", tuesdayText, collected(t, s, "tuesday", tuesdayText, tuesday), []named{scotland}, 2)

	waiting, err := s.ItemsToRelate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range waiting {
		want := []int{1}
		if tr.Item.EntryID == "monday" {
			want = []int{2}
		}
		var got []int
		for _, sp := range tr.Spans {
			got = append(got, sp.N)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s's Spans to relate are %v, want %v", tr.Item.EntryID, got, want)
		}
	}
	mentioning, err := s.SpansMentioning(t.Context(), []editor.NamedEntity{{WikidataID: "Q22", Name: "Scotland", Kind: "place"}}, 1<<40)
	if err != nil {
		t.Fatal(err)
	}
	if len(mentioning) != 1 || mentioning[0].At.EntryID != "tuesday" || mentioning[0].N != 1 {
		t.Errorf("Spans mentioning Scotland: %+v", mentioning)
	}
}

func TestSpansMentioningAnEntityAreThoseOfItemsReadBefore(t *testing.T) {
	s := relatingRecord(t)
	// Tuesday's is read first, then Monday's, published earlier.
	read(t, s, "tuesday", tuesdayText, collected(t, s, "tuesday", tuesdayText, tuesday), []named{scotland, derby})
	read(t, s, "monday", mondayText, collected(t, s, "monday", mondayText, monday), []named{scotland})
	waiting, err := s.ItemsToRelate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	order := map[string]int64{}
	for _, tr := range waiting {
		order[tr.Item.EntryID] = tr.ReadOrder
	}
	if order["tuesday"] == 0 || order["tuesday"] >= order["monday"] {
		t.Fatalf("read order = %v, want Tuesday's read first", order)
	}
	scot := editor.NamedEntity{WikidataID: "Q22", Name: "Scotland", Kind: "place"}

	got, err := s.SpansMentioning(t.Context(), []editor.NamedEntity{scot}, order["monday"])
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(entries(got), []string{"tuesday"}) {
		t.Fatalf("relating Monday's, the Spans mentioning Scotland are of %v, want Tuesday's", entries(got))
	}
	m := got[0]
	if m.N != 1 || m.Item.Title != "Title of tuesday" || !m.Item.Published.Equal(tuesday) ||
		m.Words != "The PM will argue for more powers for Scotland." || !slices.Contains(m.Entities, scot) {
		t.Errorf("the Span = %+v", m)
	}
	if got, err := s.SpansMentioning(t.Context(), []editor.NamedEntity{scot}, order["tuesday"]); err != nil || len(got) != 0 {
		t.Errorf("relating Tuesday's, Monday's Spans are candidates, read after it: %v %v", entries(got), err)
	}

	counts, err := s.ItemsMentioning(t.Context(), []editor.NamedEntity{scot, {WikidataID: "Q43475", Name: "Derby", Kind: "place"}}, order["monday"])
	if err != nil {
		t.Fatal(err)
	}
	if counts[scot] != 2 || counts[editor.NamedEntity{WikidataID: "Q43475", Name: "Derby", Kind: "place"}] != 1 {
		t.Errorf("Items mentioning = %v", counts)
	}
}

func TestARelatingIsKeptWithEachRelationAsAnEventAndShownBothWays(t *testing.T) {
	s := relatingRecord(t)
	tc := collected(t, s, "tuesday", tuesdayText, tuesday)
	read(t, s, "tuesday", tuesdayText, tc, []named{scotland})
	mc := collected(t, s, "monday", mondayText, monday)
	mr := read(t, s, "monday", mondayText, mc, []named{scotland})
	tue := editor.Address{SourceID: "number-10", EntryID: "tuesday", Span: tc.Spans[0]}
	mon := editor.Address{SourceID: "number-10", EntryID: "monday", Span: mc.Spans[0]}
	r := editor.Relating{SourceID: "number-10", EntryID: "monday", ReadingID: mr.ID, At: cutAt.Add(2 * time.Hour),
		Judge:     editor.Judge{Act: "relating", Version: 1, Model: "claude-test", RequestHash: "rel-hash"},
		Relations: []editor.Relation{{Kind: editor.Follows, From: tue, To: mon}}}

	if err := s.KeepRelating(t.Context(), r); err != nil {
		t.Fatal(err)
	}

	for kind, want := range map[string]int{"relating": 1, "relation": 1} {
		var n int
		if err := s.db.QueryRowContext(t.Context(), `SELECT count(*) FROM events WHERE kind = ?`, kind).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Errorf("%d %s events, want %d", n, kind, want)
		}
	}
	for _, tc := range []struct {
		of        editor.Address
		dir       string
		otherWord string
	}{{tue, "from", "Scotland is one"}, {mon, "to", "The PM will argue"}} {
		got, err := s.RelationsOf(t.Context(), tc.of)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("relations of %s: %+v", tc.of.EntryID, got)
		}
		g := got[0]
		if g.Relation.Kind != editor.Follows || g.Relation.From != tue || g.Relation.To != mon || g.Judge != r.Judge || g.RelatingID == 0 {
			t.Errorf("relation of %s = %+v", tc.of.EntryID, g)
		}
		if !strings.HasPrefix(g.Other.Words, tc.otherWord) || g.Other.Item.Title == "" {
			t.Errorf("its other Span = %+v", g.Other)
		}
	}
}

func TestARelatingOfOneItemsSpansToEachOtherOrNotOfItsOwnItemIsRefused(t *testing.T) {
	s := relatingRecord(t)
	tc := collected(t, s, "tuesday", tuesdayText, tuesday)
	tr := read(t, s, "tuesday", tuesdayText, tc, []named{scotland})
	mc := collected(t, s, "monday", mondayText, monday)
	read(t, s, "monday", mondayText, mc, []named{scotland})
	addr := func(entry string, c cut.Cut, n int) editor.Address {
		return editor.Address{SourceID: "number-10", EntryID: entry, Span: c.Spans[n-1]}
	}
	for name, rel := range map[string]editor.Relation{
		"within one Item":     {Kind: editor.SameAs, From: addr("tuesday", tc, 1), To: addr("tuesday", tc, 2)},
		"not of its own Item": {Kind: editor.SameAs, From: addr("monday", mc, 1), To: addr("monday", mc, 2)},
	} {
		err := s.KeepRelating(t.Context(), editor.Relating{SourceID: "number-10", EntryID: "tuesday", ReadingID: tr.ID, At: cutAt,
			Judge: editor.Judge{Act: "relating", Version: 1}, Relations: []editor.Relation{rel}})
		if err == nil {
			t.Errorf("a relation %s is kept", name)
		}
	}
	if n := count(t, s, "relatings"); n != 0 {
		t.Errorf("%d relatings kept", n)
	}
}
