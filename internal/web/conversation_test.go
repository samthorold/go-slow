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
)

// lookThenSubmit is an agent that looks at a Story, then submits read.
type lookThenSubmit struct {
	read  string
	turns *int
}

func (l lookThenSubmit) Turn(context.Context, editor.ConversationRequest) (editor.Turn, error) {
	*l.turns++
	if *l.turns == 1 {
		return editor.Turn{Model: "fake-agent", Text: `Looking at <b hx-get="/evil">S1</b>.`, StopReason: "tool_use",
			Raw:   json.RawMessage(`{"content": [{"type": "thinking", "thinking": "<i hx-get=\"/evil\">hmm</i>"}]}`),
			Calls: []editor.ToolCall{{ID: "1", Name: "story", Input: json.RawMessage(`{"story": "S1"}`)}},
			Usage: editor.Usage{InputTokens: 100, OutputTokens: 10}}, nil
	}
	return editor.Turn{Model: "fake-agent", Calls: []editor.ToolCall{{ID: "2", Name: "submit_read", Input: json.RawMessage(l.read)}},
		Usage: editor.Usage{InputTokens: 200, OutputTokens: 20}}, nil
}

func TestTheSundayReadAndItsAssessmentsLinkToTheConversationShownTurnByTurn(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	var turns int
	ed := editor.New(editor.Deps{Store: store, SundayReads: store, SundayReadModel: lookThenSubmit{assessed, &turns},
		Wikidata: noWikidata{}, Log: slog.New(slog.DiscardHandler)})
	if err := ed.SundayRead(t.Context(), sunday); err != nil {
		t.Fatal(err)
	}
	app, _ := newAppWithLog(t, sunday, store)

	read := get(t, app, "/sunday-reads/2026-10-25")
	if !strings.Contains(read.body, `href="/conversations/1"`) {
		t.Errorf("the read does not link its conversation:\n%s", read.body)
	}
	// The Assessment links to the turn that reached it.
	section := regexp.MustCompile(`(?s)<section id="assessment-1">.*?</section>`).FindString(read.body)
	if !strings.Contains(section, `href="/conversations/1#turn-2"`) {
		t.Errorf("the Assessment does not link the turn that reached it:\n%s", section)
	}

	res := get(t, app, "/conversations/1")
	if res.status != 200 || !strings.Contains(res.body, "<!doctype html>") {
		t.Fatalf("status %d, not a full page:\n%s", res.status, res.body)
	}
	for _, want := range []string{
		`href="/sunday-reads/2026-10-25"`, `id="turn-1"`, `id="turn-2"`,
		"Andy Burnham in Scotland",          // the week, as the agent was first shown it
		"Andy Burnham spoke in Motherwell.", // the Story tool's result, sent with turn 2
		"submit_read", "The Prime Minister is courting Scottish voters.",
		"continued", "applied", "330 tokens",
	} {
		if !strings.Contains(res.body, want) {
			t.Errorf("the conversation's page does not show %q:\n%s", want, res.body)
		}
	}
	// What the model said and was sent is inert.
	if strings.Contains(res.body, "<b hx-get") || strings.Contains(res.body, "<i hx-get") {
		t.Errorf("model output was not escaped:\n%s", res.body)
	}
	for _, pre := range regexp.MustCompile(`<pre[^>]*>`).FindAllString(res.body, -1) {
		if !strings.Contains(pre, "hx-disable") {
			t.Errorf("%s is not inert", pre)
		}
	}
	// Checking how the read was written is not reading it.
	rec, err := store.RegisterRecord(t.Context(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.SundayReadOpens) != 1 {
		t.Errorf("opens = %v, want only the read's own", rec.SundayReadOpens)
	}
}

// sundayConversation is the page of a Sunday read written by an agent that
// looks at a Story, then submits.
func sundayConversation(t *testing.T) string {
	t.Helper()
	store := openStore(t)
	speechStory(t, store)
	var turns int
	ed := editor.New(editor.Deps{Store: store, SundayReads: store, SundayReadModel: lookThenSubmit{assessed, &turns},
		Wikidata: noWikidata{}, Log: slog.New(slog.DiscardHandler)})
	if err := ed.SundayRead(t.Context(), sunday); err != nil {
		t.Fatal(err)
	}
	app, _ := newAppWithLog(t, sunday, store)
	return get(t, app, "/conversations/1").body
}

func TestAConversationsPageOpensWithAOneLineSummaryHeadItsOutcomeHued(t *testing.T) {
	head := summaryHead(t, sundayConversation(t))

	for _, want := range []string{`<span class="status status-supported">applied</span>`, "2 turns", "330 tokens"} {
		if !strings.Contains(head, want) {
			t.Errorf("the summary head lacks %q:\n%s", want, head)
		}
	}
}

func TestAConversationsTurnsAreBlocksWithTheirNotesInAnOpenMargin(t *testing.T) {
	page := sundayConversation(t)

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
	// The tools it called, briefly, are what it did: in the body.
	if !strings.Contains(first.body, "story") {
		t.Errorf("turn 1's body does not say it called the Story tool:\n%s", first.body)
	}
	second := blockOf(t, page, "Called <code hx-disable>submit_read</code>")
	for _, want := range []string{`href="#turn-2"`, "220 tokens", `<span class="status status-supported">applied</span>`} {
		if !strings.Contains(second.margin, want) {
			t.Errorf("turn 2's margin lacks %q:\n%s", want, second.margin)
		}
	}
	// #turn-N links land on the turn's block.
	for _, n := range []string{"1", "2"} {
		if !regexp.MustCompile(`<li class="block" id="turn-` + n + `"`).MatchString(page) {
			t.Errorf("no block carries id turn-%s:\n%s", n, page)
		}
	}
	if got := disclosure(t, page, "As the provider returned it"); got != "collapsed" {
		t.Errorf("a turn's raw form is %s, want collapsed", got)
	}
}

func TestAFailedReadLinksTheConversationOfItsAttempt(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeSundayRead(t, store, sunday, `{"statements": []}`)
	app, _ := newAppWithLog(t, sunday, store)

	read := get(t, app, "/sunday-reads/2026-10-25")
	if !strings.Contains(read.body, `href="/conversations/1"`) {
		t.Errorf("the failure does not link its conversation:\n%s", read.body)
	}
	res := get(t, app, "/conversations/1")
	if res.status != 200 || !strings.Contains(res.body, "refused") || !strings.Contains(res.body, "no statements") {
		t.Errorf("status %d; want the refused turns with their reason:\n%s", res.status, res.body)
	}
}

func TestAConversationNotInTheRecordIsNotFound(t *testing.T) {
	app := newApp(t, sunday)

	for _, path := range []string{"/conversations/99", "/conversations/x"} {
		if res := get(t, app, path); res.status != 404 {
			t.Errorf("%s: status = %d, want 404", path, res.status)
		}
	}
}
