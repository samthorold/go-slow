package web_test

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
)

var number10 = collect.Source{
	ID:          "number-10",
	Name:        "Prime Minister's Office, 10 Downing Street",
	FeedURL:     "https://www.gov.uk/government/organisations/prime-ministers-office-10-downing-street.atom",
	Attribution: collect.OGL,
	UsualGap:    4 * 24 * time.Hour,
}

func item(entry, title string, published time.Time) collect.Item {
	return collect.Item{
		SourceID:    number10.ID,
		EntryID:     entry,
		Title:       title,
		Link:        "https://www.gov.uk/government/news/" + entry,
		Summary:     "Summary of " + title,
		Published:   published,
		Updated:     published,
		Attribution: collect.OGL,
		CollectedAt: saturday,
	}
}

func seed(t *testing.T, store *sqlite.Store, since time.Time, items ...collect.Item) {
	t.Helper()
	if err := store.SyncSources(t.Context(), []collect.Source{number10}, since); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddItems(t.Context(), items); err != nil {
		t.Fatal(err)
	}
}

func TestTheItemsPageListsCollectedItemsNewestFirstLinkingToTheirOriginals(t *testing.T) {
	store := openStore(t)
	seed(t, store, saturday.AddDate(0, 0, -7),
		item("older", "PM call with the President", saturday.Add(-48*time.Hour)),
		item("newer", "PM statement on the Budget", saturday.Add(-2*time.Hour)),
	)
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/items")

	if res.status != 200 {
		t.Fatalf("status = %d, want 200", res.status)
	}
	if !strings.Contains(res.body, "<!doctype html>") {
		t.Errorf("not a full page:\n%s", res.body)
	}
	newer := strings.Index(res.body, `href="https://www.gov.uk/government/news/newer"`)
	older := strings.Index(res.body, `href="https://www.gov.uk/government/news/older"`)
	if newer < 0 || older < 0 {
		t.Fatalf("page does not link both Items to their originals:\n%s", res.body)
	}
	if newer > older {
		t.Error("the older Item is listed before the newer")
	}
	for _, want := range []string{"PM statement on the Budget", html.EscapeString(number10.Name), "Open Government Licence v3.0"} {
		if !strings.Contains(res.body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

// itemEntry is the Items page's entry for the Item linking to link.
func itemEntry(t *testing.T, body, link string) string {
	t.Helper()
	start := strings.Index(body, `href="`+link+`"`)
	if start < 0 {
		t.Fatalf("page does not list %s:\n%s", link, body)
	}
	end := strings.Index(body[start:], "</li>")
	return body[start : start+end]
}

func TestTakingUpATopicMarksTheWaitingItemsThatMatchItAsAwaitingReading(t *testing.T) {
	store := openStore(t)
	seed(t, store, saturday.AddDate(0, 0, -7),
		item("budget", "PM statement on the Budget", saturday.Add(-2*time.Hour)),
		item("call", "PM call with the President", saturday.Add(-48*time.Hour)),
	)
	// Collection's check has looked at both, while no Topic was followed.
	if err := interest.New(store).CheckNew(t.Context()); err != nil {
		t.Fatal(err)
	}
	app, _ := newAppWithLog(t, saturday, store)
	before := get(t, app, "/items")
	if !strings.Contains(itemEntry(t, before.body, "https://www.gov.uk/government/news/budget"), "touches no Topic") {
		t.Fatalf("before any Topic, the Budget statement is not shown as touching no Topic:\n%s", before.body)
	}

	addTopic(t, app, url.Values{"name": {"The Budget"}, "depth": {"close"}, "terms": {"Budget"}})

	after := get(t, app, "/items")
	budget := itemEntry(t, after.body, "https://www.gov.uk/government/news/budget")
	if !strings.Contains(budget, "Awaiting reading for") || !strings.Contains(budget, "The Budget") {
		t.Errorf("the Budget statement is not awaiting reading for The Budget:\n%s", budget)
	}
	call := itemEntry(t, after.body, "https://www.gov.uk/government/news/call")
	if !strings.Contains(call, "touches no Topic") {
		t.Errorf("the PM call is not shown as touching no Topic:\n%s", call)
	}
}

func TestRemovingATopicLeavesItsItemsTouchingNoTopic(t *testing.T) {
	store := openStore(t)
	seed(t, store, saturday.AddDate(0, 0, -7),
		item("budget", "PM statement on the Budget", saturday.Add(-2*time.Hour)))
	if err := interest.New(store).CheckNew(t.Context()); err != nil {
		t.Fatal(err)
	}
	app, _ := newAppWithLog(t, saturday, store)
	addTopic(t, app, url.Values{"name": {"The Budget"}, "depth": {"close"}, "terms": {"Budget"}})

	if res := send(t, app, http.MethodDelete, "/topics/1", nil); res.status != http.StatusOK {
		t.Fatalf("remove: status = %d", res.status)
	}

	budget := itemEntry(t, get(t, app, "/items").body, "https://www.gov.uk/government/news/budget")
	if strings.Contains(budget, "Awaiting reading") || !strings.Contains(budget, "touches no Topic") {
		t.Errorf("the Budget statement still awaits reading for a removed Topic:\n%s", budget)
	}
}

func TestCollectedTextOnTheItemsPageIsInert(t *testing.T) {
	store := openStore(t)
	hostile := item("hostile", `<button hx-delete="/">Title</button>`, saturday.Add(-time.Hour))
	hostile.Summary = `<span hx-get="/evil" hx-trigger="load">summary</span>`
	seed(t, store, saturday.AddDate(0, 0, -7), hostile)
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/items")

	if strings.Contains(res.body, `<span hx-get`) || strings.Contains(res.body, `<button hx-delete`) {
		t.Errorf("collected markup was not escaped:\n%s", res.body)
	}
	if !strings.Contains(res.body, "hx-disable") {
		t.Errorf("collected text is not inside hx-disable:\n%s", res.body)
	}
}

func TestTheItemsPageShowsAQuietSource(t *testing.T) {
	store := openStore(t)
	seed(t, store, saturday.AddDate(0, -1, 0),
		item("old", "PM call with the President", saturday.AddDate(0, 0, -10)))
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/items")

	if !strings.Contains(res.body, "quiet") {
		t.Errorf("page does not show the Source as quiet:\n%s", res.body)
	}
}

func TestTheItemsPageDoesNotFlagASourceWithinItsGap(t *testing.T) {
	store := openStore(t)
	seed(t, store, saturday.AddDate(0, -1, 0),
		item("recent", "PM statement on the Budget", saturday.Add(-time.Hour)))
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/items")

	if strings.Contains(res.body, "quiet") {
		t.Errorf("page shows a Source within its gap as quiet:\n%s", res.body)
	}
}
