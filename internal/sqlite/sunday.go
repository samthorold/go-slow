package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// The versions of the shapes of a Sunday read's events this build writes.
const (
	readShape          = 1
	readStatementShape = 1
	assessmentShape    = 1
)

// LastSundayReadWritten is when the latest Sunday read was written, in
// either shape, or zero.
func (s *Store) LastSundayReadWritten(ctx context.Context) (time.Time, error) {
	var at string
	if err := s.db.QueryRowContext(ctx, `SELECT coalesce(max(written_at), '') FROM sunday_reads`).Scan(&at); err != nil {
		return time.Time{}, fmt.Errorf("last sunday read: %w", err)
	}
	return parseTime(at), nil
}

// HasSundayRead reports whether the day's Sunday read has been written.
func (s *Store) HasSundayRead(ctx context.Context, day string) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sunday_reads WHERE day = ?`, day).Scan(&n); err != nil {
		return false, fmt.Errorf("has sunday read: %w", err)
	}
	return n > 0, nil
}

// BriefingsWritten are the Briefings over Spans written after after and up
// to upTo, in the order written, as Briefing reads each.
func (s *Store) BriefingsWritten(ctx context.Context, after, upTo time.Time) ([]editor.Briefing, error) {
	days, err := s.textsOf(ctx, `
		SELECT t.day FROM told_briefings t JOIN briefings b USING (day)
		WHERE b.written_at > ? AND b.written_at <= ?
		ORDER BY b.written_at`, formatTime(after), formatTime(upTo))
	if err != nil {
		return nil, fmt.Errorf("briefings written: %w", err)
	}
	out := make([]editor.Briefing, 0, len(days))
	for _, day := range days {
		b, _, err := s.Briefing(ctx, day)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// KeepSundayRead keeps a Sunday read for its day as an event, with its
// Judge, and each Assessment, statement and Story it gathered as an event
// of its own, tied to it, all at once, with its conversation. The day and
// the run's turns and tokens are kept in sunday_reads, beside the reads of
// the earlier shape.
func (s *Store) KeepSundayRead(ctx context.Context, r editor.SundayRead) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	fail := func(what string, err error) error { return fmt.Errorf("keep sunday read %s: %s: %w", r.Day, what, err) }
	conversation, err := keepConversation(ctx, tx, r.Conversation)
	if err != nil {
		return fail("conversation", err)
	}
	at := formatTime(r.WrittenAt)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sunday_reads (day, written_at, model, turns, input_tokens, output_tokens, conversation_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.Day, at, r.Judge.Model, r.Turns, r.Usage.InputTokens, r.Usage.OutputTokens, conversation); err != nil {
		return fail("day", err)
	}
	event := func(kind string, shape int) (int64, error) {
		res, err := tx.ExecContext(ctx, `INSERT INTO events (kind, shape, at) VALUES (?, ?, ?)`, kind, shape, at)
		if err != nil {
			return 0, fail(kind, err)
		}
		return res.LastInsertId()
	}
	id, err := event("sunday_read", readShape)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO told_reads (event_id, day, act, version, model) VALUES (?, ?, ?, ?, ?)`,
		id, r.Day, r.Judge.Act, r.Judge.Version, r.Judge.Model); err != nil {
		return fail("read", err)
	}
	reading := func(o editor.OtherSpan) any {
		if o.ReadingID == 0 {
			return nil
		}
		return o.ReadingID
	}
	made := make([]int64, len(r.Assessments))
	for i, a := range r.Assessments {
		if made[i], err = event("assessment", assessmentShape); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO told_assessments (event_id, read_id, position, statement, as_at, supersedes_id) VALUES (?, ?, ?, ?, ?, ?)`,
			made[i], id, i, a.Statement, a.AsAt.Format(time.DateOnly), nullID(a.Supersedes)); err != nil {
			return fail("assessment", err)
		}
		n := 0
		for _, o := range a.Spans {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO assessment_grounds (assessment_id, position, source_id, entry_id, span_start, span_end, reading_id)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				made[i], n, o.At.SourceID, o.At.EntryID, o.At.Span.Start, o.At.Span.End, reading(o)); err != nil {
				return fail("assessment's Span", err)
			}
			n++
		}
		for _, e := range a.Earlier {
			if _, err := tx.ExecContext(ctx, `INSERT INTO assessment_grounds (assessment_id, position, earlier_id) VALUES (?, ?, ?)`,
				made[i], n, e.ID); err != nil {
				return fail("assessment's earlier Assessment", err)
			}
			n++
		}
	}
	// Each lookup the read cites is kept once, however many statements cite
	// it.
	lookups := map[editor.Lookup]int64{}
	lookup := func(l editor.Lookup) (int64, error) {
		if id, ok := lookups[l]; ok {
			return id, nil
		}
		var id int64
		err := tx.QueryRowContext(ctx, `
			INSERT INTO reference_lookups (reference, item_id, revision, looked_up_at, licence) VALUES (?, ?, ?, ?, ?) RETURNING id`,
			l.Reference, l.ItemID, l.Revision, formatTime(l.At), l.Licence).Scan(&id)
		lookups[l] = id
		return id, err
	}
	for i, st := range r.Statements {
		sid, err := event("read_statement", readStatementShape)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO told_read_statements (event_id, read_id, position, text) VALUES (?, ?, ?, ?)`,
			sid, id, i, st.Text); err != nil {
			return fail("statement", err)
		}
		for j, c := range st.Cites {
			var source, entry, start, end, readingID, assessment, lookupID any
			switch c.Kind {
			case editor.CitesSpan:
				source, entry, start, end, readingID = c.Span.At.SourceID, c.Span.At.EntryID, c.Span.At.Span.Start, c.Span.At.Span.End, reading(c.Span)
			case editor.CitesAssessment:
				assessment = c.Assessment.ID
				if c.NewAssessment != 0 {
					assessment = made[c.NewAssessment-1]
				}
			case editor.CitesLookup:
				l := c.Lookup
				l.At = l.At.UTC()
				if lookupID, err = lookup(l); err != nil {
					return fail("lookup", err)
				}
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO told_read_citations (statement_id, position, source_id, entry_id, span_start, span_end, reading_id, assessment_id, lookup_id)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				sid, j, source, entry, start, end, readingID, assessment, lookupID); err != nil {
				return fail("citation", err)
			}
		}
	}
	for _, g := range r.Gathered {
		gid, err := event("gathering", gatheringShape)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO gatherings (event_id, read_id, source_id, entry_id, span_start, span_end) VALUES (?, ?, ?, ?, ?, ?)`,
			gid, id, g.From.SourceID, g.From.EntryID, g.From.Span.Start, g.From.Span.End); err != nil {
			return fail("gathering", err)
		}
		for j, a := range g.Story {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO gathered_spans (gathering_id, position, source_id, entry_id, span_start, span_end)
				VALUES (?, ?, ?, ?, ?, ?)`,
				gid, j, a.SourceID, a.EntryID, a.Span.Start, a.Span.End); err != nil {
				return fail("gathered Span", err)
			}
		}
	}
	return tx.Commit()
}

// SaveSundayReadFailure keeps a failure to write a Sunday's read, with the
// conversation of the attempt that failed.
func (s *Store) SaveSundayReadFailure(ctx context.Context, f editor.SundayReadFailure) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	conversation, err := keepConversation(ctx, tx, f.Conversation)
	if err != nil {
		return fmt.Errorf("save sunday read failure: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sunday_read_failures (day, failed_at, reason, conversation_id) VALUES (?, ?, ?, ?)`,
		f.Day, formatTime(f.At), f.Reason, conversation); err != nil {
		return fmt.Errorf("save sunday read failure: %w", err)
	}
	return tx.Commit()
}

// SundayReadFailures are the failures to write the day's Sunday read, the
// earliest first.
func (s *Store) SundayReadFailures(ctx context.Context, day string) ([]editor.SundayReadFailure, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT failed_at, reason, coalesce(conversation_id, 0) FROM sunday_read_failures WHERE day = ? ORDER BY failed_at, id`, day)
	if err != nil {
		return nil, fmt.Errorf("sunday read failures: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.SundayReadFailure
	for rows.Next() {
		f := editor.SundayReadFailure{Day: day}
		var at string
		if err := rows.Scan(&at, &f.Reason, &f.ConversationID); err != nil {
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

// SundayRead is the day's Sunday read over Spans, reporting whether one was
// written in that shape: its Judge, turns and tokens, its statements in
// order, each with what it cites, its Assessments in order, each with what
// it rests on, and the Stories it gathered, as it was written. The Spans are
// as their Items' newest readings read them.
func (s *Store) SundayRead(ctx context.Context, day string) (editor.SundayRead, bool, error) {
	r := editor.SundayRead{Day: day}
	var at string
	err := s.db.QueryRowContext(ctx, `
		SELECT t.event_id, r.written_at, t.act, t.version, t.model, r.turns, r.input_tokens, r.output_tokens,
			coalesce(r.conversation_id, 0)
		FROM told_reads t JOIN sunday_reads r USING (day) WHERE t.day = ?`, day).
		Scan(&r.ID, &at, &r.Judge.Act, &r.Judge.Version, &r.Judge.Model, &r.Turns, &r.Usage.InputTokens,
			&r.Usage.OutputTokens, &r.ConversationID)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, fmt.Errorf("sunday read %s: %w", day, err)
	}
	r.WrittenAt = parseTime(at)
	if r.Assessments, err = s.assessments(ctx, `a.read_id = ?`, -1, r.ID); err != nil {
		return r, false, fmt.Errorf("sunday read %s: %w", day, err)
	}
	slices.Reverse(r.Assessments) // one read's are all as at its day, the latest made first
	if r.Statements, err = s.readStatements(ctx, r.ID); err != nil {
		return r, false, fmt.Errorf("sunday read %s: %w", day, err)
	}
	if r.Gathered, err = s.gatherings(ctx, `g.read_id = ?`, r.ID); err != nil {
		return r, false, fmt.Errorf("sunday read %s: %w", day, err)
	}
	return r, true, nil
}

// EarlierSundayRead is the day's Sunday read of the earlier shape, over
// Stories of Facts, reporting whether one was written so: the text of its
// statements and of its Assessments, in order, as written.
func (s *Store) EarlierSundayRead(ctx context.Context, day string) (statements, assessments []string, found bool, err error) {
	if statements, err = s.textsOf(ctx, `SELECT st.text FROM read_statements st JOIN sunday_reads r ON r.id = st.read_id
		WHERE r.day = ? ORDER BY st.position`, day); err != nil {
		return nil, nil, false, fmt.Errorf("earlier sunday read %s: %w", day, err)
	}
	if assessments, err = s.textsOf(ctx, `SELECT a.statement FROM assessments a JOIN sunday_reads r ON r.id = a.read_id
		WHERE r.day = ? ORDER BY a.position`, day); err != nil {
		return nil, nil, false, fmt.Errorf("earlier sunday read %s: %w", day, err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM sunday_reads r WHERE r.day = ?
		AND NOT EXISTS (SELECT 1 FROM told_reads t WHERE t.day = r.day))`, day).Scan(&found); err != nil {
		return nil, nil, false, fmt.Errorf("earlier sunday read %s: %w", day, err)
	}
	return statements, assessments, found, nil
}

// textsOf runs a query of one text column with args.
func (s *Store) textsOf(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
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

// readTakenOutBy is, for the read's statement aliased st, the JSON array of
// the Feedback standing that takes it out of its read: wrong on an
// Assessment it cites.
var readTakenOutBy = `(SELECT json_group_array(id) FROM (
		SELECT DISTINCT w.event_id AS id FROM told_read_citations wc JOIN feedback_given w ON w.judgement_id = wc.assessment_id
		WHERE wc.statement_id = st.event_id AND w.kind = 'wrong' AND ` + standing("w.event_id") + `
		ORDER BY id))`

// readStatements are the read's statements in order, each with what it
// cites: a Span as its Item's newest reading reads it, an Assessment, or a
// lookup.
func (s *Store) readStatements(ctx context.Context, readID int64) ([]editor.ReadStatement, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT st.event_id, st.text, `+readTakenOutBy+`,
			coalesce(c.source_id, ''), coalesce(c.entry_id, ''), coalesce(c.span_start, 0), coalesce(c.span_end, 0),
			coalesce(c.reading_id, 0), coalesce(c.assessment_id, 0),
			coalesce(l.id, 0), coalesce(l.reference, ''), coalesce(l.item_id, ''), coalesce(l.revision, 0),
			coalesce(l.looked_up_at, ''), coalesce(l.licence, '')
		FROM told_read_statements st
			JOIN told_read_citations c ON c.statement_id = st.event_id
			LEFT JOIN reference_lookups l ON l.id = c.lookup_id
		WHERE st.read_id = ?
		ORDER BY st.position, c.position`, readID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []editor.ReadStatement
	for rows.Next() {
		var id, assessment int64
		var text, by, lookedUp string
		var c editor.Citation
		a := &c.Span.At
		l := &c.Lookup
		if err := rows.Scan(&id, &text, &by, &a.SourceID, &a.EntryID, &a.Span.Start, &a.Span.End, &c.Span.ReadingID, &assessment,
			&l.ID, &l.Reference, &l.ItemID, &l.Revision, &lookedUp, &l.Licence); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].ID != id {
			st := editor.ReadStatement{ID: id, Text: text}
			if err := json.Unmarshal([]byte(by), &st.TakenOutBy); err != nil {
				return nil, err
			}
			out = append(out, st)
		}
		switch {
		case a.SourceID != "":
			c.Kind = editor.CitesSpan
		case assessment != 0:
			c.Kind, c.Assessment.ID = editor.CitesAssessment, assessment
		default:
			c.Kind, l.At = editor.CitesLookup, parseTime(lookedUp)
		}
		out[len(out)-1].Cites = append(out[len(out)-1].Cites, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()
	for i := range out {
		for j, c := range out[i].Cites {
			switch c.Kind {
			case editor.CitesSpan:
				sp, err := s.SpanAt(ctx, c.Span.At)
				if err != nil {
					return nil, err
				}
				sp.At, sp.ReadingID = c.Span.At, c.Span.ReadingID
				out[i].Cites[j].Span = sp
			case editor.CitesAssessment:
				as, err := s.assessments(ctx, `a.event_id = ?`, -1, c.Assessment.ID)
				if err != nil {
					return nil, err
				}
				if len(as) == 1 {
					out[i].Cites[j].Assessment = as[0]
				}
			}
		}
	}
	return out, nil
}

// Assessments are at most limit Assessments, the latest first, each with
// what it rests on and what supersedes it, as the acts see them: none under
// wrong standing (system design: What Feedback took out is never shown).
func (s *Store) Assessments(ctx context.Context, limit int) ([]editor.Assessment, error) {
	out, err := s.assessments(ctx, `NOT `+wrongStanding("a.event_id"), limit)
	if err != nil {
		return nil, fmt.Errorf("assessments: %w", err)
	}
	return out, nil
}

// Assessment is one Assessment, reporting whether it is in the Record.
func (s *Store) Assessment(ctx context.Context, id int64) (editor.Assessment, bool, error) {
	as, err := s.assessments(ctx, `a.event_id = ?`, -1, id)
	if err != nil {
		return editor.Assessment{}, false, fmt.Errorf("assessment %d: %w", id, err)
	}
	if len(as) == 0 {
		return editor.Assessment{}, false, nil
	}
	return as[0], true, nil
}

// assessments are the Assessments matching where (SQL over told_assessments
// aliased a), the latest first (by as at, then the order they were made),
// at most limit (all, if negative), each with what it rests on.
func (s *Store) assessments(ctx context.Context, where string, limit int, args ...any) ([]editor.Assessment, error) {
	out, err := s.assessmentsOnly(ctx, where, limit, args...)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if err := s.groundsOf(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// assessmentsOnly are the Assessments matching where, without what they
// rest on.
func (s *Store) assessmentsOnly(ctx context.Context, where string, limit int, args ...any) ([]editor.Assessment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.event_id, a.statement, a.as_at, coalesce(a.supersedes_id, 0),
			coalesce((SELECT later.event_id FROM told_assessments later WHERE later.supersedes_id = a.event_id), 0), r.day,
			`+wrongStanding("a.event_id")+`
		FROM told_assessments a JOIN told_reads r ON r.event_id = a.read_id
		WHERE `+where+`
		ORDER BY a.as_at DESC, a.event_id DESC
		LIMIT `+fmt.Sprint(limit), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []editor.Assessment
	for rows.Next() {
		var a editor.Assessment
		var asAt string
		if err := rows.Scan(&a.ID, &a.Statement, &asAt, &a.Supersedes, &a.SupersededBy, &a.Day, &a.Withdrawn); err != nil {
			return nil, err
		}
		if a.AsAt, err = time.Parse(time.DateOnly, asAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// groundsOf fills in what a rests on: its Spans, as their Items' newest
// readings read them, and its earlier Assessments.
func (s *Store) groundsOf(ctx context.Context, a *editor.Assessment) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT coalesce(source_id, ''), coalesce(entry_id, ''), coalesce(span_start, 0), coalesce(span_end, 0),
			coalesce(reading_id, 0), coalesce(earlier_id, 0)
		FROM assessment_grounds WHERE assessment_id = ? ORDER BY position`, a.ID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var spans []editor.OtherSpan
	var earlier []int64
	for rows.Next() {
		var o editor.OtherSpan
		var e int64
		var start, end int
		if err := rows.Scan(&o.At.SourceID, &o.At.EntryID, &start, &end, &o.ReadingID, &e); err != nil {
			return err
		}
		o.At.Span = cut.Span{Start: start, End: end}
		if e != 0 {
			earlier = append(earlier, e)
			continue
		}
		spans = append(spans, o)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_ = rows.Close()
	for _, o := range spans {
		sp, err := s.SpanAt(ctx, o.At)
		if err != nil {
			return err
		}
		sp.At, sp.ReadingID = o.At, o.ReadingID
		a.Spans = append(a.Spans, sp)
	}
	for _, id := range earlier {
		as, err := s.assessmentsOnly(ctx, `a.event_id = ?`, -1, id)
		if err != nil {
			return err
		}
		a.Earlier = append(a.Earlier, as...)
	}
	return nil
}
