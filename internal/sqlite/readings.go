package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// The versions of the shapes of a reading's events this build writes.
const (
	readingShape  = 1
	refersToShape = 1
	nameShape     = 1
	speakerShape  = 1
	rejectedShape = 1
)

// ItemsToRead are the Items awaiting reading: every Item a Cut lists the
// Spans of and no reading has read, whatever it is about, each with its
// newest Cut, oldest first. Work marked failed awaits nothing.
func (s *Store) ItemsToRead(ctx context.Context) ([]editor.ToRead, error) {
	out, err := s.uncutItemsToRead(ctx)
	if err != nil {
		return nil, err
	}
	for i, tr := range out {
		c, _, _, err := s.NewestCut(ctx, tr.Item.SourceID, tr.Item.EntryID)
		if err != nil {
			return nil, err
		}
		out[i].Cut = c
	}
	return out, nil
}

// uncutItemsToRead are the Items awaiting reading, as ItemsToRead lists
// them, before their Cuts are read.
func (s *Store) uncutItemsToRead(ctx context.Context) ([]editor.ToRead, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT items.source_id, items.entry_id, items.title, items.link, items.summary,
			items.content, items.published
		FROM items
		WHERE EXISTS (SELECT 1 FROM cuts
				WHERE cuts.source_id = items.source_id AND cuts.entry_id = items.entry_id)
			AND NOT EXISTS (SELECT 1 FROM readings r
				WHERE r.source_id = items.source_id AND r.entry_id = items.entry_id)
			AND `+notFailed("reading")+`
		ORDER BY items.published, items.collected_at, items.source_id, items.entry_id`)
	if err != nil {
		return nil, fmt.Errorf("items to read: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.ToRead
	for rows.Next() {
		var it editor.Item
		var published string
		if err := rows.Scan(&it.SourceID, &it.EntryID, &it.Title, &it.Link, &it.Summary,
			&it.Content, &published); err != nil {
			return nil, fmt.Errorf("items to read: %w", err)
		}
		it.Published = parseTime(published)
		out = append(out, editor.ToRead{Item: it})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("items to read: %w", err)
	}
	return out, nil
}

// KeepReading keeps a reading as an event, with its Judge, and each of its
// judgements as an event of its own, tied to it, all at once. A judgement
// addressing another Item than the reading's is refused, and nothing is
// kept: what a Span refers to, names or who speaks it is judged from its
// own Item alone.
func (s *Store) KeepReading(ctx context.Context, r editor.Reading) error {
	for _, a := range readingAddresses(r) {
		if a.SourceID != r.SourceID || a.EntryID != r.EntryID {
			return fmt.Errorf("keep reading of %s: a judgement addresses another Item, %s %s", r.EntryID, a.SourceID, a.EntryID)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	at := formatTime(r.At)
	event := func(kind string, shape int) (int64, error) {
		res, err := tx.ExecContext(ctx, `INSERT INTO events (kind, shape, at) VALUES (?, ?, ?)`, kind, shape, at)
		if err != nil {
			return 0, fmt.Errorf("keep reading of %s: %s: %w", r.EntryID, kind, err)
		}
		return res.LastInsertId()
	}
	id, err := event("reading", readingShape)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO readings (event_id, source_id, entry_id, cut_id, act, version, model, request_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, r.SourceID, r.EntryID, r.CutID, r.Judge.Act, r.Judge.Version, r.Judge.Model, r.Judge.RequestHash); err != nil {
		return fmt.Errorf("keep reading of %s: %w", r.EntryID, err)
	}
	keep := func(kind string, shape int, query string, args ...any) error {
		eid, err := event(kind, shape)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, query, append([]any{eid, id}, args...)...); err != nil {
			return fmt.Errorf("keep reading of %s: %s: %w", r.EntryID, kind, err)
		}
		return nil
	}
	for _, ref := range r.RefersTo {
		if err := keep("refers_to", refersToShape, `
			INSERT INTO refers_to (event_id, reading_id, from_start, from_end, to_start, to_end)
			VALUES (?, ?, ?, ?, ?, ?)`,
			ref.From.Span.Start, ref.From.Span.End, ref.To.Span.Start, ref.To.Span.End); err != nil {
			return err
		}
	}
	for _, n := range r.Names {
		if err := keep("name", nameShape, `
			INSERT INTO span_names (event_id, reading_id, span_start, span_end, name, kind, office)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			n.At.Span.Start, n.At.Span.End, n.Name, n.Kind, n.Office); err != nil {
			return err
		}
	}
	for _, sp := range r.Speakers {
		if err := keep("speaker", speakerShape, `
			INSERT INTO speakers (event_id, reading_id, span_start, span_end, speaker_start, speaker_end)
			VALUES (?, ?, ?, ?, ?, ?)`,
			sp.Of.Span.Start, sp.Of.Span.End, sp.By.Span.Start, sp.By.Span.End); err != nil {
			return err
		}
	}
	for _, a := range r.Rejected {
		if err := keep("rejected", rejectedShape, `
			INSERT INTO rejections (event_id, reading_id, span_start, span_end) VALUES (?, ?, ?, ?)`,
			a.Span.Start, a.Span.End); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// readingAddresses are every address r's judgements are about.
func readingAddresses(r editor.Reading) []editor.Address {
	var out []editor.Address
	for _, ref := range r.RefersTo {
		out = append(out, ref.From, ref.To)
	}
	for _, n := range r.Names {
		out = append(out, n.At)
	}
	for _, sp := range r.Speakers {
		out = append(out, sp.Of, sp.By)
	}
	return append(out, r.Rejected...)
}

// NewestReading is the newest reading of the Item, with its judgements,
// reporting whether the Item has one.
func (s *Store) NewestReading(ctx context.Context, sourceID, entryID string) (editor.Reading, bool, error) {
	r := editor.Reading{SourceID: sourceID, EntryID: entryID}
	var at string
	err := s.db.QueryRowContext(ctx, `
		SELECT readings.event_id, readings.cut_id, readings.act, readings.version, readings.model,
			readings.request_hash, events.at, `+spansReadingCall("readings")+`
		FROM readings JOIN events ON events.id = readings.event_id
		WHERE readings.source_id = ? AND readings.entry_id = ?
		ORDER BY readings.event_id DESC LIMIT 1`, sourceID, entryID,
	).Scan(&r.ID, &r.CutID, &r.Judge.Act, &r.Judge.Version, &r.Judge.Model, &r.Judge.RequestHash, &at, &r.Call)
	if errors.Is(err, sql.ErrNoRows) {
		return editor.Reading{}, false, nil
	}
	if err != nil {
		return editor.Reading{}, false, fmt.Errorf("newest reading of %s: %w", entryID, err)
	}
	r.At = parseTime(at)
	addr := func(start, end int) editor.Address {
		return editor.Address{SourceID: sourceID, EntryID: entryID, Span: cut.Span{Start: start, End: end}}
	}
	each := func(query string, scan func(*sql.Rows) error) error {
		rows, err := s.db.QueryContext(ctx, query, r.ID)
		if err != nil {
			return fmt.Errorf("newest reading of %s: %w", entryID, err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return fmt.Errorf("newest reading of %s: %w", entryID, err)
			}
		}
		return rows.Err()
	}
	if err := each(`SELECT from_start, from_end, to_start, to_end FROM refers_to WHERE reading_id = ? ORDER BY event_id`,
		func(rows *sql.Rows) error {
			var fs, fe, ts, te int
			if err := rows.Scan(&fs, &fe, &ts, &te); err != nil {
				return err
			}
			r.RefersTo = append(r.RefersTo, editor.RefersTo{From: addr(fs, fe), To: addr(ts, te)})
			return nil
		}); err != nil {
		return editor.Reading{}, false, err
	}
	if err := each(`SELECT span_start, span_end, name, kind, office FROM span_names WHERE reading_id = ? ORDER BY event_id`,
		func(rows *sql.Rows) error {
			var start, end int
			var n editor.Name
			if err := rows.Scan(&start, &end, &n.Name, &n.Kind, &n.Office); err != nil {
				return err
			}
			n.At = addr(start, end)
			r.Names = append(r.Names, n)
			return nil
		}); err != nil {
		return editor.Reading{}, false, err
	}
	if err := each(`SELECT span_start, span_end, speaker_start, speaker_end FROM speakers WHERE reading_id = ? ORDER BY event_id`,
		func(rows *sql.Rows) error {
			var s0, s1, b0, b1 int
			if err := rows.Scan(&s0, &s1, &b0, &b1); err != nil {
				return err
			}
			r.Speakers = append(r.Speakers, editor.Speaker{Of: addr(s0, s1), By: addr(b0, b1)})
			return nil
		}); err != nil {
		return editor.Reading{}, false, err
	}
	if err := each(`SELECT span_start, span_end FROM rejections WHERE reading_id = ? ORDER BY event_id`,
		func(rows *sql.Rows) error {
			var start, end int
			if err := rows.Scan(&start, &end); err != nil {
				return err
			}
			r.Rejected = append(r.Rejected, addr(start, end))
			return nil
		}); err != nil {
		return editor.Reading{}, false, err
	}
	return r, true, nil
}
