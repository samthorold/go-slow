package web_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/editor/frozentest"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// usageModel answers every call with the same output, reporting usage.
type usageModel struct {
	output string
	usage  editor.Usage
}

func (u usageModel) Complete(context.Context, editor.Request) (editor.Response, error) {
	return editor.Response{Output: json.RawMessage(u.output), Model: "fake", Usage: u.usage}, nil
}

// spokenBriefing is a Record whose Briefing cites one Development: the
// speech read (1100 tokens), its Fact placed (550) and the Briefing written
// (220), each call reporting its usage.
func spokenBriefing(t *testing.T, store *sqlite.Store) {
	t.Helper()
	briefSpeech(t, store, reading, `{"new_stories": [{"ref": "N1", "title": "Andy Burnham in Scotland"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}]}`, spoke)
}

// briefSpeech reads the speech, places its Facts and writes the Briefing,
// with the outputs given, reading's call reporting 1100 tokens, placing's
// 550 and the Briefing's 220.
func briefSpeech(t *testing.T, store *sqlite.Store, read, place, brief string) {
	t.Helper()
	speech := item("speech", "PM speech in Scotland", saturday.Add(-48*time.Hour))
	speech.Content = "<p>It’s good to be here in Motherwell.</p>"
	seed(t, store, saturday.AddDate(0, 0, -7), speech)
	if _, errs, err := interest.New(store).Add(t.Context(), interest.Draft{Name: "Scotland", Terms: "Scotland"}); err != nil || errs != nil {
		t.Fatalf("add Topic: %v %v", errs, err)
	}
	drawFacts(t, store, speech, read, editor.Usage{InputTokens: 1000, OutputTokens: 100})
	log := slog.New(slog.DiscardHandler)
	if err := frozentest.PlaceWaiting(t.Context(), frozentest.Deps{Store: store, Log: log,
		Model: usageModel{place, editor.Usage{InputTokens: 500, OutputTokens: 50}}}, saturday); err != nil {
		t.Fatal(err)
	}
	// A Briefing over Stories, the earlier shape, whose trail still shows.
	if err := frozentest.Brief(t.Context(), frozentest.BriefDeps{Store: store, Log: log,
		Model: usageModel{brief, editor.Usage{InputTokens: 200, OutputTokens: 20}}}, saturday); err != nil {
		t.Fatal(err)
	}
}

func TestABriefingsTrailListsItsCallAndThePlacingAndReadingCallsBehindEachDevelopment(t *testing.T) {
	store := openStore(t)
	spokenBriefing(t, store)
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/briefings/2026-10-24/trail")

	if res.status != 200 || !strings.Contains(res.body, "<!doctype html>") {
		t.Fatalf("status %d, not a full page:\n%s", res.status, res.body)
	}
	links := regexp.MustCompile(`href="(/calls/\d+)"`).FindAllStringSubmatch(res.body, -1)
	var acts []string
	for _, l := range links {
		call := get(t, app, l[1]).body
		for _, act := range []string{"briefing", "placing", "reading"} {
			if strings.Contains(call, "<h1>A "+act+" call</h1>") {
				acts = append(acts, act)
			}
		}
	}
	if strings.Join(acts, " ") != "briefing placing reading" {
		t.Errorf("the trail links the calls %v, want the briefing's, then the Development's placing and reading:\n%s", acts, res.body)
	}
	_, rows := trailRows(t, res.body)
	if len(rows) != 2 {
		t.Fatalf("%d rows, want the writing call's and the one Development's:\n%s", len(rows), res.body)
	}
	cells := regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`).FindAllStringSubmatch(rows[1], -1)
	if len(cells) != 4 {
		t.Fatalf("the Development's row has %d cells, want 4:\n%s", len(cells), rows[1])
	}
	for i, want := range []string{
		`<a href="/stories/1#development-1"><span hx-disable>Andy Burnham spoke in Motherwell.</span></a>`,
		`<a href="/stories/1"><span hx-disable>Andy Burnham in Scotland</span></a>`,
		`<a class="machine" href="/calls/2">placed · call 2 · 550 tok</a>`,
		`<a class="machine" href="/calls/1">read · call 1 · 1,100 tok</a>`,
	} {
		if got := strings.TrimSpace(cells[i][1]); got != want {
			t.Errorf("cell %d is %s, want %s", i+1, got, want)
		}
	}
}

// trailRows is the trail's table: its head's cells, then each body row's.
func trailRows(t *testing.T, page string) (head []string, rows []string) {
	t.Helper()
	table := regexp.MustCompile(`(?s)<table[ >].*?</table>`).FindString(page)
	if table == "" {
		t.Fatalf("the trail has no table:\n%s", page)
	}
	thead := regexp.MustCompile(`(?s)<thead>.*?</thead>`).FindString(table)
	for _, m := range regexp.MustCompile(`(?s)<th(?:\s[^>]*)?>(.*?)</th>`).FindAllStringSubmatch(thead, -1) {
		head = append(head, strings.TrimSpace(m[1]))
	}
	tbody := regexp.MustCompile(`(?s)<tbody>.*?</tbody>`).FindString(table)
	return head, regexp.MustCompile(`(?s)<tr[\s>].*?</tr>`).FindAllString(tbody, -1)
}

func TestABriefingsTrailOpensWithItsTotalsThenATableWhoseFirstRowIsTheWritingCall(t *testing.T) {
	store := openStore(t)
	spokenBriefing(t, store)
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, "/briefings/2026-10-24/trail").body

	table := strings.Index(page, "<table")
	if total := strings.Index(page, "1,870 tokens over 3 calls"); total < 0 || table < 0 || total > table {
		t.Errorf("the trail does not open with its totals, then the table:\n%s", page)
	}
	head, rows := trailRows(t, page)
	if strings.Join(head, "|") != "Development|Story|Placed|Read" {
		t.Errorf("the table's head is %q, want Development|Story|Placed|Read", head)
	}
	if len(rows) != 2 {
		t.Fatalf("%d rows, want the writing call's and the one Development's:\n%s", len(rows), page)
	}
	if !regexp.MustCompile(`<a class="machine" href="/calls/\d+">written · call \d+ · 220 tok</a>`).MatchString(rows[0]) {
		t.Errorf("the first row is not the writing call, as a machine label with its tokens:\n%s", rows[0])
	}
}

func TestATrailSaysABriefingWrittenEmptyMadeNoCall(t *testing.T) {
	store := openStore(t)
	writeStoryBriefing(t, store, saturday, spoke)
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, "/briefings/2026-10-24/trail").body

	_, rows := trailRows(t, page)
	if len(rows) != 1 || !strings.Contains(rows[0], "written without a model call") || strings.Contains(page, `href="/calls/`) {
		t.Errorf("the trail does not say the empty Briefing made no call:\n%s", page)
	}
}

func TestTheBriefingLinksToItsTrailAndOpeningTheTrailRecordsNoOpen(t *testing.T) {
	store := openStore(t)
	spokenBriefing(t, store)
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, "/briefings/2026-10-24").body
	how := regexp.MustCompile(`(?s)id="how-written".*?</p>`).FindString(page)
	if !strings.Contains(how, `href="/briefings/2026-10-24/trail"`) || !strings.Contains(how, "How this was written") {
		t.Errorf("the Briefing does not link its trail:\n%s", page)
	}
	if n := strings.Count(page, "How this was written") + strings.Count(page, "How it was written"); n != 1 {
		t.Errorf("%d links say how it was written, want 1:\n%s", n, page)
	}
	if res := get(t, app, "/briefings/2026-10-24/trail"); res.status != 200 {
		t.Fatalf("status %d", res.status)
	}

	rec, err := store.RegisterRecord(t.Context(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.BriefingOpens) != 1 || len(rec.SundayReadOpens) != 0 {
		t.Errorf("opens = %v %v, want only the Briefing's own", rec.BriefingOpens, rec.SundayReadOpens)
	}
}

func TestABriefingsTrailFollowsItAsWrittenThoughFeedbackTookAStatementOut(t *testing.T) {
	store := relatedRecord(t)
	tell(t, store, saturday, teller{text: "Derby will host the headquarters, it said.", tell: "Derby will host"})
	app, _ := newAppWithLog(t, saturday, store)
	path, form := giveForm(t, get(t, app, "/").body, "Wrong: this statement")
	if res := send(t, app, "POST", path, form); res.status != 303 {
		t.Fatalf("status %d:\n%s", res.status, res.body)
	}

	res := get(t, app, "/briefings/2026-10-24/trail")

	if !strings.Contains(res.body, "Derby will host the headquarters.") {
		t.Errorf("the trail drops what the Briefing was written from:\n%s", res.body)
	}
}

func TestADayWithNoBriefingHasNoTrail(t *testing.T) {
	app := newApp(t, saturday)

	for _, path := range []string{"/briefings/2026-10-24/trail", "/briefings/2026-10-25/trail", "/briefings/x/trail"} {
		if res := get(t, app, path); res.status != 404 {
			t.Errorf("%s: status = %d, want 404", path, res.status)
		}
	}
}

func TestATrailCountsEachCallOnceThoughSeveralDevelopmentsShareIt(t *testing.T) {
	store := openStore(t)
	// The speech read into two Facts, each made a Development by one placing
	// call, and both cited by the Briefing.
	twoFacts := strings.Replace(reading, `"facts": [`, `"facts": [{"statement": "Andy Burnham was in Motherwell.", "as_at": "2026-10-22",
             "quote": "here in Motherwell"}, `, 1)
	briefSpeech(t, store, twoFacts, `{"new_stories": [{"ref": "N1", "title": "Andy Burnham in Scotland"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}, {"fact": "F2", "developments_in": ["N1"]}]}`,
		`{"stories": [{"story": "S1", "statements": [
		  {"text": "The Prime Minister spoke in Motherwell.", "developments": ["D1", "D2"]}]}]}`)
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/briefings/2026-10-24/trail")

	for _, want := range []string{"Andy Burnham was in Motherwell.", "Andy Burnham spoke in Motherwell.", "1,870 tokens over 3 calls"} {
		if !strings.Contains(res.body, want) {
			t.Errorf("the trail lacks %q:\n%s", want, res.body)
		}
	}
}

func TestASundayReadsTrailIsItsConversationTurnByTurnWithTokens(t *testing.T) {
	store := toldRecord(t)
	var turns int
	ed := editor.New(editor.Deps{Store: store, SundayReads: store, SundayReadModel: lookThenSubmit{assessed, &turns},
		Wikidata: noWikidata{}, Log: slog.New(slog.DiscardHandler)})
	if err := ed.SundayRead(t.Context(), sunday); err != nil {
		t.Fatal(err)
	}
	app, _ := newAppWithLog(t, sunday, store)

	m := regexp.MustCompile(`<a href="(/conversations/\d+)">How this was written</a>`).FindStringSubmatch(
		get(t, app, "/sunday-reads/2026-10-25").body)
	if m == nil {
		t.Fatal("the read does not link how this was written")
	}
	res := get(t, app, m[1])

	first, second := strings.Index(res.body, `id="turn-1"`), strings.Index(res.body, `id="turn-2"`)
	if first < 0 || second < first {
		t.Fatalf("the turns are not shown in order:\n%s", res.body)
	}
	if !strings.Contains(res.body[first:second], "110 tokens") || !strings.Contains(res.body[second:], "220 tokens") {
		t.Errorf("a turn does not show its tokens:\n%s", res.body)
	}
	if !strings.Contains(res.body[:first], "330 tokens over 2 turns") {
		t.Errorf("the turns' tokens are not summed:\n%s", res.body)
	}
	rec, err := store.RegisterRecord(t.Context(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.SundayReadOpens) != 1 {
		t.Errorf("opens = %v, want only the read's own", rec.SundayReadOpens)
	}
}
