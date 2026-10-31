package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

// keepPrecedents keeps, with an act's result on an Item, the Feedback it was
// shown as precedent, in the order shown.
func keepPrecedents(ctx context.Context, tx *sql.Tx, act, sourceID, entryID string, feedbackIDs []int64, at time.Time) error {
	for i, id := range feedbackIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO precedents_shown (act, source_id, entry_id, feedback_id, rank, shown_at)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			act, sourceID, entryID, id, i+1, formatTime(at)); err != nil {
			return fmt.Errorf("keep precedent %d of %s %s: %w", id, act, entryID, err)
		}
	}
	return nil
}

// keepShown keeps, with the judgement id, the Feedback its judge was shown
// as precedent, in the order shown.
func keepShown(ctx context.Context, tx *sql.Tx, id int64, feedbackIDs []int64) error {
	for i, f := range feedbackIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO judgement_precedents (judgement_id, rank, feedback_id) VALUES (?, ?, ?)`, id, i+1, f); err != nil {
			return fmt.Errorf("keep precedent %d of judgement %d: %w", f, id, err)
		}
	}
	return nil
}

// shownWith are the ids of the Feedback the judgement id's judge was shown
// as precedent, in the order shown.
func (s *Store) shownWith(ctx context.Context, id int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT feedback_id FROM judgement_precedents WHERE judgement_id = ? ORDER BY rank`, id)
	if err != nil {
		return nil, fmt.Errorf("precedents of judgement %d: %w", id, err)
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var f int64
		if err := rows.Scan(&f); err != nil {
			return nil, fmt.Errorf("precedents of judgement %d: %w", id, err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}


// StandingWrongOnRelations is the wrong standing on relations, newest
// first, each with the Spans at its two ends as their Items' newest readings
// read them.
func (s *Store) StandingWrongOnRelations(ctx context.Context) ([]editor.Precedent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.event_id, e.at, rel.from_source_id, rel.from_entry_id, rel.from_start, rel.from_end,
			rel.to_source_id, rel.to_entry_id, rel.to_start, rel.to_end, rel.kind
		FROM feedback_given f
			JOIN events e ON e.id = f.event_id
			JOIN relations rel ON rel.event_id = f.judgement_id
		WHERE f.kind = 'wrong' AND `+standing("f.event_id")+`
		ORDER BY f.event_id DESC`)
	type found struct {
		p      editor.Precedent
		at, to editor.Address
	}
	all, err := scanned(rows, err, func(rows *sql.Rows) (found, error) {
		var f found
		var at string
		err := rows.Scan(&f.p.FeedbackID, &at,
			&f.at.SourceID, &f.at.EntryID, &f.at.Span.Start, &f.at.Span.End,
			&f.to.SourceID, &f.to.EntryID, &f.to.Span.Start, &f.to.Span.End, &f.p.Relation)
		f.p.Kind, f.p.Judged, f.p.GivenAt = "wrong", "relation", parseTime(at)
		return f, err
	})
	if err != nil {
		return nil, fmt.Errorf("standing wrong on relations: %w", err)
	}
	out := make([]editor.Precedent, 0, len(all))
	for _, f := range all {
		p := f.p
		if p.At, err = s.SpanAt(ctx, f.at); err != nil {
			return nil, err
		}
		if p.To, err = s.SpanAt(ctx, f.to); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// StandingTaste is the unwanted and missed standing, newest first, each
// with the Span it lands on as its Item's newest reading reads it, and for
// unwanted the day of the Briefing it was given on.
func (s *Store) StandingTaste(ctx context.Context) ([]editor.Precedent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.event_id, f.kind, e.at, f.source_id, f.entry_id, f.span_start, f.span_end, coalesce(t.day, '')
		FROM feedback_given f
			JOIN events e ON e.id = f.event_id
			LEFT JOIN told_briefings t ON t.event_id = f.briefing_id
		WHERE f.kind IN ('unwanted', 'missed') AND `+standing("f.event_id")+`
		ORDER BY e.at DESC, f.event_id DESC`)
	type found struct {
		p  editor.Precedent
		at editor.Address
	}
	all, err := scanned(rows, err, func(rows *sql.Rows) (found, error) {
		var f found
		var at string
		err := rows.Scan(&f.p.FeedbackID, &f.p.Kind, &at, &f.at.SourceID, &f.at.EntryID,
			&f.at.Span.Start, &f.at.Span.End, &f.p.Day)
		f.p.GivenAt = parseTime(at)
		return f, err
	})
	if err != nil {
		return nil, fmt.Errorf("standing unwanted and missed: %w", err)
	}
	out := make([]editor.Precedent, 0, len(all))
	for _, f := range all {
		if f.p.At, err = s.SpanAt(ctx, f.at); err != nil {
			return nil, err
		}
		out = append(out, f.p)
	}
	return out, nil
}

// scanned is every row of a query's result (rows, or the query's error err)
// as scan reads it, the rows closed before it returns, so what the rows were
// read for can query again.
func scanned[T any](rows *sql.Rows, err error, scan func(*sql.Rows) (T, error)) ([]T, error) {
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
