package sqlite

import (
	"context"
	"fmt"

	"github.com/samthorold/go-slow/internal/editor"
)

// itemWorkKey is the work key an act on an Item is kept under, as the
// Editor's acts key it: the Item's Source and entry, a line apart.
const itemWorkKey = `items.source_id || char(10) || items.entry_id`

// notFailed is a condition on items that the named act's work on the Item
// is not marked failed.
func notFailed(act string) string {
	return `NOT EXISTS (SELECT 1 FROM work_attempts w WHERE w.act = '` + act + `'
		AND w.work_key = ` + itemWorkKey + ` AND w.failed = 1)`
}

// Attempts is how many attempts at the act's work with key have failed.
func (s *Store) Attempts(ctx context.Context, act, key string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT coalesce((SELECT attempts FROM work_attempts WHERE act = ? AND work_key = ?), 0)`,
		act, key).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("attempts at %s: %w", act, err)
	}
	return n, nil
}

// FailedWork is the work marked failed, each with its Item, most recently
// failed first.
func (s *Store) FailedWork(ctx context.Context) ([]editor.FailedWork, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT w.act, items.source_id, items.entry_id, items.title, w.attempts, w.last_failed_at, w.last_error,
			coalesce((SELECT max(c.id) FROM model_calls c WHERE c.act = w.act AND c.work_key = w.work_key), 0)
		FROM work_attempts w JOIN items ON w.work_key = `+itemWorkKey+`
		WHERE w.failed = 1
		ORDER BY w.last_failed_at DESC, w.act, w.work_key`)
	if err != nil {
		return nil, fmt.Errorf("failed work: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.FailedWork
	for rows.Next() {
		var f editor.FailedWork
		var at string
		if err := rows.Scan(&f.Act, &f.SourceID, &f.EntryID, &f.Title, &f.Attempts, &at, &f.Why, &f.LastCall); err != nil {
			return nil, fmt.Errorf("failed work: %w", err)
		}
		f.LastFailedAt = parseTime(at)
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed work: %w", err)
	}
	return out, nil
}

// RetryFailed clears the failed marks on the named act's work, or every
// act's when act is empty, resetting its attempts, so the next run takes it
// up again. It reports how much work it cleared.
func (s *Store) RetryFailed(ctx context.Context, act string) (int, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM work_attempts WHERE failed = 1 AND (? = '' OR act = ?)`, act, act)
	if err != nil {
		return 0, fmt.Errorf("retry failed work: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("retry failed work: %w", err)
	}
	return int(n), nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
