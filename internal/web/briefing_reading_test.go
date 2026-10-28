package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// The Briefing is for reading: its statements lead, and how each was made
// (its citations) and the Feedback on what it cites recede. How that looks is
// judged in a browser; what the server is held to is the structure the
// stylesheet works on.

// detailsBlocks matches a disclosure, closed or open, with what it holds.
var detailsBlocks = regexp.MustCompile(`(?s)<details[^>]*>.*?</details>`)

func TestEachStatementOnTheBriefingHasItsCitationsCollapsedInADisclosure(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeBriefing(t, store, saturday, spoke)
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/")

	m := regexp.MustCompile(`(?s)<p hx-disable>The Prime Minister spoke in Motherwell\.</p>\s*(<details[^>]*>.*?</details>)`).FindStringSubmatch(res.body)
	if m == nil {
		t.Fatalf("the statement has no disclosure after it:\n%s", res.body)
	}
	if strings.Contains(m[1], "<details open") {
		t.Errorf("the citations are open at rest:\n%s", m[1])
	}
	for _, want := range []string{"<summary", "Andy Burnham spoke in Motherwell.", "PM speech in Scotland", `href="/stories/1#development-1"`} {
		if !strings.Contains(m[1], want) {
			t.Errorf("the disclosure lacks %q:\n%s", want, m[1])
		}
	}
	// At rest, the Item's title is nowhere outside a closed disclosure.
	if rest := detailsBlocks.ReplaceAllString(res.body, ""); strings.Contains(rest, "PM speech in Scotland") {
		t.Errorf("the Item's title shows at rest:\n%s", rest)
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

func TestAStatementOffersFeedbackOnEachDevelopmentItCitesLabelledByItsFact(t *testing.T) {
	app := twoDevelopments(t)

	res := get(t, app, "/")

	statement := strings.Index(res.body, "The Prime Minister spoke in Motherwell.</p>")
	if statement < 0 {
		t.Fatalf("no statement:\n%s", res.body)
	}
	block := detailsBlocks.ReplaceAllString(res.body[statement:strings.Index(res.body, "</section>")], "")
	groups := developmentActions.FindAllStringSubmatch(block, -1)
	if len(groups) != 2 {
		t.Fatalf("%d groups of Development controls beside the statement, want 2:\n%s", len(groups), block)
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

// The controls shown on hover are hidden at rest in a way that keeps them
// focusable (not display: none or visibility: hidden, which take them out of
// the tab order), so tabbing into a statement or a Story's heading reaches
// them and reveals them, as hovering does.
func TestTheStylesheetHidesTheBriefingsControlsAtRestAndRevealsThemOnHoverOrFocus(t *testing.T) {
	app := newApp(t, saturday)
	css := get(t, app, "/static/go-slow.css").body

	rest := regexp.MustCompile(`(?m)^\.briefed-story \.actions\s*\{([^}]*)\}`).FindStringSubmatch(css)
	if rest == nil {
		t.Fatalf("no rule hides the Briefing's controls at rest:\n%s", css)
	}
	if strings.Contains(rest[1], "display: none") || strings.Contains(rest[1], "visibility: hidden") {
		t.Errorf("the controls are hidden at rest so the keyboard cannot reach them:\n%s", rest[0])
	}
	for _, block := range []string{".statement", ".story-head"} {
		for _, state := range []string{":hover", ":focus-within"} {
			if sel := ".briefed-story " + block + state + " .actions"; !strings.Contains(css, sel) {
				t.Errorf("no rule reveals the controls on %s", sel)
			}
		}
	}
}

// actionGroups matches every group of Feedback controls that is shown only
// on hover or focus of what it acts on: the Story's, and each Development's.
var actionGroups = regexp.MustCompile(`(?s)<div class="actions[^"]*" role="group" aria-label="([^"]*)">(.*?)</div>`)

func TestGivenFeedbackOnTheBriefingShowsAtRestOutsideTheControlsShownOnHover(t *testing.T) {
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
	// The given mark says which Development it is on.
	if !regexp.MustCompile(`Andy Burnham spoke in Motherwell\.</span>\s*<span class="feedback given">New to me ✓, given`).MatchString(res.body) {
		t.Errorf("the given mark on the Development does not say which it is on:\n%s", res.body)
	}
}
