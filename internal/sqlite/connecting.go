package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

// FactsToConnect are the Facts placed and not yet connected, by Item, in the
// order the Items were published. Connecting waits for placing, since the
// Facts a Fact is weighed against include those in its Stories.
func (s *Store) FactsToConnect(ctx context.Context) ([]editor.ToConnect, error) {
	out, err := s.toConnect(ctx, `EXISTS (SELECT 1 FROM fact_placings p WHERE p.fact_id = facts.id)
		AND NOT EXISTS (SELECT 1 FROM fact_connectings c WHERE c.fact_id = facts.id)`)
	if err != nil {
		return nil, fmt.Errorf("facts to connect: %w", err)
	}
	return out, nil
}

// FactsPlacedSince are the Facts placed at or after since, by Item, in the
// order the Items were published: the day's Facts, for connecting to run
// again over before the Briefing.
func (s *Store) FactsPlacedSince(ctx context.Context, since time.Time) ([]editor.ToConnect, error) {
	out, err := s.toConnect(ctx, `EXISTS (SELECT 1 FROM fact_placings p WHERE p.fact_id = facts.id AND p.placed_at >= ?)`,
		formatTime(since))
	if err != nil {
		return nil, fmt.Errorf("facts placed since: %w", err)
	}
	return out, nil
}

func (s *Store) toConnect(ctx context.Context, where string, args ...any) ([]editor.ToConnect, error) {
	byItem, err := s.factsByItem(ctx, where, args...)
	if err != nil {
		return nil, err
	}
	out := make([]editor.ToConnect, 0, len(byItem))
	for _, tp := range byItem {
		out = append(out, editor.ToConnect{Item: tp.Item, Facts: tp.Facts})
	}
	return out, nil
}

// FactsNear are at most limit other Facts that share an Entity with any of
// facts, or are Developments in a Story with one, those nearest the latest of
// facts in date first.
func (s *Store) FactsNear(ctx context.Context, facts []editor.Fact, limit int) ([]editor.Fact, error) {
	var ids []int64
	var latest time.Time
	for _, f := range facts {
		ids = append(ids, f.ID)
		if f.AsAt.After(latest) {
			latest = f.AsAt
		}
	}
	mine, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH mine(id) AS (SELECT value FROM json_each(?1)),
		near(id) AS (
			SELECT theirs.fact_id FROM fact_entities ours
				JOIN fact_entities theirs ON theirs.entity_id = ours.entity_id
			WHERE ours.fact_id IN mine
			UNION
			SELECT theirs.fact_id FROM developments ours
				JOIN developments theirs ON theirs.story_id = ours.story_id
			WHERE ours.fact_id IN mine
		)
		SELECT facts.id, facts.source_id, facts.entry_id, facts.statement, facts.as_at, facts.quote, `+factEntities+`
		FROM facts
		WHERE facts.id IN near AND facts.id NOT IN mine AND facts.withdrawn_by IS NULL
		ORDER BY abs(julianday(facts.as_at) - julianday(?2)), facts.id DESC
		LIMIT ?3`, string(mine), latest.Format(time.DateOnly), limit)
	if err != nil {
		return nil, fmt.Errorf("facts near: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.Fact
	for rows.Next() {
		var f editor.Fact
		var asAt, ents string
		if err := rows.Scan(&f.ID, &f.SourceID, &f.EntryID, &f.Statement, &asAt, &f.Quote, &ents); err != nil {
			return nil, fmt.Errorf("facts near: %w", err)
		}
		if err := scanFact(&f, asAt, ents); err != nil {
			return nil, fmt.Errorf("facts near: %w", err)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("facts near: %w", err)
	}
	return out, nil
}

// SaveConnecting gives each Entity its role in its Fact, keeps the cause
// Connections, and marks every Fact connected, all at once. A Connection
// already kept is kept once.
func (s *Store) SaveConnecting(ctx context.Context, c editor.Connecting, connectedAt time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, r := range c.Roles {
		if _, err := tx.ExecContext(ctx, `
			UPDATE fact_entities SET role = ?
			WHERE fact_id = ? AND entity_id = (SELECT id FROM entities WHERE name = ?)`,
			r.Role, r.FactID, r.Entity); err != nil {
			return fmt.Errorf("role of %q in fact %d: %w", r.Entity, r.FactID, err)
		}
	}
	for _, cs := range c.Causes {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO fact_connections (from_fact_id, to_fact_id, kind, made_at, model)
			VALUES (?, ?, 'cause', ?, ?) ON CONFLICT DO NOTHING`,
			cs.CauseID, cs.EffectID, formatTime(connectedAt), c.Model); err != nil {
			return fmt.Errorf("cause from fact %d to %d: %w", cs.CauseID, cs.EffectID, err)
		}
	}
	for _, f := range c.Facts {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO fact_connectings (fact_id, connected_at, model) VALUES (?, ?, ?)
			ON CONFLICT (fact_id) DO UPDATE SET connected_at = excluded.connected_at, model = excluded.model`,
			f, formatTime(connectedAt), c.Model); err != nil {
			return fmt.Errorf("mark fact %d connected: %w", f, err)
		}
	}
	return tx.Commit()
}

// traced selects a Fact, with the title of its Item, from the facts table
// aliased as f joined to items as i.
func traced(f, i string) string {
	return f + `.id, ` + f + `.source_id, ` + f + `.entry_id, ` + f + `.statement, ` + f + `.as_at, ` + f + `.quote,
	(SELECT json_group_array(entities.name ORDER BY entities.name)
		FROM fact_entities JOIN entities ON entities.id = fact_entities.entity_id
		WHERE fact_entities.fact_id = ` + f + `.id), ` + i + `.title`
}

// ConnectedStories are the Stories connected to the Story id, derived from
// what they share: Developments, and Entities their Facts involve in the same
// role. For each Entity and role, the latest pair of Facts shows why.
func (s *Store) ConnectedStories(ctx context.Context, id int64) ([]editor.StoryConnection, error) {
	var out []editor.StoryConnection
	at := map[int64]int{}
	story := func(id int64, title string) *editor.StoryConnection {
		n, ok := at[id]
		if !ok {
			n = len(out)
			at[id] = n
			out = append(out, editor.StoryConnection{ID: id, Title: title})
		}
		return &out[n]
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT other.story_id, stories.title, `+traced("f", "i")+`
		FROM developments mine
			JOIN developments other ON other.fact_id = mine.fact_id AND other.story_id != mine.story_id
			JOIN stories ON stories.id = other.story_id
			JOIN facts f ON f.id = mine.fact_id JOIN items i ON i.source_id = f.source_id AND i.entry_id = f.entry_id
		WHERE mine.story_id = ? AND mine.withdrawn_by IS NULL AND other.withdrawn_by IS NULL
		ORDER BY other.story_id, f.as_at, f.id`, id)
	if err != nil {
		return nil, fmt.Errorf("connected stories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var other int64
		var title string
		var d editor.Development
		var asAt, ents string
		if err := rows.Scan(&other, &title, &d.ID, &d.SourceID, &d.EntryID, &d.Statement, &asAt, &d.Quote, &ents, &d.ItemTitle); err != nil {
			return nil, fmt.Errorf("connected stories: %w", err)
		}
		if err := scanFact(&d.Fact, asAt, ents); err != nil {
			return nil, fmt.Errorf("connected stories: %w", err)
		}
		c := story(other, title)
		c.Shared = append(c.Shared, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("connected stories: %w", err)
	}
	_ = rows.Close()

	rows, err = s.db.QueryContext(ctx, `
		SELECT other.story_id, stories.title, entities.name, here.role, `+traced("f", "i")+`, `+traced("g", "j")+`
		FROM developments mine
			JOIN fact_entities here ON here.fact_id = mine.fact_id AND here.role IS NOT NULL
			JOIN fact_entities there ON there.entity_id = here.entity_id AND there.role = here.role
				AND there.fact_id != here.fact_id
			JOIN developments other ON other.fact_id = there.fact_id AND other.story_id != mine.story_id
			JOIN stories ON stories.id = other.story_id
			JOIN entities ON entities.id = here.entity_id
			JOIN facts f ON f.id = here.fact_id JOIN items i ON i.source_id = f.source_id AND i.entry_id = f.entry_id
			JOIN facts g ON g.id = there.fact_id JOIN items j ON j.source_id = g.source_id AND j.entry_id = g.entry_id
		WHERE mine.story_id = ? AND mine.withdrawn_by IS NULL AND other.withdrawn_by IS NULL
		ORDER BY other.story_id, entities.name, here.role, f.as_at DESC, g.as_at DESC, f.id DESC, g.id DESC`, id)
	if err != nil {
		return nil, fmt.Errorf("connected stories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	type reason struct {
		story        int64
		entity, role string
	}
	seen := map[reason]bool{}
	for rows.Next() {
		var other int64
		var title string
		var e editor.SharedEntity
		var asAt, ents, asAt2, ents2 string
		if err := rows.Scan(&other, &title, &e.Entity, &e.Role,
			&e.Here.ID, &e.Here.SourceID, &e.Here.EntryID, &e.Here.Statement, &asAt, &e.Here.Quote, &ents, &e.Here.ItemTitle,
			&e.There.ID, &e.There.SourceID, &e.There.EntryID, &e.There.Statement, &asAt2, &e.There.Quote, &ents2, &e.There.ItemTitle,
		); err != nil {
			return nil, fmt.Errorf("connected stories: %w", err)
		}
		if r := (reason{other, e.Entity, e.Role}); seen[r] {
			continue
		} else {
			seen[r] = true
		}
		if err := scanFact(&e.Here.Fact, asAt, ents); err != nil {
			return nil, fmt.Errorf("connected stories: %w", err)
		}
		if err := scanFact(&e.There.Fact, asAt2, ents2); err != nil {
			return nil, fmt.Errorf("connected stories: %w", err)
		}
		c := story(other, title)
		c.Entities = append(c.Entities, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("connected stories: %w", err)
	}
	return out, nil
}

// storyCauses are the cause Connections to or from the Developments of the
// Story id, in the order of their effects' dates.
func (s *Store) storyCauses(ctx context.Context, id int64) ([]editor.CauseConnection, error) {
	return s.causes(ctx, `EXISTS (SELECT 1 FROM developments d
		WHERE d.story_id = ? AND d.fact_id IN (c.from_fact_id, c.to_fact_id))`, id)
}

// Causes are every cause Connection in the Record, each Fact traced to its
// Item, in the order of their effects' dates.
func (s *Store) Causes(ctx context.Context) ([]editor.CauseConnection, error) {
	return s.causes(ctx, `1`)
}

func (s *Store) causes(ctx context.Context, where string, args ...any) ([]editor.CauseConnection, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+traced("f", "i")+`, `+traced("g", "j")+`, c.withdrawn_by IS NOT NULL
		FROM fact_connections c
			JOIN facts f ON f.id = c.from_fact_id JOIN items i ON i.source_id = f.source_id AND i.entry_id = f.entry_id
			JOIN facts g ON g.id = c.to_fact_id JOIN items j ON j.source_id = g.source_id AND j.entry_id = g.entry_id
		WHERE c.kind = 'cause' AND `+where+`
		ORDER BY g.as_at, f.as_at, g.id, f.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("causes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.CauseConnection
	for rows.Next() {
		var c editor.CauseConnection
		var asAt, ents, asAt2, ents2 string
		if err := rows.Scan(
			&c.Cause.ID, &c.Cause.SourceID, &c.Cause.EntryID, &c.Cause.Statement, &asAt, &c.Cause.Quote, &ents, &c.Cause.ItemTitle,
			&c.Effect.ID, &c.Effect.SourceID, &c.Effect.EntryID, &c.Effect.Statement, &asAt2, &c.Effect.Quote, &ents2, &c.Effect.ItemTitle,
			&c.Withdrawn,
		); err != nil {
			return nil, fmt.Errorf("causes: %w", err)
		}
		if err := scanFact(&c.Cause.Fact, asAt, ents); err != nil {
			return nil, fmt.Errorf("causes: %w", err)
		}
		if err := scanFact(&c.Effect.Fact, asAt2, ents2); err != nil {
			return nil, fmt.Errorf("causes: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
