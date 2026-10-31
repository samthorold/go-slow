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

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
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

// readSpeech seeds a speech, cut into S1 "It’s good to be here in
// Motherwell.", and reads it with output, the reading's call kept.
func readSpeech(t *testing.T, store *sqlite.Store, output string) {
	t.Helper()
	speech := item("speech", "PM speech in Scotland", saturday.Add(-48*time.Hour))
	speech.Content = "<p>It’s good to be here in Motherwell.</p>"
	// GOV.UK's entry ids, like most Atom ids, hold slashes and colons.
	speech.EntryID = "tag:www.gov.uk,2005:/government/speeches/pm-speech-in-scotland?x=1#y"
	seed(t, store, saturday.AddDate(0, 0, -7), speech)
	if err := cut.New(store).CutNew(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Deps{Store: store, Model: fakeModel{output}, Log: slog.New(slog.DiscardHandler)})
	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
}

// speechReading names Motherwell in the speech's one Span.
const speechReading = `{"names": [{"at": {"span": 1, "words": "Motherwell"}, "kind": "place", "name": "Motherwell", "office": ""}],
  "refers_to": [], "rejected": [], "speakers": []}`

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

func TestAnItemsPageShowsItAsCollected(t *testing.T) {
	store := openStore(t)
	readSpeech(t, store, speechReading)
	app, _ := newAppWithLog(t, saturday, store)

	res := itemPage(t, app, "PM speech in Scotland")

	if res.status != 200 || !strings.Contains(res.body, "<!doctype html>") {
		t.Fatalf("status %d, not a full page:\n%s", res.status, res.body)
	}
	for _, want := range []string{
		"PM speech in Scotland", "It’s good to be here in Motherwell.",
		`href="https://www.gov.uk/government/news/speech"`, "Open Government Licence v3.0",
	} {
		if !strings.Contains(res.body, want) {
			t.Errorf("Item page lacks %q:\n%s", want, res.body)
		}
	}
}

func TestAnItemNotInTheRecordIsNotFound(t *testing.T) {
	app := newApp(t, saturday)

	if res := get(t, app, "/items/govuk-number-10/"+url.PathEscape("tag:nothing")); res.status != 404 {
		t.Errorf("status = %d, want 404", res.status)
	}
}
