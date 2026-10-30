package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/samthorold/go-slow/internal/editor"
)

// PendingBatches are the batches submitted and not yet ended, oldest first,
// each with its requests in the order they were submitted: results are
// applied in that order, so of two Items read in one batch the one submitted
// later, published later, is read second.
func (s *Store) PendingBatches(ctx context.Context) ([]editor.PendingBatch, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT batches.id, batches.submitted_at, r.request_id, r.act, r.work_key,
			r.model, i.text, s.text, n.text, r.precedents
		FROM batches JOIN batch_requests r ON r.batch_id = batches.id
			LEFT JOIN call_texts i ON i.hash = r.instructions
			LEFT JOIN call_texts s ON s.hash = r.schema
			LEFT JOIN call_texts n ON n.hash = r.input
		ORDER BY batches.submitted_at, batches.id, r.rowid`)
	if err != nil {
		return nil, fmt.Errorf("pending batches: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.PendingBatch
	for rows.Next() {
		var id, submitted string
		var r editor.PendingRequest
		var model, instructions, schema, input, precedents sql.NullString
		if err := rows.Scan(&id, &submitted, &r.ID, &r.Act, &r.Key,
			&model, &instructions, &schema, &input, &precedents); err != nil {
			return nil, fmt.Errorf("pending batches: %w", err)
		}
		if model.Valid {
			req := editor.Request{Act: r.Act, Instructions: instructions.String, Input: input.String}
			if err := json.Unmarshal([]byte(schema.String), &req.Schema); err != nil {
				return nil, fmt.Errorf("pending batches: schema: %w", err)
			}
			if err := json.Unmarshal([]byte(precedents.String), &r.Precedents); err != nil {
				return nil, fmt.Errorf("pending batches: precedents: %w", err)
			}
			if len(r.Precedents) == 0 {
				r.Precedents = nil
			}
			r.Request, r.Model = &req, model.String
		}
		if len(out) == 0 || out[len(out)-1].ID != id {
			out = append(out, editor.PendingBatch{ID: id, SubmittedAt: parseTime(submitted)})
		}
		out[len(out)-1].Requests = append(out[len(out)-1].Requests, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pending batches: %w", err)
	}
	return out, nil
}

// SaveBatch keeps a submitted batch as pending.
func (s *Store) SaveBatch(ctx context.Context, b editor.PendingBatch) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := saveBatch(ctx, tx, b); err != nil {
		return err
	}
	return tx.Commit()
}

// EndBatch removes an ended batch, keeps its failed attempts and keeps the
// batches its failed requests were resubmitted in, all at once.
func (s *Store) EndBatch(ctx context.Context, batchID string, failures []editor.WorkFailure, resubmitted []editor.PendingBatch) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM batches WHERE id = ?`, batchID); err != nil {
		return fmt.Errorf("end batch %s: %w", batchID, err)
	}
	for _, f := range failures {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO work_attempts (act, work_key, attempts, last_failed_at, last_error, failed)
			VALUES (?, ?, 1, ?, ?, ?)
			ON CONFLICT (act, work_key) DO UPDATE SET attempts = attempts + 1,
				last_failed_at = excluded.last_failed_at, last_error = excluded.last_error,
				failed = excluded.failed`,
			f.Act, f.Key, formatTime(f.At), f.Why, boolInt(f.Failed)); err != nil {
			return fmt.Errorf("end batch %s: %w", batchID, err)
		}
		if f.Call != nil {
			if err := keepCall(ctx, tx, *f.Call); err != nil {
				return fmt.Errorf("end batch %s: %w", batchID, err)
			}
		}
	}
	for _, b := range resubmitted {
		if err := saveBatch(ctx, tx, b); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func saveBatch(ctx context.Context, tx *sql.Tx, b editor.PendingBatch) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO batches (id, submitted_at) VALUES (?, ?)`,
		b.ID, formatTime(b.SubmittedAt)); err != nil {
		return fmt.Errorf("save batch %s: %w", b.ID, err)
	}
	for _, r := range b.Requests {
		var model, instructions, schema, input, precedents sql.NullString
		if r.Request != nil {
			t, err := keepRequest(ctx, tx, *r.Request)
			if err != nil {
				return fmt.Errorf("save batch %s: %w", b.ID, err)
			}
			ps, err := json.Marshal(nonNil(r.Precedents))
			if err != nil {
				return err
			}
			model = sql.NullString{String: r.Model, Valid: true}
			instructions = sql.NullString{String: t.instructions, Valid: true}
			schema = sql.NullString{String: t.schema, Valid: true}
			input = sql.NullString{String: t.input, Valid: true}
			precedents = sql.NullString{String: string(ps), Valid: true}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO batch_requests (batch_id, request_id, act, work_key,
				model, instructions, schema, input, precedents)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			b.ID, r.ID, r.Act, r.Key, model, instructions, schema, input, precedents); err != nil {
			return fmt.Errorf("save batch %s: %w", b.ID, err)
		}
	}
	return nil
}
