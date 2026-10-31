package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/samthorold/go-slow/internal/feedback"
)

// The Feedback kept before the Record became events: a piece on a Sunday
// read or its Assessments is still given so (feedback.Service); one on a
// Story, a Development, a Fact or a Connection is no longer given, and the
// marks the pieces given made (withdrawn_by, unwanted_by, missed_by) stay as
// they were, read by the pages of what they marked and by the register.

// About reports whether what f is about is in the Record.
func (s *Store) About(ctx context.Context, f feedback.Feedback) (bool, error) {
	query, id := `SELECT 1 FROM sunday_reads WHERE id = ?`, f.ReadID
	if f.Level == feedback.Assessment {
		query, id = `SELECT 1 FROM assessments WHERE id = ?`, f.AssessmentID
	}
	var found bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (`+query+`)`, id).Scan(&found); err != nil {
		return false, fmt.Errorf("about feedback: %w", err)
	}
	return found, nil
}

// KeptFeedback is every piece of the Feedback kept before the Record became
// events, as given, newest first, the undone with the rest.
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
		SELECT feedback.id, kind, level, feedback.day, given_at, coalesce(undone_at, ''),
			coalesce(feedback.assessment_id, 0), coalesce(a.statement, ''), coalesce(feedback.read_id, 0),
			coalesce(ar.day, r.day, ''), coalesce(ar.conversation_id, r.conversation_id, 0)
		FROM feedback
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
		var kind, level, at, undone string
		if err := rows.Scan(&k.ID, &kind, &level, &k.Day, &at, &undone,
			&k.AssessmentID, &k.Assessment, &k.ReadID, &k.ReadDay, &k.Conversation); err != nil {
			return nil, fmt.Errorf("kept feedback: %w", err)
		}
		k.Kind, k.Level, k.GivenAt, k.UndoneAt = feedback.Kind(kind), feedback.Level(level), parseTime(at), parseTime(undone)
		out = append(out, k)
	}
	return out, rows.Err()
}

// Give keeps f as given and applies its effect to the Record, all at once.
// Effects mark what they change with the Feedback that changed it; nothing
// is deleted. It keeps no Depth: Feedback carries none.
func (s *Store) Give(ctx context.Context, f feedback.Feedback, e feedback.Effect) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var id int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO feedback (kind, level, title, fact_ids, day, depth, given_at, assessment_id, read_id)
		VALUES (?, ?, '', '[]', ?, 0, ?, ?, ?) RETURNING id`,
		string(f.Kind), string(f.Level), f.Day, formatTime(f.GivenAt),
		nullID(f.AssessmentID), nullID(f.ReadID)).Scan(&id); err != nil {
		return 0, fmt.Errorf("keep feedback: %w", err)
	}
	if e.WithdrawAssessment != 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE assessments SET withdrawn_by = coalesce(withdrawn_by, ?) WHERE id = ?`,
			id, e.WithdrawAssessment); err != nil {
			return 0, fmt.Errorf("apply feedback %d: %w", id, err)
		}
	}
	return id, tx.Commit()
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
	if err := lift(ctx, tx, e); err != nil {
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

// assessmentWithdrawnBy is an Assessment's mark, worked out from the
// Feedback standing: the earliest wrong on it not undone, or NULL.
const assessmentWithdrawnBy = `(SELECT min(w.id) FROM feedback w WHERE w.undone_at IS NULL
		AND w.kind = 'wrong' AND w.level = 'assessment' AND w.assessment_id = assessments.id)`

// lift works out again each mark e made, from the Feedback standing.
func lift(ctx context.Context, tx *sql.Tx, e feedback.Effect) error {
	if e.WithdrawAssessment == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `UPDATE assessments SET withdrawn_by = `+assessmentWithdrawnBy+` WHERE id = ?`, e.WithdrawAssessment)
	return err
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
