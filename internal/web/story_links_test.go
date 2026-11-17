package web_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// cutStoryOf follows an Item's Page's link to the Story its Cut made.
func cutStoryOf(t *testing.T, app http.Handler, itemPage string) response {
	t.Helper()
	page := get(t, app, itemPage).body
	m := regexp.MustCompile(`<a href="(/stories/\d+)">[^<]*Story[^<]*</a>`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("the Item's Page does not link the Story its Cut made:\n%s", page)
	}
	return get(t, app, m[1])
}

// weaveVisitAndBudget weaves a speech published Thursday (S1 "It’s good to
// be here in Motherwell.", S2 "Scotland will get £2 billion.") and a budget
// statement published Wednesday (S3 "The Budget sets aside £2 billion for
// Scotland."), with one Story whose Editor's Spans cite S2, then S3, then
// S1 again.
func weaveVisitAndBudget(t *testing.T, store *sqlite.Store) {
	t.Helper()
	speech := item("speech", "PM speech in Scotland", saturday.Add(-48*time.Hour))
	speech.Content = "<p>It’s good to be here in Motherwell.</p><p>Scotland will get £2 billion.</p>"
	budget := item("budget", "Budget statement", saturday.Add(-72*time.Hour))
	budget.Content = "<p>The Budget sets aside £2 billion for Scotland.</p>"
	// Cut in this order: Items are cut in the order collected.
	speech.CollectedAt = saturday.Add(-2 * time.Minute)
	budget.CollectedAt = saturday.Add(-time.Minute)
	seed(t, store, saturday.AddDate(0, 0, -7), speech, budget)
	weave(t, store, saturday, `{"stories": [{"editor_spans": [
	  {"body": "The money was announced twice.", "cites": [2, 3]},
	  {"body": "The second time, in Motherwell.", "cites": [1, 2]}
	]}]}`)
}

func TestAStorysTimelineOrdersTheSpansItHoldsByPublication(t *testing.T) {
	store := openStore(t)
	weaveVisitAndBudget(t, store)
	app, _ := newAppWithLog(t, saturday, store)

	res := storyPage(t, app, "The money was announced twice.")

	at := strings.Index(res.body, `<h2 id="timeline">Timeline</h2>`)
	if at < 0 {
		t.Fatalf("the Story's Page has no Timeline:\n%s", res.body)
	}
	timeline := res.body[at:]
	budget := strings.Index(timeline, "The Budget sets aside £2 billion for Scotland.")
	visit := strings.Index(timeline, "It’s good to be here in Motherwell.")
	money := strings.Index(timeline, "Scotland will get £2 billion.")
	if budget < 0 || visit < budget || money < visit {
		t.Errorf("the Timeline does not order its Spans by publication, then as their Item has them:\n%s", timeline)
	}
	if strings.Count(timeline, "Scotland will get £2 billion.") != 1 {
		t.Errorf("the Timeline shows a Span cited twice more than once:\n%s", timeline)
	}
	for _, want := range []string{"Wed 21 Oct 2026, 07:30", "Thu 22 Oct 2026, 07:30", `<a href="/items/number-10/budget/spans/`} {
		if !strings.Contains(timeline, want) {
			t.Errorf("the Timeline lacks %q:\n%s", want, timeline)
		}
	}
}

// weaveOnSunday weaves, after Saturday's twoOpinions, a Sunday Story citing
// the ferries (S6, published Sunday), the earlier Editor's Span "Motherwell
// is where…" (S2, written Saturday) and the speech's "Scotland will get £2
// billion." (S3, published Thursday), in that order.
func weaveOnSunday(t *testing.T, store *sqlite.Store) time.Time {
	t.Helper()
	weaveSpeech(t, store, twoOpinions)
	sunday := saturday.Add(24 * time.Hour)
	ferries := item("ferries", "Ferries to the islands", sunday)
	ferries.Content = "<p>Ferries to Scotland's islands will run more often.</p>"
	seed(t, store, saturday.AddDate(0, 0, -7), ferries)
	weave(t, store, sunday, `{"stories": [{"editor_spans": [
	  {"body": "The ferries follow the money to the islands.", "cites": [6, 2, 3]}
	]}]}`)
	return sunday
}

func TestAStorysTimelineHoldsAnEarlierEditorsSpanAtItsJudgementsDate(t *testing.T) {
	store := openStore(t)
	sunday := weaveOnSunday(t, store)
	app, _ := newAppWithLog(t, sunday, store)

	res := storyPage(t, app, "The ferries follow the money to the islands.")

	at := strings.Index(res.body, `<h2 id="timeline">Timeline</h2>`)
	if at < 0 {
		t.Fatalf("the Story's Page has no Timeline:\n%s", res.body)
	}
	timeline := res.body[at:]
	speech := strings.Index(timeline, "Scotland will get £2 billion.")
	earlier := strings.Index(timeline, "Motherwell is where the money lands first")
	ferries := strings.Index(timeline, "Ferries to Scotland")
	if speech < 0 || earlier < speech || ferries < earlier {
		t.Errorf("the Timeline does not place the earlier Editor's Span at its Judgement's date:\n%s", timeline)
	}
	row := timeline[earlier:ferries]
	for _, want := range []string{"Sat 24 Oct 2026, 07:30", "spoken by the Editor"} {
		if !strings.Contains(row, want) {
			t.Errorf("the earlier Editor's Span's row lacks %q:\n%s", want, row)
		}
	}
	if !regexp.MustCompile(`<a href="/stories/\d+/spans/1"><span hx-disable>Motherwell is where`).MatchString(timeline) {
		t.Errorf("the earlier Editor's Span does not link its Page:\n%s", timeline)
	}
}

func TestAnEditorsSpansPageListsTheEditorsSpansCitingIt(t *testing.T) {
	store := openStore(t)
	sunday := weaveOnSunday(t, store)
	app, _ := newAppWithLog(t, sunday, store)
	timeline := storyPage(t, app, "The ferries follow the money to the islands.").body
	m := regexp.MustCompile(`<a href="(/stories/\d+/spans/1)"><span hx-disable>Motherwell is where`).FindStringSubmatch(timeline)
	if m == nil {
		t.Fatalf("the Timeline does not link the earlier Editor's Span's Page:\n%s", timeline)
	}
	citing := regexp.MustCompile(`<a href="((/stories/\d+)/spans/1)">spoken by the Editor</a>`).FindStringSubmatch(timeline)
	if citing == nil {
		t.Fatalf("the Story's Page does not link its Editor's Span:\n%s", timeline)
	}

	res := get(t, app, m[1])

	at := strings.Index(res.body, `<h2 id="cited-by">`)
	if res.status != 200 || at < 0 {
		t.Fatalf("status %d, the Editor's Span's Page does not show what cites it:\n%s", res.status, res.body)
	}
	cites := res.body[at:]
	for _, want := range []string{
		`<a href="` + citing[1] + `">The ferries follow the money to the islands.</a>`,
		`<a href="` + citing[2] + `">`, "spoken by the Editor", "Sun 25 Oct 2026, 07:30",
	} {
		if !strings.Contains(cites, want) {
			t.Errorf("what cites the Editor's Span lacks %q:\n%s", want, cites)
		}
	}
	if !strings.Contains(get(t, app, citing[1]).body, `<h2 id="cited-by">`) {
		t.Errorf("an Editor's Span nothing cites has no section saying so")
	}
	if strings.Contains(get(t, app, citing[1]).body, `href="/items///`) {
		t.Errorf("an Editor's Span's Page links an earlier Editor's Span it cites as an Item's")
	}
}

func TestOpeningATimelineOrWhatCitesAnEditorsSpanChangesNothing(t *testing.T) {
	store := openStore(t)
	sunday := weaveOnSunday(t, store)
	app, _ := newAppWithLog(t, sunday, store)
	state := func() string {
		made, err := store.StoriesMade(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		citing, err := store.CitingEditorSpan(t.Context(), editor.EditorSpanAt{Story: made[len(made)-1].ID, N: 1})
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("%+v %+v", made, citing)
	}
	before := state()

	story := storyPage(t, app, "The ferries follow the money to the islands.").body
	m := regexp.MustCompile(`<a href="(/stories/\d+/spans/1)"><span hx-disable>Motherwell is where`).FindStringSubmatch(story)
	if m == nil {
		t.Fatalf("the Timeline does not link the earlier Editor's Span's Page:\n%s", story)
	}
	span := get(t, app, m[1]).body

	if after := state(); after != before {
		t.Errorf("opening the pages changed the Record:\nbefore %s\nafter  %s", before, after)
	}
	if storyPage(t, app, "The ferries follow the money to the islands.").body != story || get(t, app, m[1]).body != span {
		t.Errorf("the pages changed on being opened again")
	}
}

func TestASpansPageListsTheStoriesHoldingItAndTheEditorsSpansCitingIt(t *testing.T) {
	store := openStore(t)
	weaveSpeech(t, store, twoOpinions)
	app, _ := newAppWithLog(t, saturday, store)
	cutStory := regexp.MustCompile(`<a href="(/stories/\d+)">The Story its Cut made`).FindStringSubmatch(
		get(t, app, "/items/number-10/speech").body)
	if cutStory == nil {
		t.Fatal("the Item's Page does not link its Cut's Story")
	}

	// S2, "Scotland will get £2 billion.", which only the second Story cites.
	res := get(t, app, "/items/number-10/speech/spans/47-77")

	at := strings.Index(res.body, `<h2 id="stories">`)
	if res.status != 200 || at < 0 {
		t.Fatalf("status %d, the Span's Page lists no Stories:\n%s", res.status, res.body)
	}
	stories := res.body[at:]
	for _, want := range []string{
		`<a href="` + cutStory[1] + `">`,
		`">Two billion is a down payment on devolution.</a>`,
	} {
		if !strings.Contains(stories, want) {
			t.Errorf("the Span's Page does not list the Story %q:\n%s", want, stories)
		}
	}
	if strings.Contains(stories, "steelworks") || strings.Contains(stories, "Motherwell is where") {
		t.Errorf("the Span's Page lists a Story that does not hold it:\n%s", stories)
	}
	for _, n := range []string{"1", "2"} {
		if !regexp.MustCompile(`<a href="/stories/\d+/spans/` + n + `">`).MatchString(stories) {
			t.Errorf("the Span's Page does not link the Editor's Span %s citing it:\n%s", n, stories)
		}
	}
	if strings.Count(stories, "spoken by the Editor") != 2 {
		t.Errorf("each Editor's Span citing the Span does not say it is the Editor's:\n%s", stories)
	}
}

func TestAnEditorsSpansPageShowsItAsTheEditorsWithItsStoryAndWhatItCites(t *testing.T) {
	store := openStore(t)
	weaveSpeech(t, store, twoOpinions)
	app, _ := newAppWithLog(t, saturday, store)
	span := get(t, app, "/items/number-10/speech/spans/47-77").body
	m := regexp.MustCompile(`<a href="((/stories/\d+)/spans/2)">`).FindStringSubmatch(span)
	if m == nil {
		t.Fatalf("the Span's Page does not link the Editor's Span citing it:\n%s", span)
	}

	res := get(t, app, m[1])

	if res.status != 200 || !strings.Contains(res.body, "<!doctype html>") {
		t.Fatalf("status %d, not a full page:\n%s", res.status, res.body)
	}
	for _, want := range []string{
		"The visit and the money are one move.", "spoken by the Editor",
		`<a href="` + m[2] + `">Two billion is a down payment on devolution.</a>`,
		`<a href="/items/number-10/speech/spans/3-40">`, `<a href="/items/number-10/speech/spans/47-77">`,
		"Sat 24 Oct 2026, 07:30",
	} {
		if !strings.Contains(res.body, want) {
			t.Errorf("the Editor's Span's Page lacks %q:\n%s", want, res.body)
		}
	}

	if !strings.Contains(get(t, app, m[2]).body, `<a href="`+m[1]+`">`) {
		t.Errorf("the Story's Page does not link its Editor's Span's Page %s", m[1])
	}

	cutStory := regexp.MustCompile(`<a href="(/stories/\d+)">The Story its Cut made`).FindStringSubmatch(
		get(t, app, "/items/number-10/speech").body)
	for _, path := range []string{m[2] + "/spans/3", m[2] + "/spans/0", m[2] + "/spans/x", cutStory[1] + "/spans/1", "/stories/99/spans/1"} {
		if res := get(t, app, path); res.status != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", path, res.status)
		}
	}
}

func TestEveryStoryListedByAnEditorsSpansWordsSaysTheyAreTheEditors(t *testing.T) {
	store := openStore(t)
	weaveSpeech(t, store, twoOpinions)
	app, _ := newAppWithLog(t, saturday, store)

	for _, path := range []string{"/stories", "/items/number-10/speech/spans/3-40"} {
		body := get(t, app, path).body
		for _, l := range regexp.MustCompile(`<li><a href="/stories/\d+">[^<]*</a>[^\n]*`).FindAllString(body, -1) {
			if !strings.Contains(l, "The Story its Cut made") && !strings.Contains(l, "by the Editor") {
				t.Errorf("%s: a Story listed by the Editor's words does not say they are the Editor's: %s", path, l)
			}
		}
	}
}

func TestAnItemsPageLinksTheStoryItsCutMadeWhichShowsItsSpans(t *testing.T) {
	store := openStore(t)
	weaveSpeech(t, store, twoOpinions)
	app, _ := newAppWithLog(t, saturday, store)

	res := cutStoryOf(t, app, "/items/number-10/speech")

	if res.status != 200 || !strings.Contains(res.body, "<!doctype html>") {
		t.Fatalf("status %d, not a full page:\n%s", res.status, res.body)
	}
	first := strings.Index(res.body, "It’s good to be here in Motherwell.")
	second := strings.Index(res.body, "Scotland will get £2 billion.")
	if first < 0 || second < first {
		t.Errorf("the Cut's Story does not show its Spans in order:\n%s", res.body)
	}
	if !strings.Contains(res.body, `<a href="/items/number-10/speech/spans/`) || !strings.Contains(res.body, "PM speech in Scotland") {
		t.Errorf("the Cut's Story does not link its Spans' pages and its Item:\n%s", res.body)
	}
	if strings.Contains(res.body, "spoken by the Editor") || strings.Contains(res.body, "Two billion is a down payment") {
		t.Errorf("the Cut's Story shows the Editor's words as its own:\n%s", res.body)
	}

	list := get(t, app, "/stories").body
	if n := len(regexp.MustCompile(`<a href="/stories/\d+">`).FindAllString(list, -1)); n != 2 {
		t.Errorf("the Stories Page lists %d Stories, want the Editor's 2 alone:\n%s", n, list)
	}
}
