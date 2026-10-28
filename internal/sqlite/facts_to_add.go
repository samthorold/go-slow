package sqlite

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/samthorold/go-slow/internal/editor"
)

// FactsToAdd are the Facts the Owner may say a Story missed, best first: at
// most limit of those whose Items mention an Entity its Developments' Items
// mention, and are not yet one in it, found as placing finds its candidates
// (system design: Each kind of Feedback has the levels it can fix). Each is
// ranked as placing ranks candidates, by the Entities it shares with the
// Story, each weighted by how few Stories it is in, and the newest first
// where they tie. Withdrawn Facts are never offered.
func (s *Store) FactsToAdd(ctx context.Context, storyID int64, limit int) ([]editor.Fact, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH live AS (
			SELECT developments.story_id, facts.source_id, facts.entry_id
			FROM developments JOIN facts ON facts.id = developments.fact_id
			WHERE developments.withdrawn_by IS NULL AND facts.withdrawn_by IS NULL AND NOT `+addedByUndone+`
		),
		mentioned AS (
			SELECT DISTINCT live.story_id, m.entity_id
			FROM live JOIN item_mentions m ON m.source_id = live.source_id AND m.entry_id = live.entry_id
		),
		ours AS (SELECT entity_id FROM mentioned WHERE story_id = ?1)
		SELECT DISTINCT facts.id, facts.source_id, facts.entry_id, facts.statement, facts.as_at, facts.quote,
			ours.entity_id,
			(SELECT count(*) FROM mentioned m WHERE m.entity_id = ours.entity_id),
			(SELECT count(DISTINCT story_id) FROM live)
		FROM ours
			JOIN item_mentions theirs ON theirs.entity_id = ours.entity_id
			JOIN facts ON facts.source_id = theirs.source_id AND facts.entry_id = theirs.entry_id
		WHERE facts.withdrawn_by IS NULL
			AND NOT EXISTS (SELECT 1 FROM developments d WHERE d.story_id = ?1 AND d.fact_id = facts.id)
		ORDER BY facts.id, ours.entity_id`, storyID)
	if err != nil {
		return nil, fmt.Errorf("facts to add: %w", err)
	}
	defer func() { _ = rows.Close() }()
	type ranked struct {
		fact  editor.Fact
		score float64
	}
	var out []ranked
	for rows.Next() {
		var f editor.Fact
		var asAt string
		var entity int64
		var in, stories int
		if err := rows.Scan(&f.ID, &f.SourceID, &f.EntryID, &f.Statement, &asAt, &f.Quote, &entity, &in, &stories); err != nil {
			return nil, fmt.Errorf("facts to add: %w", err)
		}
		if n := len(out); n == 0 || out[n-1].fact.ID != f.ID {
			if err := scanFact(&f, asAt); err != nil {
				return nil, fmt.Errorf("facts to add: %w", err)
			}
			out = append(out, ranked{fact: f})
		}
		out[len(out)-1].score += rarity(in, stories)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("facts to add: %w", err)
	}
	slices.SortStableFunc(out, func(a, b ranked) int {
		if c := cmp.Compare(b.score, a.score); c != 0 {
			return c
		}
		if c := b.fact.AsAt.Compare(a.fact.AsAt); c != 0 {
			return c
		}
		return cmp.Compare(b.fact.ID, a.fact.ID)
	})
	facts := make([]editor.Fact, 0, min(limit, len(out)))
	for _, r := range out[:min(limit, len(out))] {
		facts = append(facts, r.fact)
	}
	return facts, nil
}
