package web_test

import (
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/editor"
)

// iconButton matches a Feedback control as every page renders it: a small
// icon button of its kind, its full label its accessible name and its title,
// the icon itself hidden from assistive technology.
var iconButton = regexp.MustCompile(`<button class="give give-([a-z-]+)" aria-label="([^"]+)" title="([^"]+)"><span aria-hidden="true">([^<]+)</span></button>`)

// feedbackButtons are the buttons of the forms giving Feedback in html.
func feedbackButtons(html string) []string {
	var out []string
	for _, f := range regexp.MustCompile(`(?s)<form class="feedback" hx-post="[^"]+"[^>]*>(.*?)</form>`).FindAllStringSubmatch(html, -1) {
		out = append(out, regexp.MustCompile(`(?s)<button.*?</button>`).FindString(f[1]))
	}
	return out
}

// icons are the icon of each kind of Feedback, the same on every page.
var icons = map[string]string{"wrong": "✗", "new-to-me": "✦", "unwanted": "⊘", "missed": "⊕"}

func TestEveryFeedbackControlOnAnItemsPageIsAnIconButtonLabelledInFull(t *testing.T) {
	store := openStore(t)
	readRelease(t, store, releaseReading)
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, "/items/"+number10.ID+"/release").body

	buttons := feedbackButtons(page)
	if len(buttons) == 0 {
		t.Fatalf("the Item's page offers no Feedback:\n%s", page)
	}
	for _, b := range buttons {
		m := iconButton.FindStringSubmatch(b)
		if m == nil {
			t.Errorf("not an icon button labelled in full: %s", b)
			continue
		}
		if m[2] != m[3] {
			t.Errorf("its title %q is not its label %q", m[3], m[2])
		}
		if icons[m[1]] != m[4] {
			t.Errorf("%s's icon is %q, want %q", m[1], m[4], icons[m[1]])
		}
	}
	if !strings.Contains(page, `aria-label="Wrong: “the city” refers to “Derby”"`) {
		t.Errorf("no control labelled for the refers to it judges:\n%s", page)
	}
}

func TestTheSundayReadsControlsAreIconButtonsTheReadsBesideItsTitle(t *testing.T) {
	store := toldRecord(t)
	writeSundayRead(t, store, sunday, assessed)
	app, _ := newAppWithLog(t, sunday, store)

	page := get(t, app, "/sunday-reads/2026-10-25").body

	buttons := feedbackButtons(page)
	if len(buttons) < 3 {
		t.Fatalf("the read offers %d controls, want its own and its Assessment's:\n%s", len(buttons), page)
	}
	for _, b := range buttons {
		if !iconButton.MatchString(b) {
			t.Errorf("not an icon button labelled in full: %s", b)
		}
	}
	besideIt(t, page, "<h1>Sunday read</h1>", "New to me")
}

// besideIt asserts that the first Feedback control after what in html, in
// the same paragraph and before any other pill, is labelled label.
func besideIt(t *testing.T, html, what, label string) {
	t.Helper()
	at := strings.Index(html, what)
	if at < 0 {
		t.Errorf("no %q in:\n%s", what, html)
		return
	}
	rest := html[at+len(what):]
	ctrl := strings.Index(rest, `aria-label="`+label+`"`)
	if ctrl < 0 {
		t.Errorf("no control %q after %q:\n%s", label, what, rest)
		return
	}
	between := rest[:ctrl]
	if strings.Contains(between, "</p>") || strings.Contains(between, "</div>") || strings.Contains(between, `class="pill"`) ||
		strings.Contains(between, "</form>") {
		t.Errorf("the control %q does not sit beside %q:\n%s", label, what, html)
	}
	// A form's start tag closes an open paragraph, so a browser would put
	// the control on a line of its own: the line holding it is no <p>.
	before := html[:at]
	if p := max(strings.LastIndex(before, "<p>"), strings.LastIndex(before, "<p ")); p > strings.LastIndex(before, "</p>") &&
		p > strings.LastIndex(before, "<div") {
		t.Errorf("the control %q is in a paragraph, which its form would close:\n%s", label, html[p:])
	}
}

// How it looks is judged in a browser; what the server can be held to is
// that no rule hides a page's controls at rest or reveals them on hover or
// focus, and that the icon buttons are quiet at rest and take the link
// colour on hover and focus.
func TestNoControlIsHiddenAtRestAndTheIconsAreQuietUntilHoveredOrFocused(t *testing.T) {
	app := newApp(t, saturday)
	css := get(t, app, "/static/go-slow.css").body

	for _, rule := range regexp.MustCompile(`(?s)([^{}]*)\{([^}]*)\}`).FindAllStringSubmatch(css, -1) {
		sel, decl := rule[1], rule[2]
		if !strings.Contains(sel, ".actions") {
			continue
		}
		if strings.Contains(decl, "clip-path") || strings.Contains(decl, "display: none") || strings.Contains(decl, "visibility") {
			t.Errorf("a rule hides controls at rest: %s{%s}", sel, decl)
		}
		if strings.Contains(sel, ":hover") || strings.Contains(sel, ":focus-within") {
			t.Errorf("a rule reveals controls on hover or focus: %s{%s}", sel, decl)
		}
	}
	for name, rule := range map[string]*regexp.Regexp{
		"an icon button quiet at rest":      regexp.MustCompile(`(?m)^button\.give\s*\{[^}]*color:\s*var\(--muted\)`),
		"an icon button's link colour":      regexp.MustCompile(`button\.give:hover,\s*button\.give:focus-visible\s*\{[^}]*color:\s*var\(--link\)`),
		"Feedback given, in its kind's hue": regexp.MustCompile(`(?m)^\.given-wrong[^{]*\{[^}]*var\(--kind-wrong\)`),
		"a visually hidden label for given": regexp.MustCompile(`(?m)^\.visually-hidden\s*\{[^}]*clip-path:`),
	} {
		if !rule.MatchString(css) {
			t.Errorf("the stylesheet has no rule for %s", name)
		}
	}
}

func TestOnABriefingUnwantedSitsBesideEachSpanCitedAndTheStatementsControlsAtItsBlock(t *testing.T) {
	store := relatedRecord(t)
	tell(t, store, saturday, teller{text: "Derby will host the headquarters, it said.", tell: "Derby will host"})
	app, _ := newAppWithLog(t, saturday, store)

	statement := blockOf(t, get(t, app, "/").body, "Derby will host the headquarters, it said.")

	besideIt(t, statement.margin, `<span hx-disable>Derby will host the headquarters.</span></a>`,
		"Unwanted: Derby will host the headquarters.")
	for _, label := range []string{"Wrong: this statement", "New to me: this statement"} {
		if !strings.Contains(statement.margin, `aria-label="`+label+`"`) {
			t.Errorf("the statement's block has no %q:\n%s", label, statement.margin)
		}
	}
	for _, b := range feedbackButtons(statement.margin) {
		if !iconButton.MatchString(b) {
			t.Errorf("not an icon button labelled in full: %s", b)
		}
	}
}

func TestOnAnItemsPageEachControlSitsBesideWhatItJudgesAndMissedOncePerSpan(t *testing.T) {
	store := openStore(t)
	readRelease(t, store, releaseReading)
	lookups := editor.NewLookups(editor.LookupDeps{Store: store, Wikidata: derbyOnly{}, Log: slog.New(slog.DiscardHandler)})
	if err := lookups.LookUpWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, "/items/"+number10.ID+"/release").body

	s2 := spanBlock(t, page, 2).margin
	besideIt(t, s2, `<span hx-disable>Derby</span> · <span class="pill-tag">place</span></a>`, "Wrong: “Derby” is Derby")
	besideIt(t, s2, "spoken by <span hx-disable>Andy Burnham</span>", "Wrong: spoken by “Andy Burnham”")
	besideIt(t, s2, `<span hx-disable>the city</span> refers to <span hx-disable>Derby</span>`, "Wrong: “the city” refers to “Derby”")
	besideIt(t, s2, `">S2</a>`, "Missed")
	for n := 1; n <= 4; n++ {
		if c := strings.Count(spanBlock(t, page, n).margin, `aria-label="Missed"`); c != 1 {
			t.Errorf("S%d offers missed %d times, want once", n, c)
		}
	}
}
