package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// The Briefing looks as a Story's page does: its statements lead in the
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
	store := openStore(t)
	speechStory(t, store)
	writeStoryBriefing(t, store, saturday, spoke)
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/")

	region := openRegion(t, res.body)
	statement := blockOf(t, region, "The Prime Minister spoke in Motherwell.")
	for _, want := range []string{"Andy Burnham spoke in Motherwell.", `href="/stories/1#development-1"`,
		`<span class="citation">`, `<a href="/items/number-10/`, "<cite hx-disable>PM speech in Scotland</cite>"} {
		if !strings.Contains(statement.margin, want) {
			t.Errorf("the statement's margin lacks %q:\n%s", want, statement.margin)
		}
	}
	if strings.Contains(statement.body, "PM speech in Scotland") {
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
	store := openStore(t)
	speechStory(t, store)
	writeStoryBriefing(t, store, saturday, spoke)
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/")

	statement := blockOf(t, openRegion(t, res.body), "The Prime Minister spoke in Motherwell.")
	if !strings.Contains(statement.margin, noTen) {
		t.Errorf("the statement's citation does not name its Source as %q:\n%s", noTen, statement.margin)
	}
}

func TestEachStoryOnTheBriefingIsATitleBlockWithItsFeedbackInItsMargin(t *testing.T) {
	store := openStore(t)
	briefed(t, store)
	app, _ := newAppWithLog(t, saturday, store)
	send(t, app, "POST", today, url.Values{"kind": {"new to me"}, "level": {"story"}, "story": {"1"}})

	page := get(t, app, "/").body

	region := openRegion(t, page)
	title := blockOf(t, region, `<a href="/stories/1"><span hx-disable>Andy Burnham in Scotland</span></a>`)
	if !givenWithUndo(title.margin, "New to me", 1, "/briefings/2026-10-24") {
		t.Errorf("the title's margin does not show New to me as given, with its undo:\n%s", title.margin)
	}
	story := actionGroups.FindStringSubmatch(title.margin)
	if story == nil || story[1] != "Feedback on this Story" || !offers(story[2], "unwanted", "story", "story", "1") {
		t.Errorf("the title's margin does not offer Unwanted on the Story:\n%s", title.margin)
	}
	if actionGroups.MatchString(title.body) || strings.Contains(title.body, "✓, given") {
		t.Errorf("the title's Feedback is in the body column:\n%s", title.body)
	}
	if strings.Contains(page, `class="title-row"`) {
		t.Errorf("a Story's title is still in a title row:\n%s", page)
	}
}

// developmentActions matches the Feedback controls a statement offers on one
// Development it cites, labelled by that Development's Fact.
var developmentActions = regexp.MustCompile(`(?s)<div class="actions[^"]*" role="group" aria-label="Feedback on: ([^"]*)">(.*?)</div>`)

// twoDevelopments briefs one statement citing two Developments of one
// Story, each from a Fact of the speech.
func twoDevelopments(t *testing.T) http.Handler {
	t.Helper()
	store := openStore(t)
	twoFacts := strings.Replace(reading, `"facts": [`, `"facts": [{"statement": "Andy Burnham was in Motherwell.", "as_at": "2026-10-22",
             "quote": "here in Motherwell"}, `, 1)
	briefSpeech(t, store, twoFacts, `{"new_stories": [{"ref": "N1", "title": "Andy Burnham in Scotland"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}, {"fact": "F2", "developments_in": ["N1"]}]}`,
		`{"stories": [{"story": "S1", "statements": [
		  {"text": "The Prime Minister spoke in Motherwell.", "developments": ["D1", "D2"]}]}]}`)
	app, _ := newAppWithLog(t, saturday, store)
	return app
}

func TestAStatementOffersFeedbackOnEachDevelopmentItCitesInItsMarginLabelledByItsFact(t *testing.T) {
	app := twoDevelopments(t)

	res := get(t, app, "/")

	// The controls sit in the statement's margin, beside its citations, as
	// a block's do on a Story's page; folding the margin hides them with it.
	statement := blockOf(t, res.body, "The Prime Minister spoke in Motherwell.")
	if actionGroups.MatchString(statement.body) {
		t.Errorf("Development controls are in the body column:\n%s", statement.body)
	}
	groups := developmentActions.FindAllStringSubmatch(statement.margin, -1)
	if len(groups) != 2 {
		t.Fatalf("%d groups of Development controls in the statement's margin, want 2:\n%s", len(groups), statement.margin)
	}
	labels := map[string]bool{}
	for _, g := range groups {
		labels[g[1]] = true
		if !strings.Contains(g[2], g[1]) {
			t.Errorf("the controls do not show what they act on (%q):\n%s", g[1], g[2])
		}
		fact := regexp.MustCompile(`name="fact" value="(\d+)"`).FindStringSubmatch(g[2])
		if fact == nil {
			t.Fatalf("the group gives no Feedback on a Fact:\n%s", g[2])
		}
		for _, want := range [][]string{
			{"unwanted", "development", "story", "1", "fact", fact[1]},
			{"wrong", "fact", "fact", fact[1]},
			{"new to me", "development", "story", "1", "fact", fact[1]},
		} {
			if !offers(g[2], want[0], want[1], want[2:]...) {
				t.Errorf("the controls for %q do not offer %s on its %s:\n%s", g[1], want[0], want[1], g[2])
			}
		}
		if strings.Contains(g[2], "PM speech in Scotland") {
			t.Errorf("the controls repeat the Item's title:\n%s", g[2])
		}
	}
	if !labels["Andy Burnham was in Motherwell."] || !labels["Andy Burnham spoke in Motherwell."] {
		t.Errorf("the controls are not labelled by each Development's Fact: %v", labels)
	}
}

// The Sunday read is still a reading page: the controls of its title and
// its Assessments sit with their text, hidden at rest in a way that keeps
// them focusable (not display: none or visibility: hidden, which take them
// out of the tab order), so tabbing in reaches them and reveals them, as
// hovering does.
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

// actionGroups matches every group of Feedback controls that is shown only
// on hover or focus of what it acts on: the Story's, and each Development's.
var actionGroups = regexp.MustCompile(`(?s)<div class="actions[^"]*" role="group" aria-label="([^"]*)">(.*?)</div>`)

func TestGivenFeedbackOnTheBriefingShowsAtRestInTheMarginOutsideTheControlsShownOnHover(t *testing.T) {
	store := openStore(t)
	briefed(t, store)
	app, _ := newAppWithLog(t, saturday, store)
	send(t, app, "POST", today, url.Values{"kind": {"new to me"}, "level": {"story"}, "story": {"1"}})
	send(t, app, "POST", today, url.Values{"kind": {"new to me"}, "level": {"development"}, "story": {"1"}, "fact": {"1"}})

	res := get(t, app, "/")

	groups := map[string]string{}
	for _, g := range actionGroups.FindAllStringSubmatch(res.body, -1) {
		groups[g[1]] = g[2]
		if strings.Contains(g[2], "✓, given") {
			t.Errorf("given Feedback is among the controls shown only on hover (%s):\n%s", g[1], g[2])
		}
	}
	page := "/briefings/2026-10-24"
	if !givenWithUndo(res.body, "New to me", 1, page) || !givenWithUndo(res.body, "New to me", 2, page) {
		t.Errorf("the given Feedback is not shown with its undo:\n%s", res.body)
	}
	story, ok := groups["Feedback on this Story"]
	if !ok || !offers(story, "unwanted", "story", "story", "1") || offers(story, "new to me", "story", "story", "1") {
		t.Errorf("the Story's controls do not offer just what is not given:\n%s", res.body)
	}
	dev, ok := groups["Feedback on: Andy Burnham spoke in Motherwell."]
	if !ok || !offers(dev, "unwanted", "development", "story", "1", "fact", "1") || !offers(dev, "wrong", "fact", "fact", "1") ||
		offers(dev, "new to me", "development", "story", "1", "fact", "1") {
		t.Errorf("the Development's controls do not offer just what is not given:\n%s", res.body)
	}
	// The given mark sits in the statement's margin and says which
	// Development it is on.
	statement := blockOf(t, res.body, "The Prime Minister spoke in Motherwell.")
	if !regexp.MustCompile(`Andy Burnham spoke in Motherwell\.</span>\s*<span class="feedback given">New to me ✓, given`).MatchString(statement.margin) {
		t.Errorf("the given mark on the Development is not in the statement's margin, saying which it is on:\n%s", statement.margin)
	}
	if strings.Contains(statement.body, "✓, given") {
		t.Errorf("given Feedback is in the statement's body column:\n%s", statement.body)
	}
}
