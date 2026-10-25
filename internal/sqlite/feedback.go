package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/samthorold/go-slow/internal/feedback"
	"github.com/samthorold/go-slow/internal/interest"
)

// About reports whether what f is about is in the Record, whether a missed
// Development is already one, and the closest Depth of the Topics that tie
// the Items of the Facts it concerns.
func (s *Store) About(ctx context.Context, f feedback.Feedback) (feedback.About, error) {
	var a feedback.About
	exists := func(query string, args ...any) (bool, error) {
		var n int
		err := s.db.QueryRowContext(ctx, `SELECT EXISTS (`+query+`)`, args...).Scan(&n)
		return n == 1, err
	}
	story := func() (bool, error) { return exists(`SELECT 1 FROM stories WHERE id = ?`, f.StoryID) }
	fact := func(id int64) (bool, error) { return exists(`SELECT 1 FROM facts WHERE id = ?`, id) }
	development := func() (bool, error) {
		return exists(`SELECT 1 FROM developments WHERE story_id = ? AND fact_id = ?`, f.StoryID, f.FactID)
	}
	var err error
	var facts []int64
	switch {
	case f.Level == feedback.Story && f.Kind == feedback.Missed:
		a.Found = true
		for _, id := range f.FactIDs {
			ok, err := fact(id)
			if err != nil {
				return a, fmt.Errorf("about feedback: %w", err)
			}
			a.Found = a.Found && ok
		}
		facts = f.FactIDs
	case f.Level == feedback.Story:
		if a.Found, err = story(); err != nil {
			return a, fmt.Errorf("about feedback: %w", err)
		}
		depth, err := s.depthOf(ctx, `SELECT fact_id FROM developments WHERE story_id = ?`, f.StoryID)
		a.Depth = depth
		if err != nil {
			return a, fmt.Errorf("about feedback: %w", err)
		}
		return a, nil
	case f.Level == feedback.Development:
		okStory, err := story()
		if err != nil {
			return a, fmt.Errorf("about feedback: %w", err)
		}
		okFact, err := fact(f.FactID)
		if err != nil {
			return a, fmt.Errorf("about feedback: %w", err)
		}
		if a.Developed, err = development(); err != nil {
			return a, fmt.Errorf("about feedback: %w", err)
		}
		a.Found = okStory && okFact && (f.Kind == feedback.Missed || a.Developed)
		facts = []int64{f.FactID}
	case f.Level == feedback.Fact:
		if a.Found, err = fact(f.FactID); err != nil {
			return a, fmt.Errorf("about feedback: %w", err)
		}
		facts = []int64{f.FactID}
	}
	ids, err := json.Marshal(facts)
	if err != nil {
		return a, err
	}
	if a.Depth, err = s.depthOf(ctx, `SELECT value FROM json_each(?)`, string(ids)); err != nil {
		return a, fmt.Errorf("about feedback: %w", err)
	}
	return a, nil
}

// depthOf is the closest Depth of the Topics that tie the Items of the
// Facts the query selects, or zero if none does.
func (s *Store) depthOf(ctx context.Context, factIDs string, args ...any) (interest.Depth, error) {
	var d interest.Depth
	err := s.db.QueryRowContext(ctx, `
		SELECT coalesce(max(topics.depth), 0)
		FROM facts
			JOIN item_topics ON item_topics.source_id = facts.source_id AND item_topics.entry_id = facts.entry_id
			JOIN topics ON topics.id = item_topics.topic_id
		WHERE facts.id IN (`+factIDs+`)`, args...).Scan(&d)
	return d, err
}

// Give keeps f as given and applies its effect to the Record, all at once.
// Effects mark what they change with the Feedback that changed it; nothing
// is deleted.
func (s *Store) Give(ctx context.Context, f feedback.Feedback, e feedback.Effect) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	facts, err := json.Marshal(f.FactIDs)
	if err != nil {
		return 0, err
	}
	var id int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO feedback (kind, level, story_id, fact_id, to_fact_id, title, fact_ids, day, depth, given_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		string(f.Kind), string(f.Level), nullID(f.StoryID), nullID(f.FactID), nullID(f.ToFactID),
		f.Title, string(facts), f.Day, int(f.Depth), formatTime(f.GivenAt)).Scan(&id); err != nil {
		return 0, fmt.Errorf("keep feedback: %w", err)
	}
	if err := apply(ctx, tx, id, f, e); err != nil {
		return 0, fmt.Errorf("apply feedback %d: %w", id, err)
	}
	return id, tx.Commit()
}

func apply(ctx context.Context, tx *sql.Tx, by int64, f feedback.Feedback, e feedback.Effect) error {
	exec := func(query string, args ...any) error {
		_, err := tx.ExecContext(ctx, query, args...)
		return err
	}
	if ns := e.NewStory; ns != nil {
		var story int64
		if err := tx.QueryRowContext(ctx, `INSERT INTO stories (title, started_at, missed_by) VALUES (?, ?, ?) RETURNING id`,
			ns.Title, formatTime(f.GivenAt), by).Scan(&story); err != nil {
			return err
		}
		for _, fact := range ns.FactIDs {
			if err := exec(`INSERT INTO developments (story_id, fact_id, missed_by) VALUES (?, ?, ?)`, story, fact, by); err != nil {
				return err
			}
		}
	}
	if d := e.AddDevelopment; d != nil {
		if err := exec(`INSERT INTO developments (story_id, fact_id, missed_by) VALUES (?, ?, ?)`, d.A, d.B, by); err != nil {
			return err
		}
	}
	if e.UnwantedStory != 0 {
		if err := exec(`UPDATE stories SET unwanted_by = coalesce(unwanted_by, ?) WHERE id = ?`, by, e.UnwantedStory); err != nil {
			return err
		}
	}
	if d := e.UnwantedDevelopment; d != nil {
		if err := exec(`UPDATE developments SET unwanted_by = coalesce(unwanted_by, ?) WHERE story_id = ? AND fact_id = ?`,
			by, d.A, d.B); err != nil {
			return err
		}
	}
	if e.WithdrawFact != 0 {
		if err := exec(`UPDATE facts SET withdrawn_by = coalesce(withdrawn_by, ?) WHERE id = ?`, by, e.WithdrawFact); err != nil {
			return err
		}
		if err := exec(`UPDATE developments SET withdrawn_by = coalesce(withdrawn_by, ?) WHERE fact_id = ?`, by, e.WithdrawFact); err != nil {
			return err
		}
	}
	return nil
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
