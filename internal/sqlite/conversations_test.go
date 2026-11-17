package sqlite

import (
	"testing"
)

// keptConversation keeps, as the retired Sunday read did, a conversation of
// one turn that failed with no response, and returns its id.
func keptConversation(t *testing.T, s *Store) int64 {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO call_texts (hash, text) VALUES ('i', 'Read the week.'), ('t', '[{"name":"story","schema":{"type":"object"}}]')`,
		`INSERT INTO conversations (id, act, work_key, at, model, instructions, tools, outcome, why)
			VALUES (1, 'sunday_read', '2026-10-25', '2026-10-25T09:00:00.000000000Z', 'm', 'i', 't', 'failed', 'stopped')`,
		`INSERT INTO conversation_turns (conversation_id, turn, at, request_hash, sent, outcome, why)
			VALUES (1, 1, '2026-10-25T09:00:00.000000000Z', 'h', '[{"user":"The week."}]', 'failed', 'the model refused')`,
	} {
		if _, err := s.db.ExecContext(t.Context(), q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return 1
}

func TestAKeptConversationIsReadAsKept(t *testing.T) {
	s := openTestStore(t)
	id := keptConversation(t, s)

	c, found, err := s.Conversation(t.Context(), id)
	if err != nil || !found {
		t.Fatalf("conversation %d: %v %v", id, found, err)
	}
	if c.Act != "sunday_read" || c.Key != "2026-10-25" || c.Instructions != "Read the week." || len(c.Tools) != 1 ||
		len(c.Turns) != 1 || c.Turns[0].Response != nil || len(c.Turns[0].Sent) != 1 || c.Turns[0].Sent[0].User != "The week." {
		t.Errorf("conversation = %+v; want it as kept, its one turn with no response", c)
	}
}

func TestAKeptConversationIsNeverRewrittenOrRemoved(t *testing.T) {
	s := openTestStore(t)
	keptConversation(t, s)

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
}

func TestAConversationNotKeptIsNotFound(t *testing.T) {
	s := openTestStore(t)

	if _, found, err := s.Conversation(t.Context(), 1); err != nil || found {
		t.Errorf("Conversation(1) = %v, %v; want none", found, err)
	}
}
