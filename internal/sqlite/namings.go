package sqlite

import (
	"context"
	"fmt"

	"github.com/samthorold/go-slow/internal/editor"
)

// storyNameShape is the version of the shape of a Story's Name's event this
// build writes.
const storyNameShape = 1

// StoriesToName are the Stories the Editor made that have no Name and whose
// naming has not been marked failed, oldest first, each with its Editor's
// Spans. A Story a Cut made is never among them: it is no woven Story.
func (s *Store) StoriesToName(ctx context.Context) ([]editor.Story, error) {
	return s.stories(ctx, `
		WHERE n.event_id IS NULL
			AND NOT EXISTS (SELECT 1 FROM work_attempts a
				WHERE a.act = 'naming' AND a.work_key = CAST(s.event_id AS TEXT) AND a.failed = 1)
		ORDER BY s.event_id`)
}

// KeepName keeps a Story's Name as an event, with its Judge.
func (s *Store) KeepName(ctx context.Context, n editor.StoryName) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	id, err := addEvent(ctx, tx, "story_name", storyNameShape, n.At)
	if err != nil {
		return fmt.Errorf("keep name: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO story_names (event_id, story_id, name, act, version, model, request_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, n.Story, n.Text, n.Judge.Act, n.Judge.Version, n.Judge.Model, n.Judge.RequestHash); err != nil {
		return fmt.Errorf("keep name: %w", err)
	}
	return tx.Commit()
}
