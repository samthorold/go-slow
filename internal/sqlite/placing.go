package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

// factEntities is a subquery giving a Fact's Entities as a JSON array.
const factEntities = `(SELECT json_group_array(entities.name ORDER BY entities.name)
	FROM fact_entities JOIN entities ON entities.id = fact_entities.entity_id
	WHERE fact_entities.fact_id = facts.id)`

// FactsToPlace are the Facts not yet placed, by Item, in the order the
// Items were published.
func (s *Store) FactsToPlace(ctx context.Context) ([]editor.ToPlace, error) {
	out, err := s.factsByItem(ctx, `NOT EXISTS (SELECT 1 FROM fact_placings p WHERE p.fact_id = facts.id)`)
	if err != nil {
		return nil, fmt.Errorf("facts to place: %w", err)
	}
	return out, nil
}

// factsByItem are the Facts matching where, by Item, in the order the Items
// were published.
func (s *Store) factsByItem(ctx context.Context, where string, args ...any) ([]editor.ToPlace, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT facts.id, facts.source_id, facts.entry_id, facts.statement, facts.as_at, facts.quote,
			`+factEntities+`, items.title, items.link, items.published
		FROM facts JOIN items USING (source_id, entry_id)
		WHERE `+where+`
		ORDER BY items.published, items.collected_at, facts.source_id, facts.entry_id, facts.id`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []editor.ToPlace
	for rows.Next() {
		var f editor.Fact
		var it editor.Item
		var asAt, ents, published string
		if err := rows.Scan(&f.ID, &f.SourceID, &f.EntryID, &f.Statement, &asAt, &f.Quote, &ents,
			&it.Title, &it.Link, &published); err != nil {
			return nil, err
		}
		if err := scanFact(&f, asAt, ents); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].Item.SourceID != f.SourceID || out[n-1].Item.EntryID != f.EntryID {
			it.SourceID, it.EntryID, it.Published = f.SourceID, f.EntryID, parseTime(published)
			out = append(out, editor.ToPlace{Item: it})
		}
		out[len(out)-1].Facts = append(out[len(out)-1].Facts, f)
	}
	return out, rows.Err()
}

func scanFact(f *editor.Fact, asAt, ents string) error {
	t, err := time.Parse(time.DateOnly, asAt)
	if err != nil {
		return fmt.Errorf("as at: %w", err)
	}
	f.AsAt = t
	if err := json.Unmarshal([]byte(ents), &f.Entities); err != nil {
		return fmt.Errorf("entities: %w", err)
	}
	return nil
}

// StoriesSharing are the Stories whose Developments involve any of the
// named Entities, with their Timelines.
func (s *Store) StoriesSharing(ctx context.Context, entities []string) ([]editor.Story, error) {
	ids, err := s.storiesSharing(ctx, entities)
	if err != nil {
		return nil, fmt.Errorf("stories sharing: %w", err)
	}
	out := make([]editor.Story, 0, len(ids))
	for _, id := range ids {
		st, _, err := s.Story(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// storiesSharing are the ids of the Stories whose Developments involve any
// of the named Entities.
func (s *Store) storiesSharing(ctx context.Context, entities []string) ([]int64, error) {
	names, err := json.Marshal(entities)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT developments.story_id
		FROM developments
			JOIN fact_entities ON fact_entities.fact_id = developments.fact_id
			JOIN entities ON entities.id = fact_entities.entity_id
		WHERE entities.name IN (SELECT value FROM json_each(?))
		ORDER BY developments.story_id`, string(names))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Story is one Story with its Timeline, reporting whether it is in the
// Record.
func (s *Store) Story(ctx context.Context, id int64) (editor.Story, bool, error) {
	st := editor.Story{ID: id}
	err := s.db.QueryRowContext(ctx, `SELECT title FROM stories WHERE id = ?`, id).Scan(&st.Title)
	if errors.Is(err, sql.ErrNoRows) {
		return st, false, nil
	}
	if err != nil {
		return st, false, fmt.Errorf("story %d: %w", id, err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT facts.id, facts.source_id, facts.entry_id, facts.statement, facts.as_at, facts.quote,
			`+factEntities+`, items.title
		FROM developments JOIN facts ON facts.id = developments.fact_id
			JOIN items USING (source_id, entry_id)
		WHERE developments.story_id = ?
		ORDER BY facts.as_at, items.published, facts.id`, id)
	if err != nil {
		return st, false, fmt.Errorf("story %d: %w", id, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var d editor.Development
		var asAt, ents string
		if err := rows.Scan(&d.ID, &d.SourceID, &d.EntryID, &d.Statement, &asAt, &d.Quote, &ents, &d.ItemTitle); err != nil {
			return st, false, fmt.Errorf("story %d: %w", id, err)
		}
		if err := scanFact(&d.Fact, asAt, ents); err != nil {
			return st, false, fmt.Errorf("story %d: %w", id, err)
		}
		for _, e := range d.Entities {
			if !slices.Contains(st.Entities, e) {
				st.Entities = append(st.Entities, e)
			}
		}
		st.Timeline = append(st.Timeline, d)
	}
	if err := rows.Err(); err != nil {
		return st, false, fmt.Errorf("story %d: %w", id, err)
	}
	slices.Sort(st.Entities)
	if st.Causes, err = s.storyCauses(ctx, id); err != nil {
		return st, false, fmt.Errorf("story %d: %w", id, err)
	}
	return st, true, nil
}

// ListStories lists the Stories, the one moved on most recently first.
func (s *Store) ListStories(ctx context.Context) ([]editor.StoryListing, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT stories.id, stories.title, count(facts.id), coalesce(max(facts.as_at), '')
		FROM stories LEFT JOIN developments ON developments.story_id = stories.id
			LEFT JOIN facts ON facts.id = developments.fact_id
		GROUP BY stories.id
		ORDER BY max(facts.as_at) DESC, stories.id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list stories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.StoryListing
	for rows.Next() {
		var l editor.StoryListing
		var latest string
		if err := rows.Scan(&l.ID, &l.Title, &l.Developments, &latest); err != nil {
			return nil, fmt.Errorf("list stories: %w", err)
		}
		if latest != "" {
			if l.Latest, err = time.Parse(time.DateOnly, latest); err != nil {
				return nil, fmt.Errorf("list stories: %w", err)
			}
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list stories: %w", err)
	}
	return out, nil
}

// SavePlacing starts the placing's new Stories, makes its Facts
// Developments, and marks every Fact it placed, all at once.
func (s *Store) SavePlacing(ctx context.Context, p editor.Placing, placedAt time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	develop := func(storyID, factID int64) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO developments (story_id, fact_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
			storyID, factID); err != nil {
			return fmt.Errorf("development of fact %d in story %d: %w", factID, storyID, err)
		}
		return nil
	}
	for _, ns := range p.NewStories {
		var id int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO stories (title, started_at) VALUES (?, ?) RETURNING id`,
			ns.Title, formatTime(placedAt)).Scan(&id); err != nil {
			return fmt.Errorf("start story %q: %w", ns.Title, err)
		}
		for _, f := range ns.FactIDs {
			if err := develop(id, f); err != nil {
				return err
			}
		}
	}
	for _, d := range p.Developments {
		if err := develop(d.StoryID, d.FactID); err != nil {
			return err
		}
	}
	for _, f := range p.Facts {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO fact_placings (fact_id, placed_at, model) VALUES (?, ?, ?)`,
			f, formatTime(placedAt), p.Model); err != nil {
			return fmt.Errorf("mark fact %d placed: %w", f, err)
		}
	}
	return tx.Commit()
}
