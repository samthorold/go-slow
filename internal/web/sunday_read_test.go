package web_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// sunday is the Sunday after saturday, past the Briefing's time.
var sunday = saturday.AddDate(0, 0, 1)

// foldedRegion asserts page holds its blocks in one margined region,
// starting folded, with the one control that unfolds it, and returns the
// region. The Sunday read is a reading page: its margin starts folded.
func foldedRegion(t *testing.T, page string) string {
	t.Helper()
	regions := regexp.MustCompile(`<div class="margined" data-margin="(open|folded)">`).FindAllStringSubmatch(page, -1)
	if len(regions) != 1 || regions[0][1] != "folded" {
		t.Fatalf("want one margined region, folded; got %v:\n%s", regions, page)
	}
	controls := regexp.MustCompile(`<button type="button" class="margin-fold" data-margin-fold aria-expanded="false">[^<]+</button>`).FindAllString(page, -1)
	if len(controls) != 1 {
		t.Errorf("want one fold button, not expanded, got %d:\n%s", len(controls), page)
	}
	if !strings.Contains(page, `<script src="/static/margin-fold.js"></script>`) {
		t.Errorf("the page does not load the fold behaviour:\n%s", page)
	}
	return page[strings.Index(page, `class="margined"`):]
}

// submitter is an agent that submits read at once.
type submitter struct{ read string }

func (s submitter) Turn(context.Context, editor.ConversationRequest) (editor.Turn, error) {
	return editor.Turn{Model: "fake-agent", Calls: []editor.ToolCall{{ID: "1", Name: "submit_read", Input: json.RawMessage(s.read)}}}, nil
}

// writeSundayRead writes the Sunday read at at, as an agent submitting read.
func writeSundayRead(t *testing.T, store *sqlite.Store, at time.Time, read string) {
	t.Helper()
	ed := editor.New(editor.Deps{Store: store, SundayReads: store, SundayReadModel: submitter{read},
		Wikidata: noWikidata{}, Log: slog.New(slog.DiscardHandler)})
	if err := ed.SundayRead(t.Context(), at); err != nil {
		t.Fatal(err)
	}
}

const assessed = `{"statements": [
	{"text": "The Prime Minister is <b hx-get=\"/evil\">courting</b> Scotland.", "cites": ["N1", "S1:F1"]}],
	"assessments": [{"ref": "N1", "statement": "The Prime Minister is courting Scottish voters.", "facts": ["F1"], "supersedes": ""}],
	"stories": []}`

func TestTheSundayReadHasItsOwnPageLinkedFromThatDaysBriefing(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeStoryBriefing(t, store, sunday, `{}`)
	writeSundayRead(t, store, sunday, assessed)
	app, _ := newAppWithLog(t, sunday, store)

	briefing := get(t, app, "/")
	if !strings.Contains(briefing.body, `href="/sunday-reads/2026-10-25"`) {
		t.Fatalf("Sunday's Briefing does not link the Sunday read:\n%s", briefing.body)
	}
	if res := get(t, app, "/briefings/2026-10-24"); strings.Contains(res.body, "/sunday-reads/") {
		t.Errorf("Saturday's Briefing links a Sunday read:\n%s", res.body)
	}

	res := get(t, app, "/sunday-reads/2026-10-25")
	if res.status != 200 || !strings.Contains(res.body, "<!doctype html>") {
		t.Fatalf("status %d, not a full page:\n%s", res.status, res.body)
	}
	// The statement is the Editor's words: escaped and inert.
	if strings.Contains(res.body, "<b hx-get") || !regexp.MustCompile(`hx-disable>The Prime Minister is &lt;b`).MatchString(res.body) {
		t.Errorf("the statement is not escaped inside hx-disable:\n%s", res.body)
	}
	// Each citation traces: the Assessment, at its own anchor with the Facts
	// it rests on, and the Development on its Story's Timeline.
	for _, want := range []string{
		`href="#assessment-1"`, `id="assessment-1"`, "The Prime Minister is courting Scottish voters.",
		`href="/stories/1#development-1"`, "Andy Burnham spoke in Motherwell.",
	} {
		if !strings.Contains(res.body, want) {
			t.Errorf("the read's page does not show %q:\n%s", want, res.body)
		}
	}
}

func TestTheSundayReadsStatementsHaveWhatTheyCiteBesideThemInAFoldedMargin(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeSundayRead(t, store, sunday, assessed)
	app, _ := newAppWithLog(t, sunday, store)

	page := get(t, app, "/sunday-reads/2026-10-25").body

	region := foldedRegion(t, page)
	statement := blockOf(t, region, "The Prime Minister is &lt;b")
	for _, want := range []string{`href="#assessment-1"`, `href="/stories/1#development-1"`,
		"Andy Burnham spoke in Motherwell.", "<cite hx-disable>PM speech in Scotland</cite>"} {
		if !strings.Contains(statement.margin, want) {
			t.Errorf("the statement's margin lacks %q:\n%s", want, statement.margin)
		}
	}
	// New to me on the read is its title's action, shown on hover or focus.
	title := regexp.MustCompile(`(?s)<div class="title-row">\s*<h1>Sunday read</h1>(.*?)</div>\s*</div>`).FindStringSubmatch(page)
	if title == nil || !actionGroups.MatchString(title[0]) || !offers(title[1], "new to me", "sunday read", "read", "1") {
		t.Errorf("the read's title carries no New to me among its actions:\n%s", page)
	}
}

func TestTheSundayReadCitesAnItemByItsSourcesShortNameBeforeItsTitle(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeSundayRead(t, store, sunday, assessed)
	app, _ := newAppWithLog(t, sunday, store)

	page := get(t, app, "/sunday-reads/2026-10-25").body

	statement := blockOf(t, foldedRegion(t, page), "The Prime Minister is &lt;b")
	if !strings.Contains(statement.margin, noTen) {
		t.Errorf("the statement's citation does not name its Source as %q:\n%s", noTen, statement.margin)
	}
}

func TestAssessmentsAreSetApartAsJudgementWithTheirNotesInTheMarginAndActionsOnHover(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeSundayRead(t, store, sunday, assessed)
	app, _ := newAppWithLog(t, sunday, store)

	page := get(t, app, "/sunday-reads/2026-10-25").body

	region := foldedRegion(t, page)
	heading := strings.Index(region, "<h2>Assessments</h2>")
	if heading < 0 {
		t.Fatalf("no Assessments heading in the region:\n%s", region)
	}
	as := blockOf(t, region[heading:], "The Prime Minister is courting Scottish voters.")
	if !strings.Contains(as.body, `class="block-body assessment"`) {
		t.Errorf("the Assessment is not set apart as judgement:\n%s", as.body)
	}
	if !strings.Contains(as.margin, "Rests on") || !strings.Contains(as.margin, "Andy Burnham spoke in Motherwell.") {
		t.Errorf("the margin does not say what it rests on:\n%s", as.margin)
	}
	if !regexp.MustCompile(`<a class="machine" href="/conversations/\d+#turn-1">reached · turn 1</a>`).MatchString(as.margin) {
		t.Errorf("the margin has no machine label \"reached · turn N\" linking the turn:\n%s", as.margin)
	}
	groups := actionGroups.FindAllStringSubmatch(as.body, -1)
	if len(groups) != 1 || !offers(groups[0][2], "wrong", "assessment", "assessment", "1") ||
		!offers(groups[0][2], "new to me", "assessment", "assessment", "1") {
		t.Errorf("the Assessment's body does not hold its actions, Wrong and New to me:\n%s", as.body)
	}
	if actionGroups.MatchString(as.margin) {
		t.Errorf("the Assessment's actions are in the margin, which folds away:\n%s", as.margin)
	}

	css := get(t, app, "/static/go-slow.css").body
	if !regexp.MustCompile(`(?m)^\.assessment\s*\{[^}]*border-left:`).MatchString(css) {
		t.Errorf("no rule gives an Assessment its left rule:\n%s", css)
	}
}

func TestAWithdrawnAssessmentIsStruckThroughAndQuiet(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeSundayRead(t, store, sunday, assessed)
	app, _ := newAppWithLog(t, sunday, store)
	send(t, app, "POST", readFeedback, url.Values{"kind": {"wrong"}, "level": {"assessment"}, "assessment": {"1"}})

	page := get(t, app, "/sunday-reads/2026-10-25").body

	as := blockOf(t, page, "The Prime Minister is courting Scottish voters.")
	if !regexp.MustCompile(`class="block-body assessment quiet">\s*<p><del hx-disable>The Prime Minister is courting Scottish voters\.</del>`).MatchString(as.body) {
		t.Errorf("the withdrawn Assessment is not struck through and quiet:\n%s", as.body)
	}
}

func TestTheStoriesARunStartedAreAQuietListAtTheEnd(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeSundayRead(t, store, sunday, `{"statements": [{"text": "He spoke in Motherwell.", "cites": ["S1:F1"]}],
		"assessments": [], "stories": [{"title": "Scotland's vote", "developments": ["S1:F1"]}]}`)
	app, _ := newAppWithLog(t, sunday, store)

	page := get(t, app, "/sunday-reads/2026-10-25").body

	if !regexp.MustCompile(`(?s)<section class="quiet" aria-labelledby="stories-started">\s*<h2 id="stories-started">Stories started</h2>.*Scotland&#39;s vote`).MatchString(page) {
		t.Errorf("the Stories started are not a quiet list:\n%s", page)
	}
	if strings.Index(page, "Stories started") < strings.Index(page, `class="margined"`) {
		t.Errorf("the Stories started come before the read:\n%s", page)
	}
}

const readFeedback = "/sunday-reads/2026-10-25/feedback"

func TestWrongOnAnAssessmentWithdrawsItAndTheReadReRendersWithoutWhatCitesIt(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeSundayRead(t, store, sunday, `{"statements": [
		{"text": "The Prime Minister is courting Scotland.", "cites": ["N1"]},
		{"text": "He spoke in Motherwell.", "cites": ["S1:F1"]}],
		"assessments": [{"ref": "N1", "statement": "The Prime Minister is courting Scottish voters.", "facts": ["F1"], "supersedes": ""}],
		"stories": []}`)
	app, _ := newAppWithLog(t, sunday, store)

	before := get(t, app, "/sunday-reads/2026-10-25")
	for _, want := range []string{`hx-post="` + readFeedback + `"`, `name="level" value="assessment"`, `name="level" value="sunday read"`, `hx-confirm=`} {
		if !strings.Contains(before.body, want) {
			t.Fatalf("the read offers no Feedback (%s):\n%s", want, before.body)
		}
	}

	res := send(t, app, "POST", readFeedback, url.Values{"kind": {"wrong"}, "level": {"assessment"}, "assessment": {"1"}})

	if res.status != 303 || res.header.Get("Location") != "/sunday-reads/2026-10-25" {
		t.Fatalf("status %d, Location %q:\n%s", res.status, res.header.Get("Location"), res.body)
	}
	after := get(t, app, "/sunday-reads/2026-10-25")
	if strings.Contains(after.body, "The Prime Minister is courting Scotland.") || !strings.Contains(after.body, "He spoke in Motherwell.") {
		t.Errorf("want only the statement citing the withdrawn Assessment taken out:\n%s", after.body)
	}
	if !strings.Contains(after.body, "Withdrawn") {
		t.Errorf("the withdrawn Assessment is not marked:\n%s", after.body)
	}
	list := get(t, app, "/feedback")
	if !strings.Contains(list.body, "The Prime Minister is courting Scottish voters.") || !strings.Contains(list.body, `href="/assessments/1"`) {
		t.Errorf("the Feedback page does not show it:\n%s", list.body)
	}
}

func TestWrongOnAnAssessmentIsUndoneFromTheReadWhichReRendersWithWhatCitesIt(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeSundayRead(t, store, sunday, `{"statements": [
		{"text": "The Prime Minister is courting Scotland.", "cites": ["N1"]}],
		"assessments": [{"ref": "N1", "statement": "The Prime Minister is courting Scottish voters.", "facts": ["F1"], "supersedes": ""}],
		"stories": []}`)
	app, _ := newAppWithLog(t, sunday, store)
	send(t, app, "POST", readFeedback, url.Values{"kind": {"wrong"}, "level": {"assessment"}, "assessment": {"1"}})
	send(t, app, "POST", readFeedback, url.Values{"kind": {"new to me"}, "level": {"sunday read"}, "read": {"1"}})
	page := get(t, app, "/sunday-reads/2026-10-25")
	path := undoForm(page.body, 1)
	if path != "/sunday-reads/2026-10-25/feedback/1/undone" || undoForm(page.body, 2) == "" {
		t.Fatalf("the read offers no undo of its Feedback: %q\n%s", path, page.body)
	}

	res := send(t, app, "PUT", path, nil)

	if res.status != 303 || res.header.Get("Location") != "/sunday-reads/2026-10-25" {
		t.Fatalf("status %d, Location %q:\n%s", res.status, res.header.Get("Location"), res.body)
	}
	after := get(t, app, "/sunday-reads/2026-10-25")
	if !strings.Contains(after.body, "The Prime Minister is courting Scotland.") || strings.Contains(after.body, "<strong>Withdrawn</strong>") {
		t.Errorf("the read does not re-render with the Assessment and what cites it:\n%s", after.body)
	}
}

func TestNewToMeOnTheSundayReadIsKept(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeSundayRead(t, store, sunday, assessed)
	app, _ := newAppWithLog(t, sunday, store)

	res := send(t, app, "POST", readFeedback, url.Values{"kind": {"new to me"}, "level": {"sunday read"}, "read": {"1"}})

	if res.status != 303 {
		t.Fatalf("status %d:\n%s", res.status, res.body)
	}
	kept, err := store.KeptFeedback(t.Context())
	if err != nil || len(kept) != 1 || kept[0].ReadID != 1 || kept[0].Day != "2026-10-25" {
		t.Errorf("kept = %+v, %v", kept, err)
	}
	if list := get(t, app, "/feedback"); !strings.Contains(list.body, `href="/sunday-reads/2026-10-25"`) {
		t.Errorf("the Feedback page does not link the read:\n%s", list.body)
	}
	// An Assessment that is not this read's is not found.
	if res := send(t, app, "POST", readFeedback, url.Values{"kind": {"wrong"}, "level": {"assessment"}, "assessment": {"9"}}); res.status != 404 {
		t.Errorf("status %d, want 404", res.status)
	}
}

func TestTheRegisterCountsSundayReadsWrittenOpenedAndNewToMe(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeSundayRead(t, store, sunday, assessed)
	app, _ := newAppWithLog(t, sunday, store)

	get(t, app, "/sunday-reads/2026-10-25")
	get(t, app, "/sunday-reads/2026-10-25")
	send(t, app, "POST", readFeedback, url.Values{"kind": {"new to me"}, "level": {"assessment"}, "assessment": {"1"}})

	rec, err := store.RegisterRecord(t.Context(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.SundayReads) != 1 || rec.SundayReads[0] != "2026-10-25" {
		t.Errorf("Sunday reads written = %v", rec.SundayReads)
	}
	if len(rec.SundayReadOpens) != 1 || rec.SundayReadOpens[0].Read != "2026-10-25" || rec.SundayReadOpens[0].On != "2026-10-25" {
		t.Errorf("Sunday reads opened = %+v, want once", rec.SundayReadOpens)
	}
	if len(rec.Feedback) != 1 || rec.Feedback[0].SundayRead != "2026-10-25" {
		t.Errorf("Feedback = %+v, want it given on the read", rec.Feedback)
	}
	// A Sunday with no read is not one opened.
	get(t, app, "/sunday-reads/2026-10-18")
	if rec, _ := store.RegisterRecord(t.Context(), time.UTC); len(rec.SundayReadOpens) != 1 {
		t.Errorf("opens = %+v", rec.SundayReadOpens)
	}
}

func TestAnAssessmentsAddressLeadsToItInItsRead(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeSundayRead(t, store, sunday, assessed)
	app, _ := newAppWithLog(t, sunday, store)

	res := get(t, app, "/assessments/1")
	if res.status != 303 || res.header.Get("Location") != "/sunday-reads/2026-10-25#assessment-1" {
		t.Errorf("status %d, Location %q", res.status, res.header.Get("Location"))
	}
	if res := get(t, app, "/assessments/2"); res.status != 404 {
		t.Errorf("status %d, want 404", res.status)
	}
}

func TestASundayWithNoReadSaysSoAndOtherDaysHaveNone(t *testing.T) {
	app := newApp(t, sunday)

	if res := get(t, app, "/sunday-reads/2026-10-25"); res.status != 200 || !strings.Contains(res.body, "There is no Sunday read yet.") {
		t.Errorf("status %d:\n%s", res.status, res.body)
	}
	for _, path := range []string{"/sunday-reads/2026-10-24", "/sunday-reads/2026-11-01", "/sunday-reads/soon"} {
		if res := get(t, app, path); res.status != 404 {
			t.Errorf("%s: status = %d, want 404", path, res.status)
		}
	}
}
