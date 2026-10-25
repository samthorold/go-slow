package web_test

import (
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// placeWaiting places the Facts awaiting placing with output.
func placeWaiting(t *testing.T, store *sqlite.Store, output string) {
	t.Helper()
	ed := editor.New(editor.Deps{Store: store, Placing: fakeModel{output}, Log: slog.New(slog.DiscardHandler)})
	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
}

func TestWhatPlacingWroteIsInertOnTheStoryPages(t *testing.T) {
	store := openStore(t)
	readSpeech(t, store, reading)
	placeWaiting(t, store, `{"new_stories": [{"ref": "N1", "title": "<b hx-get=\"/evil\">Scotland</b>"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}]}`)
	app, _ := newAppWithLog(t, saturday, store)

	for _, path := range []string{"/stories", "/stories/1"} {
		res := get(t, app, path)
		if res.status != 200 || strings.Contains(res.body, "<b hx-get") {
			t.Errorf("%s: status %d, or the Story's title was not escaped:\n%s", path, res.status, res.body)
		}
		if !regexp.MustCompile(`hx-disable>&lt;b hx-get`).MatchString(res.body) {
			t.Errorf("%s: the Story's title is not inside hx-disable:\n%s", path, res.body)
		}
	}
}

func TestAStoryNotInTheRecordIsNotFound(t *testing.T) {
	app := newApp(t, saturday)

	for _, path := range []string{"/stories/7", "/stories/seven"} {
		if res := get(t, app, path); res.status != 404 {
			t.Errorf("%s: status = %d, want 404", path, res.status)
		}
	}
}

func TestAStorysPageShowsItsTimelineEachDevelopmentTracingToItsFactAndItem(t *testing.T) {
	store := openStore(t)
	readSpeech(t, store, reading)
	placeWaiting(t, store, `{"new_stories": [{"ref": "N1", "title": "Andy Burnham in Scotland"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}]}`)
	// An earlier Item, read and placed after the speech, whose Fact holds as
	// at an earlier date: the Timeline is in the order of the Facts' dates.
	earlier := item("offer", "Offer to Holyrood", saturday.Add(-72*time.Hour))
	seed(t, store, saturday.AddDate(0, 0, -7), earlier)
	if err := store.SaveReading(t.Context(), editor.Reading{
		SourceID: earlier.SourceID, EntryID: earlier.EntryID, Model: "fake",
		Entities: []editor.Entity{{Name: "Andy Burnham", Kind: "person"}},
		Facts: []editor.Fact{{Statement: "Andy Burnham made an offer to Holyrood.",
			AsAt: time.Date(2026, time.October, 21, 0, 0, 0, 0, time.UTC), Quote: "offer", Entities: []string{"Andy Burnham"}}},
	}, saturday); err != nil {
		t.Fatal(err)
	}
	placeWaiting(t, store, `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": ["S1"]}]}`)
	app, _ := newAppWithLog(t, saturday, store)

	list := get(t, app, "/stories")
	if list.status != 200 || !strings.Contains(list.body, "<!doctype html>") {
		t.Fatalf("Stories page: status %d, not a full page:\n%s", list.status, list.body)
	}
	m := regexp.MustCompile(`<a href="(/stories/\d+)"[^>]*>(?:<[^>]*>)*Andy Burnham in Scotland`).FindStringSubmatch(list.body)
	if m == nil {
		t.Fatalf("the Stories page does not link the Story to its page:\n%s", list.body)
	}
	res := get(t, app, m[1])

	if res.status != 200 || !strings.Contains(res.body, "<!doctype html>") {
		t.Fatalf("Story page: status %d, not a full page:\n%s", res.status, res.body)
	}
	offer := strings.Index(res.body, "Andy Burnham made an offer to Holyrood.")
	spoke := strings.Index(res.body, "Andy Burnham spoke in Motherwell.")
	if offer < 0 || spoke < 0 || offer > spoke {
		t.Errorf("the Timeline does not show both Developments in order:\n%s", res.body)
	}
	// Each Development traces to its Item's page.
	for _, want := range []string{"21 October 2026", "22 October 2026", "Offer to Holyrood", "PM speech in Scotland",
		`<a href="/items/number-10/offer">`} {
		if !strings.Contains(res.body, want) {
			t.Errorf("Story page lacks %q:\n%s", want, res.body)
		}
	}
}
