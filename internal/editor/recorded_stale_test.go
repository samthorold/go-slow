package editor_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/editor"
)

// A request as an act builds it, and the same act with its instructions
// changed: what a change to the act's code sends instead.
var (
	asRecorded = editor.Request{Act: "reading", Instructions: "Read the Item.", Input: "Item: one",
		Schema: map[string]any{"type": "object"}}
	changed = editor.Request{Act: "reading", Instructions: "Read the Item closely.", Input: "Item: one",
		Schema: map[string]any{"type": "object"}}
)

// recordedOn is what recording req on the model named model keeps.
func recordedOn(t *testing.T, req editor.Request, model string) recording {
	t.Helper()
	var rec recording
	if _, err := (recorder{&fakeModel{output: `{"facts": []}`}, &rec, model}).Complete(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	return rec
}

// Recording keeps the hash of the request the act sent, with the model it
// asked for: the one function a kept call's hash is.
func TestARecordingKeepsTheHashOfTheRequestItAnswered(t *testing.T) {
	rec := recordedOn(t, asRecorded, "claude-haiku-5-5")
	if want := editor.RequestHash(asRecorded, "claude-haiku-5-5"); rec.RequestHash != want {
		t.Errorf("request hash = %q, want %q", rec.RequestHash, want)
	}
}

// replayOf is a replay of rec, kept as the one attempt of sample 1 of the
// fixture "one", hashing with model, and what it reports.
func replayOf(t *testing.T, rec recording, model string) (*sampleModel, *[]error) {
	t.Helper()
	path := samplePath(t.TempDir(), "one", 1)
	keepSample(t, path, []recording{rec})
	var reported []error
	return replayingSample(t, path, model, readingCommand, func(err error) { reported = append(reported, err) }), &reported
}

func TestAReplayAnswersTheRequestItWasRecordedFor(t *testing.T) {
	m, reported := replayOf(t, recordedOn(t, asRecorded, "claude-haiku-5-5"), "claude-haiku-5-5")
	res, err := m.Complete(t.Context(), asRecorded)
	if err != nil || strings.Join(strings.Fields(string(res.Output)), "") != `{"facts":[]}` || len(*reported) != 0 {
		t.Errorf("replay = %s, %v (reported %v); want the recorded output", res.Output, err, *reported)
	}
}

// After the act's instructions change, its old recording answers a request
// the act no longer sends: the replay fails, naming the fixture and the
// command that re-records it, rather than passing on a prompt that no
// longer exists.
func TestAReplayFailsAsStaleWhenTheActsRequestHasChanged(t *testing.T) {
	rec := recordedOn(t, asRecorded, "claude-haiku-5-5")
	for name, tc := range map[string]struct {
		req   editor.Request
		model string
	}{
		"the instructions changed": {changed, "claude-haiku-5-5"},
		"the model changed":        {asRecorded, "claude-opus-5-5"},
	} {
		t.Run(name, func(t *testing.T) {
			m, reported := replayOf(t, rec, tc.model)
			_, err := m.Complete(t.Context(), tc.req)
			if err == nil {
				t.Fatal("a stale recording replayed")
			}
			for _, want := range []string{"one", "stale: re-record with", "go test -count=1 ./internal/editor -run TestRecordedReadings -record", "READING_MODEL"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not say %q", err, want)
				}
			}
			if len(*reported) != 1 {
				t.Errorf("reported %v, want the staleness once", *reported)
			}
		})
	}
}

// A recording made before recordings kept their request's hash can't show
// it answers today's request: it is stale until it is re-recorded.
func TestAReplayOfARecordingWithNoHashFailsAsStale(t *testing.T) {
	rec := recordedOn(t, asRecorded, "claude-haiku-5-5")
	rec.RequestHash = ""
	m, _ := replayOf(t, rec, "claude-haiku-5-5")
	if _, err := m.Complete(context.Background(), asRecorded); err == nil || !strings.Contains(err.Error(), "stale: re-record with") {
		t.Errorf("replay = %v; want it stale", err)
	}
}

// A conversation's opening turn as the act builds it, the model's answer to
// it in the provider's own form, and the turn after.
var (
	opening = editor.ConversationRequest{Act: "sunday read", Instructions: "Write the read.",
		Tools:    []editor.Tool{{Name: "submit_read", Description: "Submit.", Schema: map[string]any{"type": "object"}}},
		Messages: []editor.Message{{User: "The week."}}}
	firstTurn = editor.Turn{Text: "Looking.", Calls: []editor.ToolCall{{ID: "c1", Name: "list_stories", Input: []byte(`{}`)}},
		Raw: []byte(`{"role":"assistant","content":[{"type":"thinking","signature":"x"}]}`), Model: "claude-opus-5-5"}
	second = editor.ConversationRequest{Act: opening.Act, Instructions: opening.Instructions, Tools: opening.Tools,
		Messages: append(slices.Clone(opening.Messages), editor.Message{Assistant: &firstTurn},
			editor.Message{Results: []editor.ToolResult{{CallID: "c1", Content: "S1: a Story"}}})}
)

// scripted answers each turn with the next of its turns.
type scripted struct{ turns []editor.Turn }

func (s *scripted) Turn(context.Context, editor.ConversationRequest) (editor.Turn, error) {
	t := s.turns[0]
	s.turns = s.turns[1:]
	return t, nil
}

// recordedConversation is what recording the conversation of opening then
// second on the model named model keeps.
func recordedConversation(t *testing.T, model string) conversationRecording {
	t.Helper()
	var rec conversationRecording
	r := conversationRecorder{&scripted{[]editor.Turn{firstTurn, {Text: "Done.", Model: model}}}, &rec, model}
	for _, req := range []editor.ConversationRequest{opening, second} {
		if _, err := r.Turn(t.Context(), req); err != nil {
			t.Fatal(err)
		}
	}
	return rec
}

// Each recorded turn keeps the hash of its request in the form a replay
// rebuilds it: the model's earlier turns as the act reads them, since the
// provider's own form of them is not kept.
func TestARecordedConversationKeepsEachTurnsRequestHash(t *testing.T) {
	rec := recordedConversation(t, "claude-opus-5-5")
	replayed := second
	replayed.Messages = slices.Clone(second.Messages)
	neutral := firstTurn
	neutral.Raw = nil
	replayed.Messages[1] = editor.Message{Assistant: &neutral}
	for i, req := range []editor.ConversationRequest{opening, replayed} {
		if want := editor.ConversationRequestHash(req, "claude-opus-5-5"); rec.Turns[i].RequestHash != want {
			t.Errorf("turn %d: request hash = %q, want %q", i+1, rec.Turns[i].RequestHash, want)
		}
	}
}

// replayConversation replays rec on the requests given, as the act would
// send them, the model's turns in the form the replay gave them: what was
// reported, and the error the first stale turn gave.
func replayConversation(t *testing.T, rec conversationRecording, model string, reqs ...editor.ConversationRequest) ([]error, error) {
	t.Helper()
	var reported []error
	r := conversationReplay{rec: rec, next: new(int), model: model, cmd: sundayReadCommand,
		report: func(err error) { reported = append(reported, err) }}
	var last *editor.Turn
	for _, req := range reqs {
		if last != nil {
			req.Messages = slices.Clone(req.Messages)
			for i, m := range req.Messages {
				if m.Assistant != nil {
					req.Messages[i] = editor.Message{Assistant: last}
				}
			}
		}
		turn, err := r.Turn(t.Context(), req)
		if err != nil {
			return reported, err
		}
		last = &turn
	}
	return reported, nil
}

func TestAConversationReplayAnswersTheTurnsItWasRecordedFor(t *testing.T) {
	rec := recordedConversation(t, "claude-opus-5-5")
	if reported, err := replayConversation(t, rec, "claude-opus-5-5", opening, second); err != nil || len(reported) != 0 {
		t.Errorf("replay = %v (reported %v); want every turn answered", err, reported)
	}
}

func TestAConversationReplayFailsAsStaleWhenATurnsRequestHasChanged(t *testing.T) {
	rec := recordedConversation(t, "claude-opus-5-5")
	resultChanged := second
	resultChanged.Messages = slices.Clone(second.Messages)
	resultChanged.Messages[2] = editor.Message{Results: []editor.ToolResult{{CallID: "c1", Content: "S1: another Story"}}}
	instructionsChanged := opening
	instructionsChanged.Instructions = "Write the read closely."
	for name, reqs := range map[string][]editor.ConversationRequest{
		"the instructions changed": {instructionsChanged, second},
		"a tool's result changed":  {opening, resultChanged},
	} {
		t.Run(name, func(t *testing.T) {
			reported, err := replayConversation(t, rec, "claude-opus-5-5", reqs...)
			if err == nil || !strings.Contains(err.Error(), "stale: re-record with") ||
				!strings.Contains(err.Error(), "TestRecordedSundayRead") || len(reported) != 1 {
				t.Errorf("replay = %v (reported %v); want it stale", err, reported)
			}
		})
	}
}

// A conversation recorded before turns kept their request's hash is
// replayed, but not as a check of today's request: the replay says so.
func TestAConversationReplayWithNoHashesSaysSo(t *testing.T) {
	rec := recordedConversation(t, "claude-opus-5-5")
	for i := range rec.Turns {
		rec.Turns[i].RequestHash = ""
	}
	if reported, err := replayConversation(t, rec, "claude-opus-5-5", opening, second); err != nil || len(reported) != 0 {
		t.Errorf("replay = %v (reported %v); want it replayed", err, reported)
	}
	if !unhashed(rec) {
		t.Error("a conversation with no hashes is not reported unhashed")
	}
	if unhashed(recordedConversation(t, "claude-opus-5-5")) {
		t.Error("a conversation with hashes is reported unhashed")
	}
}

// The model a replay hashes with is the one the act is pinned to, so
// changing a pin makes its recordings stale, as .env.example says.
func TestTheModelAReplayHashesWithIsTheActsPin(t *testing.T) {
	if got := pinnedModel(t, "READING_MODEL"); got != "claude-haiku-5-5" {
		t.Errorf("READING_MODEL is pinned to %q, want claude-haiku-5-5", got)
	}
	if got := pinnedModel(t, "SUNDAY_READ_MODEL"); got != "claude-opus-5-5" {
		t.Errorf("SUNDAY_READ_MODEL is pinned to %q, want claude-opus-5-5", got)
	}
}
