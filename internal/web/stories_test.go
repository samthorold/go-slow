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
	ed := editor.New(editor.Deps{Store: store, PlacingModel: fakeModel{output}, Log: slog.New(slog.DiscardHandler)})
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

// connectWaiting connects the Facts awaiting connecting, an Item at a time,
// with outputs in turn.
func connectWaiting(t *testing.T, store *sqlite.Store, outputs ...string) {
	t.Helper()
	for _, out := range outputs {
		ed := editor.New(editor.Deps{Store: store, ConnectingModel: fakeModel{out}, Log: slog.New(slog.DiscardHandler)})
		waiting, err := store.FactsToConnect(t.Context())
		if err != nil || len(waiting) == 0 {
			t.Fatalf("nothing awaiting connecting: %v", err)
		}
		// Only the first Item awaiting connecting is answered by out.
		if err := ed.ConnectWaiting(t.Context(), saturday); err != nil {
			t.Fatal(err)
		}
		if after, _ := store.FactsToConnect(t.Context()); len(after) != len(waiting)-1 {
			t.Fatalf("connecting %s with %s failed its checks", waiting[0].Item.Title, out)
		}
	}
}

func TestAStorysPageShowsTheStoriesConnectedToItAndWhy(t *testing.T) {
	store := openStore(t)
	// An offer to Holyrood starts one Story; the speech, a later Item, another.
	offer := item("offer", "Offer to Holyrood", saturday.Add(-72*time.Hour))
	seed(t, store, saturday.AddDate(0, 0, -7), offer)
	if err := store.SaveReading(t.Context(), editor.Reading{
		SourceID: offer.SourceID, EntryID: offer.EntryID, Model: "fake",
		Entities: []editor.Entity{{Name: "Andy Burnham", Kind: "person"}},
		Facts: []editor.Fact{{Statement: "Andy Burnham made an offer to Holyrood.",
			AsAt: time.Date(2026, time.October, 21, 0, 0, 0, 0, time.UTC), Quote: "offer", Entities: []string{"Andy Burnham"}}},
	}, saturday); err != nil {
		t.Fatal(err)
	}
	placeWaiting(t, store, `{"new_stories": [{"ref": "N1", "title": "Scottish devolution"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}]}`)
	// Connecting gives Andy Burnham his role in the offer.
	connectWaiting(t, store, `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}]}], "causes": []}`)
	readSpeech(t, store, reading)
	placeWaiting(t, store, `{"new_stories": [{"ref": "N1", "title": "<b hx-get=\"/evil\">Burnham in Motherwell</b>"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}]}`)
	// And in the speech, which the offer caused.
	connectWaiting(t, store, `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}, {"entity": "Motherwell", "role": "place"}]}],
		  "causes": [{"cause": "C1", "effect": "F1"}]}`)
	app, _ := newAppWithLog(t, saturday, store)

	devolution := get(t, app, "/stories/1")
	at := strings.Index(devolution.body, `id="connected"`)
	if devolution.status != 200 || at < 0 {
		t.Fatalf("status %d, or no connected Stories:\n%s", devolution.status, devolution.body)
	}
	section := devolution.body[at:]
	for _, want := range []string{`<a href="/stories/2">`, `hx-disable>&lt;b hx-get`, "Andy Burnham", "actor",
		"Andy Burnham made an offer to Holyrood.", "Andy Burnham spoke in Motherwell.", `<a href="/items/number-10/offer">`} {
		if !strings.Contains(section, want) {
			t.Errorf("the connected Stories lack %q:\n%s", want, section)
		}
	}
	speech := get(t, app, "/stories/2")
	if !strings.Contains(speech.body, `<a href="/stories/1">`) || !strings.Contains(speech.body, "Scottish devolution") {
		t.Errorf("the speech's Story does not show the Story connected to it:\n%s", speech.body)
	}
	// The cause traces to the Items on each side.
	at = strings.Index(speech.body, `id="causes"`)
	if at < 0 {
		t.Fatalf("the speech's Story shows no causes:\n%s", speech.body)
	}
	causes := speech.body[at:]
	offerAt, spokeAt := strings.Index(causes, "Andy Burnham made an offer to Holyrood."), strings.Index(causes, "Andy Burnham spoke in Motherwell.")
	if offerAt < 0 || spokeAt < offerAt || !strings.Contains(causes, `<a href="/items/number-10/offer">`) ||
		!strings.Contains(causes, "PM speech in Scotland") {
		t.Errorf("the speech's Story does not show the offer as its cause, tracing to both Items:\n%s", causes)
	}
}
