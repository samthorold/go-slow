package web_test

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

const today = "/briefings/2026-10-24/feedback"

func TestWrongOnAFactFromTheBriefingWithdrawsItAndTheBriefingReRendersWithoutIt(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeBriefing(t, store, saturday, spoke)
	app, _ := newAppWithLog(t, saturday, store)

	before := get(t, app, "/")
	if !strings.Contains(before.body, `hx-post="`+today+`"`) || !strings.Contains(before.body, `hx-confirm=`) {
		t.Fatalf("the Briefing offers no Feedback on its Facts, or wrong asks no confirmation:\n%s", before.body)
	}

	res := send(t, app, "POST", today, url.Values{"kind": {"wrong"}, "level": {"fact"}, "fact": {"1"}})

	if res.status != 303 || res.header.Get("Location") != "/" {
		t.Fatalf("status %d, Location %q, want 303 to /:\n%s", res.status, res.header.Get("Location"), res.body)
	}
	after := get(t, app, "/")
	if strings.Contains(after.body, "The Prime Minister spoke in Motherwell.") {
		t.Errorf("the statement resting on the withdrawn Fact is still shown:\n%s", after.body)
	}
	story := get(t, app, "/stories/1")
	if !strings.Contains(story.body, "Withdrawn") {
		t.Errorf("the Timeline does not mark the withdrawn Development:\n%s", story.body)
	}
}

func TestUnwantedTakesAStoryOrADevelopmentOutOfTheBriefingAndMarksItInTheRecord(t *testing.T) {
	for _, tc := range []struct {
		name string
		form url.Values
		mark string
	}{
		{"story", url.Values{"kind": {"unwanted"}, "level": {"story"}, "story": {"1"}}, "Unwanted in the Briefing"},
		{"development", url.Values{"kind": {"unwanted"}, "level": {"development"}, "story": {"1"}, "fact": {"1"}}, "Unwanted in the Briefing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := openStore(t)
			speechStory(t, store)
			writeBriefing(t, store, saturday, spoke)
			app, _ := newAppWithLog(t, saturday, store)

			res := send(t, app, "POST", today, tc.form)

			if res.status != 303 {
				t.Fatalf("status %d:\n%s", res.status, res.body)
			}
			after := get(t, app, "/")
			if strings.Contains(after.body, "The Prime Minister spoke in Motherwell.") {
				t.Errorf("the unwanted %s is still in the Briefing:\n%s", tc.name, after.body)
			}
			if story := get(t, app, "/stories/1"); !strings.Contains(story.body, tc.mark) {
				t.Errorf("the Record does not mark the unwanted %s:\n%s", tc.name, story.body)
			}
		})
	}
}

func TestMissedOnAStoryFromTheBriefingStartsItGatheringTheFactsChosen(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeBriefing(t, store, saturday, spoke)
	app, _ := newAppWithLog(t, saturday, store)
	page := get(t, app, "/")
	if !strings.Contains(page.body, `name="title"`) || !strings.Contains(page.body, `name="facts" value="1"`) {
		t.Fatalf("the Briefing has no form for a missed Story offering its Facts:\n%s", page.body)
	}

	res := send(t, app, "POST", today, url.Values{"kind": {"missed"}, "level": {"story"},
		"title": {"Scottish politics"}, "facts": {"1"}})

	if res.status != 303 {
		t.Fatalf("status %d:\n%s", res.status, res.body)
	}
	story := get(t, app, "/stories/2")
	for _, want := range []string{"Scottish politics", "You started this Story", "Andy Burnham spoke in Motherwell."} {
		if !strings.Contains(story.body, want) {
			t.Errorf("the missed Story lacks %q:\n%s", want, story.body)
		}
	}
}

func TestAMissedStoryWithNoTitleReRendersItsFormWith422(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeBriefing(t, store, saturday, spoke)
	app, _ := newAppWithLog(t, saturday, store)

	for _, path := range []string{today, "/stories/1/feedback"} {
		res := send(t, app, "POST", path, url.Values{"kind": {"missed"}, "level": {"story"}, "title": {" "}, "facts": {"1"}})

		if res.status != 422 || !strings.Contains(res.body, "Give the missed Story a title.") ||
			!strings.Contains(res.body, `name="facts" value="1" checked`) {
			t.Errorf("%s: status %d, or the form is not re-rendered with its error and what was chosen:\n%s", path, res.status, res.body)
		}
		if !strings.Contains(res.body, "Andy Burnham spoke in Motherwell.") {
			t.Errorf("%s: the page is not re-rendered around the form:\n%s", path, res.body)
		}
	}
	if list := get(t, app, "/stories"); strings.Contains(list.body, "/stories/2") {
		t.Errorf("a Story was started from a form that failed:\n%s", list.body)
	}
}

func TestMissedOnADevelopmentAddsTheFactChosenToTheStory(t *testing.T) {
	for _, path := range []string{"/", "/stories/2"} {
		t.Run(path, func(t *testing.T) {
			store := openStore(t)
			connectedStories(t, store)
			// Only the speech is tied to a Topic, so only its Story is told.
			writeBriefing(t, store, saturday, `{"stories": [{"story": "S2", "statements": [
				{"text": "The Prime Minister spoke in Motherwell.", "developments": ["D1"]}]}]}`)
			app, _ := newAppWithLog(t, saturday, store)
			page := get(t, app, path)
			// The offer shares Andy Burnham with the speech's Story, and is
			// not yet a Development in it.
			form := regexp.MustCompile(`hx-post="([^"]+)"[^>]*>\s*<input type="hidden" name="kind" value="missed"><input type="hidden" name="level" value="development"><input type="hidden" name="story" value="2">`).
				FindStringSubmatch(page.body)
			if form == nil || !strings.Contains(page.body, `<option value="1">`) {
				t.Fatalf("no form for a missed Development offering the offer:\n%s", page.body)
			}

			res := send(t, app, "POST", form[1], url.Values{"kind": {"missed"}, "level": {"development"}, "story": {"2"}, "fact": {"1"}})

			if res.status != 303 || res.header.Get("Location") != path {
				t.Fatalf("status %d, Location %q:\n%s", res.status, res.header.Get("Location"), res.body)
			}
			story := get(t, app, "/stories/2")
			timeline := story.body[:strings.Index(story.body, `id="causes"`)]
			if !strings.Contains(timeline, "Andy Burnham made an offer to Holyrood.") || !strings.Contains(timeline, "You added this Development") {
				t.Errorf("the missed Development is not on the Timeline, marked:\n%s", story.body)
			}
			again := send(t, app, "POST", form[1], url.Values{"kind": {"missed"}, "level": {"development"}, "story": {"2"}, "fact": {"1"}})
			if again.status != 422 || !strings.Contains(again.body, "already a Development") {
				t.Errorf("adding it twice: status %d:\n%s", again.status, again.body)
			}
		})
	}
}

func TestEveryPieceOfFeedbackIsKeptAsGivenWithItsDepthAndWhen(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeBriefing(t, store, saturday, spoke)
	app, _ := newAppWithLog(t, saturday, store)
	for _, give := range []struct {
		path string
		form url.Values
	}{
		{today, url.Values{"kind": {"new to me"}, "level": {"development"}, "story": {"1"}, "fact": {"1"}}},
		{"/stories/1/feedback", url.Values{"kind": {"wrong"}, "level": {"fact"}, "fact": {"1"}}},
		{"/stories/1/feedback", url.Values{"kind": {"missed"}, "level": {"story"}, "title": {"<b hx-get=\"/evil\">Holyrood</b>"}}},
	} {
		if res := send(t, app, "POST", give.path, give.form); res.status != 303 {
			t.Fatalf("%v: status %d:\n%s", give.form, res.status, res.body)
		}
	}

	res := get(t, app, "/feedback")

	if res.status != 200 || !strings.Contains(res.body, "<!doctype html>") {
		t.Fatalf("status %d, not a full page:\n%s", res.status, res.body)
	}
	// Newest first; each says what was said, about what, at what Depth,
	// from where and when. A Fact withdrawn is still named as it was.
	missed := strings.Index(res.body, "missed")
	wrong := strings.Index(res.body, "wrong")
	newToMe := strings.Index(res.body, "new to me")
	if missed < 0 || wrong < missed || newToMe < wrong {
		t.Fatalf("the Feedback is not listed newest first:\n%s", res.body)
	}
	for _, want := range []string{"Andy Burnham spoke in Motherwell.", "Andy Burnham in Scotland", "close",
		`<a href="/briefings/2026-10-24">`, "24 October 2026", `hx-disable>&lt;b hx-get`} {
		if !strings.Contains(res.body, want) {
			t.Errorf("the Feedback page lacks %q:\n%s", want, res.body)
		}
	}
}

// undoForm is the path the page's undo of the piece of Feedback goes to, or
// "" if the page offers none.
func undoForm(body string, id int) string {
	m := regexp.MustCompile(`<form[^>]*hx-put="([^"]*/feedback/` + strconv.Itoa(id) + `/undone)"[^>]*hx-target="body"`).FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return m[1]
}

func TestFeedbackIsUndoneFromTheFeedbackPageAndTheBriefingReRendersWithWhatItHid(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeBriefing(t, store, saturday, spoke)
	app, _ := newAppWithLog(t, saturday, store)
	send(t, app, "POST", today, url.Values{"kind": {"wrong"}, "level": {"fact"}, "fact": {"1"}})
	path := undoForm(get(t, app, "/feedback").body, 1)
	if path != "/feedback/1/undone" {
		t.Fatalf("the Feedback page offers no undo of the piece: %q", path)
	}

	res := send(t, app, "PUT", path, nil)

	if res.status != 303 || res.header.Get("Location") != "/feedback" {
		t.Fatalf("status %d, Location %q, want 303 to /feedback:\n%s", res.status, res.header.Get("Location"), res.body)
	}
	list := get(t, app, "/feedback")
	if !strings.Contains(list.body, "Undone Sat 24 Oct 2026") || undoForm(list.body, 1) != "" {
		t.Errorf("the piece is not listed marked undone, or can be undone again:\n%s", list.body)
	}
	if after := get(t, app, "/"); !strings.Contains(after.body, "The Prime Minister spoke in Motherwell.") {
		t.Errorf("the Briefing does not re-render with the statement the undone piece hid:\n%s", after.body)
	}
	if again := send(t, app, "PUT", path, nil); again.status != 303 {
		t.Errorf("undoing it again: status %d, want 303 with nothing changed", again.status)
	}
	if res := send(t, app, "PUT", "/feedback/9/undone", nil); res.status != 404 {
		t.Errorf("undoing a piece not in the Record: status %d, want 404", res.status)
	}
}

func TestABriefingEmptiedByFeedbackSaysSoAndTheFeedbackIsUndoneFromIt(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeBriefing(t, store, saturday, spoke)
	app, _ := newAppWithLog(t, saturday, store)
	send(t, app, "POST", today, url.Values{"kind": {"unwanted"}, "level": {"story"}, "story": {"1"}})

	emptied := get(t, app, "/")

	if strings.Contains(emptied.body, "Nothing moved on") || !strings.Contains(emptied.body, "Your Feedback took every Story out of this Briefing") {
		t.Errorf("the emptied Briefing does not say Feedback took its Stories out:\n%s", emptied.body)
	}
	path := undoForm(emptied.body, 1)
	if path != "/briefings/2026-10-24/feedback/1/undone" {
		t.Fatalf("the emptied Briefing does not point to the Feedback that took its Stories out, to undo: %q\n%s", path, emptied.body)
	}

	res := send(t, app, "PUT", path, nil)

	if res.status != 303 || res.header.Get("Location") != "/" {
		t.Fatalf("status %d, Location %q, want 303 to /:\n%s", res.status, res.header.Get("Location"), res.body)
	}
	if after := get(t, app, "/"); !strings.Contains(after.body, "The Prime Minister spoke in Motherwell.") {
		t.Errorf("the Briefing does not re-render with the Story the undone piece took out:\n%s", after.body)
	}
}

func TestFeedbackGivenOnTheBriefingIsUndoneFromIt(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeBriefing(t, store, saturday, spoke)
	app, _ := newAppWithLog(t, saturday, store)
	send(t, app, "POST", today, url.Values{"kind": {"new to me"}, "level": {"development"}, "story": {"1"}, "fact": {"1"}})
	send(t, app, "POST", today, url.Values{"kind": {"missed"}, "level": {"story"}, "title": {"Scottish politics"}, "facts": {"1"}})
	page := get(t, app, "/")
	if undoForm(page.body, 1) == "" || !strings.Contains(page.body, "Your Feedback on this Briefing") {
		t.Fatalf("the Briefing does not list the Feedback given on it, to undo:\n%s", page.body)
	}
	path := undoForm(page.body, 2)

	res := send(t, app, "PUT", path, nil)

	if res.status != 303 || res.header.Get("Location") != "/" {
		t.Fatalf("status %d, Location %q, want 303 to /:\n%s", res.status, res.header.Get("Location"), res.body)
	}
	if list := get(t, app, "/stories"); strings.Contains(list.body, "Scottish politics") {
		t.Errorf("the Story the undone missed piece started is still listed:\n%s", list.body)
	}
	if after := get(t, app, "/"); !strings.Contains(after.body, "Undone Sat 24 Oct 2026") || undoForm(after.body, 1) == "" {
		t.Errorf("the Briefing does not list the piece undone beside the one standing:\n%s", after.body)
	}
}

func TestFeedbackIsUndoneFromAStorysPage(t *testing.T) {
	for _, tc := range []struct {
		name string
		form url.Values
		// gone is what the piece's mark shows, which its undo lifts.
		gone string
	}{
		{"wrong connection", url.Values{"kind": {"wrong"}, "level": {"connection"}, "fact": {"1"}, "to_fact": {"2"}},
			"Withdrawn:</strong> this Connection"},
		{"wrong fact", url.Values{"kind": {"wrong"}, "level": {"fact"}, "fact": {"2"}}, "Withdrawn:</strong> its Fact"},
		{"missed development", url.Values{"kind": {"missed"}, "level": {"development"}, "story": {"2"}, "fact": {"1"}},
			"You added this Development"},
		{"unwanted story", url.Values{"kind": {"unwanted"}, "level": {"story"}, "story": {"2"}}, "Unwanted in the Briefing"},
		{"new to me", url.Values{"kind": {"new to me"}, "level": {"story"}, "story": {"2"}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := openStore(t)
			connectedStories(t, store)
			app, _ := newAppWithLog(t, saturday, store)
			send(t, app, "POST", "/stories/2/feedback", tc.form)
			page := get(t, app, "/stories/2")
			if tc.gone != "" && !strings.Contains(page.body, tc.gone) {
				t.Fatalf("the piece made no mark %q:\n%s", tc.gone, page.body)
			}
			path := undoForm(page.body, 1)
			if path != "/stories/2/feedback/1/undone" {
				t.Fatalf("the Story's page offers no undo of the piece: %q\n%s", path, page.body)
			}

			res := send(t, app, "PUT", path, nil)

			if res.status != 303 || res.header.Get("Location") != "/stories/2" {
				t.Fatalf("status %d, Location %q, want 303 to the Story:\n%s", res.status, res.header.Get("Location"), res.body)
			}
			after := get(t, app, "/stories/2")
			if tc.gone != "" && strings.Contains(after.body, tc.gone) {
				t.Errorf("the undo did not lift the mark %q:\n%s", tc.gone, after.body)
			}
			if !strings.Contains(after.body, "Undone Sat 24 Oct 2026") {
				t.Errorf("the Story's page does not list the piece marked undone:\n%s", after.body)
			}
		})
	}
}

func TestFeedbackOnWhatIsNotInTheRecordIsNotFound(t *testing.T) {
	app := newApp(t, saturday)

	for _, path := range []string{today, "/stories/7/feedback"} {
		if res := send(t, app, "POST", path, url.Values{"kind": {"wrong"}, "level": {"fact"}, "fact": {"7"}}); res.status != 404 {
			t.Errorf("%s: status %d, want 404", path, res.status)
		}
	}
	if res := send(t, app, "POST", "/stories/7/feedback", url.Values{"kind": {"missed"}, "level": {"story"}, "title": {"T"}}); res.status != 404 {
		t.Errorf("a missed Story from a Story not in the Record: status %d, want 404", res.status)
	}
	if res := send(t, app, "POST", "/briefings/2026-10-25/feedback", url.Values{}); res.status != 404 {
		t.Errorf("a Briefing not yet due: status %d, want 404", res.status)
	}
}

// connectedStories makes two Stories: Scottish devolution (Story 1), whose
// Development is the offer (Fact 1), and Burnham in Motherwell (Story 2),
// whose Development is the speech (Fact 2), which the offer caused. Andy
// Burnham is the actor in both.
func connectedStories(t *testing.T, store *sqlite.Store) {
	t.Helper()
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
	connectWaiting(t, store, `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}]}], "causes": []}`)
	readSpeech(t, store, reading)
	placeWaiting(t, store, `{"new_stories": [{"ref": "N1", "title": "Burnham in Motherwell"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}]}`)
	connectWaiting(t, store, `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}, {"entity": "Motherwell", "role": "place"}]}],
		  "causes": [{"cause": "C1", "effect": "F1"}]}`)
}

func TestWrongOnAFactWithdrawsTheConnectionsBuiltOnItAndTheStoriesItConnected(t *testing.T) {
	store := openStore(t)
	connectedStories(t, store)
	app, _ := newAppWithLog(t, saturday, store)

	res := send(t, app, "POST", "/stories/1/feedback", url.Values{"kind": {"wrong"}, "level": {"fact"}, "fact": {"1"}})

	if res.status != 303 {
		t.Fatalf("status %d:\n%s", res.status, res.body)
	}
	speech := get(t, app, "/stories/2")
	causes := strings.Index(speech.body, `id="causes"`)
	if causes < 0 || !strings.Contains(speech.body[causes:], "Withdrawn") {
		t.Errorf("the cause resting on the wrong Fact is not marked withdrawn:\n%s", speech.body)
	}
	// The Stories were connected only through the wrong Fact's Entity.
	if strings.Contains(speech.body, `id="connected"`) {
		t.Errorf("the Stories are still connected through the withdrawn Fact:\n%s", speech.body)
	}
}

func TestWrongOnAConnectionFromAStorysPageWithdrawsIt(t *testing.T) {
	store := openStore(t)
	connectedStories(t, store)
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, "/stories/2")
	if !strings.Contains(page.body, `hx-post="/stories/2/feedback"`) {
		t.Fatalf("the Story's page offers no Feedback:\n%s", page.body)
	}

	res := send(t, app, "POST", "/stories/2/feedback",
		url.Values{"kind": {"wrong"}, "level": {"connection"}, "fact": {"1"}, "to_fact": {"2"}})

	if res.status != 303 || res.header.Get("Location") != "/stories/2" {
		t.Fatalf("status %d, Location %q, want 303 to the Story:\n%s", res.status, res.header.Get("Location"), res.body)
	}
	after := get(t, app, "/stories/2")
	causes := after.body[strings.Index(after.body, `id="causes"`):]
	if !strings.Contains(causes, "Withdrawn") {
		t.Errorf("the withdrawn Connection is not marked:\n%s", causes)
	}
	// The Facts it connected stand.
	if strings.Contains(after.body[:strings.Index(after.body, `id="causes"`)], "Withdrawn") {
		t.Errorf("a Development was withdrawn with the Connection:\n%s", after.body)
	}
}
