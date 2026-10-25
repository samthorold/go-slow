package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/samthorold/go-slow/internal/editor"
)

// keepConversation keeps a conversation turn by turn, in tx, and returns its
// id; nil for none, which keeps nothing.
func keepConversation(ctx context.Context, tx *sql.Tx, c *editor.KeptConversation) (sql.NullInt64, error) {
	if c == nil {
		return sql.NullInt64{}, nil
	}
	tools, err := json.Marshal(c.Tools)
	if err != nil {
		return sql.NullInt64{}, fmt.Errorf("keep %s conversation tools: %w", c.Act, err)
	}
	instructions, err := keepText(ctx, tx, c.Instructions)
	if err != nil {
		return sql.NullInt64{}, err
	}
	toolsHash, err := keepText(ctx, tx, string(tools))
	if err != nil {
		return sql.NullInt64{}, err
	}
	var id int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO conversations (act, work_key, at, model, instructions, tools, outcome, why)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		c.Act, c.Key, formatTime(c.At), c.Model, instructions, toolsHash, string(c.Outcome), c.Why).Scan(&id); err != nil {
		return sql.NullInt64{}, fmt.Errorf("keep %s conversation: %w", c.Act, err)
	}
	for _, t := range c.Turns {
		sent, err := json.Marshal(t.Sent)
		if err != nil {
			return sql.NullInt64{}, fmt.Errorf("keep %s turn %d: %w", c.Act, t.N, err)
		}
		var response, text, calls, reported, stop sql.NullString
		var in, out, cached sql.NullInt64
		if r := t.Response; r != nil {
			b, err := json.Marshal(r.Calls)
			if err != nil {
				return sql.NullInt64{}, fmt.Errorf("keep %s turn %d: %w", c.Act, t.N, err)
			}
			response = sql.NullString{String: string(r.Raw), Valid: r.Raw != nil}
			text = sql.NullString{String: r.Text, Valid: true}
			calls = sql.NullString{String: string(b), Valid: true}
			reported = sql.NullString{String: r.Model, Valid: true}
			stop = sql.NullString{String: r.StopReason, Valid: true}
			in = sql.NullInt64{Int64: r.Usage.InputTokens, Valid: true}
			out = sql.NullInt64{Int64: r.Usage.OutputTokens, Valid: true}
			cached = sql.NullInt64{Int64: r.Usage.CacheReadTokens, Valid: true}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO conversation_turns (conversation_id, turn, at, request_hash, sent,
				response, text, calls, reported_model, stop_reason, input_tokens, output_tokens, cache_read_tokens,
				outcome, why)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, t.N, formatTime(t.At), t.RequestHash, string(sent),
			response, text, calls, reported, stop, in, out, cached,
			string(t.Outcome), t.Why); err != nil {
			return sql.NullInt64{}, fmt.Errorf("keep %s turn %d: %w", c.Act, t.N, err)
		}
	}
	return sql.NullInt64{Int64: id, Valid: true}, nil
}

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
