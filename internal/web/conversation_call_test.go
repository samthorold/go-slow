package web_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/editor"
)

// A weaving run's kept call holds its whole conversation: its page shows
// every ask the run made and what the Record answered, then the answer
// (system design: A weaving run is shown what is new, and finds for itself
// what bears on it: its asks and their answers are kept with its call).
func TestAConversationsCallPageShowsEveryAskAndItsAnswer(t *testing.T) {
	store := openStore(t)
	req := editor.Request{Act: "weaving", Instructions: "Weave.", Input: "Story from \"Fairford\"\nS1: An attack.\n",
		Schema: map[string]any{}, Tools: []editor.Tool{{Name: "find", Description: "Find by words."}},
		Messages: []editor.Message{
			{Assistant: &editor.Turn{Text: "Looking back.", Calls: []editor.ToolCall{
				{ID: "toolu_1", Name: "find", Input: json.RawMessage(`{"words":"Houthis"}`)}}}},
			{Results: []editor.ToolResult{{CallID: "toolu_1", Content: "1 earlier Story has Spans with the words \"Houthis\"."}}},
			{Assistant: &editor.Turn{Calls: []editor.ToolCall{
				{ID: "toolu_2", Name: "open_story", Input: json.RawMessage(`{"story":99}`)}}}},
			{Results: []editor.ToolResult{{CallID: "toolu_2", Content: "There is no earlier Story 99.", IsError: true}}},
		}}
	c := editor.Call{Act: "weaving", Key: "2026-10-25", At: saturday, BatchID: "batch-1", Model: "m", Request: req,
		RequestHash: editor.RequestHash(req, "m"), Outcome: editor.Applied,
		Response: &editor.Response{Output: json.RawMessage(`{"stories": []}`), Model: "m"}}
	if err := store.KeepCall(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	calls, err := store.CallsOn(t.Context(), "weaving", "2026-10-25")
	if err != nil || len(calls) != 1 || len(calls[0].Request.Messages) != 4 || len(calls[0].Request.Tools) != 1 {
		t.Fatalf("kept %+v (%v), want the call with its conversation", calls, err)
	}
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/calls/"+strconv.FormatInt(calls[0].ID, 10))

	for _, want := range []string{
		"Looking back.", "find", "&#34;words&#34;: &#34;Houthis&#34;", "1 earlier Story has Spans with the words &#34;Houthis&#34;.",
		"open_story", "There is no earlier Story 99.", "an error", "Find by words.",
	} {
		if !strings.Contains(res.body, want) {
			t.Errorf("the call's page lacks %q:\n%s", want, res.body)
		}
	}
	if i, j := strings.Index(res.body, "&#34;words&#34;: &#34;Houthis&#34;"), strings.Index(res.body, "There is no earlier Story 99."); i < 0 || j < i {
		t.Errorf("the asks are not shown in the order made")
	}
}
