package wikidata_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/wikidata"
	"github.com/samthorold/go-slow/internal/wikidata/wikidatatest"
)

const userAgent = "go-slow/0.1 (private slow-news briefing; contact: owner@example.org)"

// clock is a fake clock: Sleep moves it on, and keeps how long it slept.
type clock struct {
	mu    sync.Mutex
	now   time.Time
	slept []time.Duration
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Sleep(_ context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.slept = append(c.slept, d)
	c.now = c.now.Add(d)
	return nil
}

func client(t *testing.T, h http.Handler) (*wikidata.Client, *clock) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := &clock{now: time.Date(2026, time.October, 25, 7, 0, 0, 0, time.UTC)}
	return wikidata.New(wikidata.Options{
		HTTP: srv.Client(), Endpoint: srv.URL + "/w/api.php", UserAgent: userAgent,
		Gap: time.Second, Now: c.Now, Sleep: c.Sleep,
	}), c
}

func recorded(t *testing.T) (*wikidata.Client, *clock) {
	t.Helper()
	return client(t, wikidatatest.Handler("testdata"))
}

func TestSearchIsWikidatasRankedItems(t *testing.T) {
	wd, _ := recorded(t)

	ids, err := wd.Search(t.Context(), "Andy Burnham")
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"Q269909", "Q94699104", "Q24083437", "Q47637556", "Q140572997"}; !slices.Equal(ids, want) {
		t.Errorf("Search = %v, want %v", ids, want)
	}
}

func TestAnItemCarriesWhatItIsAndThePositionsItsPersonHeld(t *testing.T) {
	wd, _ := recorded(t)

	items, err := wd.Items(t.Context(), []string{"Q269909"})
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 1 {
		t.Fatalf("Items = %+v", items)
	}
	it := items[0]
	if it.ID != "Q269909" || it.Label != "Andy Burnham" || !slices.Contains(it.InstanceOf, "Q5") || it.Revision != 2559101554 {
		t.Errorf("item = %s %q %v rev %d", it.ID, it.Label, it.InstanceOf, it.Revision)
	}
	pm := editor.Tenure{Holder: "Q269909", Office: "Q14211",
		Start: time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC), StartToTheDay: true}
	if !slices.Contains(it.Positions, pm) {
		t.Errorf("positions held lack Prime Minister since 20 July 2026: %+v", it.Positions)
	}
	// A start known only to the year.
	mayor := slices.IndexFunc(it.Positions, func(p editor.Tenure) bool { return p.Office == "Q28841847" })
	if mayor < 0 || it.Positions[mayor].StartToTheDay || it.Positions[mayor].Start.Year() != 2007 {
		t.Errorf("a position from 2007 = %+v", it.Positions)
	}
}

func TestAnOfficeCarriesItsHolders(t *testing.T) {
	wd, _ := recorded(t)

	items, err := wd.Items(t.Context(), []string{"Q14211", "Q7460478"})
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 2 || items[0].ID != "Q14211" || items[1].ID != "Q7460478" {
		t.Fatalf("Items are not in the order asked: %+v", items)
	}
	burnham := slices.IndexFunc(items[0].Holders, func(h editor.Tenure) bool { return h.Holder == "Q269909" })
	if burnham < 0 || !items[0].Holders[burnham].End.IsZero() ||
		!items[0].Holders[burnham].Start.Equal(time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("the office's holders = %+v", items[0].Holders)
	}
}

func TestLabelsAreEnglish(t *testing.T) {
	wd, _ := recorded(t)

	got, err := wd.Labels(t.Context(), []string{"Q126063447", "Q3615378", "Q14211", "Q609884", "Q65988624", "Q3315116"})
	if err != nil {
		t.Fatal(err)
	}

	if got["Q14211"] != "Prime Minister of the United Kingdom" || len(got) != 6 {
		t.Errorf("Labels = %v", got)
	}
}

func TestRequestsArePoliteOneAtATimeApartAndHonest(t *testing.T) {
	var seen []*http.Request
	var mu sync.Mutex
	wd, c := client(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"search": []}`))
	}))

	for range 3 {
		if _, err := wd.Search(t.Context(), "Nobody"); err != nil {
			t.Fatal(err)
		}
	}

	for _, r := range seen {
		if r.Header.Get("User-Agent") != userAgent {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		// A non-interactive client asks to wait when the servers are lagged.
		if r.URL.Query().Get("maxlag") == "" {
			t.Errorf("no maxlag on %s", r.URL)
		}
	}
	if len(c.slept) != 2 || c.slept[0] != time.Second || c.slept[1] != time.Second {
		t.Errorf("slept %v between three requests, want the gap before each after the first", c.slept)
	}
}

func TestATooManyRequestsBacksOffForRetryAfter(t *testing.T) {
	calls := 0
	wd, c := client(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "120")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"search": []}`))
	}))

	if _, err := wd.Search(t.Context(), "Nobody"); err == nil {
		t.Fatal("a 429 is not an error")
	}
	if _, err := wd.Search(t.Context(), "Nobody"); err != nil {
		t.Fatal(err)
	}

	if len(c.slept) != 1 || c.slept[0] != 120*time.Second {
		t.Errorf("slept %v, want the 120s Retry-After asked", c.slept)
	}
}

func TestAnAPIErrorIsAnError(t *testing.T) {
	wd, _ := client(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "5")
		_, _ = w.Write([]byte(`{"error": {"code": "maxlag", "info": "Waiting for a database server: 7 seconds lagged."}}`))
	}))

	if _, err := wd.Search(t.Context(), "Nobody"); err == nil {
		t.Error("an API error is not an error")
	}
}
