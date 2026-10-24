package collect_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/polite"
	"github.com/samthorold/go-slow/internal/sqlite"
)

const feedV1 = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Prime Minister's Office, 10 Downing Street - Activity on GOV.UK</title>
  <updated>2026-10-23T18:00:00+01:00</updated>
  <entry>
    <id>https://www.gov.uk/government/news/pm-statement#2026-10-23T18:00:00+01:00</id>
    <updated>2026-10-23T18:00:00+01:00</updated>
    <link rel="alternate" type="text/html" href="https://www.gov.uk/government/news/pm-statement"/>
    <title>PM statement on the Budget</title>
    <summary type="html">The Prime Minister made a statement.</summary>
  </entry>
  <entry>
    <id>https://www.gov.uk/government/news/pm-call#2026-10-22T09:00:00+01:00</id>
    <updated>2026-10-22T09:00:00+01:00</updated>
    <link rel="alternate" type="text/html" href="https://www.gov.uk/government/news/pm-call"/>
    <title>PM call with the President</title>
    <summary type="html">The Prime Minister spoke to the President.</summary>
  </entry>
</feed>`

// publisher is a fake GOV.UK: it serves one Atom feed, honouring
// conditional requests, and records every request it receives.
type publisher struct {
	mu       sync.Mutex
	feed     string
	etag     string
	requests []*http.Request
}

func (p *publisher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, r)
	switch r.URL.Path {
	case "/robots.txt":
		_, _ = io.WriteString(w, "User-agent: *\nDisallow: /search\n")
	case "/feed.atom":
		if p.etag != "" && r.Header.Get("If-None-Match") == p.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if p.etag != "" {
			w.Header().Set("ETag", p.etag)
		}
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = io.WriteString(w, p.feed)
	default:
		http.NotFound(w, r)
	}
}

func (p *publisher) feedRequests() []*http.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []*http.Request
	for _, r := range p.requests {
		if r.URL.Path == "/feed.atom" {
			out = append(out, r)
		}
	}
	return out
}

var now = time.Date(2026, time.October, 24, 7, 0, 0, 0, time.UTC)

type fixture struct {
	pub       *publisher
	store     *sqlite.Store
	collector *collect.Collector
	source    collect.Source
}

func newFixture(t *testing.T, feed, etag string) *fixture {
	t.Helper()
	pub := &publisher{feed: feed, etag: etag}
	srv := httptest.NewServer(pub)
	t.Cleanup(srv.Close)

	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "go-slow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	source := collect.Source{
		ID:          "number-10",
		Name:        "Prime Minister's Office, 10 Downing Street",
		FeedURL:     srv.URL + "/feed.atom",
		Licence:     collect.OGLv3,
		Attribution: collect.OGL,
		Keeps:       collect.KeepFullText,
		UsualGap:    4 * 24 * time.Hour,
	}
	client := polite.New(polite.Options{
		HTTP:      srv.Client(),
		UserAgent: polite.UserAgent("owner@example.org"),
		Now:       func() time.Time { return now },
		Sleep:     func(context.Context, time.Duration) error { return nil },
	})
	collector := collect.New(collect.Deps{
		Sources: []collect.Source{source},
		Fetcher: client,
		Store:   store,
		Check:   interest.New(store),
		Log:     slog.New(slog.DiscardHandler),
	})
	if err := store.SyncSources(t.Context(), []collect.Source{source}, now); err != nil {
		t.Fatal(err)
	}
	return &fixture{pub: pub, store: store, collector: collector, source: source}
}

func TestACollectionRunStoresTheFeedsEntriesAsItems(t *testing.T) {
	f := newFixture(t, feedV1, "")

	f.collector.Run(t.Context(), now)

	items, err := f.store.ListItems(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("stored %d Items, want 2: %+v", len(items), items)
	}
	first := items[0]
	if first.Title != "PM statement on the Budget" {
		t.Errorf("newest Item title = %q", first.Title)
	}
	if first.Link != "https://www.gov.uk/government/news/pm-statement" {
		t.Errorf("newest Item link = %q", first.Link)
	}
	if first.SourceName != f.source.Name {
		t.Errorf("Item source = %q, want %q", first.SourceName, f.source.Name)
	}
	if first.Attribution != collect.OGL {
		t.Errorf("Item attribution = %q, want the OGL", first.Attribution)
	}
}

func TestASecondRunAgainstAnUnchangedFeedIsConditionalAndStoresNothing(t *testing.T) {
	f := newFixture(t, feedV1, `"v1"`)

	f.collector.Run(t.Context(), now)
	f.collector.Run(t.Context(), now.Add(time.Hour))

	reqs := f.pub.feedRequests()
	if len(reqs) != 2 {
		t.Fatalf("feed fetched %d times, want 2", len(reqs))
	}
	if got := reqs[0].Header.Get("If-None-Match"); got != "" {
		t.Errorf("first request sent If-None-Match %q, want none", got)
	}
	if got := reqs[1].Header.Get("If-None-Match"); got != `"v1"` {
		t.Errorf("second request sent If-None-Match %q, want the feed's ETag", got)
	}
	items, err := f.store.ListItems(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("stored %d Items, want 2", len(items))
	}
}

func TestCollectionRunsAtStartAndOnEveryTickUntilStopped(t *testing.T) {
	f := newFixture(t, feedV1, "")
	ctx, stop := context.WithCancel(t.Context())
	ticks := make(chan time.Time)
	done := make(chan struct{})

	go func() {
		f.collector.RunOn(ctx, ticks, func() time.Time { return now })
		close(done)
	}()
	// A send returns once the timetable takes the tick, that is once the
	// run before it has finished. The run on the third tick is cut short by
	// stopping, so it may or may not have reached the feed.
	for i := range 3 {
		ticks <- now.Add(time.Duration(i+1) * time.Hour)
	}
	stop()
	<-done

	if n := len(f.pub.feedRequests()); n < 3 {
		t.Errorf("feed fetched %d times, want at least 3 (at start and on two ticks)", n)
	}
}

func TestACollectionRunTiesEachNewItemToTheTopicsTheCheckMatchesOrToNone(t *testing.T) {
	f := newFixture(t, feedV1, "")
	topics := interest.New(f.store)
	if _, errs, err := topics.Add(t.Context(), interest.Draft{
		Name: "The Budget", Depth: "close", Terms: "Budget",
	}); err != nil || errs != nil {
		t.Fatalf("add Topic: %v %v", errs, err)
	}

	f.collector.Run(t.Context(), now)

	items, err := f.store.ListItems(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("stored %d Items, want 2", len(items))
	}
	budget, call := items[0], items[1]
	if !budget.Checked || !slices.Equal(budget.Topics, []string{"The Budget"}) {
		t.Errorf("Budget statement: checked %v, Topics %v; want checked, awaiting reading for The Budget", budget.Checked, budget.Topics)
	}
	if !call.Checked || len(call.Topics) != 0 {
		t.Errorf("PM call: checked %v, Topics %v; want checked, touching no Topic", call.Checked, call.Topics)
	}
}

// readAll stands in for the Editor's reading: it records each run.
type readAll struct{ runs []time.Time }

func (r *readAll) ReadWaiting(_ context.Context, now time.Time) error {
	r.runs = append(r.runs, now)
	return nil
}

func TestACollectionRunReadsTheItemsAwaitingReadingAfterTheCheck(t *testing.T) {
	f := newFixture(t, feedV1, "")
	reader := &readAll{}
	f.collector.Read = reader

	f.collector.Run(t.Context(), now)

	if len(reader.runs) != 1 || !reader.runs[0].Equal(now) {
		t.Errorf("reading ran at %v, want once at %v", reader.runs, now)
	}
}

func TestRunningCollectionAgainStoresNoDuplicates(t *testing.T) {
	// No ETag: the server answers in full every time.
	f := newFixture(t, feedV1, "")

	f.collector.Run(t.Context(), now)
	f.collector.Run(t.Context(), now.Add(time.Hour))

	items, err := f.store.ListItems(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("stored %d Items after two runs, want 2", len(items))
	}
	if n := len(f.pub.feedRequests()); n != 2 {
		t.Errorf("feed fetched %d times, want 2", n)
	}
}
