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
// instructions, schema and input.
type requestTexts struct{ instructions, schema, input string }

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
	return t, nil
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
	precedents, err := json.Marshal(nonNil(c.Precedents))
	if err != nil {
		return err
	}
	var response, output, reported, stop sql.NullString
	var in, out, cached sql.NullInt64
	if r := c.Response; r != nil {
		response = sql.NullString{String: string(r.Raw), Valid: r.Raw != nil}
		output = sql.NullString{String: string(r.Output), Valid: r.Output != nil}
		reported = sql.NullString{String: r.Model, Valid: true}
		stop = sql.NullString{String: r.StopReason, Valid: true}
		in = sql.NullInt64{Int64: r.Usage.InputTokens, Valid: true}
		out = sql.NullInt64{Int64: r.Usage.OutputTokens, Valid: true}
		cached = sql.NullInt64{Int64: r.Usage.CacheReadTokens, Valid: true}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO model_calls (act, work_key, at, batch_id, model, request_hash,
			instructions, schema, input, precedents,
			response, output, reported_model, stop_reason, input_tokens, output_tokens, cache_read_tokens,
			outcome, why)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Act, c.Key, formatTime(c.At), c.BatchID, c.Model, c.RequestHash,
		t.instructions, t.schema, t.input, string(precedents),
		response, output, reported, stop, in, out, cached,
		string(c.Outcome), c.Why); err != nil {
		return fmt.Errorf("keep %s call: %w", c.Act, err)
	}
	return nil
}

func nonNil(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}

// callColumns are a kept call's columns, its texts joined in, as scanCall
// reads them.
const callColumns = `
	c.id, c.act, c.work_key, c.at, c.batch_id, c.model, c.request_hash,
	i.text, s.text, n.text, c.precedents,
	c.response, c.output, c.reported_model, c.stop_reason, c.input_tokens, c.output_tokens, c.cache_read_tokens,
	c.outcome, c.why
	FROM model_calls c
		JOIN call_texts i ON i.hash = c.instructions
		JOIN call_texts s ON s.hash = c.schema
		JOIN call_texts n ON n.hash = c.input`

type scanner interface{ Scan(dest ...any) error }

func scanCall(row scanner) (editor.Call, error) {
	var c editor.Call
	var at, schema, precedents, outcome string
	var response, output, reported, stop sql.NullString
	var in, out, cached sql.NullInt64
	if err := row.Scan(&c.ID, &c.Act, &c.Key, &at, &c.BatchID, &c.Model, &c.RequestHash,
		&c.Request.Instructions, &schema, &c.Request.Input, &precedents,
		&response, &output, &reported, &stop, &in, &out, &cached,
		&outcome, &c.Why); err != nil {
		return c, err
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
