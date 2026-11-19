package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/samthorold/go-slow/internal/interest"
)

// SourceCounts are, for every Source, what the Owner steers by over the
// window (from, to] (system design: Every Item is cut and hydrated, and
// Topics gate none of the work), worked out on every view and never stored:
// the Stories the Editor made that hold a Span of one of the Source's Items
// collected in it, as many as touch a Topic (storyTouches) and as many as
// don't, whenever they were made, and the Spans of its Items collected in
// it, as their newest live hydrations read them, and how many of those are
// Rejected.
func (s *Store) SourceCounts(ctx context.Context, from, to time.Time) ([]interest.SourceCounts, error) {
	topics, err := s.Topics(ctx)
	if err != nil {
		return nil, err
	}
	out, at, err := s.sourceSpans(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("source counts: %w", err)
	}
	made, err := s.StoriesMade(ctx)
	if err != nil {
		return nil, fmt.Errorf("source counts: %w", err)
	}
	touching := map[int64]bool{}
	for _, st := range made {
		touching[st.ID] = storyTouches(st, topics)
	}
	contributions, err := s.contributions(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("source counts: %w", err)
	}
	for _, c := range contributions {
		i, ok := at[c.sourceID]
		if !ok {
			continue
		}
		if touching[c.storyID] {
			out[i].Touching++
		} else {
			out[i].Untouched++
		}
	}
	return out, nil
}

// contribution is a Source contributing to a Story the Editor made.
type contribution struct {
	storyID  int64
	sourceID string
}

// contributions are the Sources contributing to each Story the Editor made
// that holds a Span of an Item collected in (from, to]: one for each Story
// and Source of such an Item, however many of its Spans the Story holds.
func (s *Store) contributions(ctx context.Context, from, to time.Time) ([]contribution, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT c.story_id, c.source_id
		FROM editor_span_cites c
			JOIN items ON items.source_id = c.source_id AND items.entry_id = c.entry_id
		WHERE items.collected_at > ? AND items.collected_at <= ?`, formatTime(from), formatTime(to))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []contribution
	for rows.Next() {
		var c contribution
		if err := rows.Scan(&c.storyID, &c.sourceID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// sourceSpans is every Source, by name, with its Spans of Items collected
// in (from, to] and how many are Rejected, and the index of each by its id.
func (s *Store) sourceSpans(ctx context.Context, from, to time.Time) ([]interest.SourceCounts, map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT sources.id,
			coalesce(sum((SELECT count(*) FROM cut_spans WHERE cut_spans.cut_id = r.cut_id)), 0),
			coalesce(sum((SELECT count(*) FROM rejections WHERE rejections.hydration_id = r.event_id)), 0)
		FROM sources
			LEFT JOIN items ON items.source_id = sources.id AND items.collected_at > ? AND items.collected_at <= ?
			LEFT JOIN hydrations r ON r.event_id = `+newestLiveHydration("items.source_id", "items.entry_id")+`
		GROUP BY sources.id
		ORDER BY sources.name`, formatTime(from), formatTime(to))
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []interest.SourceCounts
	at := map[string]int{}
	for rows.Next() {
		var c interest.SourceCounts
		if err := rows.Scan(&c.SourceID, &c.Spans, &c.Rejected); err != nil {
			return nil, nil, err
		}
		at[c.SourceID] = len(out)
		out = append(out, c)
	}
	return out, at, rows.Err()
}
