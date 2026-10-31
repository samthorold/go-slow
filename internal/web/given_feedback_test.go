package web_test

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/sqlite"
)

// offers reports whether the page offers a fresh button giving kind at level
// on the target, its hidden fields as the form sends them (name, value...).
func offers(body, kind, level string, target ...string) bool {
	fields := `<input type="hidden" name="kind" value="` + kind + `"><input type="hidden" name="level" value="` + level + `">`
	for i := 0; i+1 < len(target); i += 2 {
		fields += `<input type="hidden" name="` + target[i] + `" value="` + target[i+1] + `">`
	}
	return strings.Contains(body, fields+"\n  <button>")
}

// givenWithUndo reports whether the page shows label as given, beside an
// undo of the piece id from the page's own undo route.
func givenWithUndo(body, label string, id int, page string) bool {
	return regexp.MustCompile(regexp.QuoteMeta(label+" ✓, given") + `\s*<form[^>]*hx-put="` +
		regexp.QuoteMeta(page+"/feedback/"+strconv.Itoa(id)+"/undone") + `"`).MatchString(body)
}

func TestFeedbackGivenInlineShowsAsGivenWhereItWasGivenUntilItIsUndone(t *testing.T) {
	for _, tc := range []struct {
		name string
		// page is where it is given, and the record the page is shown over.
		page   string
		record func(t *testing.T, store *sqlite.Store)
		now    time.Time
		label  string
		// target is the button's level and hidden fields; kind is new to me.
		level  string
		target []string
	}{
		{"story on the Briefing", "/briefings/2026-10-24", briefed, saturday, "New to me", "story", []string{"story", "1"}},
		{"story on its page", "/stories/2", connectedStories, saturday, "New to me", "story", []string{"story", "2"}},
		{"development on its Story's page", "/stories/2", connectedStories, saturday, "New to me", "development", []string{"story", "2", "fact", "2"}},
		{"connection on a Story's page", "/stories/2", connectedStories, saturday, "New to me", "connection", []string{"fact", "1", "to_fact", "2"}},
		{"the Sunday read", "/sunday-reads/2026-10-25", read, sunday, "New to me", "sunday read", []string{"read", "1"}},
		{"assessment on the Sunday read", "/sunday-reads/2026-10-25", read, sunday, "New to me", "assessment", []string{"assessment", "1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := openStore(t)
			tc.record(t, store)
			app, _ := newAppWithLog(t, tc.now, store)
			form := url.Values{"kind": {"new to me"}, "level": {tc.level}}
			for i := 0; i+1 < len(tc.target); i += 2 {
				form.Set(tc.target[i], tc.target[i+1])
			}
			if before := get(t, app, tc.page); !offers(before.body, "new to me", tc.level, tc.target...) {
				t.Fatalf("the page offers no %s on it:\n%s", tc.label, before.body)
			}

			if res := send(t, app, "POST", tc.page+"/feedback", form); res.status != 303 {
				t.Fatalf("status %d:\n%s", res.status, res.body)
			}

			given := get(t, app, tc.page)
			if offers(given.body, "new to me", tc.level, tc.target...) || !givenWithUndo(given.body, tc.label, 1, tc.page) {
				t.Fatalf("the page does not show it given, with its undo, in place of a fresh button:\n%s", given.body)
			}
			if res := send(t, app, "PUT", tc.page+"/feedback/1/undone", nil); res.status != 303 {
				t.Fatalf("undo: status %d:\n%s", res.status, res.body)
			}
			undone := get(t, app, tc.page)
			if !offers(undone.body, "new to me", tc.level, tc.target...) || givenWithUndo(undone.body, tc.label, 1, tc.page) {
				t.Errorf("undone, the page does not offer the fresh button again:\n%s", undone.body)
			}
		})
	}
}

func TestFeedbackOnOneStoryIsNotShownGivenOnAnother(t *testing.T) {
	store := openStore(t)
	connectedStories(t, store)
	app, _ := newAppWithLog(t, saturday, store)

	send(t, app, "POST", "/stories/2/feedback", url.Values{"kind": {"new to me"}, "level": {"story"}, "story": {"2"}})

	other := get(t, app, "/stories/1")
	if !offers(other.body, "new to me", "story", "story", "1") || strings.Contains(other.body, "✓, given") {
		t.Errorf("the other Story's page shows New to me given:\n%s", other.body)
	}
}

func TestAnUnwantedStoryShowsItsUnwantedAsGivenOnItsPageWithItsUndo(t *testing.T) {
	store := openStore(t)
	connectedStories(t, store)
	app, _ := newAppWithLog(t, saturday, store)

	send(t, app, "POST", "/stories/2/feedback", url.Values{"kind": {"unwanted"}, "level": {"story"}, "story": {"2"}})

	page := get(t, app, "/stories/2")
	if offers(page.body, "unwanted", "story", "story", "2") || !givenWithUndo(page.body, "Unwanted", 1, "/stories/2") {
		t.Errorf("the Story's page does not show its Unwanted as given, with its undo:\n%s", page.body)
	}
}

// briefed is the speech's Story, in Saturday's Briefing.
func briefed(t *testing.T, store *sqlite.Store) {
	t.Helper()
	speechStory(t, store)
	writeStoryBriefing(t, store, saturday, spoke)
}

// read is the speech's Story, with Sunday's read assessing it.
func read(t *testing.T, store *sqlite.Store) {
	t.Helper()
	speechStory(t, store)
	writeSundayRead(t, store, sunday, assessed)
}

func TestNewToMeOnADevelopmentOfTheBriefingShowsAsGivenThereWithItsUndo(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeStoryBriefing(t, store, saturday, spoke)
	app, _ := newAppWithLog(t, saturday, store)
	if before := get(t, app, "/"); !offers(before.body, "new to me", "development", "story", "1", "fact", "1") {
		t.Fatalf("the Briefing offers no New to me on the Development:\n%s", before.body)
	}

	send(t, app, "POST", today, url.Values{"kind": {"new to me"}, "level": {"development"}, "story": {"1"}, "fact": {"1"}})

	after := get(t, app, "/")
	if offers(after.body, "new to me", "development", "story", "1", "fact", "1") {
		t.Errorf("the Briefing still offers a fresh New to me on the Development:\n%s", after.body)
	}
	if !givenWithUndo(after.body, "New to me", 1, "/briefings/2026-10-24") {
		t.Errorf("the Briefing does not show the Development's New to me as given, with its undo:\n%s", after.body)
	}
	// The same kind on another target stays on offer.
	if !offers(after.body, "new to me", "story", "story", "1") {
		t.Errorf("New to me on the Story is no longer offered:\n%s", after.body)
	}
}
