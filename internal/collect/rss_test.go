package collect_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
)

const rssFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
<channel>
  <title>An agency</title>
  <link>https://example.org</link>
  <item>
    <title> Launch moved to Thursday </title>
    <link>https://example.org/news/launch-moved/</link>
    <pubDate>Tue, 17 Nov 2026 17:00:29 +0000</pubDate>
    <guid isPermaLink="false">https://example.org/?p=1065857</guid>
    <description><![CDATA[The launch moves to Thursday [&#8230;]]]></description>
    <content:encoded><![CDATA[<p>The launch moves to Thursday, the agency said.</p>]]></content:encoded>
  </item>
</channel>
</rss>`

func agency() collect.Source {
	return collect.Source{
		ID:          "agency",
		Name:        "An agency",
		FeedURL:     "https://example.org/feed/",
		Format:      collect.RSS,
		Licence:     "Public domain",
		Attribution: "Source: An agency.",
		Keeps:       collect.KeepFullText,
		UsualGap:    7 * 24 * time.Hour,
	}
}

func TestAnRSSItemIsCollectedWithItsFullTextFromContentEncoded(t *testing.T) {
	_, s := runOnce([]collect.Source{agency()}, rssFeed)

	if len(s.items) != 1 {
		t.Fatalf("kept %d Items, want 1", len(s.items))
	}
	it := s.items[0]
	if it.EntryID != "https://example.org/?p=1065857" {
		t.Errorf("entry id = %q, want the guid", it.EntryID)
	}
	if it.Title != "Launch moved to Thursday" || it.Link != "https://example.org/news/launch-moved/" {
		t.Errorf("title %q, link %q", it.Title, it.Link)
	}
	// The description is HTML, kept as the feed carried it, as an Atom
	// summary is.
	if it.Summary != "The launch moves to Thursday [&#8230;]" {
		t.Errorf("summary = %q, want the description", it.Summary)
	}
	if it.Content != "<p>The launch moves to Thursday, the agency said.</p>" {
		t.Errorf("content = %q, want content:encoded", it.Content)
	}
	if want := time.Date(2026, time.November, 17, 17, 0, 29, 0, time.UTC); !it.Published.Equal(want) {
		t.Errorf("published %v, want %v", it.Published, want)
	}
	if it.Raw == "" || it.Attribution != "Source: An agency." {
		t.Errorf("raw %q, attribution %q", it.Raw, it.Attribution)
	}
}

// A feed that carries more than the Source's own entries is read for those
// alone, known by where they link.
func TestASourceKeepsOnlyTheEntriesLinkingUnderItsPrefix(t *testing.T) {
	feed := strings.Replace(rssFeed, "</channel>", `  <item>
    <title>Space Selfie</title>
    <link>https://example.org/image-article/space-selfie/</link>
    <guid>https://example.org/?p=1065249</guid>
  </item>
</channel>`, 1)
	src := agency()
	src.LinkPrefix = "https://example.org/news/"

	_, s := runOnce([]collect.Source{src}, feed)

	if len(s.items) != 1 || s.items[0].Title != "Launch moved to Thursday" {
		t.Errorf("kept %+v, want only the news entry", s.items)
	}
}

// NASA's news release feed carries image posts and APOD beside its
// releases; the recorded copy has two releases, an image post and an APOD.
func TestNASAsNewsReleasesAreCollectedFromItsFeedWithTheirText(t *testing.T) {
	src := listed(t, "nasa-news-releases")
	feedPath, ok := strings.CutPrefix(src.FeedURL, "https://www.nasa.gov")
	if !ok {
		t.Fatalf("NASA's feed is not on www.nasa.gov: %s", src.FeedURL)
	}
	_, base := newAPI(t, map[string]string{feedPath: testdata(t, "nasa-news-releases.rss")})
	src.FeedURL = base + feedPath
	c, store := collectorFor(t, src)

	c.Run(t.Context(), now)

	got := entryIDs(t, store)
	want := []string{
		"https://www.nasa.gov/?post_type=press-release&p=1065302",
		"https://www.nasa.gov/?post_type=press-release&p=1065857",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the Record holds %v, want only the two releases %v", got, want)
	}
	it, _, err := store.KeptItem(t.Context(), src.ID, want[0])
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "NASA Receives 16th Consecutive ‘Clean’ Financial Audit Opinion" {
		t.Errorf("release title = %q", it.Title)
	}
	if !strings.Contains(it.Content, "For the 16th consecutive year, NASA has received") {
		t.Errorf("release kept without its text: %.200q", it.Content)
	}
	if it.Attribution != collect.NASACredit {
		t.Errorf("release attributed %q, want NASA's acknowledgement", it.Attribution)
	}
}

// RSS makes the guid optional and dates its items by RFC 822, whose zones
// may be named.
func TestAnRSSItemWithNoGUIDIsKnownByItsLinkAndItsZoneNameIsRead(t *testing.T) {
	feed := `<rss version="2.0"><channel>
  <item>
    <title>Statement on monetary policy</title>
    <link>https://example.org/statement.htm</link>
    <description>Statement on monetary policy</description>
    <pubDate>Wed, 4 Nov 2026 14:00:00 EST</pubDate>
  </item>
  <item><title>Neither id nor link</title></item>
</channel></rss>`

	_, s := runOnce([]collect.Source{agency()}, feed)

	if len(s.items) != 1 {
		t.Fatalf("kept %d Items, want only the one with a link", len(s.items))
	}
	it := s.items[0]
	if it.EntryID != "https://example.org/statement.htm" {
		t.Errorf("entry id = %q, want the link", it.EntryID)
	}
	if it.Content != "" {
		t.Errorf("content = %q, want none from an item without content:encoded", it.Content)
	}
	if want := time.Date(2026, time.November, 4, 19, 0, 0, 0, time.UTC); !it.Published.Equal(want) {
		t.Errorf("published %v, want %v", it.Published, want)
	}
}
