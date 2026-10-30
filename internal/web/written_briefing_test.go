package web_test

import (
	"html"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/editor/frozentest"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// writeBriefing writes the Briefing at at, with output as the model's.
func writeBriefing(t *testing.T, store *sqlite.Store, at time.Time, output string) {
	t.Helper()
	ed := editor.New(editor.Deps{Store: store, Briefings: store, BriefingModel: fakeModel{output}, Log: slog.New(slog.DiscardHandler)})
	if err := ed.Brief(t.Context(), at); err != nil {
		t.Fatal(err)
	}
}

// speechStory reads the speech and places its Fact in a new Story, at
// placedAt (Saturday if not given).
func speechStory(t *testing.T, store *sqlite.Store, placedAt ...time.Time) {
	t.Helper()
	readSpeech(t, store, reading)
	at := saturday
	if len(placedAt) > 0 {
		at = placedAt[0]
	}
	if err := frozentest.PlaceWaiting(t.Context(), frozentest.Deps{Store: store, Log: slog.New(slog.DiscardHandler), Model: fakeModel{
		`{"new_stories": [{"ref": "N1", "title": "Andy Burnham in Scotland"}], "facts": [{"fact": "F1", "developments_in": ["N1"]}]}`}}, at); err != nil {
		t.Fatal(err)
	}
}

const spoke = `{"stories": [{"story": "S1", "statements": [
	{"text": "The Prime Minister spoke in Motherwell.", "developments": ["D1"]}]}]}`

func TestABriefingThatFailedItsChecksIsNotShownAndTheFailureIs(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeBriefing(t, store, saturday, `{"stories": [{"story": "S1", "statements": [
		{"text": "Scotland is the true north.", "developments": []}]}]}`)
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/")

	// The failure quotes the statement; no Story is told.
	if res.status != 200 || strings.Contains(res.body, "<section") {
		t.Fatalf("status %d, or the rejected Briefing is shown:\n%s", res.status, res.body)
	}
	for _, want := range []string{"Writing the Briefing failed", "untraced statement"} {
		if !strings.Contains(res.body, want) {
			t.Errorf("the failure is not shown (%q):\n%s", want, res.body)
		}
	}
}

func TestADayWithNoBriefingByTheSetTimeIsFlagged(t *testing.T) {
	// The app's Briefing is due at 07:00.
	before := get(t, newApp(t, time.Date(2026, time.October, 24, 6, 59, 0, 0, time.UTC)), "/")
	after := get(t, newApp(t, time.Date(2026, time.October, 24, 7, 0, 0, 0, time.UTC)), "/")

	if strings.Contains(before.body, "Missed") {
		t.Errorf("flagged before the set time:\n%s", before.body)
	}
	if !strings.Contains(after.body, "Missed: no Briefing was written by 07:00") {
		t.Errorf("not flagged at the set time:\n%s", after.body)
	}
}

func TestABriefingWithNothingToTellSaysSo(t *testing.T) {
	store := openStore(t)
	writeBriefing(t, store, saturday, `{}`)
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/")

	if !strings.Contains(res.body, "Nothing moved on") || strings.Contains(res.body, "Missed") {
		t.Errorf("the empty Briefing does not say so:\n%s", res.body)
	}
}

func TestPastBriefingsHaveTheirOwnPagesByDateAndMissedDaysAreFlagged(t *testing.T) {
	store := openStore(t)
	thursday := saturday.AddDate(0, 0, -2)
	speechStory(t, store, thursday)
	writeBriefing(t, store, thursday, spoke)
	// Friday's was never written; Saturday's says nothing moved on.
	writeBriefing(t, store, saturday, `{}`)
	app, _ := newAppWithLog(t, saturday, store)

	list := get(t, app, "/briefings")

	if list.status != 200 || !strings.Contains(list.body, "<!doctype html>") {
		t.Fatalf("status %d, not a full page:\n%s", list.status, list.body)
	}
	sat := strings.Index(list.body, `href="/briefings/2026-10-24"`)
	fri := strings.Index(list.body, "Friday 23 October 2026")
	thu := strings.Index(list.body, `href="/briefings/2026-10-22"`)
	if sat < 0 || fri < 0 || thu < 0 || sat > fri || fri > thu {
		t.Fatalf("the list does not show Saturday, Friday and Thursday, newest first:\n%s", list.body)
	}
	if strings.Contains(list.body, `href="/briefings/2026-10-23"`) || !strings.Contains(list.body[fri:thu], "Missed") {
		t.Errorf("Friday is not flagged as missed:\n%s", list.body)
	}

	past := get(t, app, "/briefings/2026-10-22")
	if past.status != 200 || !strings.Contains(past.body, "Thursday 22 October 2026") ||
		!strings.Contains(past.body, "The Prime Minister spoke in Motherwell.") {
		t.Errorf("Thursday's page: status %d:\n%s", past.status, past.body)
	}
	if missed := get(t, app, "/briefings/2026-10-23"); missed.status != 200 || !strings.Contains(missed.body, "Missed") {
		t.Errorf("Friday's page: status %d, not flagged:\n%s", missed.status, missed.body)
	}
	for _, path := range []string{"/briefings/2026-10-25", "/briefings/yesterday"} {
		if res := get(t, app, path); res.status != 404 {
			t.Errorf("%s: status %d, want 404", path, res.status)
		}
	}
}

func TestWhatTheBriefingSaysIsInert(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeBriefing(t, store, saturday, `{"stories": [{"story": "S1", "statements": [
		{"text": "<b hx-get=\"/evil\">Motherwell</b>", "developments": ["D1"]}]}]}`)
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/")

	if strings.Contains(res.body, "<b hx-get") || !strings.Contains(res.body, `<p hx-disable>&lt;b hx-get`) {
		t.Errorf("the statement is not escaped inside hx-disable:\n%s", res.body)
	}
}

func TestTodaysBriefingShowsEachStatementTracingToItsDevelopmentFactAndItem(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeBriefing(t, store, saturday, spoke)
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/")

	if res.status != 200 || strings.Contains(res.body, "There is no Briefing yet") {
		t.Fatalf("status %d, or no Briefing shown:\n%s", res.status, res.body)
	}
	for _, want := range []string{"Andy Burnham in Scotland", "The Prime Minister spoke in Motherwell.", "Andy Burnham spoke in Motherwell."} {
		if !strings.Contains(res.body, want) {
			t.Errorf("Briefing lacks %q:\n%s", want, res.body)
		}
	}
	// The statement links to its Development on the Story's Timeline, to
	// and to its Item, cited at the Fact on the Item's page; each link
	// resolves.
	for _, pattern := range []string{`href="(/stories/1#development-\d+)"`, `href="(/items/[^"#]+#fact-\d+)"`} {
		m := regexp.MustCompile(pattern).FindStringSubmatch(res.body)
		if m == nil {
			t.Errorf("the statement has no link matching %s:\n%s", pattern, res.body)
			continue
		}
		path, anchor, _ := strings.Cut(html.UnescapeString(m[1]), "#")
		page := get(t, app, path)
		if page.status != 200 || (anchor != "" && !strings.Contains(page.body, `id="`+anchor+`"`)) {
			t.Errorf("%s: status %d, or no element %q:\n%s", m[1], page.status, anchor, page.body)
		}
	}
}
