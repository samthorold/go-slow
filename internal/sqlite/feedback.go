package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
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
		return exists(`SELECT 1 FROM developments WHERE story_id = ? AND fact_id = ? AND NOT `+addedByUndone,
			f.StoryID, f.FactID)
	}
	var err error
	var facts []int64
	switch f.Level {
	case feedback.Assessment:
		if a.Found, err = exists(`SELECT 1 FROM assessments WHERE id = ?`, f.AssessmentID); err != nil {
			return a, fmt.Errorf("about feedback: %w", err)
		}
		a.Depth, err = s.depthOf(ctx, `SELECT fact_id FROM assessment_facts WHERE assessment_id = ?`, f.AssessmentID)
		if err != nil {
			return a, fmt.Errorf("about feedback: %w", err)
		}
		return a, nil
	case feedback.SundayRead:
		if a.Found, err = exists(`SELECT 1 FROM sunday_reads WHERE id = ?`, f.ReadID); err != nil {
			return a, fmt.Errorf("about feedback: %w", err)
		}
		a.Depth, err = s.depthOf(ctx, `SELECT c.fact_id FROM read_citations c
			JOIN read_statements st ON st.id = c.statement_id WHERE st.read_id = ? AND c.fact_id IS NOT NULL`, f.ReadID)
		if err != nil {
			return a, fmt.Errorf("about feedback: %w", err)
		}
		return a, nil
	}
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
	case f.Level == feedback.Connection:
		if a.Found, err = exists(`SELECT 1 FROM fact_connections WHERE from_fact_id = ? AND to_fact_id = ?`,
			f.FactID, f.ToFactID); err != nil {
			return a, fmt.Errorf("about feedback: %w", err)
		}
		facts = []int64{f.FactID, f.ToFactID}
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

// KeptFeedback is every piece of Feedback, as given, newest first, the
// undone with the rest.
func (s *Store) KeptFeedback(ctx context.Context) ([]feedback.Kept, error) {
	return s.kept(ctx, ``)
}

// Feedback is the piece with id, as given, reporting whether it is in the
// Record.
func (s *Store) Feedback(ctx context.Context, id int64) (feedback.Feedback, bool, error) {
	kept, err := s.kept(ctx, `WHERE feedback.id = ?`, id)
	if err != nil || len(kept) == 0 {
		return feedback.Feedback{}, false, err
	}
	return kept[0].Feedback, true, nil
}

// kept is the Feedback the where clause selects, newest first.
func (s *Store) kept(ctx context.Context, where string, args ...any) ([]feedback.Kept, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT feedback.id, kind, level, coalesce(story_id, 0), coalesce(fact_id, 0), coalesce(to_fact_id, 0),
			feedback.title, fact_ids, feedback.day, depth, given_at, coalesce(undone_at, ''),
			coalesce(stories.title, ''), coalesce(f.statement, ''), coalesce(g.statement, ''),
			coalesce(feedback.assessment_id, 0), coalesce(a.statement, ''), coalesce(feedback.read_id, 0),
			coalesce(ar.day, r.day, '')
		FROM feedback
			LEFT JOIN stories ON stories.id = feedback.story_id
			LEFT JOIN facts f ON f.id = feedback.fact_id
			LEFT JOIN facts g ON g.id = feedback.to_fact_id
			LEFT JOIN assessments a ON a.id = feedback.assessment_id
			LEFT JOIN sunday_reads ar ON ar.id = a.read_id
			LEFT JOIN sunday_reads r ON r.id = feedback.read_id
		`+where+`
		ORDER BY given_at DESC, feedback.id DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("kept feedback: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []feedback.Kept
	for rows.Next() {
		var k feedback.Kept
		var kind, level, facts, at, undone string
		if err := rows.Scan(&k.ID, &kind, &level, &k.StoryID, &k.FactID, &k.ToFactID, &k.Title, &facts, &k.Day,
			&k.Depth, &at, &undone, &k.Story, &k.Fact, &k.ToFact, &k.AssessmentID, &k.Assessment, &k.ReadID, &k.ReadDay); err != nil {
			return nil, fmt.Errorf("kept feedback: %w", err)
		}
		k.Kind, k.Level, k.GivenAt, k.UndoneAt = feedback.Kind(kind), feedback.Level(level), parseTime(at), parseTime(undone)
		if err := json.Unmarshal([]byte(facts), &k.FactIDs); err != nil {
			return nil, fmt.Errorf("kept feedback: %w", err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// FactsToAdd are the Facts the Owner may say a Story missed: those that
// share an Entity with its Developments and are not yet one in it, newest
// first. Withdrawn Facts are never offered.
func (s *Store) FactsToAdd(ctx context.Context, storyID int64) ([]editor.Fact, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT facts.id, facts.source_id, facts.entry_id, facts.statement, facts.as_at, facts.quote, `+factEntities+`
		FROM developments mine
			JOIN fact_entities ours ON ours.fact_id = mine.fact_id
			JOIN fact_entities theirs ON theirs.entity_id = ours.entity_id
			JOIN facts ON facts.id = theirs.fact_id
		WHERE mine.story_id = ?1 AND mine.withdrawn_by IS NULL AND facts.withdrawn_by IS NULL
			AND NOT EXISTS (SELECT 1 FROM developments d WHERE d.story_id = ?1 AND d.fact_id = facts.id)
		ORDER BY facts.as_at DESC, facts.id DESC`, storyID)
	if err != nil {
		return nil, fmt.Errorf("facts to add: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.Fact
	for rows.Next() {
		var f editor.Fact
		var asAt, ents string
		if err := rows.Scan(&f.ID, &f.SourceID, &f.EntryID, &f.Statement, &asAt, &f.Quote, &ents); err != nil {
			return nil, fmt.Errorf("facts to add: %w", err)
		}
		if err := scanFact(&f, asAt, ents); err != nil {
			return nil, fmt.Errorf("facts to add: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
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
		INSERT INTO feedback (kind, level, story_id, fact_id, to_fact_id, title, fact_ids, day, depth, given_at,
			assessment_id, read_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		string(f.Kind), string(f.Level), nullID(f.StoryID), nullID(f.FactID), nullID(f.ToFactID),
		f.Title, string(facts), f.Day, int(f.Depth), formatTime(f.GivenAt),
		nullID(f.AssessmentID), nullID(f.ReadID)).Scan(&id); err != nil {
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
		// A Development an undone missed piece added is in the Record,
		// withdrawn; adding it again names the new piece.
		if err := exec(`INSERT INTO developments (story_id, fact_id, missed_by) VALUES (?, ?, ?)
			ON CONFLICT (story_id, fact_id) DO UPDATE SET missed_by = excluded.missed_by`, d.A, d.B, by); err != nil {
			return err
		}
		if err := exec(`UPDATE developments SET withdrawn_by = `+developmentWithdrawnBy+` WHERE story_id = ? AND fact_id = ?`,
			d.A, d.B); err != nil {
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
		if err := exec(`UPDATE fact_connections SET withdrawn_by = coalesce(withdrawn_by, ?)
			WHERE from_fact_id = ?2 OR to_fact_id = ?2`, by, e.WithdrawFact); err != nil {
			return err
		}
	}
	if e.WithdrawAssessment != 0 {
		if err := exec(`UPDATE assessments SET withdrawn_by = coalesce(withdrawn_by, ?) WHERE id = ?`, by, e.WithdrawAssessment); err != nil {
			return err
		}
	}
	if c := e.WithdrawConnection; c != nil {
		if err := exec(`UPDATE fact_connections SET withdrawn_by = coalesce(withdrawn_by, ?)
			WHERE from_fact_id = ? AND to_fact_id = ?`, by, c.A, c.B); err != nil {
			return err
		}
	}
	return nil
}

// Undo marks f undone at at and lifts the marks e made, all at once. A mark
// is lifted by working it out again from the Feedback still standing: the
// earliest piece that makes it, as Give's marks name, or none.
func (s *Store) Undo(ctx context.Context, f feedback.Feedback, e feedback.Effect, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE feedback SET undone_at = ? WHERE id = ? AND undone_at IS NULL`,
		formatTime(at), f.ID)
	if err != nil {
		return fmt.Errorf("undo feedback %d: %w", f.ID, err)
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		// Already undone: nothing changes.
		return err
	}
	if err := lift(ctx, tx, f, e); err != nil {
		return fmt.Errorf("undo feedback %d: %w", f.ID, err)
	}
	return tx.Commit()
}

// addedByUndone is whether a Development, or a Story, was added by a
// missed piece since undone: it is no longer added, and is shown nowhere.
const (
	addedByUndone      = `EXISTS (SELECT 1 FROM feedback u WHERE u.id = developments.missed_by AND u.undone_at IS NOT NULL)`
	storyAddedByUndone = `EXISTS (SELECT 1 FROM feedback u WHERE u.id = stories.missed_by AND u.undone_at IS NOT NULL)`
)

// Each mark, worked out from the Feedback standing: the earliest piece not
// undone that makes it, or NULL.
const (
	// A Fact is withdrawn by a wrong Fact.
	factWithdrawnBy = `(SELECT min(w.id) FROM feedback w WHERE w.undone_at IS NULL
		AND w.kind = 'wrong' AND w.level = 'fact' AND w.fact_id = facts.id)`
	// A Development is withdrawn with its wrong Fact, or by undoing the
	// missed piece that added it, which then no longer adds it.
	developmentWithdrawnBy = `coalesce((SELECT min(w.id) FROM feedback w WHERE w.undone_at IS NULL
		AND w.kind = 'wrong' AND w.level = 'fact' AND w.fact_id = developments.fact_id),
		(SELECT m.id FROM feedback m WHERE m.id = developments.missed_by AND m.undone_at IS NOT NULL))`
	// A Connection is withdrawn by a wrong Connection, or with a wrong Fact
	// it connects.
	connectionWithdrawnBy = `(SELECT min(w.id) FROM feedback w WHERE w.undone_at IS NULL AND w.kind = 'wrong'
		AND ((w.level = 'fact' AND w.fact_id IN (fact_connections.from_fact_id, fact_connections.to_fact_id))
			OR (w.level = 'connection' AND w.fact_id = fact_connections.from_fact_id
				AND w.to_fact_id = fact_connections.to_fact_id)))`
	// An Assessment is withdrawn by a wrong Assessment.
	assessmentWithdrawnBy = `(SELECT min(w.id) FROM feedback w WHERE w.undone_at IS NULL
		AND w.kind = 'wrong' AND w.level = 'assessment' AND w.assessment_id = assessments.id)`
	// A Story or a Development is out of the Briefing by unwanted.
	storyUnwantedBy = `(SELECT min(w.id) FROM feedback w WHERE w.undone_at IS NULL
		AND w.kind = 'unwanted' AND w.level = 'story' AND w.story_id = stories.id)`
	developmentUnwantedBy = `(SELECT min(w.id) FROM feedback w WHERE w.undone_at IS NULL
		AND w.kind = 'unwanted' AND w.level = 'development'
		AND w.story_id = developments.story_id AND w.fact_id = developments.fact_id)`
)

// lift works out again each mark e made, from the Feedback standing.
func lift(ctx context.Context, tx *sql.Tx, f feedback.Feedback, e feedback.Effect) error {
	exec := func(query string, args ...any) error {
		_, err := tx.ExecContext(ctx, query, args...)
		return err
	}
	if e.WithdrawFact != 0 {
		if err := exec(`UPDATE facts SET withdrawn_by = `+factWithdrawnBy+` WHERE id = ?`, e.WithdrawFact); err != nil {
			return err
		}
		if err := exec(`UPDATE developments SET withdrawn_by = `+developmentWithdrawnBy+` WHERE fact_id = ?`, e.WithdrawFact); err != nil {
			return err
		}
		if err := exec(`UPDATE fact_connections SET withdrawn_by = `+connectionWithdrawnBy+`
			WHERE from_fact_id = ?1 OR to_fact_id = ?1`, e.WithdrawFact); err != nil {
			return err
		}
	}
	if c := e.WithdrawConnection; c != nil {
		if err := exec(`UPDATE fact_connections SET withdrawn_by = `+connectionWithdrawnBy+`
			WHERE from_fact_id = ? AND to_fact_id = ?`, c.A, c.B); err != nil {
			return err
		}
	}
	if e.WithdrawAssessment != 0 {
		if err := exec(`UPDATE assessments SET withdrawn_by = `+assessmentWithdrawnBy+` WHERE id = ?`, e.WithdrawAssessment); err != nil {
			return err
		}
	}
	if e.NewStory != nil || e.AddDevelopment != nil {
		if err := exec(`UPDATE developments SET withdrawn_by = `+developmentWithdrawnBy+` WHERE missed_by = ?`, f.ID); err != nil {
			return err
		}
	}
	if e.UnwantedStory != 0 {
		if err := exec(`UPDATE stories SET unwanted_by = `+storyUnwantedBy+` WHERE id = ?`, e.UnwantedStory); err != nil {
			return err
		}
	}
	if d := e.UnwantedDevelopment; d != nil {
		if err := exec(`UPDATE developments SET unwanted_by = `+developmentUnwantedBy+` WHERE story_id = ? AND fact_id = ?`,
			d.A, d.B); err != nil {
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
