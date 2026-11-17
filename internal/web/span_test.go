package web_test

import (
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/editor"
)

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

func TestASpansPageShowsItWithItsItemEntitiesAndTheReadingsCall(t *testing.T) {
	store := openStore(t)
	readRelease(t, store, releaseReading)
	if err := editor.NewLookups(editor.LookupDeps{Store: store, Wikidata: derbyOnly{}, Log: slog.New(slog.DiscardHandler)}).
		LookUpWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	app, _ := newAppWithLog(t, saturday, store)

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
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the Span's page does not show %q:\n%s", want, page)
		}
	}
	if !regexp.MustCompile(`<a class="machine" href="/calls/\d+">read · call \d+</a>`).MatchString(page) {
		t.Errorf("the Span's page does not link the reading's call:\n%s", page)
	}
}

func TestASpanNotInItsItemIsNotFound(t *testing.T) {
	store := openStore(t)
	readRelease(t, store, releaseReading)
	app, _ := newAppWithLog(t, saturday, store)
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
