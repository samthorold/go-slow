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

// api is a stand-in for Anthropic's Messages API on a local server: it
// answers with a message stopping for stopReason, and keeps the request body.
func api(t *testing.T, stopReason, text string) (*claude.Model, *map[string]any) {
	t.Helper()
	return apiReporting(t, "claude-haiku-5-5", stopReason, text)
}

// apiReporting is api, its messages reporting they were answered by
// reported, whatever model was asked for.
func apiReporting(t *testing.T, reported, stopReason, text string) (*claude.Model, *map[string]any) {
	t.Helper()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("X-Api-Key") != "test-key" {
			t.Errorf("request to %s with key %q", r.URL.Path, r.Header.Get("X-Api-Key"))
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": reported,
			"content":     []map[string]any{{"type": "text", "text": text}},
			"stop_reason": stopReason,
			"usage":       map[string]any{"input_tokens": 1200, "output_tokens": 340},
		})
	}))
	t.Cleanup(srv.Close)
	return claude.New(claude.Options{APIKey: "test-key", Model: "claude-haiku-5-5", BaseURL: srv.URL}), &got
}

var request = editor.Request{
	Act:          "reading",
	Instructions: "Read the Item.",
	Input:        "Title: PM speech in Scotland",
	Schema:       map[string]any{"type": "object", "additionalProperties": false},
}

func TestTheModelIsCalledWithTheActsInstructionsInputAndSchema(t *testing.T) {
	model, got := api(t, "end_turn", `{"facts": []}`)

	res, err := model.Complete(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}

	body := *got
	if body["model"] != "claude-haiku-5-5" {
		t.Errorf("model = %v", body["model"])
	}
	if system, _ := json.Marshal(body["system"]); string(system) != `[{"text":"Read the Item.","type":"text"}]` {
		t.Errorf("system = %s", system)
	}
	if msgs, _ := json.Marshal(body["messages"]); string(msgs) != `[{"content":[{"text":"Title: PM speech in Scotland","type":"text"}],"role":"user"}]` {
		t.Errorf("messages = %s", msgs)
	}
	if format, _ := json.Marshal(body["output_config"]); string(format) != `{"format":{"schema":{"additionalProperties":false,"type":"object"},"type":"json_schema"}}` {
		t.Errorf("output_config = %s", format)
	}
	if string(res.Output) != `{"facts": []}` || res.Model != "claude-haiku-5-5" {
		t.Errorf("response = %+v", res)
	}
	if res.Usage != (editor.Usage{InputTokens: 1200, OutputTokens: 340}) {
		t.Errorf("usage = %+v", res.Usage)
	}
	// The whole message as returned, for the kept call.
	var raw map[string]any
	if err := json.Unmarshal(res.Raw, &raw); err != nil || raw["id"] != "msg_1" || res.StopReason != "end_turn" {
		t.Errorf("raw = %s, stop reason %q", res.Raw, res.StopReason)
	}
}

func TestAResponseKeepsTheModelTheAPIReportsNotTheOneAskedFor(t *testing.T) {
	model, got := apiReporting(t, "claude-haiku-5-5-moved", "end_turn", `{"facts": []}`)

	res, err := model.Complete(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}

	if (*got)["model"] != "claude-haiku-5-5" || res.Model != "claude-haiku-5-5-moved" {
		t.Errorf("asked for %v, kept %q, want the model reported", (*got)["model"], res.Model)
	}
}

func TestTheModelTheAdapterRunsEachActOnIsNamed(t *testing.T) {
	m := claude.New(claude.Options{APIKey: "k", Model: "claude-haiku-5-5", ActModels: map[string]string{"placing": "claude-sonnet-5"}})

	if m.ModelFor("reading") != "claude-haiku-5-5" || m.ModelFor("placing") != "claude-sonnet-5" {
		t.Errorf("reading on %q, placing on %q", m.ModelFor("reading"), m.ModelFor("placing"))
	}
}

func TestARefusalOrATruncatedAnswerIsNoOutput(t *testing.T) {
	for stop, want := range map[string]error{
		"refusal":    editor.ErrRefused,
		"max_tokens": editor.ErrTruncated,
	} {
		t.Run(stop, func(t *testing.T) {
			model, _ := api(t, stop, `{"facts": [`)

			res, err := model.Complete(t.Context(), request)

			if !errors.Is(err, want) {
				t.Errorf("err = %v, want %v", err, want)
			}
			// What came back is still returned, to be kept with the call.
			if res.StopReason != stop || !strings.Contains(string(res.Raw), `"stop_reason":"`+stop+`"`) ||
				res.Model != "claude-haiku-5-5" || res.Usage.InputTokens != 1200 {
				t.Errorf("response = %+v (%s)", res, res.Raw)
			}
		})
	}
}
