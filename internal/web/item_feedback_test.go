package web_test

import (
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// itemPagePath is the speech's page, as the Items page links it.
func itemPagePath(t *testing.T, list string) string {
	t.Helper()
	m := regexp.MustCompile(`<a href="(/items/[^"]+)"[^>]*>[^<]*PM speech in Scotland`).FindStringSubmatch(list)
	if m == nil {
		t.Fatalf("the Items page does not link the speech:\n%s", list)
	}
	return html.UnescapeString(m[1])
}

func TestWrongOnAFactIsGivenFromItsItemsPage(t *testing.T) {
	store := openStore(t)
	readSpeech(t, store, reading)
	app, _ := newAppWithLog(t, saturday, store)
	page := itemPagePath(t, get(t, app, "/items").body)

	before := get(t, app, page)
	if !offers(before.body, "wrong", "fact", "fact", "1") {
		t.Fatalf("the Item's page offers no Wrong on its Fact:\n%s", before.body)
	}
	if !strings.Contains(before.body, `hx-confirm="Withdraw this Fact, and the Developments and Connections built on it?"`) {
		t.Errorf("Wrong on the Fact asks no confirmation:\n%s", before.body)
	}
	if strings.Contains(before.body, `value="new to me"`) {
		t.Errorf("the Item's page offers New to me:\n%s", before.body)
	}
	action := regexp.MustCompile(`hx-post="([^"]+)"`).FindStringSubmatch(before.body)
	if action == nil || html.UnescapeString(action[1]) != page+"/feedback" {
		t.Fatalf("Wrong is not given at the Item's own route: %v", action)
	}

	res := send(t, app, "POST", page+"/feedback", url.Values{"kind": {"wrong"}, "level": {"fact"}, "fact": {"1"}})

	if res.status != 303 || res.header.Get("Location") != page {
		t.Fatalf("status %d, Location %q, want 303 to %q:\n%s", res.status, res.header.Get("Location"), page, res.body)
	}
	if kept := get(t, app, "/feedback"); !strings.Contains(kept.body, "Andy Burnham spoke in Motherwell.") {
		t.Errorf("the Feedback is not kept:\n%s", kept.body)
	}
}

func TestAFactAlreadyWrongShowsItGivenOnItsItemsPageWithAnUndoBackToIt(t *testing.T) {
	store := openStore(t)
	readSpeech(t, store, reading)
	app, _ := newAppWithLog(t, saturday, store)
	page := itemPagePath(t, get(t, app, "/items").body)
	// Given elsewhere, on the Feedback route of a Story's page or the
	// Briefing, it is the same piece.
	send(t, app, "POST", page+"/feedback", url.Values{"kind": {"wrong"}, "level": {"fact"}, "fact": {"1"}})

	given := get(t, app, page)
	if offers(given.body, "wrong", "fact", "fact", "1") || !givenWithUndo(html.UnescapeString(given.body), "Wrong", 1, page) {
		t.Fatalf("the Item's page does not show Wrong given, with its undo, in place of a fresh button:\n%s", given.body)
	}

	res := send(t, app, "PUT", page+"/feedback/1/undone", nil)

	if res.status != 303 || res.header.Get("Location") != page {
		t.Fatalf("undo: status %d, Location %q, want 303 to %q:\n%s", res.status, res.header.Get("Location"), page, res.body)
	}
	if undone := get(t, app, page); !offers(undone.body, "wrong", "fact", "fact", "1") {
		t.Errorf("undone, the Item's page does not offer Wrong again:\n%s", undone.body)
	}
}

func TestFeedbackThatFailsOnAnItemsPageReRendersItWith422(t *testing.T) {
	store := openStore(t)
	readSpeech(t, store, reading)
	app, _ := newAppWithLog(t, saturday, store)
	page := itemPagePath(t, get(t, app, "/items").body)

	// A Fact is not unwanted: a Development or a Story is.
	res := send(t, app, "POST", page+"/feedback", url.Values{"kind": {"unwanted"}, "level": {"fact"}, "fact": {"1"}})

	if res.status != 422 || !strings.Contains(res.body, "<!doctype html>") || !strings.Contains(res.body, "Andy Burnham spoke in Motherwell.") {
		t.Fatalf("status %d, want 422 with the Item's page re-rendered:\n%s", res.status, res.body)
	}
	if !strings.Contains(html.UnescapeString(res.body), `<p role="alert">That Feedback can't be given on that.</p>`) {
		t.Errorf("the page does not say why it failed:\n%s", res.body)
	}
}
