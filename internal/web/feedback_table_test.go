package web_test

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// feedbackRows is each row of the Feedback page's table, in order.
func feedbackRows(t *testing.T, body string) []string {
	t.Helper()
	table := regexp.MustCompile(`(?s)<table class="feedback-table">(.*?)</table>`).FindStringSubmatch(body)
	if table == nil {
		t.Fatalf("the Feedback page has no table:\n%s", body)
	}
	var rows []string
	for _, m := range regexp.MustCompile(`(?s)<tr\b[^>]*>.*?</tr>`).FindAllString(table[1], -1) {
		if !strings.Contains(m, "<th") {
			rows = append(rows, m)
		}
	}
	return rows
}

func TestFeedbackIsATableNewestFirstWithItsColumns(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	app, _ := newAppWithLog(t, saturday, store)
	give(t, app,
		url.Values{"kind": {"wrong"}, "level": {"fact"}, "fact": {"1"}},
		url.Values{"kind": {"new to me"}, "level": {"story"}, "story": {"1"}},
	)

	body := get(t, app, "/feedback").body

	head := regexp.MustCompile(`(?s)<thead>(.*?)</thead>`).FindStringSubmatch(body)
	if head == nil {
		t.Fatalf("the table has no head:\n%s", body)
	}
	cols := regexp.MustCompile(`<th[^>]*>([^<]*)</th>`).FindAllStringSubmatch(head[1], -1)
	var got []string
	for _, c := range cols {
		got = append(got, c[1])
	}
	want := []string{"When", "Kind", "About", "Depth", "How it was made", ""}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("columns %q, want %q", got, want)
	}
	rows := feedbackRows(t, body)
	if len(rows) != 2 || !strings.Contains(rows[0], "new to me") || !strings.Contains(rows[1], "wrong") {
		t.Errorf("want a row per piece, newest first:\n%s", strings.Join(rows, "\n"))
	}
}

// kindLabel is the label a row's Kind cell holds for kind.
func kindLabel(kind string) string {
	return `<span class="kind kind-` + strings.ReplaceAll(kind, " ", "-") + `">` + kind + `</span>`
}

// Each kind is a short label in its own hue, the table's one hued cell, so
// the page scans by kind; the hues are their own family, apart from the
// status hues (verdicts) and the Entity's teal.
func TestEachKindIsALabelInItsOwnHue(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	app, _ := newAppWithLog(t, saturday, store)
	give(t, app,
		url.Values{"kind": {"wrong"}, "level": {"fact"}, "fact": {"1"}},
		url.Values{"kind": {"unwanted"}, "level": {"story"}, "story": {"1"}},
		url.Values{"kind": {"new to me"}, "level": {"story"}, "story": {"1"}},
		url.Values{"kind": {"missed"}, "level": {"story"}, "title": {"Holyrood"}},
	)

	rows := feedbackRows(t, get(t, app, "/feedback").body)

	kinds := []string{"missed", "new to me", "unwanted", "wrong"}
	if len(rows) != len(kinds) {
		t.Fatalf("%d rows, want %d", len(rows), len(kinds))
	}
	for i, kind := range kinds {
		if !strings.Contains(rows[i], kindLabel(kind)) {
			t.Errorf("row %d has no label for %q:\n%s", i, kind, rows[i])
		}
		if strings.Contains(rows[i], "status") {
			t.Errorf("row %d takes a status hue:\n%s", i, rows[i])
		}
	}
	css := get(t, app, "/static/go-slow.css").body
	vars := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^\s*(--[a-z-]+):\s*(#[0-9a-f]{6});`).FindAllStringSubmatch(css, -1) {
		vars[m[1]] = m[2]
	}
	taken := map[string]bool{}
	for _, v := range []string{"--supported", "--refuted", "--undecided", "--due", "--entity", "--link", "--text", "--muted"} {
		taken[vars[v]] = true
	}
	for _, kind := range kinds {
		class := "kind-" + strings.ReplaceAll(kind, " ", "-")
		rule := regexp.MustCompile(`(?m)^\.` + class + `\s*\{[^}]*var\((--[a-z-]+)\)`).FindStringSubmatch(css)
		if rule == nil {
			t.Errorf("no rule gives .%s a hue", class)
			continue
		}
		hue := vars[rule[1]]
		if hue == "" || taken[hue] {
			t.Errorf(".%s's hue %s (%q) is missing or another meaning's", class, rule[1], hue)
		}
		taken[hue] = true
	}
}

// How it was made is a machine label per call behind what the piece is
// about, worded by the act, or "you made it".
func TestHowItWasMadeIsAMachineLabelPerCallOrYouMadeIt(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	app, _ := newAppWithLog(t, saturday, store)
	give(t, app,
		url.Values{"kind": {"wrong"}, "level": {"fact"}, "fact": {"1"}},
		url.Values{"kind": {"new to me"}, "level": {"story"}, "story": {"1"}},
		url.Values{"kind": {"missed"}, "level": {"story"}, "title": {"Holyrood"}},
	)

	rows := feedbackRows(t, get(t, app, "/feedback").body)

	made := regexp.MustCompile(`(?s)<td class="made">(.*?)</td>`)
	for i, want := range []string{
		`^<span class="machine">you made it</span>$`,
		`^<a class="machine" href="/calls/(\d+)">placed · call (\d+)</a>$`,
		`^<a class="machine" href="/calls/(\d+)">read · call (\d+)</a>$`,
	} {
		cell := made.FindStringSubmatch(rows[i])
		if cell == nil || !regexp.MustCompile(want).MatchString(strings.TrimSpace(cell[1])) {
			t.Errorf("row %d's How it was made does not match %s:\n%s", i, want, rows[i])
		}
	}
}

// Undo sits in the row's actions, revealed while the pointer or keyboard
// focus is in the row; an undone row stays, struck through and quiet.
func TestUndoIsInTheRowsActionsAndAnUndoneRowIsStruckThrough(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	app, _ := newAppWithLog(t, saturday, store)
	give(t, app,
		url.Values{"kind": {"wrong"}, "level": {"fact"}, "fact": {"1"}},
		url.Values{"kind": {"new to me"}, "level": {"story"}, "story": {"1"}},
	)

	standing := feedbackRows(t, get(t, app, "/feedback").body)[1]
	actions := regexp.MustCompile(`(?s)<div class="actions[^"]*"[^>]*>(.*?)</div>`).FindStringSubmatch(standing)
	if actions == nil || undoForm(actions[1], 1) != "/feedback/1/undone" {
		t.Fatalf("the row's actions hold no Undo:\n%s", standing)
	}
	if res := send(t, app, "PUT", "/feedback/1/undone", nil); res.status != 303 || res.header.Get("Location") != "/feedback" {
		t.Fatalf("undo: status %d, Location %q", res.status, res.header.Get("Location"))
	}

	rows := feedbackRows(t, get(t, app, "/feedback").body)
	if len(rows) != 2 || !strings.HasPrefix(rows[1], `<tr class="undone">`) || !strings.Contains(rows[1], "Undone Sat 24 Oct 2026") {
		t.Errorf("the undone piece's row is gone, or not marked undone:\n%s", strings.Join(rows, "\n"))
	}
	if strings.HasPrefix(rows[0], `<tr class="undone">`) {
		t.Errorf("the standing piece's row is marked undone:\n%s", rows[0])
	}
	css := get(t, app, "/static/go-slow.css").body
	for name, rule := range map[string]*regexp.Regexp{
		"undone struck through":           regexp.MustCompile(`(?m)^\.feedback-table \.undone[^{]*\{[^}]*line-through[^}]*var\(--muted\)|(?m)^\.feedback-table \.undone[^{]*\{[^}]*var\(--muted\)[^}]*line-through`),
		"actions hidden in the tab order": regexp.MustCompile(`(?m)^\.feedback-table \.actions,?[^{]*\{[^}]*clip-path:`),
		"actions shown on hover":          regexp.MustCompile(`\.feedback-table tr:hover \.actions`),
		"actions shown on focus":          regexp.MustCompile(`\.feedback-table tr:focus-within \.actions`),
	} {
		if !rule.MatchString(css) {
			t.Errorf("the stylesheet has no rule for %s", name)
		}
	}
}

// About is what the piece is about, linked to its page, after its level as
// a quiet prefix.
func TestAboutLinksToWhatThePieceIsAboutAfterItsLevel(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	app, _ := newAppWithLog(t, saturday, store)
	give(t, app,
		url.Values{"kind": {"wrong"}, "level": {"fact"}, "fact": {"1"}},
		url.Values{"kind": {"new to me"}, "level": {"development"}, "story": {"1"}, "fact": {"1"}},
		url.Values{"kind": {"unwanted"}, "level": {"story"}, "story": {"1"}},
	)

	rows := feedbackRows(t, get(t, app, "/feedback").body)

	for i, want := range []struct{ level, href, about string }{
		{"Story", `/stories/1`, "Andy Burnham in Scotland"},
		{"Development", `/stories/1#development-1`, "Andy Burnham spoke in Motherwell."},
		{"Fact", `/items/[^"#]+#fact-1`, "Andy Burnham spoke in Motherwell."},
	} {
		about := regexp.MustCompile(`(?s)<td class="subject">(.*?)</td>`).FindStringSubmatch(rows[i])
		if about == nil {
			t.Errorf("row %d has no About cell:\n%s", i, rows[i])
			continue
		}
		prefix := regexp.MustCompile(`^<span class="quiet">` + want.level + `</span> <a href="` + want.href + `">`)
		if !prefix.MatchString(about[1]) || !strings.Contains(about[1], want.about) {
			t.Errorf("row %d's About is not %s %q linked to %s:\n%s", i, want.level, want.about, want.href, about[1])
		}
	}
}
