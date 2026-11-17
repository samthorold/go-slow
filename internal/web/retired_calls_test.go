package web_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite/sqlitetest"
)

// sunday is the day after saturday, when the retired Sunday read ran.
var sunday = saturday.Add(24 * time.Hour)

// retiredRecord is a Record holding what the retired acts left: a kept
// Briefing call and relating call, and the Sunday read's kept conversation,
// two turns long, its agent's words and its sent tool result carrying markup.
func retiredRecord(t *testing.T) (http.Handler, int64, int64) {
	t.Helper()
	path := sqlitetest.Path(t)
	// An older build kept the Sunday read's conversation; nothing writes one
	// now, so it is written here as that build left it.
	sqlitetest.Exec(t, path,
		`INSERT INTO call_texts (hash, text) VALUES ('i', 'Read the week.'), ('t', '[{"name":"story"},{"name":"submit_read"}]')`,
		`INSERT INTO conversations (id, act, work_key, at, model, instructions, tools, outcome, why)
			VALUES (1, 'sunday_read', '2026-10-25', '2026-10-25T07:30:00.000000000Z', 'claude-test', 'i', 't', 'applied', '')`,
		`INSERT INTO conversation_turns (conversation_id, turn, at, request_hash, sent, response, text, calls,
			reported_model, stop_reason, input_tokens, output_tokens, cache_read_tokens, outcome, why) VALUES
			(1, 1, '2026-10-25T07:29:00.000000000Z', 'h1', '[{"user":"The week: Derby will host the GBR headquarters."}]',
				'{"content": []}', 'Looking at <b hx-get="/evil">S1</b>.', '[{"id":"1","name":"story","input":{"span":"P1"}}]',
				'fake-agent', 'tool_use', 100, 10, 0, 'continued', ''),
			(1, 2, '2026-10-25T07:30:00.000000000Z', 'h2', '[{"results":[{"call_id":"1","content":"This is fantastic news"}]}]',
				'{"content": []}', '', '[{"id":"2","name":"submit_read","input":{}}]',
				'fake-agent', 'end_turn', 200, 20, 0, 'applied', '')`,
	)
	store := openStoreAt(t, path)
	keep := func(act, key string) int64 {
		req := editor.Request{Act: act, Instructions: "Write.", Input: "S1: Derby will host the headquarters.", Schema: map[string]any{}}
		if err := store.KeepCall(t.Context(), editor.Call{Act: act, Key: key, At: saturday, Model: "m",
			Request: req, RequestHash: editor.RequestHash(req, "m"), Precedents: []int64{3}, Outcome: editor.Applied,
			Response: &editor.Response{Output: json.RawMessage(`{"statements": []}`), Model: "m"}}); err != nil {
			t.Fatal(err)
		}
		calls, err := store.CallsOn(t.Context(), act, key)
		if err != nil || len(calls) != 1 {
			t.Fatalf("calls on %s: %v %v", act, calls, err)
		}
		return calls[0].ID
	}
	seed(t, store, saturday.AddDate(0, 0, -7), item("speech", "PM speech in Scotland", saturday.Add(-2*time.Hour)))
	briefing := keep("briefing", "2026-10-24")
	relating := keep("relating", number10.ID+"\nspeech")
	app, _ := newAppWithLog(t, sunday, store)
	return app, briefing, relating
}

func TestARetiredActsKeptCallsStayReadableByTheirPages(t *testing.T) {
	app, briefing, relating := retiredRecord(t)

	for id, act := range map[int64]string{briefing: "briefing", relating: "relating"} {
		res := get(t, app, callPath(id))
		if res.status != http.StatusOK {
			t.Fatalf("GET %s: status %d", callPath(id), res.status)
		}
		for _, want := range []string{act, "applied", "S1: Derby will host the headquarters.", "Shown as precedent the Feedback 3."} {
			if !strings.Contains(res.body, want) {
				t.Errorf("the %s call's page lacks %q:\n%s", act, want, res.body)
			}
		}
	}
	// The Briefing's work is its day, shown but linking no page: the
	// Briefing's is gone.
	res := get(t, app, callPath(briefing))
	if strings.Contains(res.body, `href="/briefings`) || !strings.Contains(res.body, "2026-10-24") {
		t.Errorf("the Briefing call's work is not its day, unlinked:\n%s", summaryHead(t, res.body))
	}
	// The relating call's work is its Item, still linked.
	if res := get(t, app, callPath(relating)); !strings.Contains(summaryHead(t, res.body), `href="/items/`) {
		t.Errorf("the relating call does not link its Item:\n%s", res.body)
	}
}

func callPath(id int64) string { return "/calls/" + strconv.FormatInt(id, 10) }

func TestTheSundayReadsKeptConversationIsShownTurnByTurn(t *testing.T) {
	app, _, _ := retiredRecord(t)

	res := get(t, app, "/conversations/1")

	if res.status != http.StatusOK || !strings.Contains(res.body, "<!doctype html>") {
		t.Fatalf("status %d, not a full page:\n%s", res.status, res.body)
	}
	for _, want := range []string{
		`id="turn-1"`, `id="turn-2"`, "2026-10-25",
		"Derby will host the GBR headquarters.", // as the agent was first shown it
		"This is fantastic news",                // the tool's result, sent with turn 2
		"submit_read", "continued", "applied", "330 tokens",
	} {
		if !strings.Contains(res.body, want) {
			t.Errorf("the conversation's page does not show %q:\n%s", want, res.body)
		}
	}
	if strings.Contains(res.body, `href="/sunday-reads`) {
		t.Errorf("the conversation links the Sunday read's page, which is gone:\n%s", res.body)
	}
	// What the model said and was sent is inert.
	if strings.Contains(res.body, "<b hx-get") {
		t.Errorf("model output was not escaped:\n%s", res.body)
	}
	for _, pre := range regexp.MustCompile(`<pre[^>]*>`).FindAllString(res.body, -1) {
		if !strings.Contains(pre, "hx-disable") {
			t.Errorf("%s is not inert", pre)
		}
	}
}

func TestAConversationsPageOpensWithAOneLineSummaryHeadItsOutcomeHued(t *testing.T) {
	app, _, _ := retiredRecord(t)

	head := summaryHead(t, get(t, app, "/conversations/1").body)

	for _, want := range []string{`<span class="status status-supported">applied</span>`, "2 turns", "330 tokens"} {
		if !strings.Contains(head, want) {
			t.Errorf("the summary head lacks %q:\n%s", want, head)
		}
	}
}

func TestAConversationsTurnsAreBlocksWithTheirNotesInAnOpenMargin(t *testing.T) {
	app, _, _ := retiredRecord(t)
	page := get(t, app, "/conversations/1").body

	regions := regexp.MustCompile(`<div class="margined" data-margin="(open|folded)">`).FindAllStringSubmatch(page, -1)
	if len(regions) != 1 || regions[0][1] != "open" {
		t.Fatalf("want one margined region, open; got %v:\n%s", regions, page)
	}
	first := blockOf(t, page, "Looking at")
	for _, want := range []string{`href="#turn-1"`, "110 tokens", `class="status">continued</span>`} {
		if !strings.Contains(first.margin, want) {
			t.Errorf("turn 1's margin lacks %q:\n%s", want, first.margin)
		}
	}
	if !strings.Contains(first.body, "story") {
		t.Errorf("turn 1's body does not say it called the Story tool:\n%s", first.body)
	}
	second := blockOf(t, page, "Called <code hx-disable>submit_read</code>")
	for _, want := range []string{`href="#turn-2"`, "220 tokens", `<span class="status status-supported">applied</span>`} {
		if !strings.Contains(second.margin, want) {
			t.Errorf("turn 2's margin lacks %q:\n%s", want, second.margin)
		}
	}
	if got := disclosure(t, page, "As the provider returned it"); got != "collapsed" {
		t.Errorf("a turn's raw form is %s, want collapsed", got)
	}
}

func TestAConversationNotInTheRecordIsNotFound(t *testing.T) {
	app := newApp(t, sunday)

	for _, path := range []string{"/conversations/99", "/conversations/x"} {
		if res := get(t, app, path); res.status != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, res.status)
		}
	}
}
