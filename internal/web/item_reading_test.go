package web_test

import (
	"encoding/json"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// release is cut into S1 "Andy Burnham said:", S2 "“This is fantastic news
// for the city.", S3 "Derby has waited long enough.”" and S4 "He visited
// the site on Monday."
func release() collect.Item {
	it := item("release", "GBR headquarters in Derby", saturday.Add(-24*time.Hour))
	it.Content = "<p>Andy Burnham said:</p><p>“This is fantastic news for the city. Derby has waited long enough.”</p><p>He visited the site on Monday.</p>"
	return it
}

const releaseReading = `{
  "names": [
    {"at": {"span": 1, "words": "Andy Burnham"}, "kind": "person", "name": "Andy Burnham", "office": "Prime Minister of the United Kingdom"},
    {"at": {"span": 3, "words": "Derby"}, "kind": "place", "name": "Derby", "office": ""}
  ],
  "refers_to": [
    {"from": {"span": 2, "words": "the city"}, "to": {"span": 3, "words": "Derby"}},
    {"from": {"span": 4, "words": "He"}, "to": {"span": 1, "words": "Andy Burnham"}}
  ],
  "rejected": [1],
  "speakers": [
    {"span": {"span": 2, "words": ""}, "speaker": {"span": 1, "words": "Andy Burnham"}},
    {"span": {"span": 3, "words": ""}, "speaker": {"span": 1, "words": "Andy Burnham"}}
  ]
}`

// readRelease seeds the release, cuts it and reads it with output.
func readRelease(t *testing.T, store *sqlite.Store, output string) {
	t.Helper()
	seed(t, store, saturday.AddDate(0, 0, -7), release())
	if err := cut.New(store).CutNew(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Deps{Store: store, Model: fakeModel{output}, Log: slog.New(slog.DiscardHandler)})
	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
}

// spanBlock is the block of the Item's page showing Span n.
func spanBlock(t *testing.T, page string, n int) block {
	t.Helper()
	at := strings.Index(page, `id="span-`+string(rune('0'+n))+`"`)
	if at < 0 {
		t.Fatalf("the page has no S%d:\n%s", n, page)
	}
	rest := page[at:]
	bodyAt := strings.Index(rest, `class="block-body`)
	marginAt := strings.Index(rest, `class="block-margin`)
	end := strings.Index(rest, "</li>")
	if bodyAt < 0 || marginAt < bodyAt || end < marginAt {
		t.Fatalf("S%d has no body column then margin:\n%s", n, rest)
	}
	return block{body: rest[bodyAt:marginAt], margin: rest[marginAt:end]}
}

func TestAnItemsPageShowsItsSpansHydratedWithTheirSpeakersNamesAndRejections(t *testing.T) {
	store := openStore(t)
	readRelease(t, store, releaseReading)
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, "/items/"+number10.ID+"/release").body

	// What a Span refers to is read in its place, with the words it was.
	quote := spanBlock(t, page, 2)
	if !strings.Contains(quote.body, `“This is fantastic news for <ins title="the city">Derby</ins>.`) {
		t.Errorf("S2 is not read hydrated:\n%s", quote.body)
	}
	if !strings.Contains(quote.margin, "spoken by <span hx-disable>Andy Burnham</span>") {
		t.Errorf("S2's margin does not say whose words it is:\n%s", quote.margin)
	}
	visit := spanBlock(t, page, 4)
	if !strings.Contains(visit.body, `<ins title="He">Andy Burnham</ins> visited the site on Monday.`) || strings.Contains(visit.margin, "spoken by") {
		t.Errorf("S4, the publisher's own, is not read hydrated:\n%s\n%s", visit.body, visit.margin)
	}
	// A Rejected Span stays, struck through, saying so.
	said := regexp.MustCompile(`<li id="span-1"[^>]*class="block struck"`)
	if !said.MatchString(page) || !strings.Contains(spanBlock(t, page, 1).margin, "rejected") {
		t.Errorf("S1 is not shown Rejected:\n%s", page)
	}
	// What a Span names is a pill: a person by their office, else its kind.
	if !strings.Contains(spanBlock(t, page, 1).margin,
		`<span class="pill"><span hx-disable>Andy Burnham</span> · <span class="pill-tag">Prime Minister of the United Kingdom</span></span>`) {
		t.Errorf("S1 does not show the name it names:\n%s", spanBlock(t, page, 1).margin)
	}
	if !strings.Contains(spanBlock(t, page, 3).margin, `<span hx-disable>Derby</span> · <span class="pill-tag">place</span>`) {
		t.Errorf("S3 does not show the place it names:\n%s", spanBlock(t, page, 3).margin)
	}
	// How it was read is one click away.
	if !regexp.MustCompile(`<a class="machine" href="/calls/\d+">read · call \d+</a>`).MatchString(page) {
		t.Errorf("the page does not link the reading's call:\n%s", page)
	}
}

func TestAnItemCutAndNotYetReadAwaitsReading(t *testing.T) {
	store := openStore(t)
	seed(t, store, saturday.AddDate(0, 0, -7), release())
	if err := cut.New(store).CutNew(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, "/items/"+number10.ID+"/release").body

	if !strings.Contains(page, "Awaiting reading.") || strings.Contains(page, "<ins") {
		t.Errorf("the page does not say the Item awaits reading:\n%s", page)
	}
}

func TestTheItemsPageSaysHowEachItemStandsWithReadingWhateverItsTopics(t *testing.T) {
	store := openStore(t)
	readRelease(t, store, releaseReading)
	awaiting := item("budget", "PM statement on the Budget", saturday.Add(-2*time.Hour))
	awaiting.Content = "<p>The Chancellor set out the Budget.</p>"
	seed(t, store, saturday.AddDate(0, 0, -7), awaiting,
		item("call", "PM call with the President", saturday.Add(-48*time.Hour)))
	if err := cut.New(store).CutNew(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, "/items").body

	for title, want := range map[string]string{
		"GBR headquarters in Derby":  "read · 2 names, 2 speakers, 1 rejected",
		"PM statement on the Budget": "awaiting reading",
		"PM call with the President": "no text yet",
	} {
		if b := blockOf(t, page, title); !strings.Contains(b.margin, `<span class="machine" hx-disable>`+want+`</span>`) {
			t.Errorf("%s is not labelled %q:\n%s", title, want, b.margin)
		}
	}
}

func TestWhatReadingJudgedIsInertOnTheItemsPage(t *testing.T) {
	store := openStore(t)
	hostile := release()
	hostile.Content = `<p>Andy Burnham said:</p><p>&lt;span hx-get="/evil"&gt;the city&lt;/span&gt; is great.</p>`
	seed(t, store, saturday.AddDate(0, 0, -7), hostile)
	if err := cut.New(store).CutNew(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(map[string]any{
		"names":     []any{map[string]any{"at": map[string]any{"span": 1, "words": "Andy Burnham"}, "kind": "person", "name": `<b hx-get="/evil">Andy</b>`, "office": ""}},
		"refers_to": []any{}, "rejected": []any{}, "speakers": []any{},
	})
	ed := editor.New(editor.Deps{Store: store, Model: fakeModel{string(out)}, Log: slog.New(slog.DiscardHandler)})
	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, "/items/"+number10.ID+"/"+url.PathEscape("release")).body

	if strings.Contains(page, `<span hx-get`) || strings.Contains(page, `<b hx-get`) {
		t.Errorf("collected text or a name was not escaped:\n%s", page)
	}
}
