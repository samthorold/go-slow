package web_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// sunday is the Sunday after saturday, past the Briefing's time.
var sunday = saturday.AddDate(0, 0, 1)

// offers reports whether the page offers a fresh button giving kind on the
// judgement id, its hidden fields as the form sends them.
func offers(body, kind string, id int64) bool {
	fields := `<input type="hidden" name="kind" value="` + kind + `"><input type="hidden" name="judgement" value="` +
		strconv.FormatInt(id, 10) + `">`
	return strings.Contains(body, fields+"\n  <button class=\"give ")
}

// undoForm is the path the page's undo of the piece of Feedback goes to, or
// "" if the page offers none.
func undoForm(body string, id int64) string {
	m := regexp.MustCompile(`<form[^>]*hx-put="([^"]*/feedback/` + strconv.FormatInt(id, 10) + `/undone)"[^>]*hx-target="body"`).FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return m[1]
}

// actionGroups matches every group of a block's or a title's own Feedback
// controls, shown at rest beside what they act on.
var actionGroups = regexp.MustCompile(`(?s)<div class="actions[^"]*" role="group" aria-label="([^"]*)">(.*?)</div>`)

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

// toldRecord is the related Record with Saturday's Briefing telling the
// later Item's "Derby will host the headquarters.", its Story gathered with
// the release's "fantastic news": what the Sunday read is shown, P1 the
// told Span.
func toldRecord(t *testing.T) *sqlite.Store {
	t.Helper()
	store := relatedRecord(t)
	tell(t, store, saturday, teller{text: "Derby will host the GBR headquarters.", tell: "Derby will host", story: []string{"fantastic news"}})
	return store
}

// writeSundayRead writes the Sunday read at at, as an agent submitting
// read, and returns it as kept.
func writeSundayRead(t *testing.T, store *sqlite.Store, at time.Time, read string) editor.SundayRead {
	t.Helper()
	ed := editor.New(editor.Deps{Store: store, SundayReads: store, SundayReadModel: submitter{read},
		Wikidata: noWikidata{}, Log: slog.New(slog.DiscardHandler)})
	if err := ed.SundayRead(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	r, found, err := store.SundayRead(t.Context(), at.Format(time.DateOnly))
	if err != nil || !found {
		t.Fatalf("no read written: %v %v", found, err)
	}
	return r
}

const assessed = `{"assessments": [{"ref": "N1", "statement": "The government is courting Derby.", "spans": ["P1"],
		"assessments": [], "supersedes": ""}],
	"statements": [{"text": "The Prime Minister is <b hx-get=\"/evil\">courting</b> Derby.", "cites": ["N1", "P1"], "stories": []}]}`

// citesTwo makes an Assessment, and one statement citing it and one citing
// only the Span.
const citesTwo = `{"assessments": [{"ref": "N1", "statement": "The government is courting Derby.", "spans": ["P1"],
		"assessments": [], "supersedes": ""}],
	"statements": [{"text": "The Prime Minister is courting Derby.", "cites": ["N1"], "stories": []},
		{"text": "Derby will host the headquarters.", "cites": ["P1"], "stories": []}]}`

const readFeedback = "/sunday-reads/2026-10-25/feedback"

func TestTheSundayReadHasItsOwnPageLinkedFromThatDaysBriefing(t *testing.T) {
	store := toldRecord(t)
	r := writeSundayRead(t, store, sunday, assessed)
	app, _ := newAppWithLog(t, sunday, store)

	if res := get(t, app, "/briefings/2026-10-25"); !strings.Contains(res.body, `href="/sunday-reads/2026-10-25"`) {
		t.Fatalf("Sunday's Briefing does not link the Sunday read:\n%s", res.body)
	}
	res := get(t, app, "/sunday-reads/2026-10-25")
	if res.status != 200 || !strings.Contains(res.body, "<!doctype html>") {
		t.Fatalf("status %d, not a full page:\n%s", res.status, res.body)
	}
	// The statement is the Editor's words: escaped and inert.
	if strings.Contains(res.body, "<b hx-get") || !regexp.MustCompile(`hx-disable>The Prime Minister is &lt;b`).MatchString(res.body) {
		t.Errorf("the statement is not escaped inside hx-disable:\n%s", res.body)
	}
	// Each citation traces: the Assessment, at its own anchor, and the Span
	// on its own page.
	a := strconv.FormatInt(r.Assessments[0].ID, 10)
	for _, want := range []string{
		`href="#assessment-` + a + `"`, `id="assessment-` + a + `"`, "The government is courting Derby.",
		`href="/items/number-10/hosting/spans/`, "Derby will host the headquarters.",
	} {
		if !strings.Contains(res.body, want) {
			t.Errorf("the read's page does not show %q:\n%s", want, res.body)
		}
	}
}

func TestTheSundayReadsStatementsHaveWhatTheyCiteBesideThemInAFoldedMargin(t *testing.T) {
	store := toldRecord(t)
	r := writeSundayRead(t, store, sunday, assessed)
	app, _ := newAppWithLog(t, sunday, store)

	page := get(t, app, "/sunday-reads/2026-10-25").body

	region := foldedRegion(t, page)
	statement := blockOf(t, region, "The Prime Minister is &lt;b")
	for _, want := range []string{`href="#assessment-`, `href="/items/number-10/hosting/spans/`,
		"Derby will host the headquarters.", "<cite hx-disable>Derby to host GBR</cite>", noTen} {
		if !strings.Contains(statement.margin, want) {
			t.Errorf("the statement's margin lacks %q:\n%s", want, statement.margin)
		}
	}
	// New to me on the read is its title's action, beside it.
	title := regexp.MustCompile(`(?s)<div class="title-row">\s*<h1>Sunday read</h1>(.*?)</div>\s*</div>`).FindStringSubmatch(page)
	if title == nil || !actionGroups.MatchString(title[0]) || !offers(title[1], "new to me", r.ID) {
		t.Errorf("the read's title carries no New to me among its actions:\n%s", page)
	}
}

func TestAssessmentsAreSetApartAsJudgementWithTheirNotesInTheMarginAndActionsWithTheirText(t *testing.T) {
	store := toldRecord(t)
	r := writeSundayRead(t, store, sunday, assessed)
	app, _ := newAppWithLog(t, sunday, store)

	page := get(t, app, "/sunday-reads/2026-10-25").body

	region := foldedRegion(t, page)
	heading := strings.Index(region, "<h2>Assessments</h2>")
	if heading < 0 {
		t.Fatalf("no Assessments heading in the region:\n%s", region)
	}
	as := blockOf(t, region[heading:], "The government is courting Derby.")
	if !strings.Contains(as.body, `class="block-body assessment"`) {
		t.Errorf("the Assessment is not set apart as judgement:\n%s", as.body)
	}
	if !strings.Contains(as.margin, "Rests on") || !strings.Contains(as.margin, "Derby will host the headquarters.") {
		t.Errorf("the margin does not say what it rests on:\n%s", as.margin)
	}
	if !regexp.MustCompile(`<a class="machine" href="/conversations/\d+#turn-1">reached · turn 1</a>`).MatchString(as.margin) {
		t.Errorf("the margin has no machine label \"reached · turn N\" linking the turn:\n%s", as.margin)
	}
	id := r.Assessments[0].ID
	groups := actionGroups.FindAllStringSubmatch(as.body, -1)
	if len(groups) != 1 || !offers(groups[0][2], "wrong", id) || !offers(groups[0][2], "new to me", id) {
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

func TestWrongOnAnAssessmentWithdrawsItAndTheReadReRendersWithoutWhatCitesIt(t *testing.T) {
	store := toldRecord(t)
	r := writeSundayRead(t, store, sunday, citesTwo)
	app, _ := newAppWithLog(t, sunday, store)
	id := strconv.FormatInt(r.Assessments[0].ID, 10)

	before := get(t, app, "/sunday-reads/2026-10-25")
	for _, want := range []string{`hx-post="` + readFeedback + `"`, `hx-confirm=`} {
		if !strings.Contains(before.body, want) {
			t.Fatalf("the read offers no Feedback (%s):\n%s", want, before.body)
		}
	}

	res := send(t, app, "POST", readFeedback, url.Values{"kind": {"wrong"}, "judgement": {id}})

	if res.status != 303 || res.header.Get("Location") != "/sunday-reads/2026-10-25" {
		t.Fatalf("status %d, Location %q:\n%s", res.status, res.header.Get("Location"), res.body)
	}
	after := get(t, app, "/sunday-reads/2026-10-25")
	if strings.Contains(after.body, "The Prime Minister is courting Derby.") || !strings.Contains(after.body, "Derby will host the headquarters.") {
		t.Errorf("want only the statement citing the withdrawn Assessment taken out:\n%s", after.body)
	}
	if !regexp.MustCompile(`class="block-body assessment quiet">\s*<p><del hx-disable>The government is courting Derby\.</del> <strong>Withdrawn</strong>`).MatchString(after.body) {
		t.Errorf("the withdrawn Assessment is not struck through and quiet:\n%s", after.body)
	}
	list := get(t, app, "/feedback")
	if !strings.Contains(list.body, "The government is courting Derby.") || !strings.Contains(list.body, `href="/assessments/`+id+`"`) {
		t.Errorf("the Feedback page does not show it:\n%s", list.body)
	}
}

func TestFeedbackOnTheReadIsUndoneFromTheReadWhichReRendersWithWhatItTookOut(t *testing.T) {
	store := toldRecord(t)
	r := writeSundayRead(t, store, sunday, citesTwo)
	app, _ := newAppWithLog(t, sunday, store)
	send(t, app, "POST", readFeedback, url.Values{"kind": {"wrong"}, "judgement": {strconv.FormatInt(r.Assessments[0].ID, 10)}})
	send(t, app, "POST", readFeedback, url.Values{"kind": {"new to me"}, "judgement": {strconv.FormatInt(r.ID, 10)}})
	pieces, err := store.KeptPieces(t.Context())
	if err != nil || len(pieces) != 2 {
		t.Fatalf("pieces = %+v, %v", pieces, err)
	}
	page := get(t, app, "/sunday-reads/2026-10-25")
	wrong := pieces[1].ID // newest first
	path := undoForm(page.body, wrong)
	if path != "/sunday-reads/2026-10-25/feedback/"+strconv.FormatInt(wrong, 10)+"/undone" || undoForm(page.body, pieces[0].ID) == "" {
		t.Fatalf("the read offers no undo of its Feedback: %q\n%s", path, page.body)
	}

	res := send(t, app, "PUT", path, nil)

	if res.status != 303 || res.header.Get("Location") != "/sunday-reads/2026-10-25" {
		t.Fatalf("status %d, Location %q:\n%s", res.status, res.header.Get("Location"), res.body)
	}
	after := get(t, app, "/sunday-reads/2026-10-25")
	if !strings.Contains(after.body, "The Prime Minister is courting Derby.") || strings.Contains(after.body, "<strong>Withdrawn</strong>") {
		t.Errorf("the read does not re-render with the Assessment and what cites it:\n%s", after.body)
	}
}

func TestNewToMeOnTheSundayReadIsKeptAndListedOnTheFeedbackPage(t *testing.T) {
	store := toldRecord(t)
	r := writeSundayRead(t, store, sunday, assessed)
	app, _ := newAppWithLog(t, sunday, store)

	res := send(t, app, "POST", readFeedback, url.Values{"kind": {"new to me"}, "judgement": {strconv.FormatInt(r.ID, 10)}})

	if res.status != 303 {
		t.Fatalf("status %d:\n%s", res.status, res.body)
	}
	list := get(t, app, "/feedback").body
	if !strings.Contains(list, `href="/sunday-reads/2026-10-25"`) || !strings.Contains(list, "the Sunday read") ||
		!regexp.MustCompile(`href="/conversations/\d+"`).MatchString(list) {
		t.Errorf("the Feedback page does not link the read and its conversation:\n%s", list)
	}
	// Wrong on the read as a whole is not given: it is no judgement of one
	// thing.
	if res := send(t, app, "POST", readFeedback, url.Values{"kind": {"wrong"}, "judgement": {strconv.FormatInt(r.ID, 10)}}); res.status != http.StatusUnprocessableEntity {
		t.Errorf("status %d, want 422", res.status)
	}
	if res := send(t, app, "POST", readFeedback, url.Values{"kind": {"wrong"}, "judgement": {"9999"}}); res.status != 404 {
		t.Errorf("status %d, want 404", res.status)
	}
}

func TestTheRegisterCountsSundayReadsWrittenOpenedAndNewToMe(t *testing.T) {
	store := toldRecord(t)
	r := writeSundayRead(t, store, sunday, assessed)
	app, _ := newAppWithLog(t, sunday, store)

	get(t, app, "/sunday-reads/2026-10-25")
	get(t, app, "/sunday-reads/2026-10-25")
	send(t, app, "POST", readFeedback, url.Values{"kind": {"new to me"}, "judgement": {strconv.FormatInt(r.Assessments[0].ID, 10)}})

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
	if len(rec.Feedback) != 1 || rec.Feedback[0].SundayRead != "2026-10-25" || rec.Feedback[0].Kind != "new to me" {
		t.Errorf("Feedback = %+v, want it given on the read", rec.Feedback)
	}
	// A Sunday with no read is not one opened.
	get(t, app, "/sunday-reads/2026-10-18")
	if rec, _ := store.RegisterRecord(t.Context(), time.UTC); len(rec.SundayReadOpens) != 1 {
		t.Errorf("opens = %+v", rec.SundayReadOpens)
	}
}

func TestAnAssessmentsAddressLeadsToItInItsRead(t *testing.T) {
	store := toldRecord(t)
	r := writeSundayRead(t, store, sunday, assessed)
	app, _ := newAppWithLog(t, sunday, store)
	id := strconv.FormatInt(r.Assessments[0].ID, 10)

	res := get(t, app, "/assessments/"+id)
	if res.status != 303 || res.header.Get("Location") != "/sunday-reads/2026-10-25#assessment-"+id {
		t.Errorf("status %d, Location %q", res.status, res.header.Get("Location"))
	}
	if res := get(t, app, "/assessments/9999"); res.status != 404 {
		t.Errorf("status %d, want 404", res.status)
	}
}

func TestAStoryTheReadGatheredIsTheSpansLatestGathering(t *testing.T) {
	store := toldRecord(t)
	r := writeSundayRead(t, store, sunday, `{"assessments": [], "statements": [{"text": "Derby's week.", "cites": ["P1"],
		"stories": [{"span": "P1", "story": []}]}]}`)
	app, _ := newAppWithLog(t, sunday, store)

	page := get(t, app, spanPath(t, app, "hosting", 1)).body

	for _, want := range []string{`<a href="/sunday-reads/2026-10-25">the Sunday read for Sunday 25 October 2026</a>`,
		`href="/conversations/` + strconv.FormatInt(r.ConversationID, 10)} {
		if !strings.Contains(page, want) {
			t.Errorf("the Span's page lacks %q:\n%s", want, page)
		}
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
