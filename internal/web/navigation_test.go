package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// indexes are the pages with an index of their own, in the navigation's
// order.
var indexes = []string{"/", "/briefings", "/stories", "/items", "/topics", "/feedback", "/register"}

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

func TestAPageThatIsNoIndexCarriesTheNavigationWithNoLinkMarkedCurrent(t *testing.T) {
	store := openStore(t)
	spokenBriefing(t, store)
	app, _ := newAppWithLog(t, saturday.AddDate(0, 0, 1), store) // Saturday's Briefing is past

	briefing := get(t, app, "/briefings/2026-10-24").body
	paths := []string{"/briefings/2026-10-24", "/briefings/2026-10-24/trail"}
	for _, link := range []string{`/stories/\d+`, `/items/[^"#]+`} {
		m := regexp.MustCompile(`href="(` + link + `)"`).FindStringSubmatch(briefing)
		if m == nil {
			t.Fatalf("the Briefing links no %s:\n%s", link, briefing)
		}
		paths = append(paths, m[1])
	}
	trail := get(t, app, "/briefings/2026-10-24/trail").body
	call := regexp.MustCompile(`href="(/calls/\d+)"`).FindStringSubmatch(trail)
	if call == nil {
		t.Fatalf("the trail links no call:\n%s", trail)
	}
	paths = append(paths, call[1])

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

func TestTodaysBriefingByItsDateIsTheNavigationsBriefing(t *testing.T) {
	app := newApp(t, saturday)

	res := get(t, app, "/briefings/2026-10-24")
	if got := current(t, "/briefings/2026-10-24", res.body); len(got) != 1 || got[0] != "/" {
		t.Errorf("links marked current %v, want only /", got)
	}
}

func TestTheBriefingsStatusPollIsAFragmentWithNoNavigation(t *testing.T) {
	app := newApp(t, saturday) // past the set time, so the Briefing is being written

	res := get(t, app, "/briefings/2026-10-24/status")
	if res.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.status)
	}
	if strings.Contains(res.body, "<nav") {
		t.Errorf("the fragment carries a navigation:\n%s", res.body)
	}
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

// top is what a page's main content shows around its heading: from the start
// of main to the end of the paragraph after the heading.
var top = regexp.MustCompile(`(?s)<main>(.*?<h1[^>]*>.*?</h1>\s*(?:<p>.*?</p>)?)`)

func TestNoPageRepeatsTheNavigationAboveItsContent(t *testing.T) {
	store := openStore(t)
	spokenBriefing(t, store)
	app, _ := newAppWithLog(t, saturday, store)

	paths := append([]string{"/briefings/2026-10-24/trail"}, indexes...)
	for page, link := range map[string]string{
		"/":                           `/stories/\d+`,
		"/briefings/2026-10-24":       `/items/[^"#]+`,
		"/briefings/2026-10-24/trail": `/calls/\d+`,
	} {
		body := get(t, app, page).body
		m := regexp.MustCompile(`href="(` + link + `)"`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("GET %s links no %s:\n%s", page, link, body)
		}
		paths = append(paths, m[1])
	}
	for _, path := range paths {
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

func TestOnEachIndexItsOwnLinkAloneIsMarkedCurrent(t *testing.T) {
	app := newApp(t, saturday)

	for _, path := range indexes {
		res := get(t, app, path)
		if got := current(t, path, res.body); len(got) != 1 || got[0] != path {
			t.Errorf("GET %s: links marked current %v, want only %s", path, got, path)
		}
	}
}
