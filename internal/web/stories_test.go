package web_test

import (
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/editor/frozentest"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// placeWaiting places the Facts awaiting placing with output.
func placeWaiting(t *testing.T, store *sqlite.Store, output string) {
	t.Helper()
	if err := frozentest.PlaceWaiting(t.Context(), frozentest.Deps{Store: store, Model: fakeModel{output}, Log: slog.New(slog.DiscardHandler)}, saturday); err != nil {
		t.Fatal(err)
	}
}

// otherStories starts n Stories, each from an Item of its own whose one Fact
// involves no Entity, so that an Entity shared by the Stories already in
// the Record is in no more than half of them, and still connects them.
func otherStories(t *testing.T, store *sqlite.Store, n int) {
	t.Helper()
	for i := range n {
		it := item("other-"+strconv.Itoa(i), "Other "+strconv.Itoa(i), saturday.Add(-96*time.Hour))
		seed(t, store, saturday.AddDate(0, 0, -7), it)
		if err := store.SaveFactReading(t.Context(), editor.FactReading{SourceID: it.SourceID, EntryID: it.EntryID, Model: "fake",
			Facts: []editor.Fact{{Statement: "Something else happened.", AsAt: time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC),
				Quote: "else"}}}, saturday); err != nil {
			t.Fatal(err)
		}
		placeWaiting(t, store, `{"new_stories": [{"ref": "N1", "title": "Other `+strconv.Itoa(i)+`"}], "facts": [{"fact": "F1", "developments_in": ["N1"]}]}`)
	}
}

func TestWhatPlacingWroteIsInertOnTheStoryPages(t *testing.T) {
	store := openStore(t)
	readSpeech(t, store, reading)
	placeWaiting(t, store, `{"new_stories": [{"ref": "N1", "title": "<b hx-get=\"/evil\">Scotland</b>"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}]}`)
	app, _ := newAppWithLog(t, saturday, store)

	for _, path := range []string{"/stories", "/stories/1"} {
		res := get(t, app, path)
		if res.status != 200 || strings.Contains(res.body, "<b hx-get") {
			t.Errorf("%s: status %d, or the Story's title was not escaped:\n%s", path, res.status, res.body)
		}
		if !regexp.MustCompile(`hx-disable>&lt;b hx-get`).MatchString(res.body) {
			t.Errorf("%s: the Story's title is not inside hx-disable:\n%s", path, res.body)
		}
	}
}

func TestAStoryNotInTheRecordIsNotFound(t *testing.T) {
	app := newApp(t, saturday)

	for _, path := range []string{"/stories/7", "/stories/seven"} {
		if res := get(t, app, path); res.status != 404 {
			t.Errorf("%s: status = %d, want 404", path, res.status)
		}
	}
}

func TestAStorysPageShowsItsTimelineEachDevelopmentTracingToItsFactAndItem(t *testing.T) {
	store := openStore(t)
	readSpeech(t, store, reading)
	placeWaiting(t, store, `{"new_stories": [{"ref": "N1", "title": "Andy Burnham in Scotland"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}]}`)
	// An earlier Item, read and placed after the speech, whose Fact holds as
	// at an earlier date: the Timeline is in the order of the Facts' dates.
	earlier := item("offer", "Offer to Holyrood", saturday.Add(-72*time.Hour))
	seed(t, store, saturday.AddDate(0, 0, -7), earlier)
	if err := store.SaveFactReading(t.Context(), editor.FactReading{
		SourceID: earlier.SourceID, EntryID: earlier.EntryID, Model: "fake",
		Entities: []editor.Entity{{Name: "Andy Burnham", Kind: "person"}},
		Facts: []editor.Fact{{Statement: "Andy Burnham made an offer to Holyrood.",
			AsAt: time.Date(2026, time.October, 21, 0, 0, 0, 0, time.UTC), Quote: "offer"}},
	}, saturday); err != nil {
		t.Fatal(err)
	}
	placeWaiting(t, store, `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": ["S1"]}]}`)
	app, _ := newAppWithLog(t, saturday, store)

	list := get(t, app, "/stories")
	if list.status != 200 || !strings.Contains(list.body, "<!doctype html>") {
		t.Fatalf("Stories page: status %d, not a full page:\n%s", list.status, list.body)
	}
	m := regexp.MustCompile(`<a href="(/stories/\d+)"[^>]*>(?:<[^>]*>)*Andy Burnham in Scotland`).FindStringSubmatch(list.body)
	if m == nil {
		t.Fatalf("the Stories page does not link the Story to its page:\n%s", list.body)
	}
	res := get(t, app, m[1])

	if res.status != 200 || !strings.Contains(res.body, "<!doctype html>") {
		t.Fatalf("Story page: status %d, not a full page:\n%s", res.status, res.body)
	}
	offer := strings.Index(res.body, "Andy Burnham made an offer to Holyrood.")
	spoke := strings.Index(res.body, "Andy Burnham spoke in Motherwell.")
	if offer < 0 || spoke < 0 || offer > spoke {
		t.Errorf("the Timeline does not show both Developments in order:\n%s", res.body)
	}
	// Each Development traces to its Item's page.
	for _, want := range []string{"21 October 2026", "22 October 2026", "Offer to Holyrood", "PM speech in Scotland",
		`<a href="/items/number-10/offer">`} {
		if !strings.Contains(res.body, want) {
			t.Errorf("Story page lacks %q:\n%s", want, res.body)
		}
	}
}

func TestADevelopmentOnAStorysPageShowsTheEntitiesItsFactInvolvesInTheirRoles(t *testing.T) {
	store := openStore(t)
	// The speech's Item mentions Andy Burnham and Motherwell.
	readSpeech(t, store, reading)
	placeWaiting(t, store, `{"new_stories": [{"ref": "N1", "title": "Burnham in Motherwell"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}]}`)
	// Connecting gives Andy Burnham a role in the speech, and Motherwell none.
	connectWaiting(t, store, `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}]}], "causes": []}`)
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/stories/1")
	d := blockOf(t, section(t, res.body, "timeline"), "Andy Burnham spoke in Motherwell.")
	if !strings.Contains(d.margin, `<span class="pill"><span hx-disable>Andy Burnham</span> · <span class="pill-tag">actor</span></span>`) {
		t.Errorf("the Development's margin does not show Andy Burnham as its actor, a pill:\n%s", d.margin)
	}
	if strings.Contains(d.margin, "Motherwell</span> ·") {
		t.Errorf("the Development shows Motherwell, which its Fact only mentions, as involved:\n%s", d.margin)
	}
}

// connectWaiting connects the Facts awaiting connecting, an Item at a time,
// with outputs in turn.
func connectWaiting(t *testing.T, store *sqlite.Store, outputs ...string) {
	t.Helper()
	for _, out := range outputs {
		waiting, err := store.FactsToConnect(t.Context())
		if err != nil || len(waiting) == 0 {
			t.Fatalf("nothing awaiting connecting: %v", err)
		}
		// Only the first Item awaiting connecting is answered by out.
		if err := frozentest.ConnectWaiting(t.Context(), frozentest.Deps{Store: store, Model: fakeModel{out}, Log: slog.New(slog.DiscardHandler)}, saturday); err != nil {
			t.Fatal(err)
		}
		if after, _ := store.FactsToConnect(t.Context()); len(after) != len(waiting)-1 {
			t.Fatalf("connecting %s with %s failed its checks", waiting[0].Item.Title, out)
		}
	}
}

func TestAReciprocalActsTwoActorsAreKeptAndEachConnectsItsStoryOnTheStoryPage(t *testing.T) {
	store := openStore(t)
	// connected reads, places in a Story of its own and connects an Item
	// whose one Fact gives each of names the role actor.
	connected := func(id, title, statement string, names ...string) {
		t.Helper()
		it := item(id, title, saturday.Add(-72*time.Hour))
		seed(t, store, saturday.AddDate(0, 0, -7), it)
		var entities []editor.Entity
		var roles []string
		for _, n := range names {
			entities = append(entities, editor.Entity{Name: n, Kind: "person"})
			roles = append(roles, `{"entity": "`+n+`", "role": "actor"}`)
		}
		if err := store.SaveFactReading(t.Context(), editor.FactReading{
			SourceID: it.SourceID, EntryID: it.EntryID, Model: "fake", Entities: entities,
			Facts: []editor.Fact{{Statement: statement, AsAt: time.Date(2026, time.October, 21, 0, 0, 0, 0, time.UTC),
				Quote: "q"}},
		}, saturday); err != nil {
			t.Fatal(err)
		}
		placeWaiting(t, store, `{"new_stories": [{"ref": "N1", "title": "`+title+`"}], "facts": [{"fact": "F1", "developments_in": ["N1"]}]}`)
		connectWaiting(t, store, `{"facts": [{"fact": "F1", "roles": [`+strings.Join(roles, ", ")+`]}], "causes": []}`)
	}
	// The two met: a reciprocal act, each of them its actor.
	connected("meeting", "Burnham and Swinney meet", "Andy Burnham and John Swinney met.", "Andy Burnham", "John Swinney")
	connected("swinney", "Swinney in Glasgow", "John Swinney spoke in Glasgow.", "John Swinney")
	connected("burnham", "Burnham in Manchester", "Andy Burnham spoke in Manchester.", "Andy Burnham")
	otherStories(t, store, 1)
	app, _ := newAppWithLog(t, saturday, store)

	meeting := get(t, app, "/stories/1")
	at := strings.Index(meeting.body, `id="connected"`)
	if meeting.status != 200 || at < 0 {
		t.Fatalf("status %d, or no connected Stories:\n%s", meeting.status, meeting.body)
	}
	section := meeting.body[at:]
	for _, want := range []string{`<span hx-disable>John Swinney</span> · <span class="pill-tag">actor</span>`,
		`<span hx-disable>Andy Burnham</span> · <span class="pill-tag">actor</span>`,
		`<a href="/stories/2">`, `<a href="/stories/3">`} {
		if !strings.Contains(section, want) {
			t.Errorf("the meeting's connected Stories lack %q:\n%s", want, section)
		}
	}
}

func TestStoriesAnnouncedTogetherShowTheItemOnEachStorysPage(t *testing.T) {
	store := openStore(t)
	update := item("update", "Cost of Living update", saturday.Add(-72*time.Hour))
	seed(t, store, saturday.AddDate(0, 0, -7), update)
	day := time.Date(2026, time.October, 21, 0, 0, 0, 0, time.UTC)
	if err := store.SaveFactReading(t.Context(), editor.FactReading{
		SourceID: update.SourceID, EntryID: update.EntryID, Model: "fake",
		Facts: []editor.Fact{
			{Statement: "Carer's Allowance was reformed.", AsAt: day, Quote: "reformed"},
			{Statement: "The energy price cap was frozen.", AsAt: day, Quote: "frozen"},
		},
	}, saturday); err != nil {
		t.Fatal(err)
	}
	placeWaiting(t, store, `{"new_stories": [{"ref": "N1", "title": "Carer's Allowance reform"}, {"ref": "N2", "title": "Energy prices"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}, {"fact": "F2", "developments_in": ["N2"]}]}`)
	app, _ := newAppWithLog(t, saturday, store)

	for _, side := range [][2]string{{"/stories/1", "/stories/2"}, {"/stories/2", "/stories/1"}} {
		page := get(t, app, side[0])
		at := strings.Index(page.body, `id="connected"`)
		if page.status != 200 || at < 0 {
			t.Fatalf("%s: status %d, or no connected Stories:\n%s", side[0], page.status, page.body)
		}
		section := page.body[at:]
		for _, want := range []string{`<a href="` + side[1] + `">`,
			`Announced together in ` + noTen + `update"><cite hx-disable>Cost of Living update</cite></a></span>`} {
			if !strings.Contains(section, want) {
				t.Errorf("%s: the connected Stories lack %q:\n%s", side[0], want, section)
			}
		}
	}
}

func TestAStorysPageCitesEachItemByItsSourcesShortNameBeforeItsTitle(t *testing.T) {
	store := openStore(t)
	update := item("update", "Cost of Living update", saturday.Add(-72*time.Hour))
	seed(t, store, saturday.AddDate(0, 0, -7), update)
	day := time.Date(2026, time.October, 21, 0, 0, 0, 0, time.UTC)
	if err := store.SaveFactReading(t.Context(), editor.FactReading{
		SourceID: update.SourceID, EntryID: update.EntryID, Model: "fake",
		Facts: []editor.Fact{
			{Statement: "Carer's Allowance was reformed.", AsAt: day, Quote: "reformed"},
			{Statement: "The energy price cap was frozen.", AsAt: day, Quote: "frozen"},
		},
	}, saturday); err != nil {
		t.Fatal(err)
	}
	placeWaiting(t, store, `{"new_stories": [{"ref": "N1", "title": "Carer's Allowance reform"}, {"ref": "N2", "title": "Energy prices"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}, {"fact": "F2", "developments_in": ["N2"]}]}`)
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, "/stories/1").body

	for _, id := range []string{"timeline", "connected"} {
		if s := section(t, page, id); !strings.Contains(s, noTen+"update") {
			t.Errorf("the %s does not cite the Item by its Source's short name:\n%s", id, s)
		}
	}
}

func TestAStorysPageShowsTheStoriesConnectedToItAndWhy(t *testing.T) {
	store := openStore(t)
	// An offer to Holyrood starts one Story; the speech, a later Item, another.
	offer := item("offer", "Offer to Holyrood", saturday.Add(-72*time.Hour))
	seed(t, store, saturday.AddDate(0, 0, -7), offer)
	if err := store.SaveFactReading(t.Context(), editor.FactReading{
		SourceID: offer.SourceID, EntryID: offer.EntryID, Model: "fake",
		Entities: []editor.Entity{{Name: "Andy Burnham", Kind: "person"}},
		Facts: []editor.Fact{{Statement: "Andy Burnham made an offer to Holyrood.",
			AsAt: time.Date(2026, time.October, 21, 0, 0, 0, 0, time.UTC), Quote: "offer"}},
	}, saturday); err != nil {
		t.Fatal(err)
	}
	placeWaiting(t, store, `{"new_stories": [{"ref": "N1", "title": "Scottish devolution"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}]}`)
	// Connecting gives Andy Burnham his role in the offer.
	connectWaiting(t, store, `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}]}], "causes": []}`)
	readSpeech(t, store, reading)
	placeWaiting(t, store, `{"new_stories": [{"ref": "N1", "title": "<b hx-get=\"/evil\">Burnham in Motherwell</b>"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}]}`)
	// And in the speech, which the offer caused.
	connectWaiting(t, store, `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}, {"entity": "Motherwell", "role": "place"}]}],
		  "causes": [{"cause": "C1", "effect": "F1"}]}`)
	otherStories(t, store, 2)
	app, _ := newAppWithLog(t, saturday, store)

	devolution := get(t, app, "/stories/1")
	at := strings.Index(devolution.body, `id="connected"`)
	if devolution.status != 200 || at < 0 {
		t.Fatalf("status %d, or no connected Stories:\n%s", devolution.status, devolution.body)
	}
	section := devolution.body[at:]
	for _, want := range []string{`<a href="/stories/2">`, `hx-disable>&lt;b hx-get`,
		`<span hx-disable>Andy Burnham</span> · <span class="pill-tag">actor</span>`} {
		if !strings.Contains(section, want) {
			t.Errorf("the connected Stories lack %q:\n%s", want, section)
		}
	}
	speech := get(t, app, "/stories/2")
	if !strings.Contains(speech.body, `<a href="/stories/1">`) || !strings.Contains(speech.body, "Scottish devolution") {
		t.Errorf("the speech's Story does not show the Story connected to it:\n%s", speech.body)
	}
	// The cause traces to the Items on each side.
	at = strings.Index(speech.body, `id="causes"`)
	if at < 0 {
		t.Fatalf("the speech's Story shows no causes:\n%s", speech.body)
	}
	causes := speech.body[at:]
	offerAt, spokeAt := strings.Index(causes, "Andy Burnham made an offer to Holyrood."), strings.Index(causes, "Andy Burnham spoke in Motherwell.")
	if offerAt < 0 || spokeAt < offerAt || !strings.Contains(causes, noTen+"offer") ||
		!strings.Contains(causes, "PM speech in Scotland") {
		t.Errorf("the speech's Story does not show the offer as its cause, tracing to both Items:\n%s", causes)
	}
}

// section is the page's section headed id, up to its end.
func section(t *testing.T, page, id string) string {
	t.Helper()
	at := strings.Index(page, `id="`+id+`"`)
	if at < 0 {
		t.Fatalf("no section %q:\n%s", id, page)
	}
	rest := page[at:]
	if end := strings.Index(rest, "</section>"); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// costOfLiving reads the Cost of Living update, which mentions Rachel
// Reeves, into facts, and places them all in one new Story, Story 1.
func costOfLiving(t *testing.T, store *sqlite.Store, facts ...editor.Fact) {
	t.Helper()
	update := item("update", "Cost of Living update", saturday.Add(-72*time.Hour))
	seed(t, store, saturday.AddDate(0, 0, -7), update)
	if err := store.SaveFactReading(t.Context(), editor.FactReading{
		SourceID: update.SourceID, EntryID: update.EntryID, Model: "fake", Facts: facts,
		Entities: []editor.Entity{{Name: "Rachel Reeves", Kind: "person"}},
	}, saturday); err != nil {
		t.Fatal(err)
	}
	var placed []string
	for i := range facts {
		placed = append(placed, `{"fact": "F`+strconv.Itoa(i+1)+`", "developments_in": ["N1"]}`)
	}
	placeWaiting(t, store, `{"new_stories": [{"ref": "N1", "title": "Cost of living"}], "facts": [`+strings.Join(placed, ", ")+`]}`)
}

func TestConsecutiveDevelopmentsOfOneItemAreOneTimelineEntryWithTheDateAndItemOnce(t *testing.T) {
	store := openStore(t)
	day := time.Date(2026, time.October, 21, 0, 0, 0, 0, time.UTC)
	costOfLiving(t, store,
		editor.Fact{Statement: "Carer's Allowance was reformed.", AsAt: day, Quote: "reformed"},
		editor.Fact{Statement: "The energy price cap was frozen.", AsAt: day, Quote: "frozen"})
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, "/stories/1")
	timeline := section(t, page.body, "timeline")

	if n := strings.Count(timeline, `class="entry"`); n != 1 {
		t.Errorf("want one timeline entry, got %d:\n%s", n, timeline)
	}
	for _, once := range []string{"Cost of Living update", "21 October 2026"} {
		if n := strings.Count(timeline, once); n != 1 {
			t.Errorf("%q shows %d times on the Timeline, want once:\n%s", once, n, timeline)
		}
	}
	if !strings.Contains(timeline, `<a href="/items/number-10/update"><cite hx-disable>Cost of Living update</cite></a>`) {
		t.Errorf("the entry does not cite its Item:\n%s", timeline)
	}
	for _, s := range []string{"Carer&#39;s Allowance was reformed.", "The energy price cap was frozen."} {
		blockOf(t, timeline, s)
	}
}

func TestALaterItemBetweenTwoRunsOfOneItemSplitsThemIntoTwoEntries(t *testing.T) {
	store := openStore(t)
	october := func(d int) time.Time { return time.Date(2026, time.October, d, 0, 0, 0, 0, time.UTC) }
	costOfLiving(t, store,
		editor.Fact{Statement: "Carer's Allowance was reformed.", AsAt: october(20), Quote: "reformed"},
		editor.Fact{Statement: "The energy price cap will be frozen from the 22nd.", AsAt: october(22), Quote: "frozen"})
	// A later Item, whose Fact falls between the update's two.
	budget := item("budget", "Budget statement", saturday.Add(-24*time.Hour))
	seed(t, store, saturday.AddDate(0, 0, -7), budget)
	if err := store.SaveFactReading(t.Context(), editor.FactReading{
		SourceID: budget.SourceID, EntryID: budget.EntryID, Model: "fake",
		Entities: []editor.Entity{{Name: "Rachel Reeves", Kind: "person"}},
		Facts:    []editor.Fact{{Statement: "Rachel Reeves set out the Budget.", AsAt: october(21), Quote: "Budget"}},
	}, saturday); err != nil {
		t.Fatal(err)
	}
	placeWaiting(t, store, `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": ["S1"]}]}`)
	app, _ := newAppWithLog(t, saturday, store)

	timeline := section(t, get(t, app, "/stories/1").body, "timeline")

	heads := regexp.MustCompile(`<p class="entry-head"><time>([^<]+)</time>.*?<cite hx-disable>([^<]+)</cite>`).FindAllStringSubmatch(timeline, -1)
	var got []string
	for _, h := range heads {
		got = append(got, h[1]+": "+h[2])
	}
	want := []string{"20 October 2026: Cost of Living update", "21 October 2026: Budget statement", "22 October 2026: Cost of Living update"}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("the entries are %q, want %q:\n%s", got, want, timeline)
	}
}

func TestAStorysTitleIsABlockInAMarginedRegionWhichStartsOpenAndOffersNoFeedback(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, "/stories/1").body

	regions := regexp.MustCompile(`<div class="margined" data-margin="(open|folded)">`).FindAllStringSubmatch(page, -1)
	if len(regions) != 1 || regions[0][1] != "open" {
		t.Fatalf("want one margined region, open; got %v:\n%s", regions, page)
	}
	if !strings.Contains(page, `aria-expanded="true"`) {
		t.Errorf("the page has no fold control, expanded:\n%s", page)
	}
	blockOf(t, page, "<h1 hx-disable>Andy Burnham in Scotland</h1>")
	// Feedback lands on judgements of Spans now: a Story of Facts takes
	// none.
	if strings.Contains(page, `class="feedback"`) || strings.Contains(page, "/stories/1/feedback") {
		t.Errorf("a Story of Facts still offers Feedback:\n%s", page)
	}
	if res := send(t, app, "POST", "/stories/1/feedback", url.Values{"kind": {"unwanted"}, "level": {"story"}, "story": {"1"}}); res.status != http.StatusMethodNotAllowed && res.status != http.StatusNotFound {
		t.Errorf("Feedback on a Story was answered %d, want no route", res.status)
	}
}

func TestACauseIsABlockReadingCauseLedToEffectWithItsItemsCallAndActionsInItsMargin(t *testing.T) {
	store := openStore(t)
	connectedStories(t, store)
	app, _ := newAppWithLog(t, saturday, store)

	causes := section(t, get(t, app, "/stories/2").body, "causes")
	c := blockOf(t, causes, "led to")

	if !regexp.MustCompile(`Andy Burnham made an offer to Holyrood\.</span> led to <span hx-disable>Andy Burnham spoke in Motherwell\.`).MatchString(c.body) {
		t.Errorf("the body does not read cause led to effect:\n%s", c.body)
	}
	for _, want := range []string{`<cite hx-disable>Offer to Holyrood</cite>`, `<cite hx-disable>PM speech in Scotland</cite>`} {
		if !strings.Contains(c.margin, want) || strings.Contains(c.body, want) {
			t.Errorf("%q is not in the margin alone:\n%s%s", want, c.body, c.margin)
		}
	}
	if !regexp.MustCompile(`<a class="machine" href="/calls/\d+">connected · call \d+</a>`).MatchString(c.margin) {
		t.Errorf("the margin has no machine label \"connected · call N\":\n%s", c.margin)
	}
}

// connectedStories makes two Stories: Scottish devolution (Story 1), whose
// Development is the offer (Fact 1), and Burnham in Motherwell (Story 2),
// whose Development is the speech (Fact 2), which the offer caused. Andy
// Burnham is the actor in both. Two other Stories share nothing with them.
func connectedStories(t *testing.T, store *sqlite.Store) {
	t.Helper()
	offer := item("offer", "Offer to Holyrood", saturday.Add(-72*time.Hour))
	seed(t, store, saturday.AddDate(0, 0, -7), offer)
	if err := store.SaveFactReading(t.Context(), editor.FactReading{
		SourceID: offer.SourceID, EntryID: offer.EntryID, Model: "fake",
		Entities: []editor.Entity{{Name: "Andy Burnham", Kind: "person"}},
		Facts: []editor.Fact{{Statement: "Andy Burnham made an offer to Holyrood.",
			AsAt: time.Date(2026, time.October, 21, 0, 0, 0, 0, time.UTC), Quote: "offer"}},
	}, saturday); err != nil {
		t.Fatal(err)
	}
	placeWaiting(t, store, `{"new_stories": [{"ref": "N1", "title": "Scottish devolution"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}]}`)
	connectWaiting(t, store, `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}]}], "causes": []}`)
	readSpeech(t, store, reading)
	placeWaiting(t, store, `{"new_stories": [{"ref": "N1", "title": "Burnham in Motherwell"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}]}`)
	connectWaiting(t, store, `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}, {"entity": "Motherwell", "role": "place"}]}],
		  "causes": [{"cause": "C1", "effect": "F1"}]}`)
	otherStories(t, store, 2)
}

func TestAConnectedStoryIsItsTitleInTheBodyWithItsReasonsInTheMargin(t *testing.T) {
	store := openStore(t)
	connectedStories(t, store)
	app, _ := newAppWithLog(t, saturday, store)

	connected := section(t, get(t, app, "/stories/1").body, "connected")
	s := blockOf(t, connected, `<a href="/stories/2"><span hx-disable>Burnham in Motherwell</span></a>`)

	if !strings.Contains(s.margin, `<span class="pill"><span hx-disable>Andy Burnham</span> · <span class="pill-tag">actor</span></span>`) {
		t.Errorf("the margin does not show Andy Burnham, the actor in both, as a pill:\n%s", s.margin)
	}
	if strings.Contains(s.body, "Andy Burnham") {
		t.Errorf("the reason is in the body:\n%s", s.body)
	}
}

func TestADevelopmentShowsItsOwnDateInItsBodyOnlyWhereItDiffersFromItsEntrys(t *testing.T) {
	store := openStore(t)
	october := func(d int) time.Time { return time.Date(2026, time.October, d, 0, 0, 0, 0, time.UTC) }
	costOfLiving(t, store,
		editor.Fact{Statement: "Carer's Allowance was reformed.", AsAt: october(20), Quote: "reformed"},
		editor.Fact{Statement: "Pensions rose.", AsAt: october(20), Quote: "rose"},
		editor.Fact{Statement: "The energy price cap was frozen.", AsAt: october(21), Quote: "frozen"})
	app, _ := newAppWithLog(t, saturday, store)

	timeline := section(t, get(t, app, "/stories/1").body, "timeline")

	if same := blockOf(t, timeline, "Pensions rose."); strings.Contains(same.body, "October") {
		t.Errorf("a Development dated as its entry shows its date:\n%s", same.body)
	}
	if differs := blockOf(t, timeline, "The energy price cap was frozen."); !strings.Contains(differs.body, "<time>21 October 2026</time>") {
		t.Errorf("a Development dated otherwise does not show its date:\n%s", differs.body)
	}
}
