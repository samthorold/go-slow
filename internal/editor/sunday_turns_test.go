package editor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

func TestAWrittenReadKeepsEveryTurnOfItsConversationInOrder(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	looking := calls("story", `{"story": "S1"}`)
	looking.Raw = json.RawMessage(`{"content": [{"type": "thinking", "thinking": "Look at S1 first."}]}`)
	looking.StopReason = "tool_use"
	agent := &fakeAgent{turns: []editor.Turn{looking, submits(uncited), submits(citesTheSpeech)}}
	ed, logs := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	r, found, err := store.SundayRead(t.Context(), "2026-10-25")
	if err != nil || !found || r.ConversationID == 0 {
		t.Fatalf("SundayRead: %+v %v %v\n%s", r, found, err, logs)
	}
	c, found, err := store.Conversation(t.Context(), r.ConversationID)
	if err != nil || !found {
		t.Fatalf("Conversation: %v %v", found, err)
	}
	if c.Act != "sunday_read" || c.Key != "2026-10-25" || c.Outcome != editor.Applied ||
		c.Instructions == "" || len(c.Tools) == 0 || !c.At.Equal(sundayMorning) {
		t.Errorf("conversation = %+v", c)
	}
	if len(c.Turns) != 3 {
		t.Fatalf("kept %d turns, want 3", len(c.Turns))
	}
	// Each turn keeps what was sent new with it: the act's opening message,
	// then the tool results of the turn before.
	first := c.Turns[0]
	if len(first.Sent) != 1 || first.Sent[0].User != agent.sent[0].Messages[0].User {
		t.Errorf("the first turn was sent %+v, want the week's Stories", first.Sent)
	}
	if first.Response == nil || string(first.Response.Raw) != string(looking.Raw) ||
		first.Response.StopReason != "tool_use" || first.Response.Usage.InputTokens != 1000 ||
		len(first.Response.Calls) != 1 || first.Response.Calls[0].Name != "story" {
		t.Errorf("the first turn's response = %+v", first.Response)
	}
	if first.Outcome != editor.Continued {
		t.Errorf("the first turn's outcome = %q, want continued", first.Outcome)
	}
	second := c.Turns[1]
	if len(second.Sent) != 1 || len(second.Sent[0].Results) != 1 || !strings.Contains(second.Sent[0].Results[0].Content, "Andy Burnham") {
		t.Errorf("the second turn was sent %+v, want the first turn's tool result", second.Sent)
	}
	// A refused submission keeps the reason the agent was told.
	if second.Outcome != editor.Refused || !strings.Contains(second.Why, "uncited") {
		t.Errorf("the refused submission = %q %q", second.Outcome, second.Why)
	}
	third := c.Turns[2]
	if third.Outcome != editor.Applied || len(third.Sent) != 1 || !third.Sent[0].Results[0].IsError {
		t.Errorf("the last turn = %+v", third)
	}
	for i, turn := range c.Turns {
		if turn.N != i+1 || turn.RequestHash == "" {
			t.Errorf("turn %d = %d, hash %q", i+1, turn.N, turn.RequestHash)
		}
	}
}

func TestEachKeptTurnCarriesTheHashOfTheRequestAsSent(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	agent := &fakeAgent{turns: []editor.Turn{calls("story", `{"story": "S1"}`), submits(citesTheSpeech)}}
	var logs bytes.Buffer
	ed := editor.New(editor.Deps{
		Store: store, SundayReads: store, SundayReadModel: agent, Wikidata: &fakeWikidata{},
		ModelFor: func(act string) string { return "model-for-" + act },
		Log:      slog.New(slog.NewTextHandler(&logs, nil)),
	})
	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	r, _, _ := store.SundayRead(t.Context(), "2026-10-25")
	c, _, err := store.Conversation(t.Context(), r.ConversationID)
	if err != nil || len(c.Turns) != 2 {
		t.Fatalf("Conversation: %+v %v\n%s", c, err, &logs)
	}
	if c.Model != "model-for-sunday_read" {
		t.Errorf("the model asked for = %q", c.Model)
	}
	for i, turn := range c.Turns {
		if want := editor.ConversationRequestHash(agent.sent[i], c.Model); turn.RequestHash != want {
			t.Errorf("turn %d hash = %s, want %s", i+1, turn.RequestHash, want)
		}
	}
	if c.Turns[0].RequestHash == c.Turns[1].RequestHash {
		t.Error("two turns of a conversation hash the same")
	}
}

func TestAConversationRequestHashCoversWhatTheModelIsAsked(t *testing.T) {
	base := editor.ConversationRequest{
		Act:          "sunday_read",
		Instructions: "Write the read.",
		Tools:        []editor.Tool{{Name: "story", Description: "A Story.", Schema: map[string]any{"type": "object", "required": []string{"story"}}}},
		Messages: []editor.Message{
			{User: "The week."},
			{Assistant: &editor.Turn{Raw: json.RawMessage(`{"content": []}`), Model: "reported", Usage: editor.Usage{InputTokens: 5}}},
			{Results: []editor.ToolResult{{CallID: "c1", Content: "S1"}}},
		},
	}
	h := editor.ConversationRequestHash(base, "m")
	if len(h) != 64 || strings.Trim(h, "0123456789abcdef") != "" {
		t.Fatalf("hash = %q, want hex SHA-256", h)
	}
	same := base
	same.Act = "another act"
	same.Messages = slices.Clone(base.Messages)
	// What the provider reported of a turn is not sent back, nor the
	// spacing of its raw form.
	same.Messages[1] = editor.Message{Assistant: &editor.Turn{Raw: json.RawMessage(`{"content":[]}`), Model: "other"}}
	if editor.ConversationRequestHash(same, "m") != h {
		t.Error("the hash depends on the act, the reported model or usage, or raw spacing")
	}
	for name, change := range map[string]func(r *editor.ConversationRequest){
		"instructions": func(r *editor.ConversationRequest) { r.Instructions += " Briefly." },
		"tools":        func(r *editor.ConversationRequest) { r.Tools = nil },
		"messages": func(r *editor.ConversationRequest) {
			r.Messages = append(slices.Clone(r.Messages), editor.Message{User: "Submit."})
		},
		"a tool result": func(r *editor.ConversationRequest) {
			r.Messages = slices.Clone(r.Messages)
			r.Messages[2] = editor.Message{Results: []editor.ToolResult{{CallID: "c1", Content: "S1", IsError: true}}}
		},
	} {
		changed := base
		change(&changed)
		if editor.ConversationRequestHash(changed, "m") == h {
			t.Errorf("changing the %s leaves the hash the same", name)
		}
	}
	if editor.ConversationRequestHash(base, "n") == h {
		t.Error("changing the model leaves the hash the same")
	}
}

// refusing is a conversation port whose model refuses, returning what came
// back with the refusal.
type refusing struct{}

func (refusing) Turn(context.Context, editor.ConversationRequest) (editor.Turn, error) {
	return editor.Turn{Raw: json.RawMessage(`{"stop_reason": "refusal"}`), Model: "fake-agent", StopReason: "refusal"},
		fmt.Errorf("%w (cyber)", editor.ErrRefused)
}

func TestEachFailedAttemptKeepsItsOwnTurns(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	looking := calls("story", `{"story": "S1"}`)
	ed, _ := reader(store, &fakeAgent{then: &looking})
	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}
	// The retry is refused by the model.
	ed, _ = reader(store, refusing{})
	if err := ed.SundayRead(t.Context(), sundayMorning.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	fs, err := store.SundayReadFailures(t.Context(), "2026-10-25")
	if err != nil || len(fs) != 2 {
		t.Fatalf("failures = %+v, %v", fs, err)
	}
	bounded, found, err := store.Conversation(t.Context(), fs[0].ConversationID)
	if err != nil || !found {
		t.Fatalf("Conversation: %v %v", found, err)
	}
	if len(bounded.Turns) != editor.SundayReadMaxTurns || bounded.Outcome != editor.Failed ||
		!strings.Contains(bounded.Why, "turns") {
		t.Errorf("the bounded attempt kept %d turns, %q %q", len(bounded.Turns), bounded.Outcome, bounded.Why)
	}
	refused, found, err := store.Conversation(t.Context(), fs[1].ConversationID)
	if err != nil || !found || refused.ID == bounded.ID {
		t.Fatalf("Conversation: %v %v, id %d", found, err, refused.ID)
	}
	if len(refused.Turns) != 1 {
		t.Fatalf("the refused attempt kept %d turns, want 1", len(refused.Turns))
	}
	turn := refused.Turns[0]
	if turn.Outcome != editor.Failed || !strings.Contains(turn.Why, "refused") ||
		turn.Response == nil || turn.Response.StopReason != "refusal" || string(turn.Response.Raw) != `{"stop_reason": "refusal"}` {
		t.Errorf("the refused turn = %+v", turn)
	}
}
