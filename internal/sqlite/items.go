package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
)

// SyncSources records the Sources Collection checks, adding new ones and
// updating the details of known ones. A new Source is recorded as added at
// now. What a feed's server said last time is kept, unless the feed itself
// changed.
func (s *Store) SyncSources(ctx context.Context, sources []collect.Source, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, src := range sources {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO sources (id, name, feed_url, usual_gap_seconds, added_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (id) DO UPDATE SET
				name = excluded.name,
				etag = iif(feed_url = excluded.feed_url, etag, ''),
				last_modified = iif(feed_url = excluded.feed_url, last_modified, ''),
				feed_url = excluded.feed_url,
				usual_gap_seconds = excluded.usual_gap_seconds`,
			src.ID, src.Name, src.FeedURL, int64(src.UsualGap/time.Second), formatTime(now))
		if err != nil {
			return fmt.Errorf("sync source %s: %w", src.ID, err)
		}
	}
	return tx.Commit()
}

// Validators returns what the Source's feed server said last time.
func (s *Store) Validators(ctx context.Context, sourceID string) (collect.Validators, error) {
	var v collect.Validators
	err := s.db.QueryRowContext(ctx,
		`SELECT etag, last_modified FROM sources WHERE id = ?`, sourceID,
	).Scan(&v.ETag, &v.LastModified)
	if err != nil {
		return v, fmt.Errorf("read validators for %s: %w", sourceID, err)
	}
	return v, nil
}

// SaveValidators keeps what the Source's feed server said this time.
func (s *Store) SaveValidators(ctx context.Context, sourceID string, v collect.Validators) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sources SET etag = ?, last_modified = ? WHERE id = ?`,
		v.ETag, v.LastModified, sourceID)
	if err != nil {
		return fmt.Errorf("save validators for %s: %w", sourceID, err)
	}
	return nil
}

// AddItems stores the Items not already in the Record (by Source and entry
// id) and returns how many were new. Items already held are left as they
// were: the Record is never rewritten.
func (s *Store) AddItems(ctx context.Context, items []collect.Item) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var added int64
	for _, it := range items {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO items (source_id, entry_id, title, link, summary, content,
				published, updated, due, raw, attribution, collected_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (source_id, entry_id) DO NOTHING`,
			it.SourceID, it.EntryID, it.Title, it.Link, it.Summary, it.Content,
			formatTime(it.Published), formatTime(it.Updated), formatTime(it.Due), it.Raw,
			it.Attribution, formatTime(it.CollectedAt))
		if err != nil {
			return 0, fmt.Errorf("add item %s: %w", it.EntryID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		added += n
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(added), nil
}

// Known reports which of the Source's entries are already in the Record.
func (s *Store) Known(ctx context.Context, sourceID string, entryIDs []string) (map[string]bool, error) {
	ids, err := json.Marshal(entryIDs)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT entry_id FROM items
		WHERE source_id = ? AND entry_id IN (SELECT value FROM json_each(?))`,
		sourceID, string(ids))
	if err != nil {
		return nil, fmt.Errorf("known items of %s: %w", sourceID, err)
	}
	defer func() { _ = rows.Close() }()
	known := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("known items of %s: %w", sourceID, err)
		}
		known[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("known items of %s: %w", sourceID, err)
	}
	return known, nil
}

// Untexted lists the Source's Items that have no text and are not yet read,
// apart from scheduled events.
func (s *Store) Untexted(ctx context.Context, sourceID string) ([]collect.Item, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT items.source_id, items.entry_id, items.title, items.link, items.summary
		FROM items LEFT JOIN item_reads USING (source_id, entry_id)
		WHERE items.source_id = ? AND items.content = '' AND items.due = ''
			AND item_reads.source_id IS NULL
		ORDER BY items.published, items.entry_id`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("untexted items of %s: %w", sourceID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []collect.Item
	for rows.Next() {
		var it collect.Item
		if err := rows.Scan(&it.SourceID, &it.EntryID, &it.Title, &it.Link, &it.Summary); err != nil {
			return nil, fmt.Errorf("untexted items of %s: %w", sourceID, err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("untexted items of %s: %w", sourceID, err)
	}
	return out, nil
}

// GiveText gives an unread Item with no text its text, and clears its
// check, so the lazy-reading check looks at it again with its text; the
// Topics it was tied to stay tied. It reports whether the Item was given
// it: one read meanwhile, or given text already, is left as it was. This
// is the one change ever made to an Item kept in the Record (system design:
// An Item is collected with its full text, or not yet).
func (s *Store) GiveText(ctx context.Context, sourceID, entryID, text string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `
		UPDATE items SET content = ?
		WHERE source_id = ? AND entry_id = ? AND content = ''
			AND NOT EXISTS (SELECT 1 FROM item_reads r
				WHERE r.source_id = items.source_id AND r.entry_id = items.entry_id)`,
		text, sourceID, entryID)
	if err != nil {
		return false, fmt.Errorf("give text to %s: %w", entryID, err)
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return false, err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM item_checks WHERE source_id = ? AND entry_id = ?`, sourceID, entryID); err != nil {
		return false, fmt.Errorf("give text to %s: %w", entryID, err)
	}
	return true, tx.Commit()
}

// ListItems lists the Items in the Record, newest first, each with whether
// the lazy-reading check has looked at it and the Topics it tied it to,
// whether it has its text and is cut, and whether its Spans have been read,
// with what its newest reading judged, apart from scheduled events, which
// Upcoming lists.
func (s *Store) ListItems(ctx context.Context) ([]collect.ListedItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT items.source_id, items.entry_id,
			sources.name, items.title, items.link, items.summary, items.published, items.attribution,
			EXISTS (SELECT 1 FROM item_checks c
				WHERE c.source_id = items.source_id AND c.entry_id = items.entry_id),
			(SELECT json_group_array(topics.name ORDER BY topics.name)
				FROM item_topics JOIN topics ON topics.id = item_topics.topic_id
				WHERE item_topics.source_id = items.source_id AND item_topics.entry_id = items.entry_id),
			items.content != '',
			EXISTS (SELECT 1 FROM cuts
				WHERE cuts.source_id = items.source_id AND cuts.entry_id = items.entry_id),
			newest.event_id IS NOT NULL,
			(SELECT count(*) FROM span_names n WHERE n.reading_id = newest.event_id),
			(SELECT count(*) FROM speakers s WHERE s.reading_id = newest.event_id),
			(SELECT count(*) FROM rejections j WHERE j.reading_id = newest.event_id)
		FROM items JOIN sources ON sources.id = items.source_id
			LEFT JOIN readings newest ON newest.event_id = `+newestLiveReading("items.source_id", "items.entry_id")+`
		WHERE items.due = ''
		ORDER BY items.published DESC, items.collected_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []collect.ListedItem
	for rows.Next() {
		var it collect.ListedItem
		var published, topics string
		if err := rows.Scan(&it.SourceID, &it.EntryID,
			&it.SourceName, &it.Title, &it.Link, &it.Summary, &published, &it.Attribution,
			&it.Checked, &topics, &it.HasText, &it.Cut, &it.Read, &it.Names, &it.Speakers, &it.Rejected); err != nil {
			return nil, fmt.Errorf("list items: %w", err)
		}
		it.Published = parseTime(published)
		if err := json.Unmarshal([]byte(topics), &it.Topics); err != nil {
			return nil, fmt.Errorf("list items: topics: %w", err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	return out, nil
}

// Upcoming lists the scheduled events not yet due at now, soonest first. An
// event the Source has since rescheduled (a later-collected event at the same
// link) is shown only at its latest date.
func (s *Store) Upcoming(ctx context.Context, now time.Time) ([]collect.ListedItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT sources.name, items.title, items.link, items.summary, items.due, items.attribution
		FROM items JOIN sources ON sources.id = items.source_id
		WHERE items.due > ?
			AND NOT EXISTS (
				SELECT 1 FROM items AS later
				WHERE later.source_id = items.source_id AND later.link = items.link
					AND later.due != '' AND later.collected_at > items.collected_at)
		ORDER BY items.due, items.title`, formatTime(now))
	if err != nil {
		return nil, fmt.Errorf("list upcoming: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []collect.ListedItem
	for rows.Next() {
		var it collect.ListedItem
		var due string
		if err := rows.Scan(&it.SourceName, &it.Title, &it.Link, &it.Summary, &due, &it.Attribution); err != nil {
			return nil, fmt.Errorf("list upcoming: %w", err)
		}
		it.Due = parseTime(due)
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list upcoming: %w", err)
	}
	return out, nil
}

// SourceStatuses reports, for each Source, when collecting it began and when
// its newest Item was published. An Item with no publication time, such as a
// scheduled event, counts from when it was collected.
func (s *Store) SourceStatuses(ctx context.Context) ([]collect.SourceStatus, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT sources.id, sources.name, sources.usual_gap_seconds, sources.added_at,
			coalesce(max(iif(items.published = '', items.collected_at, items.published)), '')
		FROM sources LEFT JOIN items ON items.source_id = sources.id
		GROUP BY sources.id
		ORDER BY sources.name`)
	if err != nil {
		return nil, fmt.Errorf("source statuses: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []collect.SourceStatus
	for rows.Next() {
		var st collect.SourceStatus
		var gap int64
		var since, newest string
		if err := rows.Scan(&st.ID, &st.Name, &gap, &since, &newest); err != nil {
			return nil, fmt.Errorf("source statuses: %w", err)
		}
		st.UsualGap = time.Duration(gap) * time.Second
		st.Since, st.Newest = parseTime(since), parseTime(newest)
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("source statuses: %w", err)
	}
	return out, nil
}

// timeLayout is RFC 3339 in UTC with fixed-width fractions, so times sort as
// text.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

// Times are kept in timeLayout; the zero time is
// kept as ”.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timeLayout)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
