package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"testing"
)

// indexes are the pages with an index of their own, in the navigation's
// order.
var indexes = []string{"/stories", "/items", "/topics"}

var (
	navigation = regexp.MustCompile(`(?s)<nav aria-label="Pages">(.*?)</nav>`)
	navLink    = regexp.MustCompile(`<a href="([^"]*)"([^>]*)>`)
	// ariaCurrent marks a link to the page shown.
	ariaCurrent = regexp.MustCompile(`\baria-current="page"`)
)

// navOf is the one navigation of a page, failing the test unless there is
// exactly one.
func navOf(t *testing.T, path, body string) string {
	t.Helper()
	navs := navigation.FindAllStringSubmatch(body, -1)
	if len(navs) != 1 {
		t.Fatalf("GET %s: %d navigations, want 1:\n%s", path, len(navs), body)
	}
	return navs[0][1]
}

func TestEveryIndexCarriesOneNavigationReachingEveryIndex(t *testing.T) {
	app := newApp(t, saturday)

	for _, path := range indexes {
		res := get(t, app, path)
		if res.status != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want 200", path, res.status)
		}
		var hrefs []string
		for _, l := range navLink.FindAllStringSubmatch(navOf(t, path, res.body), -1) {
			hrefs = append(hrefs, l[1])
		}
		if len(hrefs) != len(indexes) {
			t.Fatalf("GET %s: navigation links %v, want %v", path, hrefs, indexes)
		}
		for i := range indexes {
			if hrefs[i] != indexes[i] {
				t.Fatalf("GET %s: navigation links %v, want %v", path, hrefs, indexes)
			}
		}
	}
}

// current is the hrefs of the navigation's links marked as the page shown.
func current(t *testing.T, path, body string) []string {
	t.Helper()
	var hrefs []string
	for _, l := range navLink.FindAllStringSubmatch(navOf(t, path, body), -1) {
		if ariaCurrent.MatchString(l[2]) {
			hrefs = append(hrefs, l[1])
		}
	}
	return hrefs
}

// A form that fails validation answers the whole page, swapped into the
// body: it keeps the page's one navigation, rather than losing it.
func TestAFormReRenderedAfterFailingValidationKeepsItsPagesNavigation(t *testing.T) {
	app := newApp(t, saturday)

	res := send(t, app, http.MethodPost, "/topics", url.Values{"name": {""}, "depth": {"obsessive"}})
	if res.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.status)
	}
	if got := current(t, "/topics", res.body); len(got) != 1 || got[0] != "/topics" {
		t.Errorf("links marked current %v, want only /topics", got)
	}
}

// woven are the pages a woven Record reaches that are no index: a Story's,
// an Editor's Span's, a Span's, an Item's and a call's.
func woven(t *testing.T) (http.Handler, []string) {
	t.Helper()
	store := openStore(t)
	weaveSpeech(t, store, twoOpinions)
	app, _ := newAppWithLog(t, saturday, store)
	paths := []string{}
	page := "/stories"
	for _, link := range []string{`/stories/\d+`, `/stories/\d+/spans/\d+`, `/items/[^"#]+/spans/[^"#]+`, `/items/[^"#/]+/[^"#/]+`, `/calls/\d+`} {
		body := get(t, app, page).body
		m := regexp.MustCompile(`href="(` + link + `)["#]`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("GET %s links no %s:\n%s", page, link, body)
		}
		paths = append(paths, m[1])
		if page == "/stories" {
			page = m[1] // a Story's page links the rest
		}
	}
	return app, paths
}

func TestAPageThatIsNoIndexCarriesTheNavigationWithNoLinkMarkedCurrent(t *testing.T) {
	app, paths := woven(t)

	for _, path := range paths {
		res := get(t, app, path)
		if res.status != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want 200", path, res.status)
		}
		if got := current(t, path, res.body); len(got) != 0 {
			t.Errorf("GET %s: links marked current %v, want none", path, got)
		}
	}
}

// top is what a page's main content shows around its heading: from the start
// of main to the end of the paragraph after the heading.
var top = regexp.MustCompile(`(?s)<main>(.*?<h1[^>]*>.*?</h1>\s*(?:<p>.*?</p>)?)`)

func TestNoPageRepeatsTheNavigationAboveItsContent(t *testing.T) {
	app, paths := woven(t)

	for _, path := range append(paths, indexes...) {
		body := get(t, app, path).body
		m := top.FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("GET %s: no heading in main:\n%s", path, body)
		}
		for _, l := range navLink.FindAllStringSubmatch(m[1], -1) {
			for _, index := range indexes {
				if l[1] == index {
					t.Errorf("GET %s: links %s at its top, repeating the navigation:\n%s", path, index, m[1])
				}
			}
		}
	}
}

// The Owner reads Stories: the app's root lands on them.
func TestTheRootLandsOnTheStories(t *testing.T) {
	app := newApp(t, saturday)

	res := get(t, app, "/")
	if res.status != http.StatusSeeOther || res.location != "/stories" {
		t.Errorf("GET / = %d to %q, want 303 to /stories", res.status, res.location)
	}
}

// The old design's acts are retired: none of their pages, or the Feedback
// and register that read them, is served.
func TestNoPageOfTheRetiredActsIsServed(t *testing.T) {
	app := newApp(t, saturday)

	for _, path := range []string{
		"/briefings", "/briefings/2026-10-24", "/briefings/2026-10-24/status", "/briefings/2026-10-24/trail",
		"/sunday-reads/2026-10-25", "/assessments/1", "/feedback", "/register",
	} {
		if res := get(t, app, path); res.status != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", path, res.status)
		}
	}
	for _, path := range []string{
		"/items/s/e/feedback", "/items/s/e/spans/0-4/feedback", "/briefings/2026-10-24/feedback", "/sunday-reads/2026-10-25/feedback",
	} {
		if res := send(t, app, http.MethodPost, path, url.Values{"kind": {"wrong"}}); res.status != http.StatusNotFound && res.status != http.StatusMethodNotAllowed {
			t.Errorf("POST %s: status = %d, want it not served", path, res.status)
		}
	}
}

func TestOnEachIndexItsOwnLinkAloneIsMarkedCurrent(t *testing.T) {
	app := newApp(t, saturday)

	for _, path := range indexes {
		res := get(t, app, path)
		if got := current(t, path, res.body); len(got) != 1 || got[0] != path {
			t.Errorf("GET %s: links marked current %v, want only %s", path, got, path)
		}
	}
}
