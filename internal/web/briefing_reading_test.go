package web_test

import (
	"regexp"
	"strings"
	"testing"
)

// The Briefing looks as a Span's page does: its statements lead in the
// body column, and how each was made (its citations) and the Feedback on
// what it cites sit in its margin, which starts open. How that looks is
// judged in a browser; what the server is held to is the structure the
// stylesheet works on.

// openRegion asserts page holds its blocks in one margined region, starting
// open, with the one control that folds it, and returns the region.
func openRegion(t *testing.T, page string) string {
	t.Helper()
	regions := regexp.MustCompile(`<div class="margined" data-margin="(open|folded)">`).FindAllStringSubmatch(page, -1)
	if len(regions) != 1 || regions[0][1] != "open" {
		t.Fatalf("want one margined region, open; got %v:\n%s", regions, page)
	}
	controls := regexp.MustCompile(`<button type="button" class="margin-fold" data-margin-fold aria-expanded="true">[^<]+</button>`).FindAllString(page, -1)
	if len(controls) != 1 {
		t.Errorf("want one fold button, expanded, got %d:\n%s", len(controls), page)
	}
	if !strings.Contains(page, `<script src="/static/margin-fold.js"></script>`) {
		t.Errorf("the page does not load the fold behaviour:\n%s", page)
	}
	return page[strings.Index(page, `class="margined"`):]
}

func TestEachStatementOnTheBriefingHasItsCitationsBesideItInAMarginStartingOpen(t *testing.T) {
	app, _ := newAppWithLog(t, saturday, toldBriefing(t))

	res := get(t, app, "/")

	region := openRegion(t, res.body)
	statement := blockOf(t, region, "Derby will host the headquarters.")
	for _, want := range []string{`<span class="citation">`, `<a href="/items/number-10/hosting`,
		"<cite hx-disable>Derby to host GBR</cite>", `/items/number-10/hosting/spans/`} {
		if !strings.Contains(statement.margin, want) {
			t.Errorf("the statement's margin lacks %q:\n%s", want, statement.margin)
		}
	}
	if strings.Contains(statement.body, "Derby to host GBR") {
		t.Errorf("the Item's title is in the body column:\n%s", statement.body)
	}
	if strings.Contains(res.body, `class="citations`) {
		t.Errorf("the statement's citations are still in a disclosure:\n%s", res.body)
	}
}

// noTen is how a citation of an Item from number10 opens: its Source's
// short name, who is speaking, before the Item's title.
const noTen = `<span class="citation"><span hx-disable>No 10</span> <a href="/items/number-10/`

func TestTheBriefingCitesAnItemByItsSourcesShortNameBeforeItsTitle(t *testing.T) {
	app, _ := newAppWithLog(t, saturday, toldBriefing(t))

	res := get(t, app, "/")

	statement := blockOf(t, openRegion(t, res.body), "Derby will host the headquarters.")
	if !strings.Contains(statement.margin, noTen) {
		t.Errorf("the statement's citation does not name its Source as %q:\n%s", noTen, statement.margin)
	}
}

func TestTheStylesheetHidesTheSundayReadsControlsAtRestAndRevealsThemOnHoverOrFocus(t *testing.T) {
	app := newApp(t, saturday)
	css := get(t, app, "/static/go-slow.css").body

	// An Assessment's controls sit in its body column, which no fold hides;
	// the read's title's beside it, in its title row.
	rest := regexp.MustCompile(`(?m)^\.block-body \.actions,\s*\.title-row \.actions\s*\{([^}]*)\}`).FindStringSubmatch(css)
	if rest == nil {
		t.Fatalf("no rule hides the controls with a block's text, or a title's, at rest:\n%s", css)
	}
	if strings.Contains(rest[1], "display: none") || strings.Contains(rest[1], "visibility: hidden") {
		t.Errorf("the controls are hidden at rest so the keyboard cannot reach them:\n%s", rest[0])
	}
	for _, sel := range []string{".block:hover .block-body .actions", ".block:focus-within .block-body .actions",
		".title-row:hover .actions", ".title-row:focus-within .actions"} {
		if !strings.Contains(css, sel) {
			t.Errorf("no rule reveals the controls on %s", sel)
		}
	}
	// The fold hides only the margin.
	if regexp.MustCompile(`\[data-margin="folded"\][^{]*\.block-body`).MatchString(css) {
		t.Errorf("a folded margin hides something in the body column:\n%s", css)
	}
}
