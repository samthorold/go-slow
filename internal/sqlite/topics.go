package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/samthorold/go-slow/internal/interest"
)

// Topics lists the Topics, by name.
func (s *Store) Topics(ctx context.Context) ([]interest.Topic, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, sources, terms FROM topics ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list topics: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []interest.Topic
	for rows.Next() {
		var t interest.Topic
		var sources, terms string
		if err := rows.Scan(&t.ID, &t.Name, &sources, &terms); err != nil {
			return nil, fmt.Errorf("list topics: %w", err)
		}
		t.Sources, t.Terms = splitLines(sources), splitLines(terms)
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list topics: %w", err)
	}
	return out, nil
}

// AddTopic stores a new Topic and returns it with its id. Topics have no
// Depth; the column the Record keeps one in is read only by the Sunday read
// and by Feedback's Depth, until each is replaced, and a Topic taken up now
// is kept at following there, the middle of the scale.
func (s *Store) AddTopic(ctx context.Context, t interest.Topic) (interest.Topic, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO topics (name, depth, sources, terms) VALUES (?, ?, ?, ?)`,
		t.Name, int(interest.Following), strings.Join(t.Sources, "\n"), strings.Join(t.Terms, "\n"))
	if err != nil {
		return t, fmt.Errorf("add topic %q: %w", t.Name, err)
	}
	t.ID, err = res.LastInsertId()
	return t, err
}

// SetDepth changes the Depth the Record keeps for a Topic, reporting whether
// the Topic exists. Nothing the Owner does sets it: it is there for the
// recorded Sunday read, whose frozen Record was built with Topics at
// several Depths, until the Sunday read tells Spans.
func (s *Store) SetDepth(ctx context.Context, id int64, d interest.Depth) (bool, error) {
	return changed(s.db.ExecContext(ctx, `UPDATE topics SET depth = ? WHERE id = ?`, int(d), id))
}

// RemoveTopic removes a Topic; its ties go with it (ON DELETE CASCADE).
func (s *Store) RemoveTopic(ctx context.Context, id int64) (bool, error) {
	return changed(s.db.ExecContext(ctx, `DELETE FROM topics WHERE id = ?`, id))
}

// UncheckedItems are the Items the check tying Items to Topics has not yet looked at.
func (s *Store) UncheckedItems(ctx context.Context) ([]interest.Item, error) {
	return s.checkItems(ctx, `
		SELECT items.source_id, items.entry_id, items.title, items.summary, items.content
		FROM items LEFT JOIN item_checks USING (source_id, entry_id)
		WHERE item_checks.source_id IS NULL`)
}

func (s *Store) checkItems(ctx context.Context, query string) ([]interest.Item, error) {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("items to check: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []interest.Item
	for rows.Next() {
		var it interest.Item
		if err := rows.Scan(&it.SourceID, &it.EntryID, &it.Title, &it.Summary, &it.Content); err != nil {
			return nil, fmt.Errorf("items to check: %w", err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("items to check: %w", err)
	}
	return out, nil
}

// RecordChecks marks each Item checked and ties it to its Topics, all at
// once.
func (s *Store) RecordChecks(ctx context.Context, checks []interest.Checked) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, c := range checks {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO item_checks (source_id, entry_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
			c.SourceID, c.EntryID); err != nil {
			return fmt.Errorf("record check of %s: %w", c.EntryID, err)
		}
		if err := tie(ctx, tx, c.SourceID, c.EntryID, c.TopicIDs...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// WaitingItems are the Items not yet read, whether or not a Topic ties them.
func (s *Store) WaitingItems(ctx context.Context) ([]interest.Item, error) {
	return s.checkItems(ctx, `
		SELECT items.source_id, items.entry_id, items.title, items.summary, items.content
		FROM items LEFT JOIN item_reads USING (source_id, entry_id)
		WHERE item_reads.source_id IS NULL`)
}

// Tie adds ties between Items and Topics, all at once.
func (s *Store) Tie(ctx context.Context, ties []interest.Checked) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, c := range ties {
		if err := tie(ctx, tx, c.SourceID, c.EntryID, c.TopicIDs...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// tie ties an Item to each of the Topics.
func tie(ctx context.Context, tx *sql.Tx, sourceID, entryID string, topicIDs ...int64) error {
	for _, id := range topicIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO item_topics (source_id, entry_id, topic_id) VALUES (?, ?, ?)
			ON CONFLICT DO NOTHING`, sourceID, entryID, id); err != nil {
			return fmt.Errorf("tie %s to topic %d: %w", entryID, id, err)
		}
	}
	return nil
}

// changed reports whether a statement touched any row.
func changed(res sql.Result, err error) (bool, error) {
	if err != nil {
		return false, fmt.Errorf("change topic: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
