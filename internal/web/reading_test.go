package web_test

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
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

// readSpeech seeds a speech, ties it to a Topic and draws its Facts and
// Claims from output, as the earlier reading act did.
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
	drawFacts(t, store, speech, output, editor.Usage{})
}

// drawFacts draws the Item's Facts, Claims and mentions from output, in the
// earlier reading act's schema, into the Record, with that act's kept call,
// reporting usage, as that act left them: no act draws Facts now, but the
// Record keeps those it drew, which the pages built on Facts still show.
func drawFacts(t *testing.T, store *sqlite.Store, it collect.Item, output string, usage editor.Usage) {
	t.Helper()
	var out struct {
		Facts []struct {
			Statement string `json:"statement"`
			AsAt      string `json:"as_at"`
			Quote     string `json:"quote"`
		} `json:"facts"`
		Claims []struct {
			Statement string `json:"statement"`
			MadeBy    string `json:"made_by"`
			Quote     string `json:"quote"`
		} `json:"claims"`
		Mentions []struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
			Role string `json:"role"`
		} `json:"mentions"`
	}
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatal(err)
	}
	r := editor.FactReading{SourceID: it.SourceID, EntryID: it.EntryID, Model: "fake"}
	for _, m := range out.Mentions {
		r.Entities = append(r.Entities, editor.Entity{Name: m.Name, Kind: m.Kind, Role: m.Role})
	}
	for _, f := range out.Facts {
		asAt, err := time.Parse(time.DateOnly, f.AsAt)
		if err != nil {
			t.Fatal(err)
		}
		r.Facts = append(r.Facts, editor.Fact{Statement: f.Statement, AsAt: asAt, Quote: f.Quote})
	}
	for _, c := range out.Claims {
		r.Claims = append(r.Claims, editor.Claim{Statement: c.Statement, MadeBy: c.MadeBy, Quote: c.Quote})
	}
	if err := store.SaveFactReading(t.Context(), r, saturday); err != nil {
		t.Fatal(err)
	}
	req := editor.Request{Act: "reading", Instructions: "You are the Editor of a private slow-news briefing. Draw the Item's Facts and Claims.",
		Input: "Title: " + it.Title, Schema: map[string]any{"type": "object", "additionalProperties": false}}
	if err := store.KeepCall(t.Context(), editor.Call{
		Act: "reading", Key: it.SourceID + "\n" + it.EntryID, At: saturday, Model: "fake",
		Request: req, RequestHash: editor.RequestHash(req, "fake"),
		Response: &editor.Response{Output: json.RawMessage(output), Model: "fake", Usage: usage},
		Outcome:  editor.Applied,
	}); err != nil {
		t.Fatal(err)
	}
}

const reading = `{
  "facts": [{"statement": "Andy Burnham spoke in Motherwell.", "as_at": "2026-10-22",
             "quote": "good to be here in Motherwell"}],
  "claims": [{"statement": "Scotland is the true north.", "made_by": "Andy Burnham",
              "quote": "the true north"}],
  "mentions": [{"name": "Andy Burnham", "kind": "person", "role": ""}, {"name": "Motherwell", "kind": "place", "role": ""}]
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
		"Andy Burnham spoke in Motherwell.",
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
	// A reading gives the Item's mentions once, and ties no statement to
	// them.
	if err := store.SaveFactReading(t.Context(), editor.FactReading{
		SourceID: it.SourceID, EntryID: it.EntryID, Model: "fake",
		Entities: []editor.Entity{
			{Name: "Andy Burnham", Kind: "person", WikidataID: "Q123", Label: "Andy Burnham"},
			{Name: "Motherwell", Kind: "place"},
		},
		Facts: []editor.Fact{{Statement: "Andy Burnham spoke in Motherwell.", AsAt: time.Date(2026, time.October, 22, 0, 0, 0, 0, time.UTC),
			Quote: "good to be here in Motherwell"}},
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
	// Each Entity is a pill: its name, then its kind where it has no Role.
	mentions := section("mentions")
	for _, want := range []string{
		`<a class="pill" href="https://www.wikidata.org/wiki/Q123" hx-boost="false"><span hx-disable>Andy Burnham</span> · <span class="pill-tag">person</span></a>`,
		`<span class="pill"><span hx-disable>Motherwell</span> · <span class="pill-tag">place</span></span>`,
	} {
		if !strings.Contains(mentions, want) {
			t.Errorf("the mentions lack the pill %s:\n%s", want, mentions)
		}
	}
	if n := strings.Count(mentions, "Andy Burnham</"); n != 1 {
		t.Errorf("Andy Burnham is listed %d times among the mentions, want once:\n%s", n, mentions)
	}
	// A Fact's pills are the Entities it involves, each with its Role.
	fact := blockOf(t, section("facts"), "Andy Burnham spoke in Motherwell.")
	if !strings.Contains(fact.margin, `<span class="pill"><span hx-disable>Andy Burnham</span> · <span class="pill-tag">actor</span></span>`) {
		t.Errorf("the Fact's margin does not show Andy Burnham as its actor:\n%s", fact.margin)
	}
	if strings.Contains(fact.margin, "Motherwell</span> ·") {
		t.Errorf("the Fact shows Motherwell, which it only mentions:\n%s", fact.margin)
	}
	if claims := section("claims"); strings.Contains(claims, `class="pill"`) {
		t.Errorf("the Claim shows Entities:\n%s", claims)
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
