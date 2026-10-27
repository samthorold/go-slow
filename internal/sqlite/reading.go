package sqlite

import (
	"cmp"
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
			AND `+notFailed("reading")+`
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

// SaveReading writes a reading's Entities, as what its Item mentions, its
// Facts and Claims, each tied to the Entities it lists, and marks its Item
// read, all at once. An Entity already in the Record by name is reused.
func (s *Store) SaveReading(ctx context.Context, r editor.Reading, readAt time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	ids := map[string]int64{}
	for _, e := range r.Entities {
		id, err := saveEntity(ctx, tx, e)
		if err != nil {
			return fmt.Errorf("save entity %q: %w", e.Name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO entity_names (entity_id, name) VALUES (?, ?) ON CONFLICT DO NOTHING`,
			id, e.Name); err != nil {
			return fmt.Errorf("save entity %q: %w", e.Name, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO item_mentions (source_id, entry_id, entity_id) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`,
			r.SourceID, r.EntryID, id); err != nil {
			return fmt.Errorf("mention of %q: %w", e.Name, err)
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
	lookups := map[*editor.Lookup]int64{}
	for _, f := range r.ReferenceFacts {
		var exists bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM reference_facts WHERE statement = ? AND as_at = ?)`,
			f.Statement, f.AsAt.Format(time.DateOnly)).Scan(&exists); err != nil {
			return fmt.Errorf("save reference fact: %w", err)
		}
		if exists || f.Lookup == nil {
			continue
		}
		lookupID, ok := lookups[f.Lookup]
		if !ok {
			l := f.Lookup
			if err := tx.QueryRowContext(ctx, `
				INSERT INTO reference_lookups (reference, item_id, revision, looked_up_at, licence)
				VALUES (?, ?, ?, ?, ?) RETURNING id`,
				l.Reference, l.ItemID, l.Revision, formatTime(l.At), l.Licence).Scan(&lookupID); err != nil {
				return fmt.Errorf("save lookup: %w", err)
			}
			lookups[f.Lookup] = lookupID
		}
		var id int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO reference_facts (lookup_id, statement, as_at, quote) VALUES (?, ?, ?, ?) RETURNING id`,
			lookupID, f.Statement, f.AsAt.Format(time.DateOnly), f.Quote).Scan(&id); err != nil {
			return fmt.Errorf("save reference fact: %w", err)
		}
		if err := link("reference_fact_entities", "fact_id", id, f.Entities); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO item_reads (source_id, entry_id, read_at, model) VALUES (?, ?, ?, ?)`,
		r.SourceID, r.EntryID, formatTime(readAt), r.Model); err != nil {
		return fmt.Errorf("mark %s read: %w", r.EntryID, err)
	}
	if err := keepPrecedents(ctx, tx, "reading", r.SourceID, r.EntryID, r.Precedents, readAt); err != nil {
		return err
	}
	return tx.Commit()
}

// saveEntity is the id of the Entity e names, made if the Record has none.
//
// An Entity the Editor made (no Wikidata item) is one per name. A Wikidata
// item is one Entity whatever the Items call it. When a Wikidata item first
// appears and the Editor already made an Entity by the same name, or by the
// item's label, the two are merged: the Editor's becomes the item, and
// keeps its name among the names it is called by, so what was tied to it
// before still resolves.
func saveEntity(ctx context.Context, tx *sql.Tx, e editor.Entity) (int64, error) {
	var id int64
	if e.WikidataID == "" {
		err := tx.QueryRowContext(ctx, `
			INSERT INTO entities (name, kind) VALUES (?, ?)
			ON CONFLICT (name) DO UPDATE SET name = excluded.name
			RETURNING id`, e.Name, e.Kind).Scan(&id)
		return id, err
	}
	err := tx.QueryRowContext(ctx, `SELECT id FROM entities WHERE wikidata_id = ?`, e.WikidataID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	label := cmp.Or(e.Label, e.Name)
	// The Editor's own, by the name the Item gives or by the item's label.
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM entities
		WHERE wikidata_id IS NULL AND kind = ? AND name IN (?, ?)
		ORDER BY name = ? DESC LIMIT 1`, e.Kind, e.Name, label, e.Name).Scan(&id)
	switch {
	case err == nil:
		if _, err := tx.ExecContext(ctx, `UPDATE entities SET wikidata_id = ? WHERE id = ?`, e.WikidataID, id); err != nil {
			return 0, err
		}
		// It takes the item's label as its name, unless another Entity has it.
		if _, err := tx.ExecContext(ctx, `
			UPDATE entities SET name = ? WHERE id = ? AND NOT EXISTS (SELECT 1 FROM entities WHERE name = ?)`,
			label, id, label); err != nil {
			return 0, err
		}
	case errors.Is(err, sql.ErrNoRows):
		// A label another Entity has is told apart by the item's id.
		err = tx.QueryRowContext(ctx, `
			INSERT INTO entities (name, kind, wikidata_id)
			VALUES (CASE WHEN EXISTS (SELECT 1 FROM entities WHERE name = ?1) THEN ?1 || ' (' || ?3 || ')' ELSE ?1 END, ?2, ?3)
			RETURNING id`, label, e.Kind, e.WikidataID).Scan(&id)
		if err != nil {
			return 0, err
		}
	default:
		return 0, err
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO entity_names (entity_id, name) VALUES (?, ?) ON CONFLICT DO NOTHING`, id, label)
	return id, err
}

// Entity is the Entity the Record calls name, by its own name or any name
// an Item has called it by, reporting whether there is one.
func (s *Store) Entity(ctx context.Context, name string) (editor.KnownEntity, bool, error) {
	var e editor.KnownEntity
	var id int64
	var aliases string
	err := s.db.QueryRowContext(ctx, `
		SELECT entities.id, entities.name, entities.kind, coalesce(entities.wikidata_id, ''),
			(SELECT json_group_array(n.name ORDER BY n.name) FROM entity_names n WHERE n.entity_id = entities.id)
		FROM entities
		WHERE entities.name = ?1 OR EXISTS (SELECT 1 FROM entity_names n WHERE n.entity_id = entities.id AND n.name = ?1)
		ORDER BY entities.name = ?1 DESC, entities.id LIMIT 1`, name,
	).Scan(&id, &e.Name, &e.Kind, &e.WikidataID, &aliases)
	if errors.Is(err, sql.ErrNoRows) {
		return e, false, nil
	}
	if err != nil {
		return e, false, fmt.Errorf("entity %q: %w", name, err)
	}
	if err := json.Unmarshal([]byte(aliases), &e.Aliases); err != nil {
		return e, false, fmt.Errorf("entity %q: names: %w", name, err)
	}
	if e.Facts, err = s.referenceFacts(ctx, id); err != nil {
		return e, false, err
	}
	return e, true, nil
}

// referenceFacts are the Facts drawn from Reference sources about the
// Entity id, each with the lookup it came from.
func (s *Store) referenceFacts(ctx context.Context, id int64) ([]editor.Fact, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.statement, f.as_at, f.quote, l.reference, l.item_id, l.revision, l.looked_up_at, l.licence,
			(SELECT json_group_array(entities.name ORDER BY entities.name)
				FROM reference_fact_entities fe JOIN entities ON entities.id = fe.entity_id
				WHERE fe.fact_id = f.id)
		FROM reference_facts f JOIN reference_lookups l ON l.id = f.lookup_id
		WHERE EXISTS (SELECT 1 FROM reference_fact_entities fe WHERE fe.fact_id = f.id AND fe.entity_id = ?)
		ORDER BY f.as_at, f.id`, id)
	if err != nil {
		return nil, fmt.Errorf("reference facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.Fact
	for rows.Next() {
		var f editor.Fact
		var l editor.Lookup
		var asAt, at, ents string
		if err := rows.Scan(&f.Statement, &asAt, &f.Quote, &l.Reference, &l.ItemID, &l.Revision, &at, &l.Licence, &ents); err != nil {
			return nil, fmt.Errorf("reference facts: %w", err)
		}
		if f.AsAt, err = time.Parse(time.DateOnly, asAt); err != nil {
			return nil, fmt.Errorf("reference facts: as at: %w", err)
		}
		if err := json.Unmarshal([]byte(ents), &f.Entities); err != nil {
			return nil, fmt.Errorf("reference facts: entities: %w", err)
		}
		l.At = parseTime(at)
		f.Lookup = &l
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reference facts: %w", err)
	}
	return out, nil
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
				WHERE item_topics.source_id = items.source_id AND item_topics.entry_id = items.entry_id),
			`+readingCall(itemWorkKey)+`
		FROM items JOIN sources ON sources.id = items.source_id
			LEFT JOIN item_reads USING (source_id, entry_id)
		WHERE items.source_id = ? AND items.entry_id = ?`, sourceID, entryID,
	).Scan(&it.SourceID, &it.EntryID, &it.Title, &it.Link, &it.Summary, &it.Content,
		&published, &it.Attribution, &it.SourceName, &readAt, &it.Model, &topics, &it.ReadingCall)
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
	if it.Mentions, err = s.itemMentions(ctx, sourceID, entryID); err != nil {
		return it, false, err
	}
	if it.Facts, err = s.facts(ctx, sourceID, entryID); err != nil {
		return it, false, err
	}
	for i := range it.Facts {
		if it.Facts[i].Involves, err = s.involves(ctx, it.Facts[i].ID); err != nil {
			return it, false, err
		}
	}
	if it.Claims, err = s.claims(ctx, sourceID, entryID); err != nil {
		return it, false, err
	}
	return it, true, nil
}

// itemMentions are the Entities an Item mentions, by name, each with its
// kind and the Wikidata item it matched, if any.
func (s *Store) itemMentions(ctx context.Context, sourceID, entryID string) ([]editor.Entity, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT entities.name, entities.kind, coalesce(entities.wikidata_id, '')
		FROM item_mentions m JOIN entities ON entities.id = m.entity_id
		WHERE m.source_id = ? AND m.entry_id = ?
		ORDER BY entities.name`, sourceID, entryID)
	if err != nil {
		return nil, fmt.Errorf("mentions of %s: %w", entryID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.Entity
	for rows.Next() {
		var e editor.Entity
		if err := rows.Scan(&e.Name, &e.Kind, &e.WikidataID); err != nil {
			return nil, fmt.Errorf("mentions of %s: %w", entryID, err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) facts(ctx context.Context, sourceID, entryID string) ([]editor.Fact, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT facts.id, facts.statement, facts.as_at, facts.quote,
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
		if err := rows.Scan(&f.ID, &f.Statement, &asAt, &f.Quote, &ents); err != nil {
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
