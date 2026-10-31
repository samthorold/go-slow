package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/samthorold/go-slow/internal/editor"
)

// The versions of the shapes of the look-up's events this build writes.
const (
	lookupShape       = 1
	editorEntityShape = 1
	mergeShape        = 1
)

// lookupOn is a name's look-up date in SQL, from span_names n and items: the
// Item's date in UTC, for a name with an office whose Item gives one, and ”
// otherwise (editor.KeyOf).
const lookupOn = `CASE WHEN n.office = '' OR items.published = '' THEN '' ELSE substr(items.published, 1, 10) END`

// NamesToLookUp are the keys of the names readings judged that no look-up
// has looked up yet, each once, those first judged earliest first.
func (s *Store) NamesToLookUp(ctx context.Context) ([]editor.LookupKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT n.name, n.kind, n.office, `+lookupOn+` AS on_date
		FROM span_names n
			JOIN readings r ON r.event_id = n.reading_id
			JOIN items ON items.source_id = r.source_id AND items.entry_id = r.entry_id
		WHERE NOT EXISTS (SELECT 1 FROM lookups l
			WHERE l.name = n.name AND l.kind = n.kind AND l.office = n.office AND l.on_date = `+lookupOn+`)
		GROUP BY n.name, n.kind, n.office, on_date
		ORDER BY min(n.event_id)`)
	if err != nil {
		return nil, fmt.Errorf("names to look up: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.LookupKey
	for rows.Next() {
		var k editor.LookupKey
		if err := rows.Scan(&k.Name, &k.Kind, &k.Office, &k.On); err != nil {
			return nil, fmt.Errorf("names to look up: %w", err)
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("names to look up: %w", err)
	}
	return out, nil
}

// KeepLookup keeps a look-up as an event, all at once with what it makes:
// with no match, the Editor's own Entity for its name and kind, if there is
// none yet; with a match, a merge into the item of each Editor's Entity of
// its kind made for the key's name or the item's label, not merged before
// (system design: The model names what a Span names; plain code finds it in
// Wikidata).
func (s *Store) KeepLookup(ctx context.Context, l editor.NameLookup) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	at := formatTime(l.At)
	event := func(kind string, shape int) (int64, error) {
		res, err := tx.ExecContext(ctx, `INSERT INTO events (kind, shape, at) VALUES (?, ?, ?)`, kind, shape, at)
		if err != nil {
			return 0, fmt.Errorf("keep look-up of %q: %s: %w", l.Key.Name, kind, err)
		}
		return res.LastInsertId()
	}
	id, err := event("lookup", lookupShape)
	if err != nil {
		return err
	}
	k := l.Key
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO lookups (event_id, name, kind, office, on_date, looker, looker_version, wikidata_id, label, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, k.Name, k.Kind, k.Office, k.On, l.Judge.Name, l.Judge.Version, l.WikidataID, l.Label, l.Revision); err != nil {
		return fmt.Errorf("keep look-up of %q: %w", k.Name, err)
	}
	if err := keepWhatALookupMakes(ctx, tx, l, id, event); err != nil {
		return err
	}
	return tx.Commit()
}

// keepWhatALookupMakes keeps, in tx, the Editor's Entity or the merges the
// look-up l, kept as the event id, makes.
func keepWhatALookupMakes(ctx context.Context, tx *sql.Tx, l editor.NameLookup, id int64,
	event func(kind string, shape int) (int64, error),
) error {
	k := l.Key
	if l.WikidataID == "" {
		var made int64
		err := tx.QueryRowContext(ctx, `SELECT event_id FROM editor_entities WHERE name = ? AND kind = ?`,
			k.Name, k.Kind).Scan(&made)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("keep look-up of %q: %w", k.Name, err)
		}
		eid, err := event("entity", editorEntityShape)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO editor_entities (event_id, name, kind, lookup_id) VALUES (?, ?, ?, ?)`,
			eid, k.Name, k.Kind, id); err != nil {
			return fmt.Errorf("keep look-up of %q: %w", k.Name, err)
		}
		return nil
	}
	merged, err := unmerged(ctx, tx, k.Kind, k.Name, l.Label)
	if err != nil {
		return fmt.Errorf("keep look-up of %q: %w", k.Name, err)
	}
	for _, e := range merged {
		mid, err := event("merge", mergeShape)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO entity_merges (event_id, entity_id, lookup_id) VALUES (?, ?, ?)`,
			mid, e, id); err != nil {
			return fmt.Errorf("keep look-up of %q: merge: %w", k.Name, err)
		}
	}
	return nil
}

// unmerged are the Editor's Entities of the kind made for either name, not
// merged into an item yet, oldest first.
func unmerged(ctx context.Context, tx *sql.Tx, kind, name, other string) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT e.event_id FROM editor_entities e
		WHERE e.kind = ? AND e.name IN (?, ?)
			AND NOT EXISTS (SELECT 1 FROM entity_merges m WHERE m.entity_id = e.event_id)
		ORDER BY e.event_id`, kind, name, other)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var e int64
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EntitiesNamed is the Entity each key names, as the Record's look-ups have
// it now: the item its newest look-up matched; or, with no match, the
// Editor's Entity for its name and kind, or the item a later look-up merged
// that into. A key not yet looked up is left out. A look-up under standing
// wrong is read by no view: the key names what an older look-up matched, or
// nothing, and is not looked up again for it.
func (s *Store) EntitiesNamed(ctx context.Context, keys []editor.LookupKey) (map[editor.LookupKey]editor.NamedEntity, error) {
	out, _, err := s.entitiesNamed(ctx, keys)
	return out, err
}

// LookupsNamed is the look-up each key's Entity is read from, as
// EntitiesNamed reads it: what Feedback on the match names.
func (s *Store) LookupsNamed(ctx context.Context, keys []editor.LookupKey) (map[editor.LookupKey]int64, error) {
	_, out, err := s.entitiesNamed(ctx, keys)
	return out, err
}

// entitiesNamed is the Entity each key names, and the look-up it is read
// from.
func (s *Store) entitiesNamed(ctx context.Context, keys []editor.LookupKey) (map[editor.LookupKey]editor.NamedEntity, map[editor.LookupKey]int64, error) {
	out := map[editor.LookupKey]editor.NamedEntity{}
	from := map[editor.LookupKey]int64{}
	for _, k := range keys {
		e := editor.NamedEntity{Kind: k.Kind}
		var lookup int64
		var editorID sql.NullInt64
		var editorName, mergedID, mergedLabel sql.NullString
		err := s.db.QueryRowContext(ctx, `
			SELECT l.event_id, l.wikidata_id, l.label, e.event_id, e.name, ml.wikidata_id, ml.label
			FROM lookups l
				LEFT JOIN editor_entities e ON l.wikidata_id = '' AND e.name = l.name AND e.kind = l.kind
				LEFT JOIN entity_merges m ON m.event_id = (SELECT max(event_id) FROM entity_merges WHERE entity_id = e.event_id)
				LEFT JOIN lookups ml ON ml.event_id = m.lookup_id
			WHERE l.name = ? AND l.kind = ? AND l.office = ? AND l.on_date = ?
				AND NOT `+wrongStanding("l.event_id")+`
			ORDER BY l.event_id DESC LIMIT 1`, k.Name, k.Kind, k.Office, k.On,
		).Scan(&lookup, &e.WikidataID, &e.Name, &editorID, &editorName, &mergedID, &mergedLabel)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("entities named: %w", err)
		}
		switch {
		case e.WikidataID != "":
		case mergedID.Valid:
			e.WikidataID, e.Name = mergedID.String, mergedLabel.String
		case editorID.Valid:
			e.EditorID, e.Name = editorID.Int64, editorName.String
		default:
			// A look-up with no match and no Editor's Entity: none is made
			// without the other, so this is a Record kept by hand.
			continue
		}
		out[k], from[k] = e, lookup
	}
	return out, from, nil
}
