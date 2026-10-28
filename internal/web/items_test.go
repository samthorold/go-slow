package web_test

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
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

func TestACollectedItemHasItsTitleAndSummaryInTheBodyAndItsNotesInAnOpenMargin(t *testing.T) {
	store := openStore(t)
	seed(t, store, saturday.AddDate(0, 0, -7),
		item("budget", "PM statement on the Budget", saturday.Add(-2*time.Hour)))
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, "/items").body
	b := blockOf(t, page, "PM statement on the Budget")

	if !regexp.MustCompile(`<div class="margined" data-margin="open">`).MatchString(page) {
		t.Errorf("the collected Items are not in a margined region, open:\n%s", page)
	}
	if !strings.Contains(b.body, `<p class="quiet" hx-disable>Summary of PM statement on the Budget</p>`) {
		t.Errorf("the body lacks the quiet summary:\n%s", b.body)
	}
	for _, want := range []string{
		html.EscapeString(number10.Name), "Sat 24 Oct 2026, 05:30",
		`<span class="machine" hx-disable>not yet checked</span>`, "Open Government Licence v3.0",
	} {
		if !strings.Contains(b.margin, want) {
			t.Errorf("the margin lacks %q:\n%s", want, b.margin)
		}
		if strings.Contains(b.body, want) {
			t.Errorf("the body holds %q, want it in the margin:\n%s", want, b.body)
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
	if !strings.Contains(budget, "awaiting reading for The Budget") {
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
	if strings.Contains(budget, "awaiting reading") || !strings.Contains(budget, "touches no Topic") {
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

func TestTheItemsPageShowsAScheduledReleaseAsUpcomingUntilItIsDue(t *testing.T) {
	store := openStore(t)
	scheduled := item("gdp", "GDP first quarterly estimate", time.Time{})
	scheduled.Due = saturday.Add(48 * time.Hour)
	past := item("cpi", "Consumer price inflation", time.Time{})
	past.Due = saturday.Add(-48 * time.Hour)
	seed(t, store, saturday.AddDate(0, 0, -7), scheduled, past)
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/items")

	section := element(t, res.body, `id="upcoming"`, "</details>")
	if !strings.HasPrefix(section, "<details") || strings.HasPrefix(section, "<details open") {
		t.Errorf("Upcoming is not collapsed:\n%s", section)
	}
	if strings.Index(res.body, section) < strings.Index(res.body, `id="collected"`) {
		t.Errorf("Upcoming is not at the foot, after the collected Items")
	}
	if !strings.Contains(section, "GDP first quarterly estimate") || !strings.Contains(section, "Mon 26 Oct 2026") {
		t.Errorf("upcoming section lacks the scheduled release and its date:\n%s", section)
	}
	if strings.Contains(res.body, "Consumer price inflation") {
		t.Errorf("a release past its date is still shown:\n%s", res.body)
	}
	if n := strings.Count(res.body, ">GDP first quarterly estimate</a>"); n != 1 {
		t.Errorf("scheduled release shown %d times, want once, as upcoming", n)
	}
}

func TestAQuietSourceLeadsTheItemsPageAtFullWeight(t *testing.T) {
	store := openStore(t)
	seed(t, store, saturday.AddDate(0, -1, 0),
		item("old", "PM call with the President", saturday.AddDate(0, 0, -10)))
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/items")

	exceptions := leadingExceptions(t, res.body)
	for _, want := range []string{html.EscapeString(number10.Name), "quiet: nothing new past its usual gap"} {
		if !strings.Contains(exceptions, want) {
			t.Errorf("the exceptions lack %q:\n%s", want, exceptions)
		}
	}
	if strings.Contains(res.body, "All Sources current") {
		t.Errorf("the page says all Sources are current with one quiet:\n%s", res.body)
	}
}

// leadingExceptions is the Items page's exceptions, checked to come first,
// before the Sources and the collected Items, and at full weight.
func leadingExceptions(t *testing.T, body string) string {
	t.Helper()
	exceptions := element(t, body, `id="exceptions"`, "</section>")
	at := strings.Index(body, exceptions)
	for _, after := range []string{`id="sources"`, `id="collected"`} {
		if i := strings.Index(body, after); i >= 0 && i < at {
			t.Errorf("%s comes before the exceptions", after)
		}
	}
	if open := exceptions[:strings.Index(exceptions, ">")]; strings.Contains(open, "quiet") || strings.HasPrefix(exceptions, "<details") {
		t.Errorf("the exceptions recede: %s", open)
	}
	return exceptions
}
