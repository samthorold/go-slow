package web_test

import (
	"encoding/json"
	"html"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// callLink follows the first link to a call's page in body.
func callLink(t *testing.T, body, within string) string {
	t.Helper()
	i := strings.Index(body, within)
	if i < 0 {
		t.Fatalf("no %q in:\n%s", within, body)
	}
	m := regexp.MustCompile(`href="(/calls/\d+)"`).FindStringSubmatch(body[i:])
	if m == nil {
		t.Fatalf("no link to a call after %q:\n%s", within, body[i:])
	}
	return html.UnescapeString(m[1])
}

func TestAFactLinksToTheReadingCallThatDrewIt(t *testing.T) {
	store := openStore(t)
	readSpeech(t, store, reading)
	app, _ := newAppWithLog(t, saturday, store)

	page := itemPage(t, app, "PM speech in Scotland")
	res := get(t, app, callLink(t, page.body, "Andy Burnham spoke in Motherwell."))

	if res.status != 200 || !strings.Contains(res.body, "<!doctype html>") {
		t.Fatalf("status %d, not a full page:\n%s", res.status, res.body)
	}
	for _, want := range []string{
		"reading", "applied",
		// The request: instructions, input, schema.
		"You are the Editor of a private slow-news briefing.", "Title: PM speech in Scotland", "&#34;additionalProperties&#34;",
		// The response, and the model that answered.
		"Andy Burnham spoke in Motherwell.", "fake",
	} {
		if !strings.Contains(res.body, want) {
			t.Errorf("the call's page lacks %q:\n%s", want, res.body)
		}
	}
	if !regexp.MustCompile(`[0-9a-f]{64}`).MatchString(res.body) {
		t.Errorf("the call's page shows no request hash:\n%s", res.body)
	}
}

// failThrice fails the speech's reading three times, each attempt kept with
// its call.
func failThrice(t *testing.T, store *sqlite.Store) {
	t.Helper()
	seed(t, store, saturday.AddDate(0, 0, -7), item("speech", "PM speech in Scotland", saturday.Add(-2*time.Hour)))
	key := number10.ID + "\nspeech"
	for i := range 3 {
		at := saturday.Add(time.Duration(i-3) * time.Hour)
		why := "the result fails the act's checks: attempt " + strconv.Itoa(i+1)
		req := editor.Request{Act: "reading", Instructions: "Read.", Input: "Facts", Schema: map[string]any{}}
		c := editor.Call{Act: "reading", Key: key, At: at, BatchID: "batch-" + strconv.Itoa(i+1), Model: "m",
			Request: req, RequestHash: editor.RequestHash(req, "m"), Outcome: editor.Refused, Why: why,
			Response: &editor.Response{Output: json.RawMessage(`{}`), Model: "m", Usage: editor.Usage{InputTokens: 100, OutputTokens: 10}}}
		if err := store.EndBatch(t.Context(), c.BatchID, []editor.WorkFailure{{
			Act: "reading", Key: key, At: at, Why: why, Failed: i == 2, Call: &c,
		}}, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFailedWorkLinksToEveryAttemptAtIt(t *testing.T) {
	store := openStore(t)
	failThrice(t, store)
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, callLink(t, get(t, app, "/items").body, `id="failed"`))

	if res.status != 200 {
		t.Fatalf("status %d", res.status)
	}
	attempts := res.body[strings.Index(res.body, `id="attempts"`):]
	for _, want := range []string{"attempt 1", "attempt 2", "attempt 3", "330 tokens over 3 attempts"} {
		if !strings.Contains(attempts, want) {
			t.Errorf("the attempts lack %q:\n%s", want, attempts)
		}
	}
	if n := strings.Count(attempts, `href="/calls/`); n != 2 {
		t.Errorf("%d links to other attempts, want 2:\n%s", n, attempts)
	}
}

func TestOpeningACallsPageRecordsNoOpen(t *testing.T) {
	store := openStore(t)
	failThrice(t, store)
	app, _ := newAppWithLog(t, saturday, store)

	if res := get(t, app, "/calls/1"); res.status != 200 {
		t.Fatalf("status %d", res.status)
	}

	rec, err := store.RegisterRecord(t.Context(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.BriefingOpens) != 0 || len(rec.SundayReadOpens) != 0 {
		t.Errorf("opening a call's page recorded opens: %v %v", rec.BriefingOpens, rec.SundayReadOpens)
	}
}

func TestWhatACallSentAndGotIsInertOnItsPage(t *testing.T) {
	store := openStore(t)
	readSpeech(t, store, strings.Replace(reading, "Andy Burnham spoke in Motherwell.", `<span hx-get=\"/evil\">Andy</span>`, 1))
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/calls/1")

	if strings.Contains(res.body, "<span hx-get") {
		t.Errorf("model output was not escaped:\n%s", res.body)
	}
	for _, pre := range regexp.MustCompile(`<pre[^>]*>`).FindAllString(res.body, -1) {
		if !strings.Contains(pre, "hx-disable") {
			t.Errorf("%s is not inert", pre)
		}
	}
}

func TestACallNotInTheRecordIsNotFound(t *testing.T) {
	app := newApp(t, saturday)

	for _, path := range []string{"/calls/99", "/calls/x"} {
		if res := get(t, app, path); res.status != 404 {
			t.Errorf("%s: status = %d, want 404", path, res.status)
		}
	}
}
