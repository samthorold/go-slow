package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

// ItemsToRead are the Items awaiting reading: tied to a Topic and not yet
// read, oldest first.
func (s *Store) ItemsToRead(ctx context.Context) ([]editor.Item, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT items.source_id, items.entry_id, items.title, items.link, items.summary,
			items.content, items.published
		FROM items
		WHERE EXISTS (SELECT 1 FROM item_topics t
				WHERE t.source_id = items.source_id AND t.entry_id = items.entry_id)
			AND NOT EXISTS (SELECT 1 FROM item_reads r
				WHERE r.source_id = items.source_id AND r.entry_id = items.entry_id)
		ORDER BY items.published, items.collected_at`)
	if err != nil {
		return nil, fmt.Errorf("items to read: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.Item
	for rows.Next() {
		var it editor.Item
		var published string
		if err := rows.Scan(&it.SourceID, &it.EntryID, &it.Title, &it.Link, &it.Summary,
			&it.Content, &published); err != nil {
			return nil, fmt.Errorf("items to read: %w", err)
		}
		it.Published = parseTime(published)
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("items to read: %w", err)
	}
	return out, nil
}

// SaveReading writes a reading's Entities, Facts and Claims and marks its
// Item read, all at once. An Entity already in the Record by name is reused.
func (s *Store) SaveReading(ctx context.Context, r editor.Reading, readAt time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	ids := map[string]int64{}
	for _, e := range r.Entities {
		var id int64
		err := tx.QueryRowContext(ctx, `
			INSERT INTO entities (name, kind) VALUES (?, ?)
			ON CONFLICT (name) DO UPDATE SET name = excluded.name
			RETURNING id`, e.Name, e.Kind).Scan(&id)
		if err != nil {
			return fmt.Errorf("save entity %q: %w", e.Name, err)
		}
		ids[e.Name] = id
	}
	link := func(table, column string, id int64, names []string) error {
		for _, n := range names {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO `+table+` (`+column+`, entity_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
				id, ids[n]); err != nil {
				return fmt.Errorf("tie %s to %q: %w", table, n, err)
			}
		}
		return nil
	}
	for _, f := range r.Facts {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO facts (source_id, entry_id, statement, as_at, quote) VALUES (?, ?, ?, ?, ?)`,
			r.SourceID, r.EntryID, f.Statement, f.AsAt.Format(time.DateOnly), f.Quote)
		if err != nil {
			return fmt.Errorf("save fact: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if err := link("fact_entities", "fact_id", id, f.Entities); err != nil {
			return err
		}
	}
	for _, c := range r.Claims {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO claims (source_id, entry_id, statement, made_by, quote) VALUES (?, ?, ?, ?, ?)`,
			r.SourceID, r.EntryID, c.Statement, c.MadeBy, c.Quote)
		if err != nil {
			return fmt.Errorf("save claim: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if err := link("claim_entities", "claim_id", id, c.Entities); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO item_reads (source_id, entry_id, read_at, model) VALUES (?, ?, ?, ?)`,
		r.SourceID, r.EntryID, formatTime(readAt), r.Model); err != nil {
		return fmt.Errorf("mark %s read: %w", r.EntryID, err)
	}
	return tx.Commit()
}

// ReadItem is one Item with what reading drew from it, reporting whether
// the Item is in the Record.
func (s *Store) ReadItem(ctx context.Context, sourceID, entryID string) (editor.ReadItem, bool, error) {
	var it editor.ReadItem
	var published, readAt, topics string
	err := s.db.QueryRowContext(ctx, `
		SELECT items.source_id, items.entry_id, items.title, items.link, items.summary, items.content,
			items.published, items.attribution, sources.name,
			coalesce(item_reads.read_at, ''), coalesce(item_reads.model, ''),
			(SELECT json_group_array(topics.name ORDER BY topics.name)
				FROM item_topics JOIN topics ON topics.id = item_topics.topic_id
				WHERE item_topics.source_id = items.source_id AND item_topics.entry_id = items.entry_id)
		FROM items JOIN sources ON sources.id = items.source_id
			LEFT JOIN item_reads USING (source_id, entry_id)
		WHERE items.source_id = ? AND items.entry_id = ?`, sourceID, entryID,
	).Scan(&it.SourceID, &it.EntryID, &it.Title, &it.Link, &it.Summary, &it.Content,
		&published, &it.Attribution, &it.SourceName, &readAt, &it.Model, &topics)
	if errors.Is(err, sql.ErrNoRows) {
		return it, false, nil
	}
	if err != nil {
		return it, false, fmt.Errorf("read item %s: %w", entryID, err)
	}
	it.Published, it.ReadAt, it.Read = parseTime(published), parseTime(readAt), readAt != ""
	if err := json.Unmarshal([]byte(topics), &it.Topics); err != nil {
		return it, false, fmt.Errorf("read item %s: topics: %w", entryID, err)
	}
	if it.Facts, err = s.facts(ctx, sourceID, entryID); err != nil {
		return it, false, err
	}
	if it.Claims, err = s.claims(ctx, sourceID, entryID); err != nil {
		return it, false, err
	}
	return it, true, nil
}

func (s *Store) facts(ctx context.Context, sourceID, entryID string) ([]editor.Fact, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT facts.statement, facts.as_at, facts.quote,
			(SELECT json_group_array(entities.name ORDER BY entities.name)
				FROM fact_entities JOIN entities ON entities.id = fact_entities.entity_id
				WHERE fact_entities.fact_id = facts.id)
		FROM facts WHERE source_id = ? AND entry_id = ? ORDER BY id`, sourceID, entryID)
	if err != nil {
		return nil, fmt.Errorf("facts of %s: %w", entryID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.Fact
	for rows.Next() {
		f := editor.Fact{SourceID: sourceID, EntryID: entryID}
		var asAt, ents string
		if err := rows.Scan(&f.Statement, &asAt, &f.Quote, &ents); err != nil {
			return nil, fmt.Errorf("facts of %s: %w", entryID, err)
		}
		if f.AsAt, err = time.Parse(time.DateOnly, asAt); err != nil {
			return nil, fmt.Errorf("facts of %s: as at: %w", entryID, err)
		}
		if err := json.Unmarshal([]byte(ents), &f.Entities); err != nil {
			return nil, fmt.Errorf("facts of %s: entities: %w", entryID, err)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("facts of %s: %w", entryID, err)
	}
	return out, nil
}

func (s *Store) claims(ctx context.Context, sourceID, entryID string) ([]editor.Claim, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT claims.statement, claims.made_by, claims.quote,
			(SELECT json_group_array(entities.name ORDER BY entities.name)
				FROM claim_entities JOIN entities ON entities.id = claim_entities.entity_id
				WHERE claim_entities.claim_id = claims.id)
		FROM claims WHERE source_id = ? AND entry_id = ? ORDER BY id`, sourceID, entryID)
	if err != nil {
		return nil, fmt.Errorf("claims of %s: %w", entryID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.Claim
	for rows.Next() {
		c := editor.Claim{SourceID: sourceID, EntryID: entryID}
		var ents string
		if err := rows.Scan(&c.Statement, &c.MadeBy, &c.Quote, &ents); err != nil {
			return nil, fmt.Errorf("claims of %s: %w", entryID, err)
		}
		if err := json.Unmarshal([]byte(ents), &c.Entities); err != nil {
			return nil, fmt.Errorf("claims of %s: entities: %w", entryID, err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claims of %s: %w", entryID, err)
	}
	return out, nil
}
