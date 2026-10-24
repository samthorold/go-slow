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
				published, updated, raw, attribution, collected_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (source_id, entry_id) DO NOTHING`,
			it.SourceID, it.EntryID, it.Title, it.Link, it.Summary, it.Content,
			formatTime(it.Published), formatTime(it.Updated), it.Raw, it.Attribution,
			formatTime(it.CollectedAt))
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

// ListItems lists the Items in the Record, newest first, each with whether
// the lazy-reading check has looked at it and the Topics it tied it to.
func (s *Store) ListItems(ctx context.Context) ([]collect.ListedItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT sources.name, items.title, items.link, items.summary, items.published, items.attribution,
			EXISTS (SELECT 1 FROM item_checks c
				WHERE c.source_id = items.source_id AND c.entry_id = items.entry_id),
			(SELECT json_group_array(topics.name ORDER BY topics.name)
				FROM item_topics JOIN topics ON topics.id = item_topics.topic_id
				WHERE item_topics.source_id = items.source_id AND item_topics.entry_id = items.entry_id)
		FROM items JOIN sources ON sources.id = items.source_id
		ORDER BY items.published DESC, items.collected_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []collect.ListedItem
	for rows.Next() {
		var it collect.ListedItem
		var published, topics string
		if err := rows.Scan(&it.SourceName, &it.Title, &it.Link, &it.Summary, &published, &it.Attribution,
			&it.Checked, &topics); err != nil {
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

// SourceStatuses reports, for each Source, when collecting it began and when
// its newest Item was published.
func (s *Store) SourceStatuses(ctx context.Context) ([]collect.SourceStatus, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT sources.id, sources.name, sources.usual_gap_seconds, sources.added_at,
			coalesce(max(items.published), '')
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
