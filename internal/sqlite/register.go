package sqlite

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/samthorold/go-slow/internal/feedback"
	"github.com/samthorold/go-slow/internal/register"
)

// The outputs the Owner opens.
const (
	openedBriefing   = "briefing"
	openedSundayRead = "sunday read"
)

// OpenedBriefing keeps that the day's Briefing was opened on the day on,
// once however often it is opened that day.
func (s *Store) OpenedBriefing(ctx context.Context, day, on string) error {
	return s.opened(ctx, openedBriefing, day, on)
}

// OpenedSundayRead keeps that the day's Sunday read was opened on the day
// on, once however often it is opened that day.
func (s *Store) OpenedSundayRead(ctx context.Context, day, on string) error {
	return s.opened(ctx, openedSundayRead, day, on)
}

func (s *Store) opened(ctx context.Context, output, day, on string) error {
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO opens (output, day, opened_on) VALUES (?, ?, ?)`,
		output, day, on); err != nil {
		return fmt.Errorf("keep %s opened: %w", output, err)
	}
	return nil
}

// RegisterRecord is what the register reads from the Record, with times
// taken as days in loc.
func (s *Store) RegisterRecord(ctx context.Context, loc *time.Location) (register.Record, error) {
	r := register.Record{Location: loc}
	if err := s.db.QueryRowContext(ctx, `
		SELECT coalesce(min(day), '') FROM (SELECT day FROM briefings UNION SELECT day FROM briefing_failures)`,
	).Scan(&r.First); err != nil {
		return r, fmt.Errorf("register: first day: %w", err)
	}
	var err error
	if r.Briefings, err = s.strings(ctx, `SELECT day FROM briefings ORDER BY day`); err != nil {
		return r, fmt.Errorf("register: briefings: %w", err)
	}
	// The Sunday read's hook: the Record does not keep Sunday reads yet.
	// Once it does, r.SundayReads is the days they were written for, and
	// each Given on one carries the read's day, for foresight.
	if r.Untraced, err = s.strings(ctx, untraced); err != nil {
		return r, fmt.Errorf("register: untraced: %w", err)
	}
	if r.Feedback, err = s.givenFeedback(ctx); err != nil {
		return r, err
	}
	if err := s.opens(ctx, &r); err != nil {
		return r, err
	}
	if r.Commits, err = s.commits(ctx); err != nil {
		return r, err
	}
	return r, nil
}

// KeepCommits keeps the commits not already kept, returning how many were
// new, so that recording the whole history again changes nothing.
func (s *Store) KeepCommits(ctx context.Context, commits []register.Commit) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var n int64
	for _, c := range commits {
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO commits (hash, committed_at, editor) VALUES (?, ?, ?)`,
			c.Hash, formatTime(c.At), c.Editor)
		if err != nil {
			return 0, fmt.Errorf("keep commit %s: %w", c.Hash, err)
		}
		added, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		n += added
	}
	return int(n), tx.Commit()
}

func (s *Store) commits(ctx context.Context) ([]register.Commit, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT hash, committed_at, editor FROM commits ORDER BY committed_at, hash`)
	if err != nil {
		return nil, fmt.Errorf("register: commits: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []register.Commit
	for rows.Next() {
		var c register.Commit
		var at string
		if err := rows.Scan(&c.Hash, &at, &c.Editor); err != nil {
			return nil, fmt.Errorf("register: commits: %w", err)
		}
		c.At = parseTime(at)
		out = append(out, c)
	}
	return out, rows.Err()
}

// untraced names every Development in a Briefing that traces to no Item,
// every Fact that traces to no Item, and every Fact from a Reference source
// that traces to no lookup of it.
const untraced = `
	SELECT 'The Development of Fact ' || sd.fact_id || ' in the Briefing of ' || bs.day || ' traces to no Item.'
	FROM statement_developments sd
		JOIN briefing_statements bs ON bs.id = sd.statement_id
		LEFT JOIN facts ON facts.id = sd.fact_id
		LEFT JOIN items ON items.source_id = facts.source_id AND items.entry_id = facts.entry_id
	WHERE items.source_id IS NULL
	UNION ALL
	SELECT 'Fact ' || facts.id || ' traces to no Item.'
	FROM facts LEFT JOIN items ON items.source_id = facts.source_id AND items.entry_id = facts.entry_id
	WHERE items.source_id IS NULL
	UNION ALL
	SELECT 'Reference Fact ' || rf.id || ' traces to no lookup.'
	FROM reference_facts rf LEFT JOIN reference_lookups l ON l.id = rf.lookup_id
	WHERE l.id IS NULL`

// strings runs a query of one text column.
func (s *Store) strings(ctx context.Context, query string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// givenFeedback is every piece of Feedback, as the register counts it.
func (s *Store) givenFeedback(ctx context.Context) ([]register.Given, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kind, level, given_at FROM feedback ORDER BY given_at, id`)
	if err != nil {
		return nil, fmt.Errorf("register: feedback: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []register.Given
	for rows.Next() {
		var kind, level, at string
		if err := rows.Scan(&kind, &level, &at); err != nil {
			return nil, fmt.Errorf("register: feedback: %w", err)
		}
		out = append(out, register.Given{Kind: feedback.Kind(kind), Level: feedback.Level(level), At: parseTime(at)})
	}
	return out, rows.Err()
}

// opens fills in the outputs opened.
func (s *Store) opens(ctx context.Context, r *register.Record) error {
	rows, err := s.db.QueryContext(ctx, `SELECT output, day, opened_on FROM opens ORDER BY opened_on, output, day`)
	if err != nil {
		return fmt.Errorf("register: opens: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var o register.Open
		var output string
		if err := rows.Scan(&output, &o.Read, &o.On); err != nil {
			return fmt.Errorf("register: opens: %w", err)
		}
		switch output {
		case openedBriefing:
			if !slices.Contains(r.BriefingOpens, o.On) {
				r.BriefingOpens = append(r.BriefingOpens, o.On)
			}
		case openedSundayRead:
			r.SundayReadOpens = append(r.SundayReadOpens, o)
		}
	}
	return rows.Err()
}
