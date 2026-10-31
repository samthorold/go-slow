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
	if r.SundayReads, err = s.strings(ctx, `SELECT day FROM sunday_reads ORDER BY day`); err != nil {
		return r, fmt.Errorf("register: sunday reads: %w", err)
	}
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
	if r.Judges, err = s.judges(ctx); err != nil {
		return r, err
	}
	sw, found, err := s.Switch(ctx)
	if err != nil {
		return r, fmt.Errorf("register: %w", err)
	}
	if found {
		r.Switch = sw.At
	}
	return r, nil
}

// judges are the live Judges as each began judging for its act, in order: a
// change of version or model is a new Judge, and so is a return to an
// earlier one. A chosen candidate's readings count as made when it was
// chosen, and an unchosen candidate's not at all, as every view reads them;
// a judgement made with no call (a Briefing written empty) has no Judge.
func (s *Store) judges(ctx context.Context) ([]register.Judge, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH judged (act, version, model, at, id) AS (
			SELECT r.act, r.version, r.model, coalesce(che.at, e.at), coalesce(ch.event_id, r.event_id)
			FROM readings r JOIN events e ON e.id = r.event_id
				LEFT JOIN candidate_readings cr ON cr.reading_id = r.event_id
				LEFT JOIN candidate_choices ch ON ch.candidate_id = cr.candidate_id
				LEFT JOIN events che ON che.id = ch.event_id
			WHERE cr.reading_id IS NULL OR ch.event_id IS NOT NULL
			UNION ALL SELECT j.act, j.version, j.model, e.at, j.event_id FROM relatings j JOIN events e ON e.id = j.event_id
			UNION ALL SELECT j.act, j.version, j.model, e.at, j.event_id FROM told_briefings j JOIN events e ON e.id = j.event_id
			UNION ALL SELECT j.act, j.version, j.model, e.at, j.event_id FROM told_reads j JOIN events e ON e.id = j.event_id
		), changes AS (
			SELECT act, version, model, at, id,
				lag(version || ' ' || model) OVER (PARTITION BY act ORDER BY at, id) AS before
			FROM judged WHERE model != ''
		)
		SELECT act, version, model, at FROM changes
		WHERE before IS NULL OR before != version || ' ' || model
		ORDER BY at, id`)
	if err != nil {
		return nil, fmt.Errorf("register: judges: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []register.Judge
	for rows.Next() {
		var j register.Judge
		var at string
		if err := rows.Scan(&j.Act, &j.Version, &j.Model, &at); err != nil {
			return nil, fmt.Errorf("register: judges: %w", err)
		}
		j.From = parseTime(at)
		out = append(out, j)
	}
	return out, rows.Err()
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

// untraced names every statement of a Briefing or a Sunday read that cites
// nothing, every Assessment that cites nothing (it cites Spans and earlier
// Assessments), and every citation of a Span outside its Item's text.
const untraced = `
	SELECT 'Statement ' || (st.position + 1) || ' of the Briefing of ' || t.day || ' cites no Span.'
	FROM told_statements st JOIN told_briefings t ON t.event_id = st.briefing_id
	WHERE NOT EXISTS (SELECT 1 FROM told_citations c WHERE c.statement_id = st.event_id)
	UNION ALL
	SELECT 'Statement ' || (st.position + 1) || ' of the Briefing of ' || t.day || ' cites a Span outside its Item''s text.'
	FROM told_citations c JOIN told_statements st ON st.event_id = c.statement_id
		JOIN told_briefings t ON t.event_id = st.briefing_id
		JOIN items i ON i.source_id = c.source_id AND i.entry_id = c.entry_id
	WHERE c.span_end > length(CAST(i.content AS BLOB))
	UNION ALL
	SELECT 'Statement ' || (st.position + 1) || ' of the Sunday read of ' || r.day || ' cites nothing.'
	FROM told_read_statements st JOIN told_reads r ON r.event_id = st.read_id
	WHERE NOT EXISTS (SELECT 1 FROM told_read_citations c WHERE c.statement_id = st.event_id)
	UNION ALL
	SELECT 'Assessment ' || (a.position + 1) || ' of the Sunday read of ' || r.day || ' cites nothing.'
	FROM told_assessments a JOIN told_reads r ON r.event_id = a.read_id
	WHERE NOT EXISTS (SELECT 1 FROM assessment_grounds g WHERE g.assessment_id = a.event_id)
	UNION ALL
	SELECT 'The Sunday read of ' || r.day || ' cites a Span outside its Item''s text.'
	FROM (SELECT c.statement_id AS id, c.source_id, c.entry_id, c.span_end FROM told_read_citations c WHERE c.source_id IS NOT NULL
		UNION ALL SELECT g.assessment_id, g.source_id, g.entry_id, g.span_end FROM assessment_grounds g WHERE g.source_id IS NOT NULL) c
		JOIN items i ON i.source_id = c.source_id AND i.entry_id = c.entry_id
		JOIN told_reads r ON r.event_id = coalesce((SELECT read_id FROM told_read_statements WHERE event_id = c.id),
			(SELECT read_id FROM told_assessments WHERE event_id = c.id))
	WHERE c.span_end > length(CAST(i.content AS BLOB))`

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

// givenFeedback is every piece of Feedback standing, as the register counts
// it, with what it was given on (system design: Read from Feedback): the act
// of the judge behind the judgement wrong or new to me is about (a look-up
// is plain code, counted as one), or the Source of the Span unwanted or
// missed is about; and the Sunday read it is about, if it is one, one of
// its Assessments or a Story it gathered. A piece the Owner undid was a
// mistake in giving, not a reading of the Editor, so it is not counted.
func (s *Store) givenFeedback(ctx context.Context) ([]register.Given, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.kind,
			coalesce(f.source_id, rr.act, sr.act, CASE WHEN lk.event_id IS NOT NULL THEN 'lookup' END,
				rl.act, gb.act, sb.act, tr.act, je.kind),
			e.at, coalesce(tr.day, '')
		FROM feedback_given f JOIN events e ON e.id = f.event_id
			LEFT JOIN events je ON je.id = f.judgement_id
			LEFT JOIN refers_to rt ON rt.event_id = f.judgement_id
			LEFT JOIN readings rr ON rr.event_id = rt.reading_id
			LEFT JOIN speakers sk ON sk.event_id = f.judgement_id
			LEFT JOIN readings sr ON sr.event_id = sk.reading_id
			LEFT JOIN lookups lk ON lk.event_id = f.judgement_id
			LEFT JOIN relations rel ON rel.event_id = f.judgement_id
			LEFT JOIN relatings rl ON rl.event_id = rel.relating_id
			LEFT JOIN gatherings g ON g.event_id = f.judgement_id
			LEFT JOIN told_briefings gb ON gb.event_id = g.briefing_id
			LEFT JOIN told_statements st ON st.event_id = f.judgement_id
			LEFT JOIN told_briefings sb ON sb.event_id = st.briefing_id
			LEFT JOIN told_assessments ta ON ta.event_id = f.judgement_id
			LEFT JOIN told_reads tr ON tr.event_id = coalesce(ta.read_id, g.read_id, f.judgement_id)
		WHERE `+standing("f.event_id")+`
		ORDER BY e.at, f.event_id`)
	if err != nil {
		return nil, fmt.Errorf("register: feedback: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []register.Given
	for rows.Next() {
		var kind, on, at, read string
		if err := rows.Scan(&kind, &on, &at, &read); err != nil {
			return nil, fmt.Errorf("register: feedback: %w", err)
		}
		out = append(out, register.Given{Kind: feedback.Kind(kind), On: on, At: parseTime(at), SundayRead: read})
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
