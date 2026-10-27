package web_test

import (
	"context"
	"encoding/json"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// fakeModel answers every reading with the same output.
type fakeModel struct{ output string }

func (f fakeModel) Complete(context.Context, editor.Request) (editor.Response, error) {
	return editor.Response{Output: json.RawMessage(f.output), Model: "fake"}, nil
}

// noWikidata is a Wikidata that finds nothing, so every Entity is the
// Editor's own.
type noWikidata struct{}

func (noWikidata) Search(context.Context, string) ([]string, error) { return nil, nil }

func (noWikidata) Items(context.Context, []string) ([]editor.WikidataItem, error) { return nil, nil }

func (noWikidata) Labels(context.Context, []string) (map[string]string, error) { return nil, nil }

// readSpeech seeds a speech, ties it to a Topic and reads it with output.
func readSpeech(t *testing.T, store *sqlite.Store, output string) {
	t.Helper()
	speech := item("speech", "PM speech in Scotland", saturday.Add(-48*time.Hour))
	speech.Content = "<p>It’s good to be here in Motherwell.</p>"
	// GOV.UK's entry ids, like most Atom ids, hold slashes and colons.
	speech.EntryID = "tag:www.gov.uk,2005:/government/speeches/pm-speech-in-scotland?x=1#y"
	seed(t, store, saturday.AddDate(0, 0, -7), speech)
	topics := interest.New(store)
	if _, errs, err := topics.Add(t.Context(), interest.Draft{Name: "Scotland", Depth: "close", Terms: "Scotland"}); err != nil || errs != nil {
		t.Fatalf("add Topic: %v %v", errs, err)
	}
	ed := editor.New(editor.Deps{Store: store, Model: fakeModel{output}, Wikidata: noWikidata{}, Log: slog.New(slog.DiscardHandler)})
	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
}

const reading = `{
  "facts": [{"statement": "Andy Burnham spoke in Motherwell.", "as_at": "2026-10-22",
             "quote": "good to be here in Motherwell", "entities": [
               {"name": "Andy Burnham", "kind": "person", "role": ""}, {"name": "Motherwell", "kind": "place", "role": ""}]}],
  "claims": [{"statement": "Scotland is the true north.", "made_by": "Andy Burnham",
              "quote": "the true north", "entities": [{"name": "Andy Burnham", "kind": "person", "role": ""}]}]
}`

// itemPage follows the Items page's link to the Item's own page.
func itemPage(t *testing.T, app http.Handler, title string) response {
	t.Helper()
	list := get(t, app, "/items").body
	m := regexp.MustCompile(`<a href="(/items/[^"]+)"[^>]*>[^<]*` + regexp.QuoteMeta(title)).FindStringSubmatch(list)
	if m == nil {
		t.Fatalf("the Items page does not link %q to its page:\n%s", title, list)
	}
	return get(t, app, html.UnescapeString(m[1]))
}

func TestAnItemsPageShowsItsFactsAndClaims(t *testing.T) {
	store := openStore(t)
	readSpeech(t, store, reading)
	app, _ := newAppWithLog(t, saturday, store)

	res := itemPage(t, app, "PM speech in Scotland")

	if res.status != 200 || !strings.Contains(res.body, "<!doctype html>") {
		t.Fatalf("status %d, not a full page:\n%s", res.status, res.body)
	}
	for _, want := range []string{
		"Andy Burnham spoke in Motherwell.", "22 October 2026",
		"Scotland is the true north.", "Andy Burnham",
		`href="https://www.gov.uk/government/news/speech"`, "Open Government Licence v3.0",
	} {
		if !strings.Contains(res.body, want) {
			t.Errorf("Item page lacks %q:\n%s", want, res.body)
		}
	}
}

func TestAnItemsPageListsItsMentionsOnceAndPerFactOnlyTheEntitiesItInvolves(t *testing.T) {
	store := openStore(t)
	it := item("speech", "PM speech in Scotland", saturday.Add(-48*time.Hour))
	seed(t, store, saturday.AddDate(0, 0, -7), it)
	// A reading in the current schema ties the Fact to every mention, and
	// the Claim to none.
	if err := store.SaveReading(t.Context(), editor.Reading{
		SourceID: it.SourceID, EntryID: it.EntryID, Model: "fake",
		Entities: []editor.Entity{
			{Name: "Andy Burnham", Kind: "person", WikidataID: "Q123", Label: "Andy Burnham"},
			{Name: "Motherwell", Kind: "place"},
		},
		Facts: []editor.Fact{{Statement: "Andy Burnham spoke in Motherwell.", AsAt: time.Date(2026, time.October, 22, 0, 0, 0, 0, time.UTC),
			Quote: "good to be here in Motherwell", Entities: []string{"Andy Burnham", "Motherwell"}}},
		Claims: []editor.Claim{{Statement: "Scotland is the true north.", MadeBy: "Andy Burnham", Quote: "the true north"}},
	}, saturday); err != nil {
		t.Fatal(err)
	}
	placeWaiting(t, store, `{"new_stories": [{"ref": "N1", "title": "Burnham in Motherwell"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}]}`)
	// Connecting gives Andy Burnham a role in the Fact, and Motherwell none.
	connectWaiting(t, store, `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}]}], "causes": []}`)
	app, _ := newAppWithLog(t, saturday, store)

	res := itemPage(t, app, "PM speech in Scotland")
	if res.status != 200 {
		t.Fatalf("status %d:\n%s", res.status, res.body)
	}
	section := func(id string) string {
		t.Helper()
		at := strings.Index(res.body, `id="`+id+`"`)
		if at < 0 {
			t.Fatalf("the Item's page has no %s:\n%s", id, res.body)
		}
		end := strings.Index(res.body[at:], "</section>")
		return res.body[at : at+end]
	}
	mentions := section("mentions")
	for _, want := range []string{`<a href="https://www.wikidata.org/wiki/Q123"`, "Andy Burnham", "person", "Motherwell", "place"} {
		if !strings.Contains(mentions, want) {
			t.Errorf("the mentions lack %q:\n%s", want, mentions)
		}
	}
	if n := strings.Count(mentions, "Andy Burnham</"); n != 1 {
		t.Errorf("Andy Burnham is listed %d times among the mentions, want once:\n%s", n, mentions)
	}
	facts := section("facts")
	if !strings.Contains(facts, "Involves <span hx-disable>Andy Burnham</span> as actor") {
		t.Errorf("the Fact does not show Andy Burnham as its actor:\n%s", facts)
	}
	if strings.Contains(facts, "Motherwell</span> as") || strings.Contains(facts, "Andy Burnham, Motherwell") {
		t.Errorf("the Fact lists Motherwell, which it only mentions:\n%s", facts)
	}
	if claims := section("claims"); strings.Contains(claims, "Involves") {
		t.Errorf("the Claim shows an Entity list:\n%s", claims)
	}
}

func TestTheItemsPageSaysWhichItemsHaveBeenRead(t *testing.T) {
	store := openStore(t)
	readSpeech(t, store, reading)
	app, _ := newAppWithLog(t, saturday, store)

	entry := itemEntry(t, get(t, app, "/items").body, "https://www.gov.uk/government/news/speech")

	if !strings.Contains(entry, "Read: 1 Fact, 1 Claim") || strings.Contains(entry, "Awaiting reading") {
		t.Errorf("the read speech is not shown as read:\n%s", entry)
	}
}

func TestWhatReadingDrewIsInertOnTheItemsPage(t *testing.T) {
	store := openStore(t)
	hostile := strings.Replace(reading, "Scotland is the true north.", `<span hx-get=\"/evil\">Scotland</span>`, 1)
	readSpeech(t, store, hostile)
	app, _ := newAppWithLog(t, saturday, store)

	res := itemPage(t, app, "PM speech in Scotland")

	if strings.Contains(res.body, "<span hx-get") {
		t.Errorf("model output was not escaped:\n%s", res.body)
	}
	claims := res.body[strings.Index(res.body, `id="claims"`):]
	if !strings.Contains(claims, "hx-disable") {
		t.Errorf("Claims are not inside hx-disable:\n%s", claims)
	}
}

func TestAnItemNotInTheRecordIsNotFound(t *testing.T) {
	app := newApp(t, saturday)

	if res := get(t, app, "/items/govuk-number-10/"+url.PathEscape("tag:nothing")); res.status != 404 {
		t.Errorf("status = %d, want 404", res.status)
	}
}
