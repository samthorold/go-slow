package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// The Items collected with only a summary, under the listed Sources' real
// links and ids.
const (
	appointmentPath = "/government/news/appointment-of-lord-lieutenant-of-county-tyrone-15-october-2026"
	speechPath      = "/government/speeches/pm-speech-in-scotland"
	statementID     = "1952546"
)

// publishers is a fake of every publisher's host: a request to any host is
// answered from routes by its path, and recorded.
type publishers struct {
	mu       sync.Mutex
	routes   map[string]string
	requests []string
}

func (p *publishers) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, r.Host+r.URL.Path)
	body, ok := p.routes[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = io.WriteString(w, body)
}

// to sends every request to srv, keeping its Host, so the listed Sources'
// real URLs reach the fake.
type to struct{ srv *url.URL }

func (t to) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Host = r.URL.Host
	r.URL.Scheme, r.URL.Host = t.srv.Scheme, t.srv.Host
	return http.DefaultTransport.RoundTrip(r)
}

// summaryOnlyRecord is a Record at db holding three Items collected with
// only their summaries: the appointment and a written statement, uncut, and
// a speech a Cut already lists the Spans of (as one kept before Items were
// collected only with their text might).
func summaryOnlyRecord(t *testing.T, db string) {
	t.Helper()
	ctx := t.Context()
	store, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.SyncSources(ctx, collect.Sources(), saturday); err != nil {
		t.Fatal(err)
	}
	items := []collect.Item{
		{SourceID: "govuk-number-10", EntryID: "https://www.gov.uk" + appointmentPath + "#1", Title: "Appointment of Lord-Lieutenant of County Tyrone",
			Link: "https://www.gov.uk" + appointmentPath, Summary: "The King has been pleased to appoint Mr Baxter.", Published: saturday, CollectedAt: saturday},
		{SourceID: "govuk-number-10", EntryID: "https://www.gov.uk" + speechPath + "#1", Title: "PM speech in Scotland",
			Link: "https://www.gov.uk" + speechPath, Summary: "The Prime Minister delivered a speech.", Published: saturday, CollectedAt: saturday},
		{SourceID: "parliament-written-statements", EntryID: statementID, Title: "Small Business Regulatory Taskforce",
			Summary: "Today, the government is publishing...", Published: saturday, CollectedAt: saturday},
	}
	if _, err := store.AddItems(ctx, items); err != nil {
		t.Fatal(err)
	}
	if err := store.KeepCut(ctx, cut.Cut{SourceID: "govuk-number-10", EntryID: items[1].EntryID,
		Judge: cut.SentenceCutter, At: saturday, Spans: []cut.Span{{Start: 0, End: 5}}}); err != nil {
		t.Fatal(err)
	}
}

func TestTheBackfillCommandGivesUncutSummaryOnlyItemsTheirText(t *testing.T) {
	db := filepath.Join(t.TempDir(), "go-slow.db")
	summaryOnlyRecord(t, db)
	pub := &publishers{routes: map[string]string{
		"/api/content" + appointmentPath:                   `{"base_path":"` + appointmentPath + `","details":{"body":"<p>District Commander in Omagh.</p>"}}`,
		"/api/content" + speechPath:                        `{"base_path":"` + speechPath + `","details":{"body":"<p>Motherwell.</p>"}}`,
		"/api/writtenstatements/statements/" + statementID: `{"value":{"id":1952546,"text":"<p>Companies House and HMRC.</p>"}}`,
	}}
	srv := httptest.NewServer(pub)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	if err := admin(t.Context(), slog.New(slog.DiscardHandler), []string{"backfill"},
		adminDeps{Lookup: env(db), Web: &http.Client{Transport: to{u}}}); err != nil {
		t.Fatal(err)
	}

	store, err := sqlite.Open(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	text := func(sourceID, entryID string) editor.KeptItem {
		it, ok, err := store.KeptItem(t.Context(), sourceID, entryID)
		if err != nil || !ok {
			t.Fatalf("%s not in the Record: %v", entryID, err)
		}
		return it
	}
	if it := text("govuk-number-10", "https://www.gov.uk"+appointmentPath+"#1"); it.Content != "<p>District Commander in Omagh.</p>" {
		t.Errorf("appointment's text = %q, want its document's body", it.Content)
	}
	if it := text("parliament-written-statements", statementID); it.Content != "<p>Companies House and HMRC.</p>" {
		t.Errorf("statement's text = %q, want the statement's", it.Content)
	}
	if it := text("govuk-number-10", "https://www.gov.uk"+speechPath+"#1"); it.Content != "" {
		t.Errorf("the speech, already cut, was given text %q", it.Content)
	}
	pub.mu.Lock()
	defer pub.mu.Unlock()
	if slices.Contains(pub.requests, "www.gov.uk/api/content"+speechPath) {
		t.Errorf("fetched the text of an Item already cut: %v", pub.requests)
	}
}
