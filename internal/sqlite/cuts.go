package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
)

// cutShape is the version of a Cut event's shape this build writes.
const cutShape = 1

// Uncut lists the Items with text that no Cut lists the Spans of, apart
// from scheduled events. An Item with no text yet is left uncut until it
// is given its text, so no Cut addresses text that changes.
func (s *Store) Uncut(ctx context.Context) ([]cut.Text, error) {
	return s.uncut(ctx, `1`)
}

// UncutPublished lists the Items Uncut lists that were published from from
// up to, not including, to.
func (s *Store) UncutPublished(ctx context.Context, from, to time.Time) ([]cut.Text, error) {
	return s.uncut(ctx, `items.published >= ? AND items.published < ?`, formatTime(from), formatTime(to))
}

// uncut lists the Items Uncut lists that meet the condition where.
func (s *Store) uncut(ctx context.Context, where string, args ...any) ([]cut.Text, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT items.source_id, items.entry_id, items.content
		FROM items
		WHERE items.content != '' AND items.due = ''
			AND NOT EXISTS (SELECT 1 FROM cuts
				WHERE cuts.source_id = items.source_id AND cuts.entry_id = items.entry_id)
			AND `+where+`
		ORDER BY items.collected_at, items.source_id, items.entry_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("uncut items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []cut.Text
	for rows.Next() {
		var t cut.Text
		if err := rows.Scan(&t.SourceID, &t.EntryID, &t.Text); err != nil {
			return nil, fmt.Errorf("uncut items: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("uncut items: %w", err)
	}
	return out, nil
}

// KeepCut keeps a Cut as an event, with its Spans, in one transaction.
func (s *Store) KeepCut(ctx context.Context, c cut.Cut) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO events (kind, shape, at) VALUES ('cut', ?, ?)`, cutShape, formatTime(c.At))
	if err != nil {
		return fmt.Errorf("keep cut: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO cuts (event_id, source_id, entry_id, cutter, cutter_version)
		VALUES (?, ?, ?, ?, ?)`,
		id, c.SourceID, c.EntryID, c.Judge.Cutter, c.Judge.Version); err != nil {
		return fmt.Errorf("keep cut: %w", err)
	}
	for i, sp := range c.Spans {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO cut_spans (cut_id, n, span_start, span_end) VALUES (?, ?, ?, ?)`,
			id, i+1, sp.Start, sp.End); err != nil {
			return fmt.Errorf("keep cut: span %d: %w", i+1, err)
		}
	}
	return tx.Commit()
}

// NewestCut is the newest Cut of the Item, with the Item's text as kept,
// reporting whether the Item has one.
func (s *Store) NewestCut(ctx context.Context, sourceID, entryID string) (cut.Cut, string, bool, error) {
	c := cut.Cut{SourceID: sourceID, EntryID: entryID}
	var at, text string
	err := s.db.QueryRowContext(ctx, `
		SELECT cuts.event_id, cuts.cutter, cuts.cutter_version, events.at, items.content
		FROM cuts JOIN events ON events.id = cuts.event_id
			JOIN items USING (source_id, entry_id)
		WHERE cuts.source_id = ? AND cuts.entry_id = ?
		ORDER BY cuts.event_id DESC LIMIT 1`, sourceID, entryID,
	).Scan(&c.ID, &c.Judge.Cutter, &c.Judge.Version, &at, &text)
	if errors.Is(err, sql.ErrNoRows) {
		return cut.Cut{}, "", false, nil
	}
	if err != nil {
		return cut.Cut{}, "", false, fmt.Errorf("newest cut of %s: %w", entryID, err)
	}
	c.At = parseTime(at)
	rows, err := s.db.QueryContext(ctx,
		`SELECT span_start, span_end FROM cut_spans WHERE cut_id = ? ORDER BY n`, c.ID)
	if err != nil {
		return cut.Cut{}, "", false, fmt.Errorf("newest cut of %s: %w", entryID, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var sp cut.Span
		if err := rows.Scan(&sp.Start, &sp.End); err != nil {
			return cut.Cut{}, "", false, fmt.Errorf("newest cut of %s: %w", entryID, err)
		}
		c.Spans = append(c.Spans, sp)
	}
	if err := rows.Err(); err != nil {
		return cut.Cut{}, "", false, fmt.Errorf("newest cut of %s: %w", entryID, err)
	}
	return c, text, true, nil
}
