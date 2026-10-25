package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

// LastSundayReadWritten is when the latest Sunday read was written, or zero.
func (s *Store) LastSundayReadWritten(ctx context.Context) (time.Time, error) {
	var at string
	if err := s.db.QueryRowContext(ctx, `SELECT coalesce(max(written_at), '') FROM sunday_reads`).Scan(&at); err != nil {
		return time.Time{}, fmt.Errorf("last sunday read: %w", err)
	}
	return parseTime(at), nil
}

// SaveSundayRead keeps a Sunday read: its statements in order, each with
// what it cites, all at once.
func (s *Store) SaveSundayRead(ctx context.Context, r editor.SundayRead) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var readID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO sunday_reads (day, written_at, model, turns, input_tokens, output_tokens)
		VALUES (?, ?, ?, ?, ?, ?) RETURNING id`,
		r.Day, formatTime(r.WrittenAt), r.Model, r.Turns, r.Usage.InputTokens, r.Usage.OutputTokens).Scan(&readID); err != nil {
		return fmt.Errorf("save sunday read %s: %w", r.Day, err)
	}
	made := make([]int64, len(r.Assessments))
	for i, a := range r.Assessments {
		var supersedes any
		if a.Supersedes != 0 {
			supersedes = a.Supersedes
		}
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO assessments (read_id, position, statement, as_at, supersedes_id) VALUES (?, ?, ?, ?, ?) RETURNING id`,
			readID, i, a.Statement, a.AsAt.Format(time.DateOnly), supersedes).Scan(&made[i]); err != nil {
			return fmt.Errorf("save sunday read %s: assessment: %w", r.Day, err)
		}
		for j, f := range a.Facts {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO assessment_facts (assessment_id, fact_id, position) VALUES (?, ?, ?)`, made[i], f.ID, j); err != nil {
				return fmt.Errorf("save sunday read %s: assessment's fact %d: %w", r.Day, f.ID, err)
			}
		}
	}
	for _, st := range r.Stories {
		var id int64
		if err := tx.QueryRowContext(ctx, `INSERT INTO stories (title, started_at) VALUES (?, ?) RETURNING id`,
			st.Title, formatTime(r.WrittenAt)).Scan(&id); err != nil {
			return fmt.Errorf("save sunday read %s: start story %q: %w", r.Day, st.Title, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO read_stories (read_id, story_id) VALUES (?, ?)`, readID, id); err != nil {
			return fmt.Errorf("save sunday read %s: story %q: %w", r.Day, st.Title, err)
		}
		for _, d := range st.Timeline {
			if _, err := tx.ExecContext(ctx, `INSERT INTO developments (story_id, fact_id) VALUES (?, ?)`, id, d.ID); err != nil {
				return fmt.Errorf("save sunday read %s: gather fact %d into %q: %w", r.Day, d.ID, st.Title, err)
			}
		}
	}
	for i, st := range r.Statements {
		var id int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO read_statements (read_id, position, text) VALUES (?, ?, ?) RETURNING id`,
			readID, i, st.Text).Scan(&id); err != nil {
			return fmt.Errorf("save sunday read %s: statement: %w", r.Day, err)
		}
		for j, c := range st.Cites {
			var fact, story, assessment any
			switch c.Kind {
			case editor.CitesFact:
				fact = c.Fact.ID
			case editor.CitesDevelopment:
				fact, story = c.Fact.ID, c.Story.ID
			case editor.CitesAssessment:
				assessment = c.Assessment.ID
				if c.NewAssessment != 0 {
					assessment = made[c.NewAssessment-1]
				}
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO read_citations (statement_id, position, kind, fact_id, story_id, assessment_id)
				VALUES (?, ?, ?, ?, ?, ?)`,
				id, j, string(c.Kind), fact, story, assessment); err != nil {
				return fmt.Errorf("save sunday read %s: citation: %w", r.Day, err)
			}
		}
	}
	return tx.Commit()
}

// SaveSundayReadFailure keeps a failure to write a Sunday's read.
func (s *Store) SaveSundayReadFailure(ctx context.Context, f editor.SundayReadFailure) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO sunday_read_failures (day, failed_at, reason) VALUES (?, ?, ?)`,
		f.Day, formatTime(f.At), f.Reason); err != nil {
		return fmt.Errorf("save sunday read failure: %w", err)
	}
	return nil
}

// SundayReadFailures are the failures to write the day's Sunday read, the
// earliest first.
func (s *Store) SundayReadFailures(ctx context.Context, day string) ([]editor.SundayReadFailure, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT failed_at, reason FROM sunday_read_failures WHERE day = ? ORDER BY failed_at, id`, day)
	if err != nil {
		return nil, fmt.Errorf("sunday read failures: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.SundayReadFailure
	for rows.Next() {
		f := editor.SundayReadFailure{Day: day}
		var at string
		if err := rows.Scan(&at, &f.Reason); err != nil {
			return nil, fmt.Errorf("sunday read failures: %w", err)
		}
		f.At = parseTime(at)
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sunday read failures: %w", err)
	}
	return out, nil
}

// SundayRead is the Sunday read for the day, reporting whether one was
// written: its statements in order, each with what it cites, traced.
func (s *Store) SundayRead(ctx context.Context, day string) (editor.SundayRead, bool, error) {
	r := editor.SundayRead{Day: day}
	var at string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, written_at, model, turns, input_tokens, output_tokens FROM sunday_reads WHERE day = ?`, day).
		Scan(&r.ID, &at, &r.Model, &r.Turns, &r.Usage.InputTokens, &r.Usage.OutputTokens)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, fmt.Errorf("sunday read %s: %w", day, err)
	}
	r.WrittenAt = parseTime(at)
	if r.Statements, err = s.readStatements(ctx, r.ID); err != nil {
		return r, false, fmt.Errorf("sunday read %s: %w", day, err)
	}
	if r.Assessments, err = s.readAssessments(ctx, r.ID); err != nil {
		return r, false, fmt.Errorf("sunday read %s: %w", day, err)
	}
	started, err := s.ids(ctx, `SELECT story_id FROM read_stories WHERE read_id = ? ORDER BY story_id`, r.ID)
	if err != nil {
		return r, false, fmt.Errorf("sunday read %s: %w", day, err)
	}
	for _, id := range started {
		st, _, err := s.Story(ctx, id)
		if err != nil {
			return r, false, err
		}
		r.Stories = append(r.Stories, st)
	}
	return r, true, nil
}

func (s *Store) readStatements(ctx context.Context, readID int64) ([]editor.ReadStatement, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT read_statements.id, read_statements.text, read_citations.kind,
			coalesce(read_citations.story_id, 0), coalesce(stories.title, ''),
			coalesce(read_citations.fact_id, 0), coalesce(read_citations.assessment_id, 0)
		FROM read_statements
			JOIN read_citations ON read_citations.statement_id = read_statements.id
			LEFT JOIN stories ON stories.id = read_citations.story_id
		WHERE read_statements.read_id = ?
		ORDER BY read_statements.position, read_citations.position`, readID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	type cited struct {
		statement int
		c         editor.Citation
	}
	var out []editor.ReadStatement
	var facts []cited
	for rows.Next() {
		var id, storyID, factID, assessmentID int64
		var text, kind, title string
		if err := rows.Scan(&id, &text, &kind, &storyID, &title, &factID, &assessmentID); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].ID != id {
			out = append(out, editor.ReadStatement{ID: id, Text: text})
		}
		c := editor.Citation{Kind: editor.CitationKind(kind), Story: editor.Story{ID: storyID, Title: title}}
		c.Fact.ID, c.Assessment.ID = factID, assessmentID
		facts = append(facts, cited{len(out) - 1, c})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()
	for _, f := range facts {
		if f.c.Fact.ID != 0 {
			d, err := s.tracedFact(ctx, f.c.Fact.ID)
			if err != nil {
				return nil, err
			}
			f.c.Fact = d
		}
		if f.c.Assessment.ID != 0 {
			as, err := s.assessments(ctx, `a.id = ?`, -1, f.c.Assessment.ID)
			if err != nil {
				return nil, err
			}
			if len(as) == 1 {
				f.c.Assessment = as[0]
			}
		}
		out[f.statement].Cites = append(out[f.statement].Cites, f.c)
	}
	return out, nil
}

// FactsAbout are at most limit Facts involving the Entity called entity, by
// its name or any it is called by, as at a date before before (any date if
// zero), the latest first, each with the Stories it is a Development in.
func (s *Store) FactsAbout(ctx context.Context, entity string, before time.Time, limit int) ([]editor.RecordFact, error) {
	until := "9999-12-31"
	if !before.IsZero() {
		until = before.Format(time.DateOnly)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+traced("f", "i")+`
		FROM facts f JOIN items i ON i.source_id = f.source_id AND i.entry_id = f.entry_id
		WHERE f.as_at < ?2 AND EXISTS (
			SELECT 1 FROM fact_entities fe JOIN entities e ON e.id = fe.entity_id
			WHERE fe.fact_id = f.id AND (e.name = ?1 COLLATE NOCASE OR EXISTS (
				SELECT 1 FROM entity_names n WHERE n.entity_id = e.id AND n.name = ?1 COLLATE NOCASE)))
		ORDER BY f.as_at DESC, f.id DESC
		LIMIT ?3`, entity, until, limit)
	if err != nil {
		return nil, fmt.Errorf("facts about %q: %w", entity, err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.RecordFact
	for rows.Next() {
		var d editor.Development
		var asAt, ents string
		if err := rows.Scan(&d.ID, &d.SourceID, &d.EntryID, &d.Statement, &asAt, &d.Quote, &ents, &d.ItemTitle); err != nil {
			return nil, fmt.Errorf("facts about %q: %w", entity, err)
		}
		if err := scanFact(&d.Fact, asAt, ents); err != nil {
			return nil, fmt.Errorf("facts about %q: %w", entity, err)
		}
		out = append(out, editor.RecordFact{Fact: d})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("facts about %q: %w", entity, err)
	}
	_ = rows.Close()
	for i := range out {
		if out[i].Stories, err = s.storiesOf(ctx, out[i].Fact.ID); err != nil {
			return nil, fmt.Errorf("facts about %q: %w", entity, err)
		}
	}
	return out, nil
}

// storiesOf are the Stories the Fact id is a Development in, by id.
func (s *Store) storiesOf(ctx context.Context, factID int64) ([]editor.Story, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT stories.id, stories.title FROM developments JOIN stories ON stories.id = developments.story_id
		WHERE developments.fact_id = ? ORDER BY stories.id`, factID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []editor.Story
	for rows.Next() {
		var st editor.Story
		if err := rows.Scan(&st.ID, &st.Title); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// Assessments are at most limit Assessments, the latest first, each with the
// Facts it cites and what supersedes it.
func (s *Store) Assessments(ctx context.Context, limit int) ([]editor.Assessment, error) {
	out, err := s.assessments(ctx, `1`, limit)
	if err != nil {
		return nil, fmt.Errorf("assessments: %w", err)
	}
	return out, nil
}

// ReadAssessments are the Assessments the Sunday read readID made, in order.
func (s *Store) readAssessments(ctx context.Context, readID int64) ([]editor.Assessment, error) {
	out, err := s.assessments(ctx, `a.read_id = ?`, -1, readID)
	slices.Reverse(out) // one read's are all as at its day, latest made first
	return out, err
}

// assessments are the Assessments matching where, the latest first (by as
// at, then the order they were made), at most limit (all, if negative).
func (s *Store) assessments(ctx context.Context, where string, limit int, args ...any) ([]editor.Assessment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.statement, a.as_at, coalesce(a.supersedes_id, 0),
			coalesce((SELECT later.id FROM assessments later WHERE later.supersedes_id = a.id), 0), r.day
		FROM assessments a JOIN sunday_reads r ON r.id = a.read_id
		WHERE `+where+`
		ORDER BY a.as_at DESC, a.id DESC
		LIMIT `+fmt.Sprint(limit), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []editor.Assessment
	for rows.Next() {
		var a editor.Assessment
		var asAt string
		if err := rows.Scan(&a.ID, &a.Statement, &asAt, &a.Supersedes, &a.SupersededBy, &a.Day); err != nil {
			return nil, err
		}
		if a.AsAt, err = time.Parse(time.DateOnly, asAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()
	for i := range out {
		ids, err := s.ids(ctx, `SELECT fact_id FROM assessment_facts WHERE assessment_id = ? ORDER BY position`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			d, err := s.tracedFact(ctx, id)
			if err != nil {
				return nil, err
			}
			out[i].Facts = append(out[i].Facts, d)
		}
	}
	return out, nil
}

// ids are the ids a query of one integer column gives.
func (s *Store) ids(ctx context.Context, query string, args ...any) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// tracedFact is a Fact with the title of its Item.
func (s *Store) tracedFact(ctx context.Context, id int64) (editor.Development, error) {
	var d editor.Development
	var asAt, ents string
	if err := s.db.QueryRowContext(ctx, `SELECT `+traced("f", "i")+`
		FROM facts f JOIN items i ON i.source_id = f.source_id AND i.entry_id = f.entry_id
		WHERE f.id = ?`, id).Scan(&d.ID, &d.SourceID, &d.EntryID, &d.Statement, &asAt, &d.Quote, &ents, &d.ItemTitle); err != nil {
		return d, fmt.Errorf("fact %d: %w", id, err)
	}
	return d, scanFact(&d.Fact, asAt, ents)
}
