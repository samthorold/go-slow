package collect_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/polite"
	"github.com/samthorold/go-slow/internal/sqlite"
)

const onsPublished = `{"took":6,"breakdown":{"total":2},"releases":[
 {"uri":"/releases/constructionstatisticsgreatbritain2025","date_changes":null,
  "description":{"title":"Construction statistics, Great Britain: 2025",
   "summary":"Statistics on the construction industry.",
   "release_date":"2026-10-23T08:30:00.000Z","published":true,"cancelled":false,
   "finalised":true,"postponed":false,"census":false}},
 {"uri":"/releases/gdpmonthlyestimateukaugust2026","date_changes":null,
  "description":{"title":"GDP monthly estimate, UK: August 2026",
   "summary":"Gross domestic product (GDP) measures the value of goods and services.",
   "release_date":"2026-10-16T06:00:00.000Z","published":true,"cancelled":false,
   "finalised":true,"postponed":false,"census":false}}
]}`

const writtenStatements = `{"totalResults":18143,"results":[
 {"value":{"id":1952546,"memberId":4959,"member":null,
  "memberRole":"Parliamentary Under-Secretary of State (Minister for Small Business and Enterprise)",
  "uin":"HLWS419","dateMade":"2026-10-23T00:00:00","answeringBodyId":220,
  "answeringBodyName":"Department for Business, Innovation, Science and Trade ",
  "title":"Small Business Regulatory Taskforce: Government Response",
  "text":"Today, the government is publishing its response to the Small Business Regulatory Taskforce’s report...",
  "house":"Lords","noticeNumber":0,"hasAttachments":false,"hasLinkedStatements":false,
  "linkedStatements":null,"attachments":[]},
  "links":[{"rel":"self","href":"/Statements/1952546","method":"GET"}]}
]}`

// api is a fake publisher's API host: each path answers with its body,
// which a test may change between runs.
type api struct {
	mu     sync.Mutex
	routes map[string]string
}

func (a *api) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	body, ok := a.routes[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = io.WriteString(w, body)
}

// newAPI serves routes, returning the fake and its base URL.
func newAPI(t *testing.T, routes map[string]string) (*api, string) {
	t.Helper()
	a := &api{routes: routes}
	srv := httptest.NewServer(a)
	t.Cleanup(srv.Close)
	return a, srv.URL
}

// collectorFor builds a Collector over sources, fetching politely through a
// real Client into a fresh Record. Collecting the Sources began a month
// before now, longer than any usual gap.
func collectorFor(t *testing.T, sources ...collect.Source) (*collect.Collector, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "go-slow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SyncSources(t.Context(), sources, now.AddDate(0, -1, 0)); err != nil {
		t.Fatal(err)
	}
	client := polite.New(polite.Options{
		HTTP:      http.DefaultClient,
		UserAgent: polite.UserAgent("owner@example.org"),
		Now:       func() time.Time { return now },
		Sleep:     func(context.Context, time.Duration) error { return nil },
	})
	return collect.New(collect.Deps{
		Sources: sources,
		Fetcher: client,
		Store:   store,
		Check:   interest.New(store),
		Log:     slog.New(slog.DiscardHandler),
	}), store
}

func TestONSReleasesAreCollectedAsItemsLinkingToTheirReleasePages(t *testing.T) {
	_, base := newAPI(t, map[string]string{"/v1/search/releases": onsPublished})
	ons := collect.Source{
		ID:          "ons-releases",
		Name:        "Office for National Statistics: releases",
		FeedURL:     base + "/v1/search/releases?release-type=type-published",
		Format:      collect.ONSReleases,
		Licence:     collect.OGLv3,
		Attribution: collect.OGL,
		Keeps:       collect.KeepFullText,
		UsualGap:    4 * 24 * time.Hour,
	}
	c, store := collectorFor(t, ons)

	c.Run(t.Context(), now)

	items, err := store.ListItems(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("stored %d Items, want 2: %+v", len(items), items)
	}
	first := items[0]
	if first.Title != "Construction statistics, Great Britain: 2025" {
		t.Errorf("newest Item title = %q", first.Title)
	}
	if first.Link != "https://www.ons.gov.uk/releases/constructionstatisticsgreatbritain2025" {
		t.Errorf("newest Item link = %q", first.Link)
	}
	if first.Summary != "Statistics on the construction industry." {
		t.Errorf("newest Item summary = %q", first.Summary)
	}
	if want := time.Date(2026, time.October, 23, 8, 30, 0, 0, time.UTC); !first.Published.Equal(want) {
		t.Errorf("newest Item published %v, want %v", first.Published, want)
	}
}

const onsUpcoming = `{"took":4,"breakdown":{"total":1},"releases":[
 {"uri":"/releases/greenhousegasemissionsukprovisionalestimates2025","date_changes":null,
  "description":{"title":"Greenhouse gas emissions, UK: provisional estimates, 2025",
   "summary":"Measuring the contribution of the environment to the economy.",
   "release_date":"2026-10-28T09:30:00.000Z","published":false,"cancelled":false,
   "finalised":true,"postponed":false,"census":false,"provisional_date":"October 2026"}}
]}`

// calendarAt is the ONS release calendar, served from base.
func calendarAt(base string) collect.Source {
	return collect.Source{
		ID:          "ons-release-calendar",
		Name:        "Office for National Statistics: release calendar",
		FeedURL:     base + "/v1/search/releases?release-type=type-upcoming",
		Format:      collect.ONSReleaseCalendar,
		Licence:     collect.OGLv3,
		Attribution: collect.OGL,
		Keeps:       collect.KeepFullText,
		UsualGap:    7 * 24 * time.Hour,
	}
}

func TestAScheduledONSReleaseIsUpcomingUntilItsReleaseDate(t *testing.T) {
	_, base := newAPI(t, map[string]string{"/v1/search/releases": onsUpcoming})
	c, store := collectorFor(t, calendarAt(base))

	c.Run(t.Context(), now)

	upcoming, err := store.Upcoming(t.Context(), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(upcoming) != 1 {
		t.Fatalf("%d upcoming events, want 1: %+v", len(upcoming), upcoming)
	}
	ev := upcoming[0]
	if ev.Title != "Greenhouse gas emissions, UK: provisional estimates, 2025" {
		t.Errorf("upcoming event title = %q", ev.Title)
	}
	if ev.Link != "https://www.ons.gov.uk/releases/greenhousegasemissionsukprovisionalestimates2025" {
		t.Errorf("upcoming event link = %q", ev.Link)
	}
	due := time.Date(2026, time.October, 28, 9, 30, 0, 0, time.UTC)
	if !ev.Due.Equal(due) {
		t.Errorf("upcoming event due %v, want %v", ev.Due, due)
	}

	after, err := store.Upcoming(t.Context(), due.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Errorf("a release past its date is still upcoming: %+v", after)
	}
}

func TestTheReleaseCalendarGoesQuietOnlyWhenItStopsYieldingEvents(t *testing.T) {
	_, base := newAPI(t, map[string]string{"/v1/search/releases": onsUpcoming})
	c, store := collectorFor(t, calendarAt(base))

	c.Run(t.Context(), now)

	statuses, err := store.SourceStatuses(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 {
		t.Fatalf("%d statuses, want 1", len(statuses))
	}
	// An event has no publication time; it counts from when it was collected.
	if st := statuses[0]; st.Quiet(now.Add(24 * time.Hour)) {
		t.Errorf("calendar quiet a day after yielding an event: %+v", st)
	}
}

func TestARescheduledReleaseIsUpcomingOnlyOnItsNewDate(t *testing.T) {
	pub, base := newAPI(t, map[string]string{"/v1/search/releases": onsUpcoming})
	c, store := collectorFor(t, calendarAt(base))

	c.Run(t.Context(), now)
	pub.mu.Lock()
	pub.routes["/v1/search/releases"] = strings.Replace(onsUpcoming, "2026-10-28T09:30", "2026-11-04T09:30", 1)
	pub.mu.Unlock()
	c.Run(t.Context(), now.Add(time.Hour))

	upcoming, err := store.Upcoming(t.Context(), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(upcoming) != 1 {
		t.Fatalf("%d upcoming events, want only the rescheduled one: %+v", len(upcoming), upcoming)
	}
	if want := time.Date(2026, time.November, 4, 9, 30, 0, 0, time.UTC); !upcoming[0].Due.Equal(want) {
		t.Errorf("upcoming event due %v, want its new date %v", upcoming[0].Due, want)
	}
}

func TestParliamentsWrittenStatementsAreCollectedAsItems(t *testing.T) {
	_, base := newAPI(t, map[string]string{"/api/writtenstatements/statements": writtenStatements})
	statements := collect.Source{
		ID:          "parliament-written-statements",
		Name:        "UK Parliament: written statements",
		FeedURL:     base + "/api/writtenstatements/statements?take=50",
		Format:      collect.WrittenStatements,
		Licence:     collect.OPLv3,
		Attribution: collect.OPL,
		Keeps:       collect.KeepFullText,
		UsualGap:    10 * 24 * time.Hour,
	}
	c, store := collectorFor(t, statements)

	c.Run(t.Context(), now)

	items, err := store.ListItems(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("stored %d Items, want 1: %+v", len(items), items)
	}
	it := items[0]
	if it.Title != "Small Business Regulatory Taskforce: Government Response" {
		t.Errorf("Item title = %q", it.Title)
	}
	if it.Link != "https://questions-statements.parliament.uk/written-statements/detail/2026-10-23/HLWS419" {
		t.Errorf("Item link = %q", it.Link)
	}
	if it.Attribution != collect.OPL {
		t.Errorf("Item attribution = %q, want the Open Parliament Licence's", it.Attribution)
	}
	// Made on 23 October, a day in British Summer Time.
	if want := time.Date(2026, time.October, 22, 23, 0, 0, 0, time.UTC); !it.Published.Equal(want) {
		t.Errorf("Item published %v, want %v", it.Published, want)
	}
}
