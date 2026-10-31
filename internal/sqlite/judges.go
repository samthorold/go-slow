package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

// The versions of the shapes of a candidate's events this build writes.
const (
	candidateShape = 1
	choiceShape    = 1
)

// newestLiveReading is the event id of the newest live reading of the Item
// whose Source and entry are the SQL expressions source and entry, or NULL
// for none: the one a view reads (system design: Every judgement carries
// its judge). A reading is live if it was made as the live judge, or by a
// candidate since chosen; a chosen candidate's readings count as made when
// it was chosen, so the choice makes them newer than every live reading
// made before it, and a live reading made after it newer still.
func newestLiveReading(source, entry string) string {
	return `(SELECT lr.event_id FROM readings lr
		LEFT JOIN candidate_readings lcr ON lcr.reading_id = lr.event_id
		LEFT JOIN candidate_choices lch ON lch.candidate_id = lcr.candidate_id
		WHERE lr.source_id = ` + source + ` AND lr.entry_id = ` + entry + `
			AND (lcr.reading_id IS NULL OR lch.event_id IS NOT NULL)
		ORDER BY coalesce(lch.event_id, lr.event_id) DESC, lr.event_id DESC LIMIT 1)`
}

// NewCandidate keeps a candidate Judge as an event, and returns its id.
func (s *Store) NewCandidate(ctx context.Context, c editor.Candidate) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `INSERT INTO events (kind, shape, at) VALUES ('candidate', ?, ?)`,
		candidateShape, formatTime(c.At))
	if err != nil {
		return 0, fmt.Errorf("new candidate: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO candidates (event_id, act, version, model, range_from, range_to) VALUES (?, ?, ?, ?, ?, ?)`,
		id, c.Act, c.Version, c.Model, formatTime(c.Range.From), formatTime(c.Range.To)); err != nil {
		return 0, fmt.Errorf("new candidate: %w", err)
	}
	return id, tx.Commit()
}

// ChooseCandidate keeps the choice of a candidate, at, as an event: from
// then on the candidate is a live judge. Nothing it judged is rewritten. A
// candidate not in the Record, or already chosen, is refused.
func (s *Store) ChooseCandidate(ctx context.Context, id int64, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var known, chosen bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM candidates WHERE event_id = ?),
			EXISTS (SELECT 1 FROM candidate_choices WHERE candidate_id = ?)`, id, id).Scan(&known, &chosen); err != nil {
		return fmt.Errorf("choose candidate %d: %w", id, err)
	}
	switch {
	case !known:
		return fmt.Errorf("choose candidate %d: no such candidate", id)
	case chosen:
		return fmt.Errorf("choose candidate %d: already chosen", id)
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO events (kind, shape, at) VALUES ('choice', ?, ?)`, choiceShape, formatTime(at))
	if err != nil {
		return fmt.Errorf("choose candidate %d: %w", id, err)
	}
	eid, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO candidate_choices (event_id, candidate_id) VALUES (?, ?)`, eid, id); err != nil {
		return fmt.Errorf("choose candidate %d: %w", id, err)
	}
	return tx.Commit()
}

// PastUsage is what the act's applied calls answering one of its
// judgements in the Record cost: their count, their requests' size in bytes
// (instructions, schema and input) and their tokens in and out. Only
// reading keeps its judgements as events today, so only reading has any.
func (s *Store) PastUsage(ctx context.Context, act string) (editor.PastUsage, error) {
	var u editor.PastUsage
	if act != "reading" {
		return u, nil
	}
	err := s.db.QueryRowContext(ctx, `
		SELECT count(*),
			coalesce(sum(length(CAST(i.text AS BLOB)) + length(CAST(sc.text AS BLOB)) + length(CAST(inp.text AS BLOB))), 0),
			coalesce(sum(c.input_tokens + coalesce(c.cache_read_tokens, 0)), 0), coalesce(sum(c.output_tokens), 0)
		FROM model_calls c
			JOIN call_texts i ON i.hash = c.instructions
			JOIN call_texts sc ON sc.hash = c.schema
			JOIN call_texts inp ON inp.hash = c.input
		WHERE c.act = ? AND c.outcome = 'applied' AND c.input_tokens IS NOT NULL
			AND EXISTS (SELECT 1 FROM readings r WHERE r.request_hash = c.request_hash)`, act,
	).Scan(&u.Calls, &u.RequestBytes, &u.InputTokens, &u.OutputTokens)
	if err != nil {
		return u, fmt.Errorf("past usage of %s: %w", act, err)
	}
	return u, nil
}

// Candidates are every candidate Judge in the Record, oldest first, each with
// when it was chosen, if it was, and how many judgements it made.
func (s *Store) Candidates(ctx context.Context) ([]editor.Candidate, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.event_id, c.act, c.version, c.model, c.range_from, c.range_to, e.at,
			coalesce((SELECT ce.at FROM candidate_choices ch JOIN events ce ON ce.id = ch.event_id
				WHERE ch.candidate_id = c.event_id), ''),
			(SELECT count(*) FROM candidate_readings cr WHERE cr.candidate_id = c.event_id)
		FROM candidates c JOIN events e ON e.id = c.event_id
		ORDER BY c.event_id`)
	if err != nil {
		return nil, fmt.Errorf("candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.Candidate
	for rows.Next() {
		var c editor.Candidate
		var from, to, at, chosen string
		if err := rows.Scan(&c.ID, &c.Act, &c.Version, &c.Model, &from, &to, &at, &chosen, &c.Judgements); err != nil {
			return nil, fmt.Errorf("candidates: %w", err)
		}
		c.Range = editor.Range{From: parseTime(from), To: parseTime(to)}
		c.At = parseTime(at)
		if chosen != "" {
			c.ChosenAt = parseTime(chosen)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
