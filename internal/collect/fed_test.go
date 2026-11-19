package collect_test

import (
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
)

const fedSite = "https://www.federalreserve.gov"

// fedRelease is the press release in the recorded press release feed, whose
// page is recorded beside it.
const fedRelease = "/newsevents/pressreleases/monetary20261112a.htm"

// fedAt is the listed Fed Source with id, served from base: its recorded
// feed at its feed's path, and pages, each path with its recorded copy. The
// recorded copies link to www.federalreserve.gov; served from base, their
// entries' pages are fetched from the fake.
func fedAt(t *testing.T, id, feed string, pages map[string]string) (*api, collect.Source, string) {
	t.Helper()
	src := listed(t, id)
	feedPath, ok := strings.CutPrefix(src.FeedURL, fedSite)
	if !ok {
		t.Fatalf("%s's feed is not on the Fed's site: %s", id, src.FeedURL)
	}
	pub, base := newAPI(t, nil)
	routes := map[string]string{feedPath: strings.ReplaceAll(testdata(t, feed), fedSite, base)}
	for path, name := range pages {
		routes[path] = testdata(t, name)
	}
	pub.routes = routes
	src.FeedURL = base + feedPath
	return pub, src, base
}

func TestTheFedsPressReleasesAreCollectedWithTheirPagesArticleBody(t *testing.T) {
	_, src, base := fedAt(t, "fed-press-releases", "fed-press-releases.rss",
		map[string]string{fedRelease: "fed-monetary20261112a.htm"})
	c, store := collectorFor(t, src)

	c.Run(t.Context(), now)

	it, ok, err := store.KeptItem(t.Context(), src.ID, base+fedRelease)
	if err != nil || !ok {
		t.Fatalf("release not kept: %v %v", ok, err)
	}
	if !strings.HasPrefix(it.Content, "<p>The Federal Reserve on Thursday announced a revised tentative schedule") {
		t.Errorf("release kept without its article body as its text: %.200q", it.Content)
	}
	if !strings.Contains(it.Content, "<p>January 25-26, 2028 (Tuesday-Wednesday)</p>") {
		t.Errorf("release kept without the whole of its article body: %q", it.Content)
	}
	// The page's chrome and the heading above the body are not the text.
	for _, chrome := range []string{"Share", "For release at", "Last Update", "Board of Governors"} {
		if strings.Contains(it.Content, chrome) {
			t.Errorf("release's text carries the page's %q: %q", chrome, it.Content)
		}
	}
	if it.Attribution != collect.FedCitation {
		t.Errorf("release attributed %q, want the Board's citation", it.Attribution)
	}
}

const fedSpeech = "/newsevents/speech/waller20261117a.htm"

func TestTheFedsSpeechesAreCollectedWithTheirPagesArticleBody(t *testing.T) {
	_, src, base := fedAt(t, "fed-speeches", "fed-speeches.rss",
		map[string]string{fedSpeech: "fed-waller20261117a.htm"})
	c, store := collectorFor(t, src)

	c.Run(t.Context(), now)

	it, ok, err := store.KeptItem(t.Context(), src.ID, base+fedSpeech)
	if err != nil || !ok {
		t.Fatalf("speech not kept: %v %v", ok, err)
	}
	if !strings.HasPrefix(it.Content, "<p>Thank you for inviting me to return and speak today.") {
		t.Errorf("speech kept without its article body as its text: %.200q", it.Content)
	}
	// Its footnotes are part of the body.
	if !strings.Contains(it.Content, "preparing this text") {
		t.Errorf("speech kept without its footnotes: %q", it.Content)
	}
	for _, chrome := range []string{"Watch Live", "Governor Christopher J. Waller</a>", "Last Update"} {
		if strings.Contains(it.Content, chrome) {
			t.Errorf("speech's text carries the page's %q: %q", chrome, it.Content)
		}
	}
	if it.Attribution != collect.FedCitation {
		t.Errorf("speech attributed %q, want the Board's citation", it.Attribution)
	}
}

const fedTestimony = "/newsevents/testimony/warsh20260714a.htm"

func TestTheFedsTestimonyIsCollectedWithItsPagesArticleBody(t *testing.T) {
	_, src, base := fedAt(t, "fed-testimony", "fed-testimony.rss",
		map[string]string{fedTestimony: "fed-warsh20260714a.htm"})
	c, store := collectorFor(t, src)

	c.Run(t.Context(), now)

	it, ok, err := store.KeptItem(t.Context(), src.ID, base+fedTestimony)
	if err != nil || !ok {
		t.Fatalf("testimony not kept: %v %v", ok, err)
	}
	// The body opens with the Board's note that the same remarks went to
	// the Senate, which is part of the testimony as published.
	if !strings.HasPrefix(it.Content, "<p><em><strong>Chairman Warsh submitted identical remarks") {
		t.Errorf("testimony kept without its article body as its text: %.200q", it.Content)
	}
	if !strings.Contains(it.Content, "<p>Thank you, and I welcome your questions.</p>") {
		t.Errorf("testimony kept without the whole of its article body: %q", it.Content)
	}
	// Nor the heading, nor the related content panel beside the body.
	for _, chrome := range []string{"Watch Live", "Chairman Kevin Warsh</a>", "Hearing Transcripts", "Last Update"} {
		if strings.Contains(it.Content, chrome) {
			t.Errorf("testimony's text carries the page's %q: %q", chrome, it.Content)
		}
	}
	if it.Attribution != collect.FedCitation {
		t.Errorf("testimony attributed %q, want the Board's citation", it.Attribution)
	}
}

// The Fed publishes speeches and testimony in separate feeds, and a
// combined one. Each of its Sources reads its own, never the combined feed,
// so no speech is collected by both.
func TestNoTwoListedSourcesReadTheSameFeedAndNoneTheFedsCombinedOne(t *testing.T) {
	seen := map[string]string{}
	for _, s := range collect.Sources() {
		if other, ok := seen[s.FeedURL]; ok {
			t.Errorf("%s and %s both read %s", other, s.ID, s.FeedURL)
		}
		seen[s.FeedURL] = s.ID
		if strings.HasSuffix(s.FeedURL, "/feeds/speeches_and_testimony.xml") {
			t.Errorf("%s reads the Fed's combined feed, which carries the speeches Source's entries too", s.ID)
		}
	}
}

// A page whose shape has changed has no article body to find: its fetch has
// failed, and the entry is left out, to be collected once it has one.
func TestAPageWithNoArticleBodyIsAFailedFetchAndSeenAgainNextRun(t *testing.T) {
	pub, src, base := fedAt(t, "fed-press-releases", "fed-press-releases.rss", nil)
	page := testdata(t, "fed-monetary20261112a.htm")
	pub.mu.Lock()
	pub.routes[fedRelease] = strings.ReplaceAll(page, `id="article"`, `id="story"`)
	pub.mu.Unlock()
	c, store := collectorFor(t, src)

	c.Run(t.Context(), now)

	if got := entryIDs(t, store); len(got) != 0 {
		t.Fatalf("a release whose page has no article body was kept: %v", got)
	}

	pub.mu.Lock()
	pub.routes[fedRelease] = page
	pub.mu.Unlock()
	c.Run(t.Context(), now.Add(time.Hour))

	if got := entryIDs(t, store); len(got) != 1 || got[0] != base+fedRelease {
		t.Errorf("once its page has an article body the Record holds %v, want the release", got)
	}
}
