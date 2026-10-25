package claude_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/claude"
	"github.com/samthorold/go-slow/internal/editor"
)

// conversationAPI is a stand-in for the Messages API answering with message,
// keeping each request's body and headers.
func conversationAPI(t *testing.T, message map[string]any) (*claude.Model, *[]map[string]any, *http.Header) {
	t.Helper()
	var bodies []map[string]any
	var headers http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("request to %s", r.URL.Path)
		}
		headers = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(b, &body); err != nil {
			t.Error(err)
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(message)
	}))
	t.Cleanup(srv.Close)
	return claude.New(claude.Options{
		APIKey: "test-key", Model: "claude-haiku-5-5", BaseURL: srv.URL,
		ActModels: map[string]string{"sunday_read": "claude-opus-5-5"},
	}), &bodies, &headers
}

// usesATool is a turn of Claude's: it thinks, says a little, and calls a
// tool.
var usesATool = map[string]any{
	"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
	"content": []map[string]any{
		{"type": "thinking", "thinking": "", "signature": "sig-1"},
		{"type": "text", "text": "Looking at the Story."},
		{"type": "tool_use", "id": "toolu_1", "name": "story", "input": map[string]any{"story": "S1"}},
	},
	"stop_reason": "tool_use",
	"usage":       map[string]any{"input_tokens": 900, "output_tokens": 120, "cache_read_input_tokens": 4000, "cache_creation_input_tokens": 300},
}

var conversation = editor.ConversationRequest{
	Act:          "sunday_read",
	Instructions: "Write the Sunday read.",
	Tools: []editor.Tool{{Name: "story", Description: "A Story.", Schema: map[string]any{
		"type": "object", "properties": map[string]any{"story": map[string]any{"type": "string"}},
		"required": []string{"story"}, "additionalProperties": false,
	}}},
	Messages: []editor.Message{{User: "The week's Stories."}},
}

func TestATurnIsCalledWithTheActsToolsAndAdaptiveThinkingOnItsOwnModel(t *testing.T) {
	model, bodies, headers := conversationAPI(t, usesATool)

	if _, err := model.Turn(t.Context(), conversation); err != nil {
		t.Fatal(err)
	}

	body := (*bodies)[0]
	if body["model"] != "claude-opus-5-5" {
		t.Errorf("model = %v, want the act's own", body["model"])
	}
	if system, _ := json.Marshal(body["system"]); string(system) != `[{"text":"Write the Sunday read.","type":"text"}]` {
		t.Errorf("system = %s", system)
	}
	tools, _ := json.Marshal(body["tools"])
	for _, want := range []string{`"name":"story"`, `"strict":true`, `"description":"A Story."`, `"input_schema":{`, `"additionalProperties":false`} {
		if !strings.Contains(string(tools), want) {
			t.Errorf("tools = %s, want %s", tools, want)
		}
	}
	if thinking, _ := json.Marshal(body["thinking"]); string(thinking) != `{"type":"adaptive"}` {
		t.Errorf("thinking = %s", thinking)
	}
	if out, _ := json.Marshal(body["output_config"]); string(out) != `{"effort":"high"}` {
		t.Errorf("output_config = %s", out)
	}
	// A refusal is retried on the model Anthropic recommends for its
	// category, and the conversation's prefix is cached.
	if body["fallbacks"] != "default" || !strings.Contains(headers.Get("Anthropic-Beta"), "server-side-fallback-2026-07-01") {
		t.Errorf("fallbacks = %v, beta = %q", body["fallbacks"], headers.Get("Anthropic-Beta"))
	}
	if cc, _ := json.Marshal(body["cache_control"]); string(cc) != `{"type":"ephemeral"}` {
		t.Errorf("cache_control = %s", cc)
	}
	if _, ok := body["tool_choice"]; ok {
		t.Errorf("tool_choice = %v, want it left to the model", body["tool_choice"])
	}
}

func TestATurnsToolCallsComeBackAndItIsPassedBackUnchanged(t *testing.T) {
	model, bodies, _ := conversationAPI(t, usesATool)

	turn, err := model.Turn(t.Context(), conversation)
	if err != nil {
		t.Fatal(err)
	}

	if turn.Text != "Looking at the Story." || turn.Model != "claude-opus-5-5" || len(turn.Calls) != 1 {
		t.Fatalf("turn = %+v", turn)
	}
	if c := turn.Calls[0]; c.ID != "toolu_1" || c.Name != "story" || string(c.Input) != `{"story":"S1"}` {
		t.Errorf("call = %+v", c)
	}
	if turn.Usage != (editor.Usage{InputTokens: 1200, OutputTokens: 120, CacheReadTokens: 4000}) {
		t.Errorf("usage = %+v", turn.Usage)
	}

	// The next turn sends the conversation with this turn as it came,
	// thinking and all, and the tool's result.
	next := conversation
	next.Messages = append(next.Messages,
		editor.Message{Assistant: &turn},
		editor.Message{Results: []editor.ToolResult{{CallID: "toolu_1", Content: "no such Story", IsError: true}}},
	)
	if _, err := model.Turn(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	msgs, _ := json.Marshal((*bodies)[1]["messages"])
	for _, want := range []string{
		`{"content":[{"signature":"sig-1","thinking":"","type":"thinking"},{"text":"Looking at the Story.","type":"text"},{"id":"toolu_1","input":{"story":"S1"},"name":"story","type":"tool_use"}],"role":"assistant"}`,
		`{"content":[{"content":[{"text":"no such Story","type":"text"}],"is_error":true,"tool_use_id":"toolu_1","type":"tool_result"}],"role":"user"}`,
	} {
		if !strings.Contains(string(msgs), want) {
			t.Errorf("messages = %s\nwant %s", msgs, want)
		}
	}
}

func TestARefusedOrTruncatedTurnIsAnError(t *testing.T) {
	for stop, want := range map[string]error{"refusal": editor.ErrRefused, "max_tokens": editor.ErrTruncated} {
		t.Run(stop, func(t *testing.T) {
			msg := map[string]any{
				"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
				"content": []map[string]any{}, "stop_reason": stop, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
			}
			model, _, _ := conversationAPI(t, msg)

			if _, err := model.Turn(t.Context(), conversation); !errors.Is(err, want) {
				t.Errorf("err = %v, want %v", err, want)
			}
		})
	}
}
