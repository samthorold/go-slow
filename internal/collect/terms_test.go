package collect_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
)

const fullEntry = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>tag:example.org,2026:poll-1</id>
    <updated>2026-10-23T18:00:00+01:00</updated>
    <link href="https://example.org/poll-1"/>
    <title>Voting intention, October</title>
    <summary>Labour 30, Reform 28.</summary>
    <content type="html">The full tables.</content>
  </entry>
</feed>`

// fakeFetcher answers every URL with body, and records the URLs asked for.
type fakeFetcher struct {
	body string
	urls []string
}

func (f *fakeFetcher) Fetch(_ context.Context, url string, _ collect.Validators) (collect.Fetched, error) {
	f.urls = append(f.urls, url)
	return collect.Fetched{Body: []byte(f.body)}, nil
}

// fakeStore keeps what Collection gives it.
type fakeStore struct {
	items []collect.Item
}

func (s *fakeStore) Validators(context.Context, string) (collect.Validators, error) {
	return collect.Validators{}, nil
}

func (s *fakeStore) SaveValidators(context.Context, string, collect.Validators) error { return nil }

func (s *fakeStore) AddItems(_ context.Context, items []collect.Item) (int, error) {
	s.items = append(s.items, items...)
	return len(items), nil
}

func (s *fakeStore) Known(_ context.Context, sourceID string, _ []string) (map[string]bool, error) {
	known := map[string]bool{}
	for _, it := range s.items {
		if it.SourceID == sourceID {
			known[it.EntryID] = true
		}
	}
	return known, nil
}

func runOnce(sources []collect.Source, body string) (*fakeFetcher, *fakeStore) {
	f, s := &fakeFetcher{body: body}, &fakeStore{}
	collect.New(collect.Deps{
		Sources: sources,
		Fetcher: f,
		Store:   s,
		Log:     slog.New(slog.DiscardHandler),
	}).Run(context.Background(), now)
	return f, s
}

func pollster(keeps collect.Keep) collect.Source {
	return collect.Source{
		ID:          "pollster",
		Name:        "A pollster",
		FeedURL:     "https://example.org/polls.atom",
		Licence:     "Published polls, free to report with attribution",
		Attribution: "Source: A pollster.",
		Keeps:       keeps,
		UsualGap:    14 * 24 * time.Hour,
	}
}

func TestAnItemIsKeptInFullWhereItsSourcesTermsAllowIt(t *testing.T) {
	_, s := runOnce([]collect.Source{pollster(collect.KeepFullText)}, fullEntry)

	if len(s.items) != 1 {
		t.Fatalf("kept %d Items, want 1", len(s.items))
	}
	it := s.items[0]
	if it.Content != "The full tables." || it.Raw == "" {
		t.Errorf("Item kept without its full text: content %q, raw %q", it.Content, it.Raw)
	}
}

func TestAnItemKeepsOnlyItsSummaryWhereItsSourcesTermsAllowNoMore(t *testing.T) {
	_, s := runOnce([]collect.Source{pollster(collect.KeepSummary)}, fullEntry)

	if len(s.items) != 1 {
		t.Fatalf("kept %d Items, want 1", len(s.items))
	}
	it := s.items[0]
	if it.Title != "Voting intention, October" || it.Link != "https://example.org/poll-1" || it.Summary != "Labour 30, Reform 28." {
		t.Errorf("Item lost what it may keep: %+v", it)
	}
	if it.Content != "" || it.Raw != "" {
		t.Errorf("Item kept more than its summary: content %q, raw %q", it.Content, it.Raw)
	}
	if it.Attribution != "Source: A pollster." {
		t.Errorf("Item attribution = %q", it.Attribution)
	}
}

func TestASourceKeepingOnlySummariesFetchesAndKeepsNoText(t *testing.T) {
	s := pollster(collect.KeepSummary)
	s.Text = collect.TextFromGOVUKContent

	f, store := runOnce([]collect.Source{s}, fullEntry)

	if len(f.urls) != 1 || f.urls[0] != s.FeedURL {
		t.Errorf("fetched %v, want only the feed", f.urls)
	}
	if len(store.items) != 1 || store.items[0].Content != "" {
		t.Errorf("kept %+v, want one Item with no text", store.items)
	}
}

func TestTheListedSourcesWhoseFeedsCarryOnlySummariesFetchTheirText(t *testing.T) {
	for _, s := range collect.Sources() {
		var want collect.Text
		switch {
		case s.Format == collect.Atom && strings.HasPrefix(s.FeedURL, "https://www.gov.uk/"):
			want = collect.TextFromGOVUKContent
		case s.Format == collect.WrittenStatements:
			want = collect.TextFromWrittenStatement
		default:
			want = collect.TextInEntry
		}
		if s.Text != want {
			t.Errorf("%s gets its text by %d, want %d", s.ID, s.Text, want)
		}
	}
}

func TestEveryListedSourceDeclaresItsTermsAndIsCollected(t *testing.T) {
	sources := collect.Sources()
	ids := map[string]bool{}
	for _, s := range sources {
		if s.Licence == "" || s.Attribution == "" || s.Keeps == collect.KeepUndeclared || s.UsualGap <= 0 {
			t.Errorf("%s does not declare its licence, attribution, what may be kept and its usual gap: %+v", s.ID, s)
		}
		if ids[s.ID] {
			t.Errorf("two Sources have the id %s", s.ID)
		}
		ids[s.ID] = true
	}

	f, _ := runOnce(sources, "")

	if len(f.urls) != len(sources) {
		t.Errorf("fetched %d of %d Sources: %v", len(f.urls), len(sources), f.urls)
	}
}

// A citation names its Source by who is speaking, not where it is
// published: every listed Source has a short name, and Sources from one
// speaker share it.
func TestEveryListedSourceHasAShortNameSayingWhoIsSpeaking(t *testing.T) {
	want := map[string]string{
		"govuk-number-10":               "No 10",
		"govuk-hm-treasury":             "Treasury",
		"parliament-written-statements": "Parliament",
		"ons-releases":                  "ONS",
		"ons-release-calendar":          "ONS",
		"govuk-uk-space-agency":         "UK Space Agency",
	}
	listed := map[string]bool{}
	for _, s := range collect.Sources() {
		listed[s.ID] = true
		if s.ShortName == "" {
			t.Errorf("%s has no short name", s.ID)
			continue
		}
		if w, ok := want[s.ID]; ok && s.ShortName != w {
			t.Errorf("%s is short-named %q, want %q", s.ID, s.ShortName, w)
		}
	}
	for id := range want {
		if !listed[id] {
			t.Errorf("no Source %s is listed", id)
		}
	}
}

// The UK Space Agency publishes on GOV.UK as Number 10 does: its own
// organisation's Atom feed, each entry's text from the Content API, under
// the OGL. The GOV.UK Atom and Content API tests cover collecting it.
func TestTheUKSpaceAgencyIsCollectedFromItsGOVUKFeed(t *testing.T) {
	for _, s := range collect.Sources() {
		if s.ID != "govuk-uk-space-agency" {
			continue
		}
		if s.FeedURL != "https://www.gov.uk/government/organisations/uk-space-agency.atom" || s.Format != collect.Atom {
			t.Errorf("UK Space Agency read from %s as format %d", s.FeedURL, s.Format)
		}
		if s.Text != collect.TextFromGOVUKContent || s.Licence != collect.OGLv3 || s.Attribution != collect.OGL || s.Keeps != collect.KeepFullText {
			t.Errorf("UK Space Agency not declared as a GOV.UK Source under the OGL keeping full text: %+v", s)
		}
		return
	}
	t.Error("no UK Space Agency Source is listed")
}

func TestTheListedSourcesIncludeParliamentAndTheONS(t *testing.T) {
	want := map[collect.Format]bool{
		collect.Atom: false, collect.ONSReleases: false,
		collect.ONSReleaseCalendar: false, collect.WrittenStatements: false,
	}
	for _, s := range collect.Sources() {
		want[s.Format] = true
	}
	for f, found := range want {
		if !found {
			t.Errorf("no Source is read as format %d", f)
		}
	}
}

func TestASourceIsFetchedNoMoreOftenThanItsOwnRateLimit(t *testing.T) {
	hourly := pollster(collect.KeepFullText)
	hourly.MinInterval = time.Hour
	every := pollster(collect.KeepFullText)
	every.ID, every.FeedURL = "every-run", "https://example.org/every.atom"
	f := &fakeFetcher{body: fullEntry}
	c := collect.New(collect.Deps{
		Sources: []collect.Source{hourly, every},
		Fetcher: f,
		Store:   &fakeStore{},
		Log:     slog.New(slog.DiscardHandler),
	})

	for _, at := range []time.Duration{0, 15 * time.Minute, 45 * time.Minute, time.Hour, 75 * time.Minute} {
		c.Run(t.Context(), now.Add(at))
	}

	counts := map[string]int{}
	for _, u := range f.urls {
		counts[u]++
	}
	if n := counts[hourly.FeedURL]; n != 2 {
		t.Errorf("hourly Source fetched %d times over 75 minutes, want 2 (at 0 and 60)", n)
	}
	if n := counts[every.FeedURL]; n != 5 {
		t.Errorf("Source with no rate limit fetched %d times in 5 runs, want 5", n)
	}
}

func TestParliamentsMainSiteAndHansardAreNeverFetched(t *testing.T) {
	for _, url := range []string{
		"https://www.parliament.uk/business/news/rss",
		"https://parliament.uk/feed",
		"https://hansard.parliament.uk/rss/Commons.rss",
		"https://HANSARD.parliament.uk:443/feed",
	} {
		t.Run(url, func(t *testing.T) {
			s := pollster(collect.KeepFullText)
			s.FeedURL = url

			f, _ := runOnce([]collect.Source{s}, fullEntry)

			if len(f.urls) != 0 {
				t.Errorf("fetched %v", f.urls)
			}
		})
	}
}

func TestASourceThatDeclaresNoTermsIsNotCollected(t *testing.T) {
	f, s := runOnce([]collect.Source{pollster(collect.KeepUndeclared)}, fullEntry)

	if len(f.urls) != 0 || len(s.items) != 0 {
		t.Errorf("a Source with no declared terms was fetched %d times and kept %d Items", len(f.urls), len(s.items))
	}
}
