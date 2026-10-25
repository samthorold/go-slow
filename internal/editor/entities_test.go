package editor_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// fakeWikidata stands in for Wikidata behind the port: it answers from what
// it is given, and finds nothing else.
type fakeWikidata struct {
	search map[string][]string
	items  map[string]editor.WikidataItem
	labels map[string]string
}

func (f *fakeWikidata) Search(_ context.Context, text string) ([]string, error) {
	return f.search[text], nil
}

func (f *fakeWikidata) Items(_ context.Context, ids []string) ([]editor.WikidataItem, error) {
	var out []editor.WikidataItem
	for _, id := range ids {
		if it, ok := f.items[id]; ok {
			out = append(out, it)
		}
	}
	return out, nil
}

func (f *fakeWikidata) Labels(_ context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		if l, ok := f.labels[id]; ok {
			out[id] = l
		}
	}
	return out, nil
}

// human is Wikidata's "human" (Q5), what every person is an instance of.
const human = "Q5"

var andyBurnham = editor.WikidataItem{ID: "Q269909", Label: "Andy Burnham", InstanceOf: []string{human}, Revision: 1}

// collectAnother lands one more Item on Scotland in the Record, awaiting
// reading.
func collectAnother(t *testing.T, store *sqlite.Store, entryID string, published time.Time) {
	t.Helper()
	if _, err := store.AddItems(t.Context(), []collect.Item{{
		SourceID: number10.ID, EntryID: entryID, Title: "PM visits Scotland",
		Link: "https://www.gov.uk/" + entryID, Content: speech.Content, Published: published,
		Attribution: collect.OGL, CollectedAt: published,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := interest.New(store).CheckNew(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestAnEntityWithNoWikidataMatchIsTheEditorsAndALaterMatchMergesIt(t *testing.T) {
	store := record(t)
	wd := &fakeWikidata{}
	ed, _ := newEditor(store, &fakeModel{output: goodReading}, wd)
	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	e, found, err := store.Entity(t.Context(), "Motherwell")
	if err != nil || !found || e.WikidataID != "" {
		t.Fatalf("with no match Motherwell is %+v (found %v, %v), want the Editor's own", e, found, err)
	}

	// Wikidata gains an item for it, and a later Item names it.
	wd.search = map[string][]string{"Motherwell": {"Q1000001"}}
	wd.items = map[string]editor.WikidataItem{"Q1000001": {ID: "Q1000001", Label: "Motherwell, Lanarkshire", Aliases: []string{"Motherwell"}, Revision: 7}}
	collectAnother(t, store, "visit", saturday.Add(time.Hour))
	if err := ed.ReadWaiting(t.Context(), saturday.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	e, found, err = store.Entity(t.Context(), "Motherwell")
	if err != nil || !found {
		t.Fatalf("Entity: %v %v", found, err)
	}
	if e.WikidataID != "Q1000001" || e.Name != "Motherwell, Lanarkshire" {
		t.Errorf("after the match Motherwell is %+v, want Wikidata's Q1000001", e)
	}
	if !slices.Contains(e.Aliases, "Motherwell") {
		t.Errorf("the Editor's name is not kept as an alias: %v", e.Aliases)
	}
	// What the earlier Item tied to the Editor's Entity is tied to the item.
	got, _, err := store.ReadItem(t.Context(), speech.SourceID, speech.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got.Facts[0].Entities, "Motherwell, Lanarkshire") {
		t.Errorf("the earlier Fact involves %v, want the merged Entity", got.Facts[0].Entities)
	}
}

// readingNaming is a reading of the speech whose one Fact involves a
// person, named and given a role as the Item does.
func readingNaming(name, role string) string {
	return `{"entities": [{"name": "` + name + `", "kind": "person", "role": "` + role + `"}],
	  "facts": [{"statement": "The Prime Minister gave a speech in Motherwell.", "as_at": "2026-10-16",
	    "quote": "to be here in Motherwell", "entities": ["` + name + `"]}],
	  "claims": []}`
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// primeMinisters is Wikidata as it holds the office and two of its holders.
func primeMinisters() *fakeWikidata {
	return &fakeWikidata{
		search: map[string][]string{"Prime Minister of the United Kingdom": {"Q14211"}},
		items: map[string]editor.WikidataItem{
			"Q14211": {ID: "Q14211", Label: "Prime Minister of the United Kingdom", Holders: []editor.Tenure{
				{Holder: "Q6555365", Office: "Q14211", Start: day(2024, time.July, 5), End: day(2026, time.July, 20)},
				{Holder: "Q269909", Office: "Q14211", Start: day(2026, time.July, 20)},
			}},
			"Q269909":  andyBurnham,
			"Q6555365": {ID: "Q6555365", Label: "Keir Starmer", InstanceOf: []string{human}},
		},
	}
}

func TestAPersonNamedByTheirOfficeIsWhoeverHeldItOnTheItemsDate(t *testing.T) {
	for _, tc := range []struct {
		published time.Time
		want      string
	}{
		{day(2026, time.October, 14), "Andy Burnham"},
		{day(2025, time.March, 1), "Keir Starmer"},
	} {
		t.Run(tc.published.Format(time.DateOnly), func(t *testing.T) {
			store := record(t)
			model := &fakeModel{output: readingNaming("the Prime Minister", "Prime Minister of the United Kingdom")}
			ed, _ := newEditor(store, model, primeMinisters())
			collectAnother(t, store, "then", tc.published)
			if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}

			got, _, err := store.ReadItem(t.Context(), number10.ID, "then")
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Facts) != 1 || !slices.Equal(got.Facts[0].Entities, []string{tc.want}) {
				t.Errorf("the Prime Minister on %s is %+v, want %s", tc.published.Format(time.DateOnly), got.Facts, tc.want)
			}
		})
	}
}

func TestAPositionWikidataSaysAPersonHoldsIsAFactTracedToTheLookup(t *testing.T) {
	store := record(t)
	wd := primeMinisters()
	burnham := andyBurnham
	burnham.Revision = 2559101554
	burnham.Positions = []editor.Tenure{
		{Holder: "Q269909", Office: "Q14211", Start: day(2026, time.July, 20), StartToTheDay: true},
		// Over by the Item's date.
		{Holder: "Q269909", Office: "Q19655061", Start: day(2017, time.May, 8), End: day(2026, time.June, 18), StartToTheDay: true},
		// Known only to the year.
		{Holder: "Q269909", Office: "Q28841847", Start: day(2007, time.January, 1)},
	}
	wd.items["Q269909"] = burnham
	wd.labels = map[string]string{"Q14211": "Prime Minister of the United Kingdom", "Q19655061": "Mayor of Greater Manchester"}
	ed, _ := newEditor(store, &fakeModel{output: readingNaming("Andy Burnham", "Prime Minister of the United Kingdom")}, wd)

	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	e, _, err := store.Entity(t.Context(), "Andy Burnham")
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Facts) != 1 {
		t.Fatalf("Facts about Andy Burnham = %+v, want the one position he holds on the Item's date", e.Facts)
	}
	f := e.Facts[0]
	if f.Statement != "Andy Burnham took office as Prime Minister of the United Kingdom on 20 July 2026." ||
		!f.AsAt.Equal(day(2026, time.July, 20)) {
		t.Errorf("Fact = %+v", f)
	}
	// It traces to the lookup it came from, not to an Item.
	want := editor.Lookup{Reference: "Wikidata", ItemID: "Q269909", Revision: 2559101554, At: saturday, Licence: "CC0 1.0"}
	if f.Lookup == nil || *f.Lookup != want || f.SourceID != "" || f.EntryID != "" {
		t.Errorf("the Fact traces to %+v (Item %q %q), want %+v", f.Lookup, f.SourceID, f.EntryID, want)
	}
	if got := f.Lookup.URL(); got != "https://www.wikidata.org/w/index.php?oldid=2559101554" {
		t.Errorf("the lookup's URL = %s", got)
	}

	// Read again from another Item, the same Fact is not kept twice.
	collectAnother(t, store, "again", saturday)
	if err := ed.ReadWaiting(t.Context(), saturday.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if e, _, _ := store.Entity(t.Context(), "Andy Burnham"); len(e.Facts) != 1 {
		t.Errorf("Facts after a second reading = %+v", e.Facts)
	}
}

func TestAnEntityWithAWikidataMatchIsThatItem(t *testing.T) {
	store := record(t)
	wd := &fakeWikidata{
		search: map[string][]string{"Andy Burnham": {"Q269909"}},
		items:  map[string]editor.WikidataItem{"Q269909": andyBurnham},
	}
	ed, _ := newEditor(store, &fakeModel{output: goodReading}, wd)

	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	e, found, err := store.Entity(t.Context(), "Andy Burnham")
	if err != nil || !found {
		t.Fatalf("Entity: %v %v", found, err)
	}
	if e.WikidataID != "Q269909" {
		t.Errorf("Andy Burnham is %+v, want Wikidata's Q269909", e)
	}
}
