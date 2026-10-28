package web_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// fullPages are pages every Record has, each rendered in the layout.
var fullPages = []string{"/", "/briefings", "/stories", "/items", "/topics", "/feedback", "/register"}

// stylesheetLink matches a layout's link to a stylesheet, as served.
var stylesheetLink = regexp.MustCompile(`<link rel="stylesheet" href="([^"]+)">`)

// loaded matches what a page's head loads (scripts and linked resources),
// and what a stylesheet loads (imports and url()s).
var (
	pageLoads = regexp.MustCompile(`<(?:script|link)\b[^>]*\b(?:src|href)="([^"]*)"`)
	cssLoads  = regexp.MustCompile(`@import\s+(?:url\()?\s*["']?([^"')\s;]+)|url\(\s*["']?([^"')\s]+)`)
)

func TestPagesAndTheStylesheetLoadNothingFromAnotherHost(t *testing.T) {
	app := newApp(t, saturday)

	for _, path := range fullPages {
		res := get(t, app, path)
		for _, m := range pageLoads.FindAllStringSubmatch(res.body, -1) {
			if !strings.HasPrefix(m[1], "/") || strings.HasPrefix(m[1], "//") {
				t.Errorf("GET %s loads %q, want a path on go-slow itself", path, m[1])
			}
			if strings.HasSuffix(m[1], ".css") {
				css := get(t, app, m[1])
				for _, c := range cssLoads.FindAllStringSubmatch(css.body, -1) {
					target := c[1] + c[2]
					if strings.Contains(target, "//") {
						t.Errorf("%s loads %q, want nothing from another host", m[1], target)
					}
				}
			}
		}
	}
}

// How it looks is judged in a browser; what the server can be held to is
// that the stylesheet carries the rules later pages rely on: keyboard focus
// outlined on everything that takes it, the quiet style as a class any block
// can take (the Briefing's citations will), a failed request standing out, and
// a hue per status any page can take.
func TestTheStylesheetCarriesFocusTheQuietStyleRequestErrorsAndStatusHues(t *testing.T) {
	app := newApp(t, saturday)
	link := stylesheetLink.FindStringSubmatch(get(t, app, "/").body)
	if link == nil {
		t.Fatal("the Briefing links no stylesheet")
	}
	css := get(t, app, link[1]).body

	for name, rule := range map[string]*regexp.Regexp{
		"keyboard focus outlined":        regexp.MustCompile(`(?m)^:focus-visible\s*\{[^}]*outline:\s*\d`),
		"the quiet class":                regexp.MustCompile(`(?m)^\.quiet\s*\{`),
		"the quiet class on hover/focus": regexp.MustCompile(`\.quiet:focus-within`),
		"a failed request standing out":  regexp.MustCompile(`#request-errors:not\(:empty\)\s*\{[^}]*background:`),
		// The register's statuses; a call's outcomes take the same hues.
		"a status":        regexp.MustCompile(`(?m)^\.status\s*\{`),
		"supported's hue": regexp.MustCompile(`(?m)^\.status-supported\s*\{[^}]*color:`),
		"refuted's hue":   regexp.MustCompile(`(?m)^\.status-refuted\s*\{[^}]*color:`),
		"undecided's hue": regexp.MustCompile(`(?m)^\.status-undecided\s*\{[^}]*color:`),
		"Due's hue":       regexp.MustCompile(`(?m)^\.status-due\s*\{[^}]*color:`),
	} {
		if !rule.MatchString(css) {
			t.Errorf("%s has no rule for %s", link[1], name)
		}
	}
}

func TestEveryPageLinksTheStylesheetServedAsCSS(t *testing.T) {
	app := newApp(t, saturday)

	for _, path := range fullPages {
		res := get(t, app, path)
		if res.status != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want 200", path, res.status)
		}
		links := stylesheetLink.FindAllStringSubmatch(res.body, -1)
		if len(links) != 1 {
			t.Fatalf("GET %s: %d stylesheets linked, want 1:\n%s", path, len(links), res.body)
		}
		css := get(t, app, links[0][1])
		if css.status != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want 200", links[0][1], css.status)
		}
		if ct := css.header.Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
			t.Errorf("GET %s: Content-Type = %q, want text/css", links[0][1], ct)
		}
	}
}
