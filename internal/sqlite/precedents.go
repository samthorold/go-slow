package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
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

// StandingWrong is the wrong standing on judgements of the kinds judged,
// newest first, each with the Spans at its two ends as their Items' newest
// readings read them; for reading's kinds, with the Span of the Cut its
// first end lies in and the names its reading gives in the Spans its ends
// lie in.
func (s *Store) StandingWrong(ctx context.Context, judged []string) ([]editor.Precedent, error) {
	if len(judged) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.event_id, je.kind, e.at,
			coalesce(rr.source_id, sr.source_id, rel.from_source_id),
			coalesce(rr.entry_id, sr.entry_id, rel.from_entry_id),
			coalesce(rt.from_start, sk.span_start, rel.from_start),
			coalesce(rt.from_end, sk.span_end, rel.from_end),
			coalesce(rr.source_id, sr.source_id, rel.to_source_id),
			coalesce(rr.entry_id, sr.entry_id, rel.to_entry_id),
			coalesce(rt.to_start, sk.speaker_start, rel.to_start),
			coalesce(rt.to_end, sk.speaker_end, rel.to_end),
			coalesce(rel.kind, ''), coalesce(rt.reading_id, sk.reading_id, 0)
		FROM feedback_given f
			JOIN events e ON e.id = f.event_id
			JOIN events je ON je.id = f.judgement_id
			LEFT JOIN refers_to rt ON rt.event_id = f.judgement_id
			LEFT JOIN readings rr ON rr.event_id = rt.reading_id
			LEFT JOIN speakers sk ON sk.event_id = f.judgement_id
			LEFT JOIN readings sr ON sr.event_id = sk.reading_id
			LEFT JOIN relations rel ON rel.event_id = f.judgement_id
		WHERE f.kind = 'wrong' AND je.kind IN (?`+strings.Repeat(", ?", len(judged)-1)+`)
			AND (rt.event_id IS NOT NULL OR sk.event_id IS NOT NULL OR rel.event_id IS NOT NULL)
			AND `+standing("f.event_id")+`
		ORDER BY f.event_id DESC`, anys(judged)...)
	if err != nil {
		return nil, fmt.Errorf("standing wrong: %w", err)
	}
	type found struct {
		p         editor.Precedent
		at, to    editor.Address
		readingID int64
	}
	var all []found
	for rows.Next() {
		var f found
		var at string
		if err := rows.Scan(&f.p.FeedbackID, &f.p.Judged, &at,
			&f.at.SourceID, &f.at.EntryID, &f.at.Span.Start, &f.at.Span.End,
			&f.to.SourceID, &f.to.EntryID, &f.to.Span.Start, &f.to.Span.End,
			&f.p.Relation, &f.readingID); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("standing wrong: %w", err)
		}
		f.p.Kind, f.p.GivenAt = "wrong", parseTime(at)
		all = append(all, f)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
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
		if f.readingID != 0 {
			if p.Within, p.Names, err = s.namedAround(ctx, f.readingID, p.At.Item.Content, f.at, f.to); err != nil {
				return nil, err
			}
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
	if err != nil {
		return nil, fmt.Errorf("standing unwanted and missed: %w", err)
	}
	type found struct {
		p  editor.Precedent
		at editor.Address
	}
	var all []found
	for rows.Next() {
		var f found
		var at string
		if err := rows.Scan(&f.p.FeedbackID, &f.p.Kind, &at, &f.at.SourceID, &f.at.EntryID,
			&f.at.Span.Start, &f.at.Span.End, &f.p.Day); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("standing unwanted and missed: %w", err)
		}
		f.p.GivenAt = parseTime(at)
		all = append(all, f)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
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

// namedAround is, by the reading id, the words of the Span of its Cut that
// at lies in, and the names it gives in the Spans of its Cut that at and to
// lie in, each once, as the Item's text gives them.
func (s *Store) namedAround(ctx context.Context, id int64, text string, at, to editor.Address) (string, []string, error) {
	r, err := s.readingByID(ctx, id)
	if err != nil {
		return "", nil, err
	}
	spans, err := s.cutSpans(ctx, r.CutID)
	if err != nil {
		return "", nil, err
	}
	around := func(a editor.Address) cut.Span {
		for _, sp := range spans {
			if sp.Start <= a.Span.Start && a.Span.End <= sp.End {
				return sp
			}
		}
		return a.Span
	}
	within := []cut.Span{around(at), around(to)}
	var names []string
	for _, n := range r.Names {
		in := slices.ContainsFunc(within, func(sp cut.Span) bool { return sp.Start <= n.At.Span.Start && n.At.Span.End <= sp.End })
		if w := cut.Words(text, n.At.Span); in && !slices.Contains(names, w) {
			names = append(names, w)
		}
	}
	return cut.Words(text, within[0]), names, nil
}

// anys is ss as arguments to a query.
func anys(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
