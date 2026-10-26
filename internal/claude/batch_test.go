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

// batchAPI is a stand-in for Anthropic's Message Batches API: it accepts a
// batch, reports it as status, and serves results as JSON lines. It keeps
// the body of the batch created.
func batchAPI(t *testing.T, status string, results ...map[string]any) (*claude.Model, *map[string]any) {
	t.Helper()
	return batchAPIWith(t, claude.Options{Model: "claude-haiku-5-5"}, status, results...)
}

// batchAPIWith is batchAPI with the adapter built from o.
func batchAPIWith(t *testing.T, o claude.Options, status string, results ...map[string]any) (*claude.Model, *map[string]any) {
	t.Helper()
	var created map[string]any
	batch := func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msgbatch_1", "type": "message_batch", "processing_status": status,
			"request_counts": map[string]any{"processing": 0, "succeeded": 0, "errored": 0, "canceled": 0, "expired": 0},
			"created_at":     "2026-10-24T07:00:00Z", "expires_at": "2026-10-25T07:00:00Z",
		})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "test-key" {
			t.Errorf("request with key %q", r.Header.Get("X-Api-Key"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages/batches":
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &created); err != nil {
				t.Error(err)
			}
			batch(w)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/messages/batches/msgbatch_1":
			batch(w)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/messages/batches/msgbatch_1/results":
			if status != "ended" {
				t.Error("results fetched before the batch ended")
			}
			w.Header().Set("Content-Type", "application/x-jsonl")
			enc := json.NewEncoder(w)
			for _, res := range results {
				_ = enc.Encode(res)
			}
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	o.APIKey, o.BaseURL = "test-key", srv.URL
	return claude.New(o), &created
}

func TestEachRequestInABatchRunsOnItsActsModel(t *testing.T) {
	model, created := batchAPIWith(t, claude.Options{
		Model: "claude-haiku-5-5", ActModels: map[string]string{"placing": "claude-sonnet-5-5"},
	}, "in_progress")
	placing := request
	placing.Act = "placing"

	if _, err := model.Submit(t.Context(), []editor.BatchRequest{
		{ID: "reading-1", Request: request},
		{ID: "placing-1", Request: placing},
	}); err != nil {
		t.Fatal(err)
	}

	reqs, _ := (*created)["requests"].([]any)
	if len(reqs) != 2 {
		t.Fatalf("batch body = %v", *created)
	}
	for i, want := range []string{"claude-haiku-5-5", "claude-sonnet-5-5"} {
		params, _ := reqs[i].(map[string]any)["params"].(map[string]any)
		if params["model"] != want {
			t.Errorf("request %d on %v, want %s", i, params["model"], want)
		}
	}
}

func message(stopReason, text string) map[string]any {
	return map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-haiku-5-5",
		"content":     []map[string]any{{"type": "text", "text": text}},
		"stop_reason": stopReason,
		"usage":       map[string]any{"input_tokens": 1200, "output_tokens": 340},
	}
}

func TestABatchIsSubmittedWithEachRequestsIdentifierAndTheActsCall(t *testing.T) {
	model, created := batchAPI(t, "in_progress")

	id, err := model.Submit(t.Context(), []editor.BatchRequest{
		{ID: "reading-1", Request: request},
		{ID: "reading-2", Request: request},
	})
	if err != nil {
		t.Fatal(err)
	}

	if id != "msgbatch_1" {
		t.Errorf("batch id = %q", id)
	}
	reqs, _ := (*created)["requests"].([]any)
	if len(reqs) != 2 {
		t.Fatalf("requests = %v", (*created)["requests"])
	}
	first, _ := json.Marshal(reqs[0])
	want := `{"custom_id":"reading-1","params":{"max_tokens":32000,` +
		`"messages":[{"content":[{"text":"Title: PM speech in Scotland","type":"text"}],"role":"user"}],` +
		`"model":"claude-haiku-5-5",` +
		`"output_config":{"format":{"schema":{"additionalProperties":false,"type":"object"},"type":"json_schema"}},` +
		`"system":[{"text":"Read the Item.","type":"text"}]}}`
	if string(first) != want {
		t.Errorf("first request =\n%s\nwant\n%s", first, want)
	}
}

func TestABatchResultKeepsTheModelTheAPIReportsNotTheOneAskedFor(t *testing.T) {
	moved := message("end_turn", `{"facts": []}`)
	moved["model"] = "claude-haiku-5-5-moved"
	model, _ := batchAPI(t, "ended",
		map[string]any{"custom_id": "reading-1", "result": map[string]any{"type": "succeeded", "message": moved}})

	poll, err := model.Poll(t.Context(), "msgbatch_1")
	if err != nil {
		t.Fatal(err)
	}

	if len(poll.Results) != 1 || poll.Results[0].Response.Model != "claude-haiku-5-5-moved" {
		t.Errorf("results = %+v, want the model reported", poll.Results)
	}
}

func TestABatchStillProcessingHasNoResults(t *testing.T) {
	model, _ := batchAPI(t, "in_progress")

	poll, err := model.Poll(t.Context(), "msgbatch_1")
	if err != nil {
		t.Fatal(err)
	}

	if poll.Ended || len(poll.Results) != 0 {
		t.Errorf("poll = %+v, want not ended", poll)
	}
}

func TestAnEndedBatchsResultsCarryTheirRequestsIdentifiers(t *testing.T) {
	model, _ := batchAPI(t, "ended",
		map[string]any{"custom_id": "reading-2", "result": map[string]any{"type": "succeeded", "message": message("end_turn", `{"facts": []}`)}},
		map[string]any{"custom_id": "reading-1", "result": map[string]any{"type": "errored",
			"error": map[string]any{"type": "error", "error": map[string]any{"type": "overloaded_error", "message": "Overloaded"}}}},
		map[string]any{"custom_id": "reading-3", "result": map[string]any{"type": "expired"}},
		map[string]any{"custom_id": "reading-4", "result": map[string]any{"type": "succeeded", "message": message("refusal", "")}},
		map[string]any{"custom_id": "reading-5", "result": map[string]any{"type": "succeeded", "message": message("max_tokens", `{"facts": [`)}},
		map[string]any{"custom_id": "reading-6", "result": map[string]any{"type": "canceled"}},
	)

	poll, err := model.Poll(t.Context(), "msgbatch_1")
	if err != nil {
		t.Fatal(err)
	}

	if !poll.Ended || len(poll.Results) != 6 {
		t.Fatalf("poll = %+v, want ended with six results", poll)
	}
	byID := map[string]editor.BatchResult{}
	for _, r := range poll.Results {
		byID[r.ID] = r
	}
	if r := byID["reading-2"]; r.Err != nil || string(r.Response.Output) != `{"facts": []}` ||
		r.Response.Model != "claude-haiku-5-5" || r.Response.Usage != (editor.Usage{InputTokens: 1200, OutputTokens: 340}) {
		t.Errorf("succeeded = %+v", r)
	}
	if r := byID["reading-1"]; r.Err == nil || !strings.Contains(r.Err.Error(), "Overloaded") {
		t.Errorf("errored = %+v", r)
	}
	for _, id := range []string{"reading-3", "reading-6"} {
		if byID[id].Err == nil {
			t.Errorf("%s has no error: %+v", id, byID[id])
		}
	}
	if r := byID["reading-4"]; !errors.Is(r.Err, editor.ErrRefused) || r.Response.StopReason != "refusal" || r.Response.Raw == nil {
		t.Errorf("refused = %+v", r)
	}
	if r := byID["reading-2"]; !strings.Contains(string(r.Response.Raw), `"stop_reason":"end_turn"`) || r.Response.StopReason != "end_turn" {
		t.Errorf("succeeded raw = %s", r.Response.Raw)
	}
	if r := byID["reading-5"]; !errors.Is(r.Err, editor.ErrTruncated) {
		t.Errorf("truncated = %+v", r)
	}
}
