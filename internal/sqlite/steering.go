package sqlite

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/samthorold/go-slow/internal/interest"
)

// SourceCounts are, for every Source, what the Owner steers by over the
// window (from, to] (system design: Every Item is read, and Topics select
// nothing), worked out on every view and never stored: the Stories gathered
// in it that hold a Span of one of the Source's Items, as many as touch a
// Topic (one of their Spans names a Topic's terms) and as many as don't,
// and the Spans of its Items collected in it, as their newest live readings
// read them, and how many of those are Rejected. A Story gathered again
// from the same Span is one Story, as its latest gathering holds it.
func (s *Store) SourceCounts(ctx context.Context, from, to time.Time) ([]interest.SourceCounts, error) {
	topics, err := s.Topics(ctx)
	if err != nil {
		return nil, err
	}
	out, at, err := s.sourceSpans(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("source counts: %w", err)
	}
	stories, err := s.storiesGathered(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("source counts: %w", err)
	}
	for _, st := range stories {
		touching := false
		for _, sp := range st {
			touching = touching || slices.ContainsFunc(topics, func(t interest.Topic) bool { return t.Touches(sp.text) })
		}
		seen := map[string]bool{}
		for _, sp := range st {
			i, ok := at[sp.sourceID]
			if !ok || seen[sp.sourceID] {
				continue
			}
			seen[sp.sourceID] = true
			if touching {
				out[i].Touching++
			} else {
				out[i].Untouched++
			}
		}
	}
	return out, nil
}

// sourceSpans is every Source, by name, with its Spans of Items collected
// in (from, to] and how many are Rejected, and the index of each by its id.
func (s *Store) sourceSpans(ctx context.Context, from, to time.Time) ([]interest.SourceCounts, map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT sources.id,
			coalesce(sum((SELECT count(*) FROM cut_spans WHERE cut_spans.cut_id = r.cut_id)), 0),
			coalesce(sum((SELECT count(*) FROM rejections WHERE rejections.reading_id = r.event_id)), 0)
		FROM sources
			LEFT JOIN items ON items.source_id = sources.id AND items.collected_at > ? AND items.collected_at <= ?
			LEFT JOIN readings r ON r.event_id = `+newestLiveReading("items.source_id", "items.entry_id")+`
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

// gatheredSpan is a Span of a gathered Story: its Source, and its Item's
// text as kept between its offsets.
type gatheredSpan struct {
	sourceID string
	text     string
}

// storiesGathered are the Stories gathered in (from, to], each the Spans it
// holds (the Span it was gathered from among them), the latest gathering
// from each Span standing for the Story.
func (s *Store) storiesGathered(ctx context.Context, from, to time.Time) ([][]gatheredSpan, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH latest AS (
			SELECT max(g.event_id) AS id FROM gatherings g JOIN events e ON e.id = g.event_id
			WHERE e.at > ? AND e.at <= ?
			GROUP BY g.source_id, g.entry_id, g.span_start, g.span_end),
		spans AS (
			SELECT g.event_id AS id, g.source_id, g.entry_id, g.span_start, g.span_end
			FROM gatherings g JOIN latest ON latest.id = g.event_id
			UNION
			SELECT gs.gathering_id, gs.source_id, gs.entry_id, gs.span_start, gs.span_end
			FROM gathered_spans gs JOIN latest ON latest.id = gs.gathering_id)
		SELECT spans.id, spans.source_id,
			CAST(substr(CAST(items.content AS BLOB), spans.span_start + 1, spans.span_end - spans.span_start) AS TEXT)
		FROM spans JOIN items ON items.source_id = spans.source_id AND items.entry_id = spans.entry_id
		ORDER BY spans.id`, formatTime(from), formatTime(to))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out [][]gatheredSpan
	last := int64(-1)
	for rows.Next() {
		var id int64
		var sp gatheredSpan
		if err := rows.Scan(&id, &sp.sourceID, &sp.text); err != nil {
			return nil, err
		}
		if id != last {
			out = append(out, nil)
			last = id
		}
		out[len(out)-1] = append(out[len(out)-1], sp)
	}
	return out, rows.Err()
}
