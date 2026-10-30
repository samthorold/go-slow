package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// keepPrecedents keeps, with an act's result on an Item, the Feedback it was
// shown as precedent, in the order shown.
func keepPrecedents(ctx context.Context, tx *sql.Tx, act, sourceID, entryID string, feedbackIDs []int64, at time.Time) error {
	for i, id := range feedbackIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO precedents_shown (act, source_id, entry_id, feedback_id, rank, shown_at)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			act, sourceID, entryID, id, i+1, formatTime(at)); err != nil {
			return fmt.Errorf("keep precedent %d of %s %s: %w", id, act, entryID, err)
		}
	}
	return nil
}
