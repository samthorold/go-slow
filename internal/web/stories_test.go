package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// weaveSpeech seeds a speech, cut into S1 "It’s good to be here in
// Motherwell." and S2 "Scotland will get £2 billion.", and weaves it with
// output through the batch runner, as main does, its call kept.
func weaveSpeech(t *testing.T, store *sqlite.Store, output string) {
	t.Helper()
	speech := item("speech", "PM speech in Scotland", saturday.Add(-48*time.Hour))
	speech.Content = "<p>It’s good to be here in Motherwell.</p><p>Scotland will get £2 billion.</p>"
	seed(t, store, saturday.AddDate(0, 0, -7), speech)
	weave(t, store, saturday, output)
}

// weave cuts what awaits its Cut at at and weaves it with output through the
// batch runner, as main does, its call kept.
func weave(t *testing.T, store *sqlite.Store, at time.Time, output string) {
	t.Helper()
	if err := cut.New(store).CutNew(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	ed := editor.New(editor.Deps{Store: store, Weavings: store, Log: log})
	b := editor.NewBatches(editor.BatchDeps{Store: store, Batcher: &refusingBatcher{output: output},
		Acts: []editor.BatchAct{ed.Weaving(6 * time.Hour)}, Log: log})
	// Submitted, then applied once its batch has ended.
	for range 2 {
		if err := b.ReadWaiting(t.Context(), at); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAStorysPageShowsAnEarlierEditorsSpanItCitesLinkingTheStoryThatWroteIt(t *testing.T) {
	store := openStore(t)
	weaveSpeech(t, store, twoOpinions)
	// Sunday's run is shown the two Stories made (S1 to S5, the earlier
	// Editor's Span "Motherwell is where…" S2), then the ferries as S6.
	sunday := saturday.Add(24 * time.Hour)
	ferries := item("ferries", "Ferries to the islands", sunday)
	ferries.Content = "<p>Ferries to Scotland's islands will run more often.</p>"
	seed(t, store, saturday.AddDate(0, 0, -7), ferries)
	weave(t, store, sunday, `{"stories": [{"editor_spans": [
	  {"body": "The ferries follow the money to the islands.", "cites": [2, 6]}
	]}]}`)
	app, _ := newAppWithLog(t, sunday, store)

	res := storyPage(t, app, "The ferries follow the money to the islands.")

	if res.status != 200 {
		t.Fatalf("status %d:\n%s", res.status, res.body)
	}
	cited := regexp.MustCompile(`<a href="(/stories/\d+)"><span hx-disable>Motherwell is where the money lands first, as it did a decade ago when the steelworks closed.</span></a>`).
		FindStringSubmatch(res.body)
	if cited == nil {
		t.Fatalf("the page does not show the earlier Editor's Span's words linking a Story:\n%s", res.body)
	}
	if got := get(t, app, cited[1]); !strings.Contains(got.body, "<h1>Motherwell is where the money lands first") {
		t.Errorf("the Story it links is not the one that wrote it:\n%s", got.body)
	}
	if !strings.Contains(res.body, "Ferries to the islands") {
		t.Errorf("the page lacks the ferries it cites:\n%s", res.body)
	}
}

// twoOpinions is a weaving run's answer making two Stories of the speech.
const twoOpinions = `{"stories": [
  {"editor_spans": [{"body": "Motherwell is where the money lands first, as it did a decade ago when the steelworks closed.", "cites": [1]}]},
  {"editor_spans": [
    {"body": "Two billion is a down payment on devolution.", "cites": [2]},
    {"body": "The visit and the money are one move.", "cites": [1, 2]}
  ]}
]}`

func TestTheStoriesPageListsTheStoriesTheEditorMadeNewestFirstByTheirOpeningWords(t *testing.T) {
	store := openStore(t)
	weaveSpeech(t, store, twoOpinions)
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/stories")

	if res.status != 200 || !strings.Contains(res.body, "<!doctype html>") {
		t.Fatalf("status %d, not a full page:\n%s", res.status, res.body)
	}
	newer := strings.Index(res.body, "Two billion is a down payment on devolution.")
	older := strings.Index(res.body, "Motherwell is where the money lands first, as it did a…")
	if newer < 0 || older < 0 || newer > older {
		t.Errorf("the Stories are not listed newest first by their opening words:\n%s", res.body)
	}
	if strings.Contains(res.body, "when the steelworks closed") {
		t.Errorf("a Story is listed by more than its opening words:\n%s", res.body)
	}
	if !regexp.MustCompile(`<a href="/stories/\d+"`).MatchString(res.body) {
		t.Errorf("the Stories do not link their pages:\n%s", res.body)
	}
	if !strings.Contains(res.body, `<a href="/stories" aria-current="page">`) {
		t.Errorf("the navigation does not mark the Stories page current:\n%s", res.body)
	}
}

// nameNewest names the newest Story the Editor made through the batch
// runner, as main does, with the naming model answering name; every other
// Story's naming is refused.
func nameNewest(t *testing.T, store *sqlite.Store, name string) {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	ed := editor.New(editor.Deps{Store: store, Namings: store, Log: log})
	b := editor.NewBatches(editor.BatchDeps{Store: store, Batcher: &namingBatcher{name: name},
		Acts: []editor.BatchAct{ed.Naming()}, Log: log})
	for range 2 {
		if err := b.ReadWaiting(t.Context(), saturday); err != nil {
			t.Fatal(err)
		}
	}
}

// namingBatcher ends every batch at once, naming the Story opening with
// newestOpening name and answering every other naming with no name.
type namingBatcher struct {
	refusingBatcher
	name string
}

func (b *namingBatcher) Poll(_ context.Context, id string) (editor.BatchPoll, error) {
	var out []editor.BatchResult
	for _, r := range b.submitted[id] {
		output := `{"name": ""}`
		if strings.Contains(r.Request.Input, newestOpening) {
			output = fmt.Sprintf(`{"name": %q}`, b.name)
		}
		out = append(out, editor.BatchResult{ID: r.ID, Response: editor.Response{Output: json.RawMessage(output), Model: "fake"}})
	}
	return editor.BatchPoll{Ended: true, Results: out}, nil
}

// newestOpening opens the newer Story of twoOpinions.
const newestOpening = "Two billion is a down payment on devolution."

func TestAStoryWithANameIsListedAndHeadedByItAndOneWithoutByItsOpeningWords(t *testing.T) {
	store := openStore(t)
	weaveSpeech(t, store, twoOpinions)
	nameNewest(t, store, "Devolution's price")
	app, _ := newAppWithLog(t, saturday, store)

	list := get(t, app, "/stories").body

	if !regexp.MustCompile(`<a href="/stories/\d+">Devolution&#39;s price</a>`).MatchString(list) {
		t.Errorf("the named Story is not listed by its Name:\n%s", list)
	}
	if strings.Contains(list, newestOpening) {
		t.Errorf("the named Story is listed by its opening words too:\n%s", list)
	}
	if !strings.Contains(list, "Motherwell is where the money lands first, as it did a…") {
		t.Errorf("the Story with no Name is not listed by its opening words:\n%s", list)
	}
	res := storyPage(t, app, "Devolution&#39;s price")
	if !strings.Contains(res.body, "<h1>Devolution&#39;s price</h1>") ||
		!strings.Contains(res.body, "<title>Devolution&#39;s price") {
		t.Errorf("the Story's Page is not headed by its Name:\n%s", res.body)
	}
	if !strings.Contains(res.body, newestOpening) {
		t.Errorf("the Story's Page lacks its Editor's Spans:\n%s", res.body)
	}
	call := regexp.MustCompile(`<a class="machine" href="/calls/(\d+)">named · call \d+</a>`).FindStringSubmatch(res.body)
	if call == nil {
		t.Fatalf("the Story's Page does not link the call that named it:\n%s", res.body)
	}
	if c := get(t, app, "/calls/"+call[1]); c.status != 200 || !strings.Contains(c.body, "A naming call") {
		t.Errorf("the call it links is not the naming call (status %d):\n%s", c.status, c.body)
	}
	if !strings.Contains(res.body, "naming v1") {
		t.Errorf("the Story's Page does not name its Name's Judge:\n%s", res.body)
	}
}

func TestASpansPageAndAnEditorsSpansPageKnowAStoryByItsName(t *testing.T) {
	store := openStore(t)
	weaveSpeech(t, store, twoOpinions)
	nameNewest(t, store, "Devolution's price")
	app, _ := newAppWithLog(t, saturday, store)

	// S2, "Scotland will get £2 billion.", which only the named Story cites.
	span := get(t, app, "/items/number-10/speech/spans/47-77").body
	stories := span[strings.Index(span, `<h2 id="stories">`):]
	if n := strings.Count(stories, ">Devolution&#39;s price</a>"); n != 3 {
		t.Errorf("the Span's Page names the Story %d times, want 3 (as holding it, and in each citing Span):\n%s", n, stories)
	}
	if regexp.MustCompile(`<a href="/stories/\d+">` + regexp.QuoteMeta(newestOpening)).MatchString(stories) {
		t.Errorf("the Span's Page lists the named Story by its opening words:\n%s", stories)
	}
	m := regexp.MustCompile(`<a href="(/stories/\d+/spans/2)">`).FindStringSubmatch(stories)
	if m == nil {
		t.Fatalf("the Span's Page does not link the Editor's Span citing it:\n%s", stories)
	}
	es := get(t, app, m[1]).body
	if !regexp.MustCompile(`In <a href="/stories/\d+">Devolution&#39;s price</a>`).MatchString(es) {
		t.Errorf("the Editor's Span's Page does not know its Story by its Name:\n%s", es)
	}
}

func TestAFailedNamingLinksItsStory(t *testing.T) {
	store := openStore(t)
	weaveSpeech(t, store, twoOpinions)
	nameNewest(t, store, "Devolution's price")
	// The older Story's naming is refused on every attempt.
	for range editor.MaxAttempts {
		nameNewest(t, store, "Devolution's price")
	}
	app, _ := newAppWithLog(t, saturday, store)

	section := leadingExceptions(t, get(t, app, "/items").body)

	for _, want := range []string{
		"naming", "Motherwell is where the money lands first, as it did a…", "the name is empty",
		"<code>docker compose run --rm app retry naming</code>",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("failed work lacks %q:\n%s", want, section)
		}
	}
	if !regexp.MustCompile(`<a href="/stories/\d+" hx-disable>Motherwell`).MatchString(section) {
		t.Errorf("the failed naming does not link its Story:\n%s", section)
	}
}

func TestWithNoStoriesTheStoriesPageSaysSo(t *testing.T) {
	app := newApp(t, saturday)

	res := get(t, app, "/stories")

	if res.status != 200 || !strings.Contains(res.body, "The Editor has made no Stories yet.") {
		t.Errorf("status %d:\n%s", res.status, res.body)
	}
}

// storyPage follows the Stories page's link to the Story it lists by its
// opening words.
func storyPage(t *testing.T, app http.Handler, opening string) response {
	t.Helper()
	list := get(t, app, "/stories").body
	m := regexp.MustCompile(`<a href="(/stories/\d+)">` + regexp.QuoteMeta(opening)).FindStringSubmatch(list)
	if m == nil {
		t.Fatalf("the Stories page does not link %q to its page:\n%s", opening, list)
	}
	return get(t, app, m[1])
}

func TestAStorysPageShowsItsEditorsSpansInOrderWithWhatEachCitesAndHowItWasMade(t *testing.T) {
	store := openStore(t)
	weaveSpeech(t, store, twoOpinions)
	app, _ := newAppWithLog(t, saturday, store)

	res := storyPage(t, app, "Two billion is a down payment on devolution.")

	if res.status != 200 || !strings.Contains(res.body, "<!doctype html>") {
		t.Fatalf("status %d, not a full page:\n%s", res.status, res.body)
	}
	first := strings.Index(res.body, "Two billion is a down payment on devolution.")
	second := strings.Index(res.body, "The visit and the money are one move.")
	if first < 0 || second < first {
		t.Errorf("the Editor's Spans are not shown in the order written:\n%s", res.body)
	}
	if strings.Contains(res.body, "steelworks") {
		t.Errorf("the page shows another Story's Editor's Spans:\n%s", res.body)
	}
	blocks := res.body[first:]
	for _, want := range []string{
		// What each cites: the Span's words linking its page, with its Item.
		`<a href="/items/number-10/speech/spans/`, "Scotland will get £2 billion.",
		"It’s good to be here in Motherwell.", "PM speech in Scotland",
		// Whose words: the Editor's, always.
		"the Editor",
	} {
		if !strings.Contains(blocks, html.EscapeString(want)) && !strings.Contains(blocks, want) {
			t.Errorf("the Story's Page lacks %q:\n%s", want, blocks)
		}
	}
	// How the Judgement was made: its call, and its Judge.
	call := regexp.MustCompile(`<a class="machine" href="/calls/(\d+)">woven · call \d+</a>`).FindStringSubmatch(res.body)
	if call == nil {
		t.Fatalf("the Story's Page does not link the call that made it:\n%s", res.body)
	}
	if c := get(t, app, "/calls/"+call[1]); c.status != 200 || !strings.Contains(c.body, "A weaving call") {
		t.Errorf("the call it links is not the weaving call (status %d):\n%s", c.status, c.body)
	}
	if !strings.Contains(res.body, fmt.Sprintf("weaving v%d", editor.WeavingVersion)) {
		t.Errorf("the Story's Page does not name its Judge:\n%s", res.body)
	}
}

func TestAStoryThatIsNotInTheRecordIsNotFound(t *testing.T) {
	app := newApp(t, saturday)

	for _, path := range []string{"/stories/99", "/stories/x"} {
		if res := get(t, app, path); res.status != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", path, res.status)
		}
	}
}

func TestOpeningTheStoriesPagesOrWhatTheyLinkChangesNothing(t *testing.T) {
	store := openStore(t)
	weaveSpeech(t, store, twoOpinions)
	app, logs := newAppWithLog(t, saturday, store)
	// state is what the Record says of weaving: the Stories made, what
	// awaits the next run, and the calls kept.
	state := func() string {
		made, err := store.StoriesMade(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		waiting, err := store.StoriesToWeave(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		calls, err := store.CallsOn(t.Context(), "weaving", "2026-10-24")
		if err != nil {
			t.Fatal(err)
		}
		held, err := store.HoldingSpan(t.Context(), editor.Address{SourceID: "number-10", EntryID: "speech", Span: cut.Span{Start: 47, End: 77}})
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("%+v %+v %d %+v", made, waiting, len(calls), held)
	}
	before := state()

	story := storyPage(t, app, "Two billion is a down payment on devolution.").body
	m := regexp.MustCompile(`<a href="(/stories/\d+/spans/1)">`).FindStringSubmatch(story)
	if m == nil {
		t.Fatalf("the Story's Page does not link its Editor's Span:\n%s", story)
	}
	pages := map[string]string{}
	open := func() {
		for _, path := range []string{"/stories", "/items/number-10/speech", "/items/number-10/speech/spans/47-77", m[1]} {
			pages[path] = get(t, app, path).body
		}
		pages["cut"] = cutStoryOf(t, app, "/items/number-10/speech").body
	}
	open()
	first := fmt.Sprint(pages)

	if after := state(); after != before {
		t.Errorf("opening the pages changed the Record:\nbefore %s\nafter  %s", before, after)
	}
	open()
	if fmt.Sprint(pages) != first || storyPage(t, app, "Two billion is a down payment on devolution.").body != story {
		t.Errorf("the pages changed on being opened again")
	}
	if strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Errorf("opening the pages logged an error:\n%s", logs)
	}
}
