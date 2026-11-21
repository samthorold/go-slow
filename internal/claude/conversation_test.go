package claude_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/editor"
)

// conversing is a weaving run's third turn: its tools, and the turns before
// it as they came back, each with the results sent back.
var conversing = editor.Request{
	Act: "weaving", Instructions: "Weave.", Input: "S1: Ferries.", Effort: editor.EffortMedium,
	Schema: map[string]any{"type": "object", "additionalProperties": false},
	Tools: []editor.Tool{{Name: "find", Description: "Find by words.",
		Schema: map[string]any{"type": "object", "properties": map[string]any{"words": map[string]any{"type": "string"}},
			"required": []string{"words"}, "additionalProperties": false}}},
	Messages: []editor.Message{
		{Assistant: &editor.Turn{Raw: json.RawMessage(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5",
			"content":[{"type":"thinking","thinking":"","signature":"sig-1"},
				{"type":"tool_use","id":"toolu_1","name":"find","input":{"words":"ferries"}}],
			"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":5}}`)}},
		{Results: []editor.ToolResult{{CallID: "toolu_1", Content: "No earlier Story."}}},
		{Assistant: &editor.Turn{Raw: json.RawMessage(`{"id":"msg_2","type":"message","role":"assistant","model":"claude-opus-5-5",
			"content":[{"type":"tool_use","id":"toolu_2","name":"open_story","input":{"story":9}}],
			"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":5}}`)}},
		{Results: []editor.ToolResult{{CallID: "toolu_2", Content: "There is no Story 9.", IsError: true}}},
	},
}

// A conversation's turn is sent with its tools as strict tools beside its
// output format, its input as the opening user turn, then each turn the
// model took passed back as it came (thinking included) and the results sent
// back as tool results; cached, since every turn sends what the last did.
func TestAConversationsTurnIsSentWithItsToolsAndTheTurnsBeforeIt(t *testing.T) {
	model, created := batchAPI(t, "in_progress")

	if _, err := model.Submit(t.Context(), []editor.BatchRequest{{ID: "weaving-1", Request: conversing}}); err != nil {
		t.Fatal(err)
	}

	reqs, _ := (*created)["requests"].([]any)
	params, _ := reqs[0].(map[string]any)["params"].(map[string]any)
	got, _ := json.Marshal(params)
	for _, want := range []string{
		`"tools":[{"description":"Find by words.","input_schema":{"additionalProperties":false,"properties":{"words":{"type":"string"}},"required":["words"],"type":"object"},"name":"find","strict":true}]`,
		`"output_config":{"effort":"medium","format":`,
		`{"content":[{"text":"S1: Ferries.","type":"text"}],"role":"user"}`,
		`{"signature":"sig-1","thinking":"","type":"thinking"}`,
		`{"content":[{"content":[{"text":"No earlier Story.","type":"text"}],"is_error":false,"tool_use_id":"toolu_1","type":"tool_result"}],"role":"user"}`,
		`"is_error":true,"tool_use_id":"toolu_2"`,
		`"cache_control":{"type":"ephemeral"}`,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("sent %s\nwant it to hold %s", got, want)
		}
	}
	if msgs, _ := params["messages"].([]any); len(msgs) != 5 {
		t.Errorf("sent %d messages, want the opening and four after it", len(msgs))
	}
}

// A request that is no conversation is sent as it was: no tools, no cache.
func TestARequestWithNoConversationIsSentWithNoToolsOrCache(t *testing.T) {
	model, created := batchAPI(t, "in_progress")

	if _, err := model.Submit(t.Context(), []editor.BatchRequest{{ID: "hydration-1", Request: request}}); err != nil {
		t.Fatal(err)
	}

	reqs, _ := (*created)["requests"].([]any)
	params, _ := reqs[0].(map[string]any)["params"].(map[string]any)
	if _, ok := params["tools"]; ok {
		t.Errorf("sent tools: %v", params)
	}
	if _, ok := params["cache_control"]; ok {
		t.Errorf("sent cache_control: %v", params)
	}
}

// A turn that calls tools comes back with its calls, its text, and the turn
// as it came, to pass back.
func TestATurnCallingToolsComesBackWithItsCalls(t *testing.T) {
	msg := message("tool_use", "Let me look.")
	msg["content"] = []map[string]any{{"type": "text", "text": "Let me look."},
		{"type": "tool_use", "id": "toolu_1", "name": "find", "input": map[string]any{"words": "Fairford"}}}
	model, _ := batchAPI(t, "ended", map[string]any{"custom_id": "weaving-1", "result": map[string]any{"type": "succeeded", "message": msg}})

	poll, err := model.Poll(t.Context(), "msgbatch_1")
	if err != nil {
		t.Fatal(err)
	}

	r := poll.Results[0]
	if r.Err != nil || len(r.Response.Calls) != 1 || r.Response.Calls[0].ID != "toolu_1" || r.Response.Calls[0].Name != "find" ||
		string(r.Response.Calls[0].Input) != `{"words":"Fairford"}` || string(r.Response.Output) != "Let me look." ||
		r.Response.StopReason != "tool_use" || !strings.Contains(string(r.Response.Raw), `"tool_use"`) {
		t.Errorf("result %+v", r)
	}
}
