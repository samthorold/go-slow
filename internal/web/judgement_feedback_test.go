package web_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// giveForm is the form of the Feedback button labelled label in html, as
// the Owner's browser would post it: its path and its fields.
func giveForm(t *testing.T, html, label string) (string, url.Values) {
	t.Helper()
	forms := regexp.MustCompile(`(?s)<form class="feedback" hx-post="([^"]+)"[^>]*>(.*?)</form>`).FindAllStringSubmatch(html, -1)
	for _, f := range forms {
		if !strings.Contains(f[2], `aria-label="`+label+`"`) {
			continue
		}
		v := url.Values{}
		for _, in := range regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`).FindAllStringSubmatch(f[2], -1) {
			v.Add(in[1], in[2])
		}
		return f[1], v
	}
	t.Fatalf("no Feedback button %q in:\n%s", label, html)
	return "", nil
}

// undoOf is the path the undo of the piece of Feedback of kind on what
// puts to, as the page's list of its Feedback offers it.
func undoOf(t *testing.T, page, kind, what string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<li>\s*<span class="kind kind-[a-z-]+">` + regexp.QuoteMeta(kind) + `</span>\s*(?:<a [^>]*>)?<span hx-disable>` +
		regexp.QuoteMeta(what) + `</span>.*?hx-put="([^"]+)"`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("the page does not list %s on %q, to undo:\n%s", kind, what, page)
	}
	return m[1]
}

func TestWrongOnARefersToFromItsItemsPageTakesItOutOfEveryViewAndUndoingBringsItBack(t *testing.T) {
	store := openStore(t)
	readRelease(t, store, releaseReading)
	app, _ := newAppWithLog(t, saturday, store)
	page := "/items/" + number10.ID + "/release"
	label := "Wrong: “the city” refers to “Derby”"

	path, form := giveForm(t, spanBlock(t, get(t, app, page).body, 2).margin, label)
	res := send(t, app, http.MethodPost, path, form)

	if res.status != http.StatusSeeOther || res.header.Get("Location") != page {
		t.Fatalf("status %d to %q, want 303 back to the Item's page", res.status, res.header.Get("Location"))
	}
	body := get(t, app, page).body
	quote := spanBlock(t, body, 2)
	if strings.Contains(quote.body, "<ins") || strings.Contains(quote.margin, "Derby</span> · ") {
		t.Errorf("S2 still reads “the city” as Derby, or mentions Derby through it:\n%s\n%s", quote.body, quote.margin)
	}
	if span := get(t, app, spanPath(t, app, "release", 2)).body; strings.Contains(span, `<ins title="the city">`) {
		t.Errorf("S2's own page still reads “the city” as Derby:\n%s", span)
	}

	res = send(t, app, http.MethodPut, undoOf(t, body, "wrong", "“the city” refers to “Derby”"), nil)

	if res.status != http.StatusSeeOther || res.header.Get("Location") != page {
		t.Fatalf("undo: status %d to %q, want 303 back to the Item's page", res.status, res.header.Get("Location"))
	}
	if again := spanBlock(t, get(t, app, page).body, 2); !strings.Contains(again.body, `<ins title="the city">Derby</ins>`) {
		t.Errorf("undoing did not bring the reference back:\n%s", again.body)
	}
}

func TestABriefingHidesAStatementGivenWrongAndOneCitingASpanToldUnwantedAndSaysSoWhenAllAreTakenOut(t *testing.T) {
	for _, c := range []struct{ name, label, kind, what string }{
		{"wrong", "Wrong: this statement", "wrong", "Derby will host the headquarters, it said."},
		{"unwanted", "Unwanted: Derby will host the headquarters.", "unwanted", "“Derby will host the headquarters.”"},
	} {
		t.Run(c.name, func(t *testing.T) {
			store := relatedRecord(t)
			tell(t, store, saturday, teller{text: "Derby will host the headquarters, it said.", tell: "Derby will host"})
			app, _ := newAppWithLog(t, saturday, store)

			path, form := giveForm(t, get(t, app, "/").body, c.label)
			res := send(t, app, http.MethodPost, path, form)

			if res.status != http.StatusSeeOther || res.header.Get("Location") != "/" {
				t.Fatalf("status %d to %q, want 303 back to today's Briefing", res.status, res.header.Get("Location"))
			}
			page := get(t, app, "/").body
			if strings.Contains(page, `<p hx-disable>Derby will host the headquarters, it said.</p>`) {
				t.Errorf("the statement is still told:\n%s", page)
			}
			if !strings.Contains(page, "Your Feedback took everything this Briefing told out of it") {
				t.Errorf("the Briefing does not say Feedback took everything out:\n%s", page)
			}

			send(t, app, http.MethodPut, undoOf(t, page, c.kind, c.what), nil)

			if page := get(t, app, "/").body; !strings.Contains(page, `<p hx-disable>Derby will host the headquarters, it said.</p>`) {
				t.Errorf("undoing did not bring the statement back:\n%s", page)
			}
		})
	}
}

func TestNewToMeOnAStatementShowsGivenAtRestInItsMarginWithItsUndoAndTakesNothingOut(t *testing.T) {
	store := relatedRecord(t)
	tell(t, store, saturday, teller{text: "Derby will host the headquarters, it said.", tell: "Derby will host"})
	app, _ := newAppWithLog(t, saturday, store)
	path, form := giveForm(t, get(t, app, "/").body, "New to me: this statement")

	if res := send(t, app, http.MethodPost, path, form); res.status != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", res.status)
	}

	page := get(t, app, "/").body
	statement := blockOf(t, page, "Derby will host the headquarters, it said.")
	actions := regexp.MustCompile(`(?s)<div class="actions" role="group" aria-label="Feedback">(.*?)</div>`).FindStringSubmatch(statement.margin)
	if actions == nil {
		t.Fatalf("the statement's block has no Feedback:\n%s", statement.margin)
	}
	given := regexp.MustCompile(`<span class="feedback given given-new-to-me" title="New to me: this statement ✓, given"><span aria-hidden="true">✦</span>` +
		`<span class="visually-hidden">New to me: this statement ✓, given</span>\s*<form class="feedback" hx-put="/briefings/2026-10-24/feedback/\d+/undone"[^>]*>` +
		`<button aria-label="Undo: New to me: this statement"`)
	if !given.MatchString(actions[1]) {
		t.Errorf("new to me does not show given at rest at the statement's block, its label read out, with its undo:\n%s", statement.margin)
	}
	if strings.Contains(actions[1], `aria-label="New to me: this statement"`) || !strings.Contains(actions[1], `aria-label="Wrong: this statement"`) {
		t.Errorf("the statement's actions do not offer just what is not given:\n%s", statement.margin)
	}
}

func TestWrongOnARelationOrAGatheredStoryFromASpansPageTakesItOffThePage(t *testing.T) {
	store := relatedRecord(t)
	tell(t, store, saturday, teller{text: "Andy Burnham called it fantastic news.", tell: "This is fantastic news",
		story: []string{"Derby will host the headquarters."}})
	relate(t, store, `{"relations": []}`)
	app, _ := newAppWithLog(t, saturday, store)
	page := spanPath(t, app, "release", 2)

	for _, c := range []struct{ label, gone string }{
		{"Wrong: this Story", `id="story"`},
		{"Wrong: followed by “Derby will host the headquarters.”", "followed by"},
	} {
		path, form := giveForm(t, get(t, app, page).body, c.label)
		res := send(t, app, http.MethodPost, path, form)

		if res.status != http.StatusSeeOther || res.header.Get("Location") != page {
			t.Fatalf("%s: status %d to %q, want 303 back to the Span's page", c.label, res.status, res.header.Get("Location"))
		}
		if body := get(t, app, page).body; strings.Contains(body, c.gone) {
			t.Errorf("%s: the page still shows what was given wrong:\n%s", c.label, body)
		}
	}
	body := get(t, app, page).body
	undoOf(t, body, "wrong", "the Story of ““This is fantastic news for the city.”")
	undoOf(t, body, "wrong", "“Derby will host the headquarters.” follows ““This is fantastic news for the city.”")
}

func TestMissedOnASpanIsGivenFromItsPageAndShowsGivenThereWithItsUndo(t *testing.T) {
	store := openStore(t)
	readRelease(t, store, releaseReading)
	app, _ := newAppWithLog(t, saturday, store)
	page := spanPath(t, app, "release", 4)

	path, form := giveForm(t, get(t, app, page).body, "Missed")
	res := send(t, app, http.MethodPost, path, form)

	if res.status != http.StatusSeeOther || res.header.Get("Location") != page {
		t.Fatalf("status %d to %q, want 303 back to the Span's page", res.status, res.header.Get("Location"))
	}
	body := get(t, app, page).body
	if !strings.Contains(body, "Missed ✓, given") {
		t.Errorf("missed does not show given:\n%s", body)
	}
	res = send(t, app, http.MethodPut, undoOf(t, body, "missed", "“He visited the site on Monday.”"), nil)
	if res.status != http.StatusSeeOther || res.header.Get("Location") != page {
		t.Fatalf("undo: status %d to %q, want 303 back to the Span's page", res.status, res.header.Get("Location"))
	}
	if body := get(t, app, page).body; strings.Contains(body, "Missed ✓, given") {
		t.Errorf("missed still shows given once undone:\n%s", body)
	}
}

func TestWrongOnANamesEntityMatchOrASpeakerTakesItOutOfTheItemsPage(t *testing.T) {
	store := readToRelate(t)
	readRelease := "/items/" + number10.ID + "/release"
	hosting := "/items/" + number10.ID + "/hosting"
	app, _ := newAppWithLog(t, saturday, store)

	for _, c := range []struct {
		page        string
		n           int
		label, gone string
	}{
		{hosting, 1, "Wrong: “Derby” is Derby", "https://www.wikidata.org/wiki/Q43475"},
		{readRelease, 3, "Wrong: spoken by “Andy Burnham”", "spoken by <span hx-disable>Andy Burnham</span>"},
	} {
		path, form := giveForm(t, spanBlock(t, get(t, app, c.page).body, c.n).margin, c.label)
		if res := send(t, app, http.MethodPost, path, form); res.status != http.StatusSeeOther {
			t.Fatalf("%s: status %d, want 303", c.label, res.status)
		}
		if body := get(t, app, c.page).body; strings.Contains(spanBlock(t, body, c.n).margin, c.gone) {
			t.Errorf("%s: the page still shows what was given wrong:\n%s", c.label, body)
		}
	}
	if body := get(t, app, hosting).body; !strings.Contains(body, "“Derby” is Derby") {
		t.Errorf("the Item's page does not list the wrong on the name's match, to undo:\n%s", body)
	}
}

func TestTheFeedbackPageListsEveryPieceNewestFirstWithTheCallsBehindAndUndonePiecesMarked(t *testing.T) {
	store := openStore(t)
	readRelease(t, store, releaseReading)
	app, _ := newAppWithLog(t, saturday, store)
	page := "/items/" + number10.ID + "/release"
	for _, label := range []string{"Wrong: “the city” refers to “Derby”", "Missed"} {
		path, form := giveForm(t, spanBlock(t, get(t, app, page).body, 2).margin, label)
		send(t, app, http.MethodPost, path, form)
	}

	body := get(t, app, "/feedback").body
	m := regexp.MustCompile(`<form class="feedback" hx-put="(/feedback/\d+/undone)"`).FindAllStringSubmatch(body, -1)
	if len(m) != 2 {
		t.Fatalf("the Feedback page does not list two pieces, each to undo:\n%s", body)
	}
	res := send(t, app, http.MethodPut, m[0][1], nil)

	if res.status != http.StatusSeeOther || res.header.Get("Location") != "/feedback" {
		t.Fatalf("undo: status %d to %q, want 303 back to the Feedback page", res.status, res.header.Get("Location"))
	}
	body = get(t, app, "/feedback").body
	missed, wrong := strings.Index(body, "kind-missed"), strings.Index(body, "kind-wrong")
	if missed < 0 || wrong < missed {
		t.Errorf("the pieces are not listed newest first:\n%s", body)
	}
	if !regexp.MustCompile(`(?s)<tr class="undone">\s*<td class="when">[^<]*</td>\s*<td><span class="kind kind-missed">`).MatchString(body) {
		t.Errorf("the undone piece is not kept, marked undone:\n%s", body)
	}
	if n := len(regexp.MustCompile(`<a class="machine" href="/calls/\d+">read · call \d+</a>`).FindAllString(body, -1)); n != 2 {
		t.Errorf("%d links to the reading call behind each piece, want 2:\n%s", n, body)
	}
}

func TestFeedbackTheDesignDoesNotGiveWhereItIsAskedForIsRefusedWith422AndThePageReRendered(t *testing.T) {
	store := openStore(t)
	readRelease(t, store, releaseReading)
	app, _ := newAppWithLog(t, saturday, store)
	page := "/items/" + number10.ID + "/release"
	margin := spanBlock(t, get(t, app, page).body, 2).margin
	_, wrong := giveForm(t, margin, "Wrong: “the city” refers to “Derby”")
	_, missed := giveForm(t, margin, "Missed")

	for name, form := range map[string]url.Values{
		"new to me on a refers to": {"kind": {"new to me"}, "judgement": wrong["judgement"]},
		"unwanted off a Briefing":  {"kind": {"unwanted"}, "span": missed["span"]},
		"wrong on a Span":          {"kind": {"wrong"}, "span": missed["span"]},
		"no kind":                  {"judgement": wrong["judgement"]},
	} {
		res := send(t, app, http.MethodPost, page+"/feedback", form)

		if res.status != http.StatusUnprocessableEntity || !strings.Contains(res.body, `<p role="alert">`) ||
			!strings.Contains(res.body, "GBR headquarters in Derby") {
			t.Errorf("%s: status %d, want 422 with the Item's page saying why:\n%s", name, res.status, res.body)
		}
	}
	if body := get(t, app, page).body; strings.Contains(body, "Your Feedback here") {
		t.Errorf("Feedback refused was kept:\n%s", body)
	}
}
