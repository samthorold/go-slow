package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

// LastBriefingWritten is when the latest Briefing was written, or zero.
func (s *Store) LastBriefingWritten(ctx context.Context) (time.Time, error) {
	var at string
	if err := s.db.QueryRowContext(ctx, `SELECT coalesce(max(written_at), '') FROM briefings`).Scan(&at); err != nil {
		return time.Time{}, fmt.Errorf("last briefing: %w", err)
	}
	return parseTime(at), nil
}

// SaveBriefingFailure keeps a failure to write a day's Briefing.
func (s *Store) SaveBriefingFailure(ctx context.Context, f editor.BriefingFailure) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO briefing_failures (day, failed_at, reason) VALUES (?, ?, ?)`,
		f.Day, formatTime(f.At), f.Reason); err != nil {
		return fmt.Errorf("save briefing failure: %w", err)
	}
	return nil
}

// BriefingDays are the days a Briefing was written for, and the first day
// one was written or tried, or empty if none ever was.
func (s *Store) BriefingDays(ctx context.Context) ([]string, string, error) {
	var first string
	if err := s.db.QueryRowContext(ctx, `
		SELECT coalesce(min(day), '') FROM (SELECT day FROM briefings UNION SELECT day FROM briefing_failures)`,
	).Scan(&first); err != nil {
		return nil, "", fmt.Errorf("briefing days: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT day FROM briefings ORDER BY day`)
	if err != nil {
		return nil, "", fmt.Errorf("briefing days: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var days []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, "", fmt.Errorf("briefing days: %w", err)
		}
		days = append(days, d)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("briefing days: %w", err)
	}
	return days, first, nil
}

// BriefingFailure is the latest failure to write the day's Briefing,
// reporting whether there is one.
func (s *Store) BriefingFailure(ctx context.Context, day string) (editor.BriefingFailure, bool, error) {
	f := editor.BriefingFailure{Day: day}
	var at string
	err := s.db.QueryRowContext(ctx, `
		SELECT failed_at, reason FROM briefing_failures WHERE day = ? ORDER BY failed_at DESC, id DESC LIMIT 1`,
		day).Scan(&at, &f.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return f, false, nil
	}
	if err != nil {
		return f, false, fmt.Errorf("briefing failure: %w", err)
	}
	f.At = parseTime(at)
	return f, true, nil
}
