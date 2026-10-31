package web_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

// poll is the element that polls the day's Briefing status, if there is one.
var poll = regexp.MustCompile(`<[^>]*hx-get="/briefings/2026-10-24/status"[^>]*>`)

func TestWhileTheBriefingIsBeingWrittenTheDaysPagePollsUntilItIsWritten(t *testing.T) {
	store := openStore(t)
	app, _ := newAppWithLog(t, saturday, store) // 07:30, past the set time

	page := get(t, app, "/")
	if !strings.Contains(page.body, "being written") {
		t.Errorf("the page does not say the Briefing is being written:\n%s", page.body)
	}
	el := poll.FindString(page.body)
	for _, want := range []string{`hx-trigger="every `, `hx-target="this"`, `hx-swap="outerHTML"`} {
		if !strings.Contains(el, want) {
			t.Errorf("the page's poll %q lacks %s", el, want)
		}
	}

	waiting := get(t, app, "/briefings/2026-10-24/status")
	if waiting.status != http.StatusOK || strings.Contains(waiting.body, "<!doctype html>") {
		t.Fatalf("status %d, or not a fragment:\n%s", waiting.status, waiting.body)
	}
	if !strings.Contains(poll.FindString(waiting.body), "hx-trigger") {
		t.Errorf("the fragment does not poll again while the Briefing is being written:\n%s", waiting.body)
	}

	tell(t, store, saturday, teller{})
	written := get(t, app, "/briefings/2026-10-24/status")
	if strings.Contains(written.body, "hx-trigger") {
		t.Errorf("the fragment polls on once the Briefing is written:\n%s", written.body)
	}
	if !strings.Contains(written.body, `href="/briefings/2026-10-24"`) {
		t.Errorf("the fragment does not link to the written Briefing:\n%s", written.body)
	}
}

func TestOnlyTodaysBriefingPastTheSetTimeIsPolledFor(t *testing.T) {
	before := get(t, newApp(t, time.Date(2026, time.October, 24, 6, 59, 0, 0, time.UTC)), "/")
	nextDay := get(t, newApp(t, saturday.AddDate(0, 0, 1)), "/briefings/2026-10-24")

	for name, res := range map[string]response{"before the set time": before, "a past day": nextDay} {
		if poll.MatchString(res.body) || strings.Contains(res.body, "being written") {
			t.Errorf("%s: polled for:\n%s", name, res.body)
		}
	}
}
