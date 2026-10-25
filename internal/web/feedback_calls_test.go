package web_test

import (
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// give gives each piece of Feedback in turn, from the Story's page.
func give(t *testing.T, app http.Handler, forms ...url.Values) {
	t.Helper()
	for _, form := range forms {
		if res := send(t, app, "POST", "/stories/1/feedback", form); res.status != 303 {
			t.Fatalf("%v: status %d:\n%s", form, res.status, res.body)
		}
	}
}

func TestFeedbackOnAFactLinksToTheReadingCallThatDrewIt(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	app, _ := newAppWithLog(t, saturday, store)
	give(t, app, url.Values{"kind": {"wrong"}, "level": {"fact"}, "fact": {"1"}})

	page := get(t, app, "/feedback")
	res := get(t, app, callLink(t, page.body, "<strong>wrong</strong>"))

	if res.status != 200 || !strings.Contains(res.body, "reading") || !strings.Contains(res.body, "Title: PM speech in Scotland") {
		t.Errorf("status %d, or the link is not to the reading call:\n%s", res.status, res.body)
	}
}

// offerLedToSpeech is a Story of two Developments, the offer and then the
// speech, each placed by its own call, and the offer led to the speech.
func offerLedToSpeech(t *testing.T, store *sqlite.Store) {
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
	placeWaiting(t, store, `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": ["S1"]}]}`)
	connectWaiting(t, store, `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}, {"entity": "Motherwell", "role": "place"}]}],
		  "causes": [{"cause": "C1", "effect": "F1"}]}`)
}

func TestFeedbackOnAConnectionLinksToTheConnectingCallThatMadeIt(t *testing.T) {
	store := openStore(t)
	offerLedToSpeech(t, store)
	app, _ := newAppWithLog(t, saturday, store)
	give(t, app, url.Values{"kind": {"new to me"}, "level": {"connection"}, "fact": {"1"}, "to_fact": {"2"}})

	page := get(t, app, "/feedback")
	res := get(t, app, callLink(t, page.body, "<strong>new to me</strong>"))

	if res.status != 200 || !strings.Contains(res.body, "Facts to connect:") || !strings.Contains(res.body, "Item: PM speech in Scotland") {
		t.Errorf("status %d, or the link is not to the connecting call that made it:\n%s", res.status, res.body)
	}
}

func TestFeedbackOnAStoryLinksToThePlacingCallOfEachOfItsDevelopments(t *testing.T) {
	store := openStore(t)
	offerLedToSpeech(t, store)
	app, _ := newAppWithLog(t, saturday, store)
	give(t, app, url.Values{"kind": {"new to me"}, "level": {"story"}, "story": {"1"}})

	page := get(t, app, "/feedback")

	links := regexp.MustCompile(`href="(/calls/\d+)"`).FindAllStringSubmatch(page.body, -1)
	if len(links) != 2 || links[0][1] == links[1][1] {
		t.Fatalf("want a link to each of the two placing calls, got %v:\n%s", links, page.body)
	}
	for i, item := range []string{"Item: Offer to Holyrood", "Item: PM speech in Scotland"} {
		if res := get(t, app, links[i][1]); !strings.Contains(res.body, "You are placing the Facts drawn from one Item.") ||
			!strings.Contains(res.body, item) {
			t.Errorf("link %d is not to the placing call of %q:\n%s", i, item, res.body)
		}
	}
}

func TestFeedbackOnTheSundayReadOrAnAssessmentLinksToTheReadsConversation(t *testing.T) {
	for _, tc := range []struct {
		level string
		form  url.Values
	}{
		{"assessment", url.Values{"kind": {"new to me"}, "level": {"assessment"}, "assessment": {"1"}}},
		{"sunday read", url.Values{"kind": {"new to me"}, "level": {"sunday read"}, "read": {"1"}}},
	} {
		t.Run(tc.level, func(t *testing.T) {
			store := openStore(t)
			speechStory(t, store)
			var turns int
			ed := editor.New(editor.Deps{Store: store, SundayReads: store, SundayReadModel: lookThenSubmit{assessed, &turns},
				Wikidata: noWikidata{}, Log: slog.New(slog.DiscardHandler)})
			if err := ed.SundayRead(t.Context(), sunday); err != nil {
				t.Fatal(err)
			}
			app, _ := newAppWithLog(t, sunday, store)
			if res := send(t, app, "POST", "/sunday-reads/2026-10-25/feedback", tc.form); res.status != 303 {
				t.Fatalf("status %d:\n%s", res.status, res.body)
			}

			page := get(t, app, "/feedback")

			i := strings.Index(page.body, "<strong>new to me</strong>")
			if i < 0 || !strings.Contains(page.body[i:], `href="/conversations/1"`) {
				t.Errorf("the Feedback does not link to the read's conversation:\n%s", page.body)
			}
		})
	}
}

func TestWhatMissedAddedLinksToNoCallAndSaysTheOwnerMadeIt(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	app, _ := newAppWithLog(t, saturday, store)
	give(t, app,
		url.Values{"kind": {"missed"}, "level": {"story"}, "title": {"Scottish politics"}},
		url.Values{"kind": {"missed"}, "level": {"development"}, "story": {"2"}, "fact": {"1"}},
		url.Values{"kind": {"unwanted"}, "level": {"development"}, "story": {"2"}, "fact": {"1"}},
		url.Values{"kind": {"new to me"}, "level": {"story"}, "story": {"2"}},
	)

	page := get(t, app, "/feedback")

	if strings.Contains(page.body, `href="/calls/`) {
		t.Errorf("what the Owner made links to a call:\n%s", page.body)
	}
	if n := strings.Count(page.body, "You made what it is about, not an act: no call is behind it."); n != 4 {
		t.Errorf("%d pieces say the Owner made what they are about, want 4:\n%s", n, page.body)
	}
}

func TestFeedbackOnADevelopmentOrItsStoryLinksToThePlacingCall(t *testing.T) {
	for _, tc := range []struct {
		level string
		form  url.Values
	}{
		{"development", url.Values{"kind": {"new to me"}, "level": {"development"}, "story": {"1"}, "fact": {"1"}}},
		{"story", url.Values{"kind": {"new to me"}, "level": {"story"}, "story": {"1"}}},
	} {
		t.Run(tc.level, func(t *testing.T) {
			store := openStore(t)
			speechStory(t, store)
			app, _ := newAppWithLog(t, saturday, store)
			give(t, app, tc.form)

			page := get(t, app, "/feedback")
			res := get(t, app, callLink(t, page.body, "<strong>new to me</strong>"))

			if res.status != 200 || !strings.Contains(res.body, "You are placing the Facts drawn from one Item.") {
				t.Errorf("status %d, or the link is not to the placing call:\n%s", res.status, res.body)
			}
		})
	}
}
