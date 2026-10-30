package editor_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/polite"
	"github.com/samthorold/go-slow/internal/wikidata"
	"github.com/samthorold/go-slow/internal/wikidata/wikidatatest"
)

// human is Wikidata's "human" (Q5), what every person is an instance of.
const human = "Q5"

var andyBurnham = editor.WikidataItem{ID: "Q269909", Label: "Andy Burnham", InstanceOf: []string{human}, Revision: 1}

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

const pm = "Prime Minister of the United Kingdom"

// named is a person's name as reading judges it, with an office.
func named(name, kind, office string) editor.Name {
	return editor.Name{Name: name, Kind: kind, Office: office}
}

func TestAPersonNamedByTheirOfficeIsWhoeverHeldItOnTheItemsDate(t *testing.T) {
	for _, tc := range []struct {
		published time.Time
		want      string
	}{
		{day(2026, time.October, 14).Add(9 * time.Hour), "Andy Burnham"},
		{day(2025, time.March, 1).Add(9 * time.Hour), "Keir Starmer"},
	} {
		t.Run(tc.published.Format(time.DateOnly), func(t *testing.T) {
			key := editor.KeyOf(named("the Prime Minister", "person", pm), tc.published)

			got, err := editor.LookUp(t.Context(), primeMinisters(), key, saturday)
			if err != nil {
				t.Fatal(err)
			}
			if got.Label != tc.want {
				t.Errorf("the Prime Minister on %s is %+v, want %s", tc.published.Format(time.DateOnly), got, tc.want)
			}
		})
	}
}

func TestANameIsLookedUpAsAtTheItemsDateOnlyWhereItHasAnOffice(t *testing.T) {
	published := time.Date(2026, time.October, 15, 23, 30, 0, 0, time.UTC)
	if k := editor.KeyOf(named("the Prime Minister", "person", pm), published); k.On != "2026-10-15" {
		t.Errorf("an office's key is on %q, want the Item's date", k.On)
	}
	if k := editor.KeyOf(named("Derby", "place", ""), published); k.On != "" {
		t.Errorf("a name with no office is keyed on %q, want no date: Derby is Derby on any day", k.On)
	}
}

func TestANameWithNoCertainMatchIsNoMatch(t *testing.T) {
	wd := &fakeWikidata{
		search: map[string][]string{"Burnham": {"Q1"}, "Andy Burnham": {"Q269909"}},
		items: map[string]editor.WikidataItem{
			"Q1":      {ID: "Q1", Label: "Burnham", InstanceOf: []string{"Q532"}},
			"Q269909": andyBurnham,
		},
	}
	for _, n := range []editor.Name{
		// A village is not a person.
		named("Burnham", "person", ""),
		// A person is not a place.
		named("Andy Burnham", "place", ""),
	} {
		got, err := editor.LookUp(t.Context(), wd, editor.KeyOf(n, saturday), saturday)
		if err != nil {
			t.Fatal(err)
		}
		if got.WikidataID != "" {
			t.Errorf("%+v matched %+v, want no match", n, got)
		}
	}
}

// recordedWikidata is Wikidata answering from the responses recorded in
// the Wikidata adapter's testdata, faked at HTTP, counting the searches
// it is sent.
func recordedWikidata(t *testing.T) (editor.Wikidata, map[string]int) {
	t.Helper()
	searches := map[string]int{}
	recorded := wikidatatest.Handler("../wikidata/testdata")
	srv := httptest.NewServer(countSearches(recorded, searches))
	t.Cleanup(srv.Close)
	return wikidata.New(wikidata.Options{
		HTTP: srv.Client(), Endpoint: srv.URL, UserAgent: "go-slow-test",
		Now: time.Now, Sleep: polite.Sleep,
	}), searches
}

// countSearches passes each request to h, counting each search by its text.
// Requests reach it one at a time, as the adapter sends them.
func countSearches(h http.Handler, searches map[string]int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query(); q.Get("action") == "wbsearchentities" {
			searches[q.Get("search")]++
		}
		h.ServeHTTP(w, r)
	})
}

func TestThreeNamesForThePrimeMinisterAreOneWikidataItem(t *testing.T) {
	wd, _ := recordedWikidata(t)
	for _, n := range []editor.Name{
		named("Andy Burnham", "person", ""),
		named("the Prime Minister", "person", pm),
		named("Burnham", "person", pm),
	} {
		got, err := editor.LookUp(t.Context(), wd, editor.KeyOf(n, speech.Published), saturday)
		if err != nil {
			t.Fatal(err)
		}
		if got.WikidataID != "Q269909" || got.Label != "Andy Burnham" || got.Revision == 0 {
			t.Errorf("%q is %+v, want Andy Burnham, Q269909, with the revision seen", n.Name, got)
		}
		if got.Judge != editor.Looker || !got.At.Equal(saturday) || got.Key != editor.KeyOf(n, speech.Published) {
			t.Errorf("the look-up of %q is %+v, want it kept with its key, its Looker and when", n.Name, got)
		}
	}
}
