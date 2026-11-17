package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/samthorold/go-slow/internal/editor"
)

// Conversation is one kept conversation with its turns in order, reporting
// whether the Record holds it.
func (s *Store) Conversation(ctx context.Context, id int64) (editor.KeptConversation, bool, error) {
	c := editor.KeptConversation{ID: id}
	var at, outcome, tools string
	err := s.db.QueryRowContext(ctx, `
		SELECT c.act, c.work_key, c.at, c.model, i.text, t.text, c.outcome, c.why
		FROM conversations c
			JOIN call_texts i ON i.hash = c.instructions
			JOIN call_texts t ON t.hash = c.tools
		WHERE c.id = ?`, id).Scan(&c.Act, &c.Key, &at, &c.Model, &c.Instructions, &tools, &outcome, &c.Why)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	if err != nil {
		return c, false, fmt.Errorf("conversation %d: %w", id, err)
	}
	c.At, c.Outcome = parseTime(at), editor.Outcome(outcome)
	if err := json.Unmarshal([]byte(tools), &c.Tools); err != nil {
		return c, false, fmt.Errorf("conversation %d tools: %w", id, err)
	}
	if c.Turns, err = s.turns(ctx, id); err != nil {
		return c, false, fmt.Errorf("conversation %d: %w", id, err)
	}
	return c, true, nil
}

func (s *Store) turns(ctx context.Context, id int64) ([]editor.KeptTurn, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT turn, at, request_hash, sent,
			response, text, calls, reported_model, stop_reason, input_tokens, output_tokens, cache_read_tokens,
			outcome, why
		FROM conversation_turns WHERE conversation_id = ? ORDER BY turn`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []editor.KeptTurn
	for rows.Next() {
		var t editor.KeptTurn
		var at, sent, outcome string
		var response, text, calls, reported, stop sql.NullString
		var in, outTokens, cached sql.NullInt64
		if err := rows.Scan(&t.N, &at, &t.RequestHash, &sent,
			&response, &text, &calls, &reported, &stop, &in, &outTokens, &cached,
			&outcome, &t.Why); err != nil {
			return nil, err
		}
		t.At, t.Outcome = parseTime(at), editor.Outcome(outcome)
		if err := json.Unmarshal([]byte(sent), &t.Sent); err != nil {
			return nil, fmt.Errorf("turn %d sent: %w", t.N, err)
		}
		if reported.Valid {
			t.Response = &editor.Turn{
				Text: text.String, Model: reported.String, StopReason: stop.String,
				Usage: editor.Usage{InputTokens: in.Int64, OutputTokens: outTokens.Int64, CacheReadTokens: cached.Int64},
			}
			if response.Valid {
				t.Response.Raw = json.RawMessage(response.String)
			}
			if err := json.Unmarshal([]byte(calls.String), &t.Response.Calls); err != nil {
				return nil, fmt.Errorf("turn %d calls: %w", t.N, err)
			}
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
