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
// can take (the Briefing's citations will), and a failed request standing out.
func TestTheStylesheetCarriesFocusTheQuietStyleAndRequestErrors(t *testing.T) {
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
	} {
		if !rule.MatchString(css) {
			t.Errorf("%s has no rule for %s", link[1], name)
		}
	}
}

// Likewise for the body-and-margin layout and its visual vocabulary, which
// every page showing the Editor's work shares: a block's margin beside its
// body, folded away in a folded region, dropped below the block on a narrow
// window; Feedback actions in the margin revealed on hover and focus; and
// the pill, citation and machine label as classes any page can take.
func TestTheStylesheetCarriesTheBodyAndMarginLayoutAndItsVocabulary(t *testing.T) {
	app := newApp(t, saturday)
	link := stylesheetLink.FindStringSubmatch(get(t, app, "/").body)
	if link == nil {
		t.Fatal("the Briefing links no stylesheet")
	}
	css := get(t, app, link[1]).body

	for name, rule := range map[string]*regexp.Regexp{
		"a block's body and margin side by side": regexp.MustCompile(`(?m)^\.block\s*\{[^}]*grid-template-columns:`),
		"a folded region's margin hidden":        regexp.MustCompile(`\[data-margin="folded"\] \.block-margin\s*\{[^}]*display:\s*none`),
		"the margin below the block when narrow": regexp.MustCompile(`@media \(max-width: [^)]+\)\s*\{\s*\.block\s*\{[^}]*grid-template-columns:\s*1fr`),
		"actions hidden at rest":                 regexp.MustCompile(`(?m)^\.block-margin \.actions\s*\{[^}]*clip-path:`),
		"actions shown on hover and focus":       regexp.MustCompile(`\.block:hover \.block-margin \.actions,\s*\.block:focus-within \.block-margin \.actions\s*\{`),
		"the pill":                               regexp.MustCompile(`(?m)^\.pill\s*\{[^}]*border:`),
		"the pill's tag in small caps":           regexp.MustCompile(`(?m)^\.pill-tag\s*\{[^}]*small-caps`),
		"the citation's title in italics":        regexp.MustCompile(`(?m)^\.citation cite\s*\{[^}]*font-style:\s*italic`),
		"the machine label in monospace":         regexp.MustCompile(`(?m)^\.machine\s*\{[^}]*monospace`),
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
