package web_test

import (
	"database/sql"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// toldBriefing is a Record whose Briefing for Saturday tells the later
// Item's Span, related to the release's.
func toldBriefing(t *testing.T) *sqlite.Store {
	t.Helper()
	store := relatedRecord(t)
	tell(t, store, saturday, teller{text: "Derby will host the headquarters.", tell: "Derby will host",
		story: []string{"This is fantastic news"}})
	return store
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
	app, _ := newAppWithLog(t, saturday, toldBriefing(t))

	page := get(t, app, "/briefings/2026-10-24/trail").body

	table := strings.Index(page, "<table")
	if total := strings.Index(page, "tokens over 3 calls"); total < 0 || table < 0 || total > table {
		t.Errorf("the trail does not open with its totals, then the table:\n%s", page)
	}
	head, rows := trailRows(t, page)
	if strings.Join(head, "|") != "Span|Item|Read|Related" {
		t.Errorf("the table's head is %q, want Span|Item|Read|Related", head)
	}
	if len(rows) != 2 {
		t.Fatalf("%d rows, want the writing call's and the one Span's:\n%s", len(rows), page)
	}
	if !regexp.MustCompile(`<a class="machine" href="/calls/\d+">written · call \d+ · \d+ tok</a>`).MatchString(rows[0]) {
		t.Errorf("the first row is not the writing call, as a machine label with its tokens:\n%s", rows[0])
	}
}

func TestATrailSaysABriefingWrittenEmptyMadeNoCall(t *testing.T) {
	store := openStore(t)
	tell(t, store, saturday, teller{})
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, "/briefings/2026-10-24/trail").body

	_, rows := trailRows(t, page)
	if len(rows) != 1 || !strings.Contains(rows[0], "written without a model call") || strings.Contains(page, `href="/calls/`) {
		t.Errorf("the trail does not say the empty Briefing made no call:\n%s", page)
	}
}

func TestTheBriefingLinksToItsTrailAndOpeningTheTrailRecordsNoOpen(t *testing.T) {
	store := toldBriefing(t)
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

func TestATrailCountsEachCallOnceThoughSeveralSpansShareIt(t *testing.T) {
	store := relatedRecord(t)
	// Two Spans of the release, both behind its one reading call (it was
	// related with no call: nothing was read before it).
	tell(t, store, saturday, teller{text: "Andy Burnham called it fantastic news, long awaited.",
		tell: "This is fantastic news", story: []string{"Derby will host"}, also: "Derby has waited long enough"})
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/briefings/2026-10-24/trail")

	_, rows := trailRows(t, res.body)
	if len(rows) != 3 {
		t.Fatalf("%d rows, want the writing call's and the two Spans':\n%s", len(rows), res.body)
	}
	if !strings.Contains(res.body, "tokens over 2 calls") {
		t.Errorf("the trail does not count each call once:\n%s", res.body)
	}
}

// earlierBriefing is a Record holding, for Saturday, the day of a Briefing
// written before the Record kept only its Items and its events: all the
// Record keeps of one.
func earlierBriefing(t *testing.T) *sqlite.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "go-slow.db")
	store, err := sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(t.Context(), `
		INSERT INTO briefings (day, written_at, model) VALUES ('2026-10-24', '2026-10-24T07:00:00.000000000Z', 'claude-test')`); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestABriefingWrittenBeforeTheRecordKeptOnlyEventsSaysWhatItSaidWentAndHasNoTrail(t *testing.T) {
	store := earlierBriefing(t)
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, "/briefings/2026-10-24").body

	if !strings.Contains(page, "what it said went with the old design") || strings.Contains(page, "Missed") {
		t.Errorf("the page does not say the earlier Briefing's content went:\n%s", page)
	}
	if strings.Contains(page, `id="how-written"`) {
		t.Errorf("the page links a trail for a Briefing whose content went:\n%s", page)
	}
	if res := get(t, app, "/briefings/2026-10-24/trail"); res.status != 404 {
		t.Errorf("its trail: status %d, want 404", res.status)
	}
	rec, err := store.RegisterRecord(t.Context(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.BriefingOpens) != 1 {
		t.Errorf("opens = %v, want the earlier Briefing's opening kept", rec.BriefingOpens)
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
