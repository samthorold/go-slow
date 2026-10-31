package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/samthorold/go-slow/internal/editor"
)

// KeptItem is one Item as collected, with its Source's name, reporting
// whether the Item is in the Record.
func (s *Store) KeptItem(ctx context.Context, sourceID, entryID string) (editor.KeptItem, bool, error) {
	var it editor.KeptItem
	var published string
	err := s.db.QueryRowContext(ctx, `
		SELECT items.source_id, items.entry_id, items.title, items.link, items.summary, items.content,
			items.published, items.attribution, sources.name
		FROM items JOIN sources ON sources.id = items.source_id
		WHERE items.source_id = ? AND items.entry_id = ?`, sourceID, entryID,
	).Scan(&it.SourceID, &it.EntryID, &it.Title, &it.Link, &it.Summary, &it.Content,
		&published, &it.Attribution, &it.SourceName)
	if errors.Is(err, sql.ErrNoRows) {
		return it, false, nil
	}
	if err != nil {
		return it, false, fmt.Errorf("item %s: %w", entryID, err)
	}
	it.Published = parseTime(published)
	return it, true, nil
}
