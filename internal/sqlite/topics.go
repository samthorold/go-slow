package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/samthorold/go-slow/internal/interest"
)

// Topics lists the Topics, by name.
func (s *Store) Topics(ctx context.Context) ([]interest.Topic, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, terms FROM topics ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list topics: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []interest.Topic
	for rows.Next() {
		var t interest.Topic
		var terms string
		if err := rows.Scan(&t.ID, &t.Name, &terms); err != nil {
			return nil, fmt.Errorf("list topics: %w", err)
		}
		t.Terms = splitLines(terms)
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list topics: %w", err)
	}
	return out, nil
}

// AddTopic stores a new Topic and returns it with its id.
func (s *Store) AddTopic(ctx context.Context, t interest.Topic) (interest.Topic, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO topics (name, terms) VALUES (?, ?)`, t.Name, strings.Join(t.Terms, "\n"))
	if err != nil {
		return t, fmt.Errorf("add topic %q: %w", t.Name, err)
	}
	t.ID, err = res.LastInsertId()
	return t, err
}

// RemoveTopic removes a Topic.
func (s *Store) RemoveTopic(ctx context.Context, id int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM topics WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("remove topic: %w", err)
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
