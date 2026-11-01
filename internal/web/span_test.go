package web_test

import (
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// hosting is cut into S1 "Derby will host the headquarters."
const hostingReading = `{"names": [{"at": {"span": 1, "words": "Derby"}, "kind": "place", "name": "Derby", "office": ""}],
  "refers_to": [], "rejected": [], "speakers": []}`

// relatedRecord is a store holding the release, read, and a later Item
// read after it saying Derby will host the headquarters, related to it: S1
// of the later Item follows the release's S2.
func relatedRecord(t *testing.T) *sqlite.Store {
	t.Helper()
	store := readToRelate(t)
	ed := editor.New(editor.Deps{Store: store, RelatingModel: fakeModel{`{"relations": [{"span": "S1", "to": "C1", "verdict": "follows"}]}`},
		Log: slog.New(slog.DiscardHandler)})
	if err := ed.RelateWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	return store
}

// readToRelate is a store holding the release, read, and a later Item read
// after it saying Derby will host the headquarters, what both name looked
// up, awaiting relating.
func readToRelate(t *testing.T) *sqlite.Store {
	t.Helper()
	store := openStore(t)
	readRelease(t, store, releaseReading)
	hosting := item("hosting", "Derby to host GBR", saturday)
	hosting.Content = "<p>Derby will host the headquarters.</p>"
	seed(t, store, saturday.AddDate(0, 0, -7), hosting)
	if err := cut.New(store).CutNew(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	if err := editor.New(editor.Deps{Store: store, Model: fakeModel{hostingReading}, Log: log}).ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	if err := editor.NewLookups(editor.LookupDeps{Store: store, Wikidata: derbyOnly{}, Log: log}).LookUpWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	return store
}

// spanPath is the page of Span n of the Item entry, as its Item's page
// links it.
func spanPath(t *testing.T, app http.Handler, entry string, n int) string {
	t.Helper()
	page := get(t, app, "/items/"+number10.ID+"/"+entry).body
	m := regexp.MustCompile(`<a href="(/items/` + number10.ID + `/` + entry + `/spans/\d+-\d+)">S` + strconv.Itoa(n) + `</a>`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("the Item's page does not link S%d's page:\n%s", n, page)
	}
	return m[1]
}

func TestARejectedSpansPageStrikesThroughOnlyItsText(t *testing.T) {
	store := openStore(t)
	readRelease(t, store, releaseReading)
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, spanPath(t, app, "release", 1)).body

	at := strings.Index(page, `<li class="block`)
	if at < 0 {
		t.Fatalf("the Span's page has no block:\n%s", page)
	}
	rest := page[at:]
	marginAt := strings.Index(rest, `class="block-margin`)
	end := strings.Index(rest, "</li>")
	if marginAt < 0 || end < marginAt {
		t.Fatalf("the Span's block has no margin:\n%s", rest)
	}
	if !strings.HasPrefix(rest, `<li class="block">`) || !strings.Contains(rest[:marginAt], `<div class="block-body struck"`) {
		t.Errorf("the Rejected Span's text alone is not struck through:\n%s", rest[:end])
	}
	if margin := rest[marginAt:end]; !strings.Contains(margin, "rejected") || strings.Contains(margin, "struck") {
		t.Errorf("the Rejected Span's margin does not say so plainly:\n%s", margin)
	}
}

func TestASpansPageShowsItWithItsItemEntitiesAndRelationsBothWaysEachWithItsCall(t *testing.T) {
	app, _ := newAppWithLog(t, saturday, relatedRecord(t))

	res := get(t, app, spanPath(t, app, "release", 2))

	if res.status != http.StatusOK || !strings.Contains(res.body, "<!doctype html>") {
		t.Fatalf("status %d:\n%s", res.status, res.body)
	}
	page := res.body
	for _, want := range []string{
		`“This is fantastic news for <ins title="the city">Derby</ins>.`,
		`<cite hx-disable>GBR headquarters in Derby</cite>`,
		`<span hx-disable>Derby</span> · <span class="pill-tag">place</span>`,
		"spoken by <span hx-disable>Andy Burnham</span>",
		"followed by",
		"Derby will host the headquarters.",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the Span's page does not show %q:\n%s", want, page)
		}
	}
	if !regexp.MustCompile(`<a href="/items/` + number10.ID + `/hosting/spans/\d+-\d+">`).MatchString(page) {
		t.Errorf("the relation does not link the Span it relates to:\n%s", page)
	}
	if !regexp.MustCompile(`<a class="machine" href="/calls/\d+">related · call \d+</a>`).MatchString(page) {
		t.Errorf("the relation does not link the call that made it:\n%s", page)
	}

	other := get(t, app, spanPath(t, app, "hosting", 1)).body
	if !strings.Contains(other, "follows") || !strings.Contains(other, "This is fantastic news for") {
		t.Errorf("the later Span's page does not show it follows the release's:\n%s", other)
	}
}

func TestASpanNotInItsItemIsNotFound(t *testing.T) {
	app, _ := newAppWithLog(t, saturday, relatedRecord(t))
	for _, path := range []string{
		"/items/" + number10.ID + "/release/spans/0-99999",
		"/items/" + number10.ID + "/release/spans/5-2",
		"/items/" + number10.ID + "/release/spans/nonsense",
		"/items/" + number10.ID + "/nothing/spans/0-5",
	} {
		if res := get(t, app, path); res.status != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, res.status)
		}
	}
}
