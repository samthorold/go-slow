package collect_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/polite"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// appointment is a GOV.UK document as the Content API returned it, whose
// feed entry carries only its description as a summary.
const appointment = "/government/news/appointment-of-lord-lieutenant-of-county-tyrone-15-october-2026"

func testdata(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// govukFeed is a GOV.UK Atom feed served from base, with one entry for the
// appointment, carrying only its summary.
func govukFeed(base string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>` + base + appointment + `#2026-10-15T11:00:00+01:00</id>
    <updated>2026-10-15T11:00:00+01:00</updated>
    <link rel="alternate" type="text/html" href="` + base + appointment + `"/>
    <title>Appointment of Lord-Lieutenant of County Tyrone: 15 October 2026</title>
    <summary type="html">The King has been pleased to appoint Mr William James Baxter QPM DL as His Majesty’s Lord-Lieutenant of County Tyrone.</summary>
  </entry>
</feed>`
}

// govukAt is a GOV.UK Atom Source served from base, whose entries' text is
// the Content API document for their path.
func govukAt(base string) collect.Source {
	return collect.Source{
		ID:          "govuk-number-10",
		Name:        "Prime Minister's Office, 10 Downing Street (GOV.UK)",
		FeedURL:     base + "/government/organisations/prime-ministers-office-10-downing-street.atom",
		Format:      collect.Atom,
		Text:        collect.TextFromGOVUKContent,
		Licence:     collect.OGLv3,
		Attribution: collect.OGL,
		Keeps:       collect.KeepFullText,
		UsualGap:    4 * 24 * time.Hour,
	}
}

func TestANewGOVUKItemIsKeptWithItsDocumentsBodyAsItsText(t *testing.T) {
	pub, base := newAPI(t, nil)
	pub.routes = map[string]string{
		"/government/organisations/prime-ministers-office-10-downing-street.atom": govukFeed(base),
		"/api/content" + appointment: testdata(t, "govuk-appointment-of-lord-lieutenant-of-county-tyrone-15-october-2026.json"),
	}
	src := govukAt(base)
	c, store := collectorFor(t, src)

	c.Run(t.Context(), now)

	it, ok, err := store.ReadItem(t.Context(), src.ID, base+appointment+"#2026-10-15T11:00:00+01:00")
	if err != nil || !ok {
		t.Fatalf("Item not kept: %v %v", ok, err)
	}
	if !strings.Contains(it.Content, "District Commander in Omagh") {
		t.Errorf("Item kept without its document's body as its text: %q", it.Content)
	}
}

// listed is the listed Source with id, failing the test if there is none.
func listed(t *testing.T, id string) collect.Source {
	t.Helper()
	for _, s := range collect.Sources() {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no listed Source has the id %s", id)
	return collect.Source{}
}

// budgetSpeech is the Chancellor's Budget speech in HM Treasury's recorded
// feed, whose Content API document is recorded beside it.
const budgetSpeech = "/government/speeches/budget-2026-speech"

func TestTheTreasurysBudgetSpeechIsCollectedFromItsFeedWithItsText(t *testing.T) {
	pub, base := newAPI(t, nil)
	src := listed(t, "govuk-hm-treasury")
	feedPath, ok := strings.CutPrefix(src.FeedURL, "https://www.gov.uk")
	if !ok {
		t.Fatalf("the Treasury's feed is not on GOV.UK: %s", src.FeedURL)
	}
	// The recorded feed links to www.gov.uk; served from base, its entries'
	// text is fetched from the fake.
	pub.routes = map[string]string{
		feedPath:                      strings.ReplaceAll(testdata(t, "govuk-hm-treasury.atom"), "https://www.gov.uk", base),
		"/api/content" + budgetSpeech: testdata(t, "govuk-budget-2026-speech.json"),
	}
	src.FeedURL = base + feedPath
	c, store := collectorFor(t, src)

	c.Run(t.Context(), now)

	it, ok, err := store.ReadItem(t.Context(), src.ID, base+budgetSpeech+"#2026-10-28T15:45:59Z")
	if err != nil || !ok {
		t.Fatalf("Budget speech not kept: %v %v", ok, err)
	}
	if !strings.Contains(it.Content, "Chancellor of the Exchequer to deliver my first Budget") {
		t.Errorf("Budget speech kept without the speech as its text: %.200q", it.Content)
	}
	if it.Attribution != collect.OGL {
		t.Errorf("Budget speech attributed %q, want the Open Government Licence's", it.Attribution)
	}
}

// govuk is a fake GOV.UK: a feed answering conditional requests by its
// ETag, and Content API documents, any of which can be made to fail with a
// status and a Retry-After. It records every request.
type govuk struct {
	mu   sync.Mutex
	feed string
	etag string
	docs map[string]string
	// failing answers a document's path with a status, until removed.
	failing    map[string]int
	retryAfter string
	requests   []*http.Request
	inFlight   int
	maxFlight  int
}

const govukFeedPath = "/government/organisations/prime-ministers-office-10-downing-street.atom"

func (g *govuk) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.requests = append(g.requests, r)
	g.inFlight++
	g.maxFlight = max(g.maxFlight, g.inFlight)
	g.mu.Unlock()
	// Answering takes a moment, so overlapping requests would be seen.
	time.Sleep(time.Millisecond)
	g.mu.Lock()
	defer g.mu.Unlock()
	defer func() { g.inFlight-- }()
	switch p := r.URL.Path; {
	case p == "/robots.txt":
		_, _ = io.WriteString(w, "User-agent: *\nDisallow: /search/all*\n")
	case p == govukFeedPath:
		if r.Header.Get("If-None-Match") == g.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", g.etag)
		_, _ = io.WriteString(w, g.feed)
	case g.failing[p] != 0:
		if g.retryAfter != "" {
			w.Header().Set("Retry-After", g.retryAfter)
		}
		w.WriteHeader(g.failing[p])
	case g.docs[p] != "":
		_, _ = io.WriteString(w, g.docs[p])
	default:
		http.NotFound(w, r)
	}
}

func (g *govuk) set(f func(g *govuk)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f(g)
}

// requestsFor are the requests made for path.
func (g *govuk) requestsFor(path string) []*http.Request {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []*http.Request
	for _, r := range g.requests {
		if r.URL.Path == path {
			out = append(out, r)
		}
	}
	return out
}

// speech is a second GOV.UK document, hand-made in the Content API's shape.
const speech = "/government/speeches/pm-speech-in-scotland"

const speechDoc = `{"base_path":"` + speech + `","title":"PM speech in Scotland","details":{"body":"<p>Motherwell.</p>"}}`

// twoEntryFeed is a GOV.UK feed served from base, with the appointment and
// the speech.
func twoEntryFeed(base string) string {
	return strings.Replace(govukFeed(base), "</feed>", `  <entry>
    <id>`+base+speech+`#2026-10-16T12:00:00+01:00</id>
    <updated>2026-10-16T12:00:00+01:00</updated>
    <link rel="alternate" type="text/html" href="`+base+speech+`"/>
    <title>PM speech in Scotland</title>
    <summary type="html">The Prime Minister delivered a speech in Scotland.</summary>
  </entry>
</feed>`, 1)
}

// newGOVUK serves a fake GOV.UK with the two-entry feed and both documents.
func newGOVUK(t *testing.T) (*govuk, string) {
	t.Helper()
	g := &govuk{etag: `"v1"`, failing: map[string]int{}}
	srv := httptest.NewServer(g)
	t.Cleanup(srv.Close)
	g.feed = twoEntryFeed(srv.URL)
	g.docs = map[string]string{
		"/api/content" + appointment: testdata(t, "govuk-appointment-of-lord-lieutenant-of-county-tyrone-15-october-2026.json"),
		"/api/content" + speech:      speechDoc,
	}
	return g, srv.URL
}

// entryIDs are the entry ids of the Items in the Record.
func entryIDs(t *testing.T, store *sqlite.Store) []string {
	t.Helper()
	items, err := store.ListItems(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range items {
		out = append(out, it.EntryID)
	}
	slices.Sort(out)
	return out
}

func TestAnEntryWhoseTextCannotBeFetchedIsLeftOutAndKeptOnTheNextRun(t *testing.T) {
	g, base := newGOVUK(t)
	g.set(func(g *govuk) { g.failing["/api/content"+speech] = http.StatusInternalServerError })
	c, store := collectorFor(t, govukAt(base))
	appointmentID := base + appointment + "#2026-10-15T11:00:00+01:00"
	speechID := base + speech + "#2026-10-16T12:00:00+01:00"

	c.Run(t.Context(), now)

	if got := entryIDs(t, store); !slices.Equal(got, []string{appointmentID}) {
		t.Errorf("after a failed text fetch the Record holds %v, want only the appointment", got)
	}

	g.set(func(g *govuk) { delete(g.failing, "/api/content"+speech) })
	c.Run(t.Context(), now.Add(time.Hour))

	if got := entryIDs(t, store); !slices.Equal(got, []string{appointmentID, speechID}) {
		t.Errorf("once the fetch succeeds the Record holds %v, want both", got)
	}
	it, _, err := store.ReadItem(t.Context(), govukAt(base).ID, speechID)
	if err != nil || it.Content != "<p>Motherwell.</p>" {
		t.Errorf("speech kept with text %q (%v), want its body", it.Content, err)
	}
}

func TestOnlyANewEntrysTextIsFetched(t *testing.T) {
	g, base := newGOVUK(t)
	c, _ := collectorFor(t, govukAt(base))

	c.Run(t.Context(), now)
	// The feed changes, still carrying both entries.
	g.set(func(g *govuk) { g.etag = `"v2"` })
	c.Run(t.Context(), now.Add(time.Hour))

	if n := len(g.requestsFor(govukFeedPath)); n != 2 {
		t.Fatalf("feed fetched %d times, want 2", n)
	}
	if n := len(g.requestsFor("/api/content" + appointment)); n != 1 {
		t.Errorf("an entry's text fetched %d times over two runs, want once, while it was new", n)
	}
}

func TestTextIsFetchedPolitely(t *testing.T) {
	g, base := newGOVUK(t)
	g.set(func(g *govuk) {
		g.failing["/api/content"+speech] = http.StatusServiceUnavailable
		g.retryAfter = "120"
	})
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "go-slow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	src := govukAt(base)
	if err := store.SyncSources(t.Context(), []collect.Source{src}, now); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var slept []time.Duration
	c := collect.New(collect.Deps{
		Sources: []collect.Source{src},
		Fetcher: polite.New(polite.Options{
			HTTP:      http.DefaultClient,
			UserAgent: polite.UserAgent("owner@example.org"),
			Now:       func() time.Time { return now },
			Sleep: func(_ context.Context, d time.Duration) error {
				mu.Lock()
				defer mu.Unlock()
				slept = append(slept, d)
				return nil
			},
		}),
		Store: store,
		Check: interest.New(store),
		Log:   slog.New(slog.DiscardHandler),
	})

	c.Run(t.Context(), now)
	g.set(func(g *govuk) { delete(g.failing, "/api/content"+speech) })
	c.Run(t.Context(), now.Add(time.Hour))

	for _, path := range []string{"/api/content" + appointment, "/api/content" + speech} {
		for _, r := range g.requestsFor(path) {
			if ua := r.Header.Get("User-Agent"); ua != polite.UserAgent("owner@example.org") {
				t.Errorf("%s fetched as %q, not as go-slow", path, ua)
			}
		}
	}
	if !slices.Contains(slept, 120*time.Second) {
		t.Errorf("waited %v before asking GOV.UK again, want the 120s its Retry-After asked for", slept)
	}
	var most int
	g.set(func(g *govuk) { most = g.maxFlight })
	if most != 1 {
		t.Errorf("GOV.UK saw %d requests at once, want one at a time", most)
	}
}

func TestTheCheckTiesANewItemByATermFoundOnlyInItsFullText(t *testing.T) {
	_, base := newGOVUK(t)
	c, store := collectorFor(t, govukAt(base))
	if _, errs, err := interest.New(store).Add(t.Context(), interest.Draft{
		Name: "Omagh", Terms: "Omagh",
	}); err != nil || errs != nil {
		t.Fatalf("add Topic: %v %v", errs, err)
	}

	c.Run(t.Context(), now)

	items, err := store.ListItems(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		want := []string(nil)
		if strings.Contains(it.EntryID, appointment) {
			want = []string{"Omagh"}
		}
		if !slices.Equal(it.Topics, want) {
			t.Errorf("%s tied to %v, want %v", it.Title, it.Topics, want)
		}
	}
}

// statementsAt is Parliament's written statements served from base, whose
// entries' text is the statement itself.
func statementsAt(base string) collect.Source {
	return collect.Source{
		ID:          "parliament-written-statements",
		Name:        "UK Parliament: written statements",
		FeedURL:     base + "/api/writtenstatements/statements?take=50",
		Format:      collect.WrittenStatements,
		Text:        collect.TextFromWrittenStatement,
		Licence:     collect.OPLv3,
		Attribution: collect.OPL,
		Keeps:       collect.KeepFullText,
		UsualGap:    10 * 24 * time.Hour,
	}
}

func TestANewWrittenStatementItemIsKeptWithTheStatementsFullText(t *testing.T) {
	_, base := newAPI(t, map[string]string{
		"/api/writtenstatements/statements":         writtenStatements,
		"/api/writtenstatements/statements/1952546": testdata(t, "written-statement-1952546.json"),
	})
	src := statementsAt(base)
	c, store := collectorFor(t, src)

	c.Run(t.Context(), now)

	it, ok, err := store.ReadItem(t.Context(), src.ID, "1952546")
	if err != nil || !ok {
		t.Fatalf("Item not kept: %v %v", ok, err)
	}
	if !strings.Contains(it.Content, "Companies House and HMRC") {
		t.Errorf("Item kept without the statement's full text: %q", it.Content)
	}
}
