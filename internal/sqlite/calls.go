package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/samthorold/go-slow/internal/editor"
)

// execer is a transaction or the database, for writes that run in either.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// keepText keeps a text sent to a model, once however often it is sent, and
// returns the hash it is kept under.
func keepText(ctx context.Context, tx execer, text string) (string, error) {
	sum := sha256.Sum256([]byte(text))
	hash := hex.EncodeToString(sum[:])
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO call_texts (hash, text) VALUES (?, ?) ON CONFLICT DO NOTHING`, hash, text); err != nil {
		return "", fmt.Errorf("keep call text: %w", err)
	}
	return hash, nil
}

// requestTexts are a request's texts as kept: the hashes of its
// instructions, schema and input, and of its conversation (its tools and
// messages), invalid for a request that is none.
type requestTexts struct {
	instructions, schema, input string
	conversation                sql.NullString
}

// conversationOf is a request's tools and messages as kept, one JSON text.
type conversationOf struct {
	Tools    []editor.Tool    `json:",omitempty"`
	Messages []editor.Message `json:",omitempty"`
}

func keepRequest(ctx context.Context, tx execer, r editor.Request) (requestTexts, error) {
	schema, err := json.Marshal(r.Schema)
	if err != nil {
		return requestTexts{}, fmt.Errorf("keep request schema: %w", err)
	}
	var t requestTexts
	if t.instructions, err = keepText(ctx, tx, r.Instructions); err != nil {
		return t, err
	}
	if t.schema, err = keepText(ctx, tx, string(schema)); err != nil {
		return t, err
	}
	if t.input, err = keepText(ctx, tx, r.Input); err != nil {
		return t, err
	}
	if len(r.Tools) > 0 || len(r.Messages) > 0 {
		b, err := json.Marshal(conversationOf{r.Tools, r.Messages})
		if err != nil {
			return t, fmt.Errorf("keep request conversation: %w", err)
		}
		h, err := keepText(ctx, tx, string(b))
		if err != nil {
			return t, err
		}
		t.conversation = sql.NullString{String: h, Valid: true}
	}
	return t, nil
}

// readConversation sets r's tools and messages from their kept text, if
// it has one.
func readConversation(r *editor.Request, text sql.NullString) error {
	if !text.Valid {
		return nil
	}
	var c conversationOf
	if err := json.Unmarshal([]byte(text.String), &c); err != nil {
		return fmt.Errorf("conversation: %w", err)
	}
	r.Tools, r.Messages = c.Tools, c.Messages
	return nil
}

// KeepCall keeps a model call, as sent and as it came back.
func (s *Store) KeepCall(ctx context.Context, c editor.Call) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := keepCall(ctx, tx, c); err != nil {
		return err
	}
	return tx.Commit()
}

func keepCall(ctx context.Context, tx execer, c editor.Call) error {
	t, err := keepRequest(ctx, tx, c.Request)
	if err != nil {
		return err
	}
	var response, output, reported, stop, calls sql.NullString
	var in, out, cached sql.NullInt64
	if r := c.Response; r != nil {
		if len(r.Calls) > 0 {
			b, err := json.Marshal(r.Calls)
			if err != nil {
				return fmt.Errorf("keep %s call: %w", c.Act, err)
			}
			calls = sql.NullString{String: string(b), Valid: true}
		}
		response = sql.NullString{String: string(r.Raw), Valid: r.Raw != nil}
		output = sql.NullString{String: string(r.Output), Valid: r.Output != nil}
		reported = sql.NullString{String: r.Model, Valid: true}
		stop = sql.NullString{String: r.StopReason, Valid: true}
		in = sql.NullInt64{Int64: r.Usage.InputTokens, Valid: true}
		out = sql.NullInt64{Int64: r.Usage.OutputTokens, Valid: true}
		cached = sql.NullInt64{Int64: r.Usage.CacheReadTokens, Valid: true}
	}
	// precedents is the Feedback a retired act's call showed as precedent,
	// read for its page; no act shows any now, so a call kept now writes the
	// empty array the column, NOT NULL, needs.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO model_calls (act, work_key, at, batch_id, model, request_hash,
			instructions, schema, input, effort, precedents, conversation,
			response, output, reported_model, stop_reason, input_tokens, output_tokens, cache_read_tokens, calls,
			outcome, why)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '[]', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Act, c.Key, formatTime(c.At), c.BatchID, c.Model, c.RequestHash,
		t.instructions, t.schema, t.input, c.Request.Effort, t.conversation,
		response, output, reported, stop, in, out, cached, calls,
		string(c.Outcome), c.Why); err != nil {
		return fmt.Errorf("keep %s call: %w", c.Act, err)
	}
	return nil
}

// callColumns are a kept call's columns, its texts joined in, as scanCall
// reads them.
const callColumns = `
	c.id, c.act, c.work_key, c.at, c.batch_id, c.model, c.request_hash,
	i.text, s.text, n.text, c.effort, c.precedents, v.text,
	c.response, c.output, c.reported_model, c.stop_reason, c.input_tokens, c.output_tokens, c.cache_read_tokens,
	c.calls, c.outcome, c.why
	FROM model_calls c
		JOIN call_texts i ON i.hash = c.instructions
		JOIN call_texts s ON s.hash = c.schema
		JOIN call_texts n ON n.hash = c.input
		LEFT JOIN call_texts v ON v.hash = c.conversation`

type scanner interface{ Scan(dest ...any) error }

func scanCall(row scanner) (editor.Call, error) {
	var c editor.Call
	var at, schema, precedents, outcome string
	var conversation, response, output, reported, stop, calls sql.NullString
	var in, out, cached sql.NullInt64
	if err := row.Scan(&c.ID, &c.Act, &c.Key, &at, &c.BatchID, &c.Model, &c.RequestHash,
		&c.Request.Instructions, &schema, &c.Request.Input, &c.Request.Effort, &precedents, &conversation,
		&response, &output, &reported, &stop, &in, &out, &cached,
		&calls, &outcome, &c.Why); err != nil {
		return c, err
	}
	if err := readConversation(&c.Request, conversation); err != nil {
		return c, fmt.Errorf("call %d %w", c.ID, err)
	}
	c.At, c.Outcome, c.Request.Act = parseTime(at), editor.Outcome(outcome), c.Act
	if err := json.Unmarshal([]byte(schema), &c.Request.Schema); err != nil {
		return c, fmt.Errorf("call %d schema: %w", c.ID, err)
	}
	if err := json.Unmarshal([]byte(precedents), &c.Precedents); err != nil {
		return c, fmt.Errorf("call %d precedents: %w", c.ID, err)
	}
	if len(c.Precedents) == 0 {
		c.Precedents = nil
	}
	if reported.Valid {
		c.Response = &editor.Response{
			Model: reported.String, StopReason: stop.String,
			Usage: editor.Usage{InputTokens: in.Int64, OutputTokens: out.Int64, CacheReadTokens: cached.Int64},
		}
		if response.Valid {
			c.Response.Raw = json.RawMessage(response.String)
		}
		if output.Valid {
			c.Response.Output = json.RawMessage(output.String)
		}
		if calls.Valid {
			if err := json.Unmarshal([]byte(calls.String), &c.Response.Calls); err != nil {
				return c, fmt.Errorf("call %d calls: %w", c.ID, err)
			}
		}
	}
	return c, nil
}

// Call is one kept call, reporting whether the Record holds it.
func (s *Store) Call(ctx context.Context, id int64) (editor.Call, bool, error) {
	c, err := scanCall(s.db.QueryRowContext(ctx, `SELECT `+callColumns+` WHERE c.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	if err != nil {
		return c, false, fmt.Errorf("call %d: %w", id, err)
	}
	return c, true, nil
}

// CallsOn are the kept calls an act made on a piece of work, its attempts,
// in the order they were kept.
func (s *Store) CallsOn(ctx context.Context, act, key string) ([]editor.Call, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+callColumns+`
		WHERE c.act = ? AND c.work_key = ? ORDER BY c.id`, act, key)
	if err != nil {
		return nil, fmt.Errorf("calls on %s: %w", act, err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.Call
	for rows.Next() {
		c, err := scanCall(rows)
		if err != nil {
			return nil, fmt.Errorf("calls on %s: %w", act, err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("calls on %s: %w", act, err)
	}
	return out, nil
}
