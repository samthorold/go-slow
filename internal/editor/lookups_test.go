package editor_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
	"github.com/samthorold/go-slow/internal/wikidata"
	"github.com/samthorold/go-slow/internal/wikidata/wikidatatest"
)

// derbyReading reads the release as naming Derby, which "the city" refers
// to.
const derbyReading = `{
  "names": [{"at": {"span": 3, "words": "Derby"}, "kind": "place", "name": "Derby", "office": ""}],
  "refers_to": [{"from": {"span": 2, "words": "the city"}, "to": {"span": 3, "words": "Derby"}}],
  "rejected": [], "speakers": []
}`

// releases are two Items with the release's text, a day apart.
var releases = []editor.Item{
	{SourceID: number10.ID, EntryID: "monday", Title: "GBR headquarters in Derby", Content: release.Content,
		Published: release.Published},
	{SourceID: number10.ID, EntryID: "tuesday", Title: "GBR headquarters in Derby, again", Content: release.Content,
		Published: release.Published.AddDate(0, 0, 1)},
}

// readRecord is a Record holding items, cut and read, each by a reading
// answering output.
func readRecord(t *testing.T, output string, items ...editor.Item) *sqlite.Store {
	t.Helper()
	ctx := t.Context()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "go-slow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SyncSources(ctx, []collect.Source{number10}, saturday); err != nil {
		t.Fatal(err)
	}
	readMore(t, store, output, items...)
	return store
}

// readMore lands items in store, cut and read, each by a reading answering
// output.
func readMore(t *testing.T, store *sqlite.Store, output string, items ...editor.Item) {
	t.Helper()
	ctx := t.Context()
	for _, it := range items {
		if _, err := store.AddItems(ctx, []collect.Item{{
			SourceID: it.SourceID, EntryID: it.EntryID, Title: it.Title, Link: "https://www.gov.uk/" + it.EntryID,
			Content: it.Content, Published: it.Published, Attribution: collect.OGL, CollectedAt: saturday,
		}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := cut.New(store).CutNew(ctx, saturday); err != nil {
		t.Fatal(err)
	}
	ed, logs := newEditor(store, &fakeModel{output: output}, &fakeWikidata{})
	if err := ed.ReadWaiting(ctx, saturday); err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if _, read, err := store.NewestReading(ctx, it.SourceID, it.EntryID); err != nil || !read {
			t.Fatalf("%s was not read (%v)\n%s", it.EntryID, err, logs)
		}
	}
}

// naming reads the release's S1 as naming a person, with an office, and
// "He" in S4 as referring to them.
func naming(name, office string) string {
	return `{
  "names": [{"at": {"span": 1, "words": "Andy Burnham"}, "kind": "person", "name": "` + name + `", "office": "` + office + `"}],
  "refers_to": [{"from": {"span": 4, "words": "He"}, "to": {"span": 1, "words": "Andy Burnham"}}],
  "rejected": [], "speakers": []
}`
}

// releaseOn is an Item with the release's text, published on published.
func releaseOn(entryID string, published time.Time) editor.Item {
	return editor.Item{SourceID: number10.ID, EntryID: entryID, Title: "GBR headquarters in Derby",
		Content: release.Content, Published: published}
}

func TestANameWithNoMatchIsTheEditorsEntityUntilALaterLookUpMergesIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		// later is the later Item's reading of the name, which matches.
		later string
	}{
		// The later look-up matches under the same name, by its office.
		{"same name", naming("Andy Burnham", pm)},
		// The later look-up matches an item whose label is the name.
		{"the item's label", naming("Burnham", pm)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := releaseOn("first", speech.Published)
			store := readRecord(t, naming("Andy Burnham", ""), first)
			nothing, _ := lookups(store, &fakeWikidata{})
			if err := nothing.LookUpWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}
			got := spanEntities(t, store, first, 4)
			if len(got) != 1 || got[0].EditorID == 0 || got[0].WikidataID != "" || got[0].Name != "Andy Burnham" || got[0].Kind != "person" {
				t.Fatalf("with no match \"He\" mentions %+v, want the Editor's own Andy Burnham", got)
			}

			later := releaseOn("later", speech.Published.AddDate(0, 0, 1))
			readMore(t, store, tc.later, later)
			matches, _ := lookups(store, primeMinisters())
			if err := matches.LookUpWaiting(t.Context(), saturday.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}

			want := editor.NamedEntity{WikidataID: "Q269909", Name: "Andy Burnham", Kind: "person"}
			for _, it := range []editor.Item{first, later} {
				if got := spanEntities(t, store, it, 4); len(got) != 1 || got[0] != want {
					t.Errorf("%s's \"He\" mentions %+v, want %+v", it.EntryID, got, want)
				}
			}
		})
	}
}

// lookups is the look-up step over store, on wd, and its log.
func lookups(store *sqlite.Store, wd editor.Wikidata) (*editor.Lookups, *bytes.Buffer) {
	var logs bytes.Buffer
	return editor.NewLookups(editor.LookupDeps{
		Store: store, Wikidata: wd, Log: slog.New(slog.NewTextHandler(&logs, nil)),
	}), &logs
}

// spanEntities are the Entities the nth Span of the Item's newest Cut
// mentions, by its newest reading, as the Record's look-ups have them.
func spanEntities(t *testing.T, store *sqlite.Store, it editor.Item, n int) []editor.NamedEntity {
	t.Helper()
	c, _, _, err := store.NewestCut(t.Context(), it.SourceID, it.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	r, _, err := store.NewestReading(t.Context(), it.SourceID, it.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	is, err := store.EntitiesNamed(t.Context(), r.Keys(it.Published))
	if err != nil {
		t.Fatal(err)
	}
	return r.Entities(c.Spans[n-1], it.Published, is)
}

func TestALookUpWikidataRefusesIsTriedAgainOnALaterRunAfterItsBackoff(t *testing.T) {
	store := readRecord(t, derbyReading, releases[0])
	recorded := wikidatatest.Handler("../wikidata/testdata")
	busy := true
	searches := map[string]int{}
	srv := httptest.NewServer(countSearches(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if busy && r.URL.Query().Get("action") == "wbsearchentities" {
			_, _ = w.Write([]byte(`{"error": {"code": "cirrussearch-too-busy-error", "info": "Search is currently too busy. Please try again later."}}`))
			return
		}
		recorded.ServeHTTP(w, r)
	}), searches))
	t.Cleanup(srv.Close)
	// The clock moves on only by sleeping.
	now, slept := saturday, []time.Duration{}
	wd := wikidata.New(wikidata.Options{
		HTTP: srv.Client(), Endpoint: srv.URL, UserAgent: "go-slow-test", Gap: time.Second,
		Now:   func() time.Time { return now },
		Sleep: func(_ context.Context, d time.Duration) error { slept = append(slept, d); now = now.Add(d); return nil },
	})
	lu, logs := lookups(store, wd)

	// Wikidata stays too busy through the adapter's backoff.
	if err := lu.LookUpWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	if searches["Derby"] < 2 || len(slept) == 0 || slices.Max(slept) < 10*time.Second {
		t.Errorf("searched %d times, sleeping %v: want a backoff before each search was sent again", searches["Derby"], slept)
	}
	if got := spanEntities(t, store, releases[0], 2); len(got) != 0 {
		t.Errorf("a refused look-up names %+v, want nothing yet", got)
	}
	if !strings.Contains(logs.String(), "look-up failed") {
		t.Errorf("the refusal is not logged:\n%s", logs)
	}

	// On a later run Wikidata answers.
	busy = false
	if err := lu.LookUpWaiting(t.Context(), saturday.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := spanEntities(t, store, releases[0], 2); len(got) != 1 || got[0].Name != "Derby" {
		t.Errorf("after a later run \"the city\" mentions %+v, want Derby", got)
	}
}

func TestAPersonNamedByOfficeIsMatchedByThePositionWikidataSaysTheyHold(t *testing.T) {
	it := releaseOn("then", speech.Published)
	store := readRecord(t, naming("Andy Burnham", pm), it)
	wd := primeMinisters()
	burnham := andyBurnham
	burnham.Positions = []editor.Tenure{{Holder: "Q269909", Office: "Q14211", Start: day(2026, time.July, 20), StartToTheDay: true}}
	wd.items["Q269909"] = burnham
	wd.labels = map[string]string{"Q14211": pm}
	lu, _ := lookups(store, wd)

	if err := lu.LookUpWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if got := spanEntities(t, store, it, 1); len(got) != 1 || got[0].WikidataID != "Q269909" {
		t.Errorf("S1 mentions %+v, want Andy Burnham", got)
	}
}

func TestTwoItemsNamingDerbyAreOneLookUp(t *testing.T) {
	store := readRecord(t, derbyReading, releases...)
	wd, searches := recordedWikidata(t)
	lu, logs := lookups(store, wd)

	if err := lu.LookUpWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	// A later run has nothing left to look up.
	if err := lu.LookUpWaiting(t.Context(), saturday.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if searches["Derby"] != 1 {
		t.Errorf("Wikidata was searched for Derby %d times, want once\n%s", searches["Derby"], logs)
	}
	// "the city" in each Item mentions the one Entity Derby is.
	monday, tuesday := spanEntities(t, store, releases[0], 2), spanEntities(t, store, releases[1], 2)
	if len(monday) != 1 || monday[0].WikidataID == "" || monday[0].Name != "Derby" || monday[0].Kind != "place" {
		t.Fatalf("Monday's \"the city\" mentions %+v, want Derby, a Wikidata item", monday)
	}
	if len(tuesday) != 1 || tuesday[0] != monday[0] {
		t.Errorf("Tuesday's \"the city\" mentions %+v, want Monday's %+v", tuesday, monday[0])
	}
}
