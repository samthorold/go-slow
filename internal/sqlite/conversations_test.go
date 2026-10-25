package sqlite

import (
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

func TestAKeptConversationIsNeverRewrittenOrRemoved(t *testing.T) {
	s := openTestStore(t)
	at := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	if err := s.SaveSundayReadFailure(t.Context(), editor.SundayReadFailure{
		Day: "2026-10-25", At: at, Reason: "stopped",
		Conversation: &editor.KeptConversation{
			Act: "sunday_read", Key: "2026-10-25", At: at, Model: "m", Instructions: instructions,
			Tools:   []editor.Tool{{Name: "story", Schema: map[string]any{"type": "object"}}},
			Outcome: editor.Failed, Why: "stopped",
			Turns: []editor.KeptTurn{{
				N: 1, At: at, Sent: []editor.Message{{User: "The week."}}, RequestHash: "h",
				Outcome: editor.Failed, Why: "the model refused",
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{
		`UPDATE conversations SET why = ''`,
		`DELETE FROM conversations`,
		`UPDATE conversation_turns SET why = ''`,
		`DELETE FROM conversation_turns`,
	} {
		if _, err := s.db.ExecContext(t.Context(), q); err == nil {
			t.Errorf("%s: a kept conversation was changed", q)
		}
	}
	fs, err := s.SundayReadFailures(t.Context(), "2026-10-25")
	if err != nil || len(fs) != 1 {
		t.Fatalf("failures = %+v, %v", fs, err)
	}
	c, found, err := s.Conversation(t.Context(), fs[0].ConversationID)
	if err != nil || !found || len(c.Turns) != 1 || c.Turns[0].Response != nil || c.Instructions != instructions {
		t.Errorf("conversation = %+v, %v, %v; want its one turn, with no response", c, found, err)
	}
}

func TestAFailureWithNoModelCallKeepsNoConversation(t *testing.T) {
	s := openTestStore(t)
	if err := s.SaveSundayReadFailure(t.Context(), editor.SundayReadFailure{Day: "2026-10-25", At: time.Now(), Reason: "Record"}); err != nil {
		t.Fatal(err)
	}
	fs, err := s.SundayReadFailures(t.Context(), "2026-10-25")
	if err != nil || len(fs) != 1 || fs[0].ConversationID != 0 {
		t.Errorf("failures = %+v, %v; want one with no conversation", fs, err)
	}
	if _, found, err := s.Conversation(t.Context(), 1); err != nil || found {
		t.Errorf("Conversation(1) = %v, %v; want none", found, err)
	}
}
