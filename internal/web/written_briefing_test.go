package web_test

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestABriefingThatFailedItsChecksIsNotShownAndTheFailureIs(t *testing.T) {
	store := openStore(t)
	readRelease(t, store, releaseReading)
	tell(t, store, saturday, fakeModel{`{"statements": [{"spans": [], "text": "Scotland is the true north."}]}`})
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/")

	// The failure quotes the statement; nothing is told.
	if res.status != 200 || strings.Contains(res.body, `class="blocks"`) {
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

func TestABriefingWithNothingReadSinceTheLastSaysSo(t *testing.T) {
	store := openStore(t)
	tell(t, store, saturday, fakeModel{`{}`})
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/")

	if !strings.Contains(res.body, "Nothing was read since the last Briefing.") || strings.Contains(res.body, "Missed") {
		t.Errorf("the empty Briefing does not say so:\n%s", res.body)
	}
}

func TestABriefingThatJudgedNothingWorthTellingSaysSo(t *testing.T) {
	store := openStore(t)
	readRelease(t, store, releaseReading)
	tell(t, store, saturday, fakeModel{`{"statements": []}`})
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/")

	if !strings.Contains(res.body, "Nothing read since the last Briefing was worth telling.") {
		t.Errorf("the Briefing does not say it told nothing:\n%s", res.body)
	}
}

func TestEachStatementOfTheBriefingHasTheSpansItCitesInItsMarginEachLinkingItsPage(t *testing.T) {
	store := openStore(t)
	readRelease(t, store, releaseReading)
	tell(t, store, saturday, teller{text: "Andy Burnham called it <b>fantastic</b> news for Derby.", tell: "This is fantastic news"})
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, "/").body

	for _, want := range []string{
		`<div class="margined" data-margin="open">`,
		`<p hx-disable>Andy Burnham called it &lt;b&gt;fantastic&lt;/b&gt; news for Derby.</p>`,
		`<span hx-disable>“This is fantastic news for Derby.</span>`,
		`spoken by <span hx-disable>Andy Burnham</span>`,
		`<cite hx-disable>GBR headquarters in Derby</cite>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the Briefing lacks %q:\n%s", want, page)
		}
	}
	m := regexp.MustCompile(`<a href="(/items/` + number10.ID + `/release/spans/\d+-\d+)">`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("the cited Span does not link its page:\n%s", page)
	}
	if res := get(t, app, m[1]); res.status != 200 || !strings.Contains(res.body, "fantastic news") {
		t.Errorf("%s: status %d:\n%s", m[1], res.status, res.body)
	}
}

func TestPastBriefingsHaveTheirOwnPagesByDateAndMissedDaysAreFlagged(t *testing.T) {
	store := openStore(t)
	thursday := saturday.AddDate(0, 0, -2)
	// Thursday's had nothing read; Friday's was never written; Saturday's
	// tells the release.
	tell(t, store, thursday, teller{})
	readRelease(t, store, releaseReading)
	tell(t, store, saturday, teller{text: "The Mayor called it fantastic news.", tell: "This is fantastic news"})
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
		!strings.Contains(past.body, "Nothing was read since the last Briefing.") {
		t.Errorf("Thursday's page: status %d:\n%s", past.status, past.body)
	}
	if sat := get(t, app, "/briefings/2026-10-24"); !strings.Contains(sat.body, "The Mayor called it fantastic news.") {
		t.Errorf("Saturday's page:\n%s", sat.body)
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
	readRelease(t, store, releaseReading)
	tell(t, store, saturday, teller{text: `<b hx-get="/evil">Motherwell</b>`, tell: "This is fantastic news"})
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/")

	if strings.Contains(res.body, "<b hx-get") || !strings.Contains(res.body, `<p hx-disable>&lt;b hx-get`) {
		t.Errorf("the statement is not escaped inside hx-disable:\n%s", res.body)
	}
}
