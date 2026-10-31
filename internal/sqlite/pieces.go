package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/feedback"
)

// The versions of the shapes of Feedback's events this build writes.
const (
	feedbackShape = 1
	undoingShape  = 1
)

// standing is whether a piece of Feedback, the event id the SQL expression
// piece is, is standing: not undone.
func standing(piece string) string {
	return `NOT EXISTS (SELECT 1 FROM feedback_undoings fu WHERE fu.feedback_id = ` + piece + `)`
}

// wrongStanding is whether wrong stands on the judgement the SQL expression
// judgement is: every view stops reading a judgement under it (system
// design: Feedback lands on a judgement).
func wrongStanding(judgement string) string {
	return `EXISTS (SELECT 1 FROM feedback_given w WHERE w.kind = 'wrong' AND w.judgement_id = ` + judgement + `
		AND ` + standing("w.event_id") + `)`
}

// JudgementKind is the kind of the event id, reporting whether the Record
// holds it.
func (s *Store) JudgementKind(ctx context.Context, id int64) (feedback.Judged, bool, error) {
	var kind string
	err := s.db.QueryRowContext(ctx, `SELECT kind FROM events WHERE id = ?`, id).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("judgement %d: %w", id, err)
	}
	return feedback.Judged(kind), true, nil
}

// SpanInItem reports whether sp lies within its Item's text as kept.
func (s *Store) SpanInItem(ctx context.Context, sp feedback.Span) (bool, error) {
	var in bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM items WHERE source_id = ? AND entry_id = ?
			AND length(CAST(content AS BLOB)) >= ?)`, sp.SourceID, sp.EntryID, sp.End).Scan(&in)
	if err != nil {
		return false, fmt.Errorf("span in item %s: %w", sp.EntryID, err)
	}
	return in, nil
}

// ToldIn reports whether the day's Briefing over Spans has a statement
// citing sp.
func (s *Store) ToldIn(ctx context.Context, day string, sp feedback.Span) (bool, error) {
	var told bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM told_briefings t
			JOIN told_statements st ON st.briefing_id = t.event_id
			JOIN told_citations c ON c.statement_id = st.event_id
			WHERE t.day = ? AND c.source_id = ? AND c.entry_id = ? AND c.span_start = ? AND c.span_end = ?)`,
		day, sp.SourceID, sp.EntryID, sp.Start, sp.End).Scan(&told)
	if err != nil {
		return false, fmt.Errorf("told in %s: %w", day, err)
	}
	return told, nil
}

// GivePiece keeps p as given, as an event, returning its id.
func (s *Store) GivePiece(ctx context.Context, p feedback.Piece) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `INSERT INTO events (kind, shape, at) VALUES ('feedback', ?, ?)`,
		feedbackShape, formatTime(p.GivenAt))
	if err != nil {
		return 0, fmt.Errorf("give feedback: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	var judgement, briefing, source, entry, start, end any
	if p.Judgement != 0 {
		judgement = p.Judgement
	}
	if p.Span.SourceID != "" {
		source, entry, start, end = p.Span.SourceID, p.Span.EntryID, p.Span.Start, p.Span.End
	}
	if p.Kind == feedback.Unwanted {
		if err := tx.QueryRowContext(ctx, `SELECT event_id FROM told_briefings WHERE day = ?`, p.Day).Scan(&briefing); err != nil {
			return 0, fmt.Errorf("give feedback: the Briefing for %s: %w", p.Day, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO feedback_given (event_id, kind, judgement_id, briefing_id, source_id, entry_id, span_start, span_end)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, string(p.Kind), judgement, briefing, source, entry, start, end); err != nil {
		return 0, fmt.Errorf("give feedback: %w", err)
	}
	return id, tx.Commit()
}

// UndoPiece keeps the undoing of the piece id, at at, as an event.
func (s *Store) UndoPiece(ctx context.Context, id int64, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `INSERT INTO events (kind, shape, at) VALUES ('undoing', ?, ?)`,
		undoingShape, formatTime(at))
	if err != nil {
		return fmt.Errorf("undo feedback %d: %w", id, err)
	}
	eid, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO feedback_undoings (event_id, feedback_id) VALUES (?, ?)`, eid, id); err != nil {
		return fmt.Errorf("undo feedback %d: %w", id, err)
	}
	return tx.Commit()
}

// Piece is the piece of Feedback id, reporting whether it is in the Record.
func (s *Store) Piece(ctx context.Context, id int64) (feedback.Piece, bool, error) {
	ps, err := s.pieces(ctx, `WHERE f.event_id = ?`, id)
	if err != nil || len(ps) == 0 {
		return feedback.Piece{}, false, err
	}
	return ps[0], true, nil
}

// KeptPieces are every piece of Feedback kept as an event, newest first,
// the undone with the rest, each with what it is about as the Record holds
// it and the calls behind that.
func (s *Store) KeptPieces(ctx context.Context) ([]feedback.KeptPiece, error) {
	ps, err := s.pieces(ctx, ``)
	if err != nil {
		return nil, err
	}
	texts := map[[2]string]string{}
	words := func(sp feedback.Span) (string, error) {
		k := [2]string{sp.SourceID, sp.EntryID}
		text, ok := texts[k]
		if !ok {
			it, _, err := s.itemAt(ctx, sp.SourceID, sp.EntryID)
			if err != nil {
				return "", err
			}
			text, texts[k] = it.Content, it.Content
		}
		if sp.End > len(text) || sp.Start >= sp.End {
			return "", nil
		}
		return cut.Words(text, cut.Span{Start: sp.Start, End: sp.End}), nil
	}
	out := make([]feedback.KeptPiece, 0, len(ps))
	for _, p := range ps {
		k, err := s.keptPiece(ctx, p)
		if err != nil {
			return nil, err
		}
		at, err := words(k.At)
		if err != nil {
			return nil, err
		}
		to, err := words(k.To)
		if err != nil {
			return nil, err
		}
		switch p.Judged {
		case feedback.RefersTo:
			k.What = "“" + at + "” refers to “" + to + "”"
		case feedback.Speaker:
			k.What = "“" + at + "” spoken by “" + to + "”"
		case feedback.Relation:
			k.What = "“" + at + "” " + k.What + " “" + to + "”"
		case feedback.Gathering:
			k.What = "the Story of “" + at + "”"
		case feedback.Lookup, feedback.Statement, feedback.Assessment, feedback.SundayRead:
		default:
			k.What = "“" + at + "”"
		}
		out = append(out, k)
	}
	return out, nil
}

// relationWords word each kind of relation as its from Span relates.
var relationWords = map[string]string{"follows": "follows", "same_as": "is the same as", "contradicts": "contradicts"}

// keptPiece is p with what it is about, from the Record: the Spans it lands
// on, a look-up's key, the words a statement, a look-up or a relation is
// named by, and the calls behind it.
func (s *Store) keptPiece(ctx context.Context, p feedback.Piece) (feedback.KeptPiece, error) {
	k := feedback.KeptPiece{Piece: p}
	at, to := &k.At, &k.To
	var readCall, relatedCall, writtenCall int64
	var relation, statement, entity, assessment string
	err := s.db.QueryRowContext(ctx, `
		SELECT
			coalesce(rr.source_id, sr.source_id, rel.from_source_id, g.source_id, f.source_id, ''),
			coalesce(rr.entry_id, sr.entry_id, rel.from_entry_id, g.entry_id, f.entry_id, ''),
			coalesce(rt.from_start, sk.span_start, rel.from_start, g.span_start, f.span_start, 0),
			coalesce(rt.from_end, sk.span_end, rel.from_end, g.span_end, f.span_end, 0),
			coalesce(rr.source_id, sr.source_id, rel.to_source_id, ''),
			coalesce(rr.entry_id, sr.entry_id, rel.to_entry_id, ''),
			coalesce(rt.to_start, sk.speaker_start, rel.to_start, 0),
			coalesce(rt.to_end, sk.speaker_end, rel.to_end, 0),
			coalesce(rel.kind, ''), coalesce(st.text, ''),
			coalesce(lk.name, ''), coalesce(lk.kind, ''), coalesce(lk.office, ''), coalesce(lk.on_date, ''),
			coalesce(CASE WHEN lk.wikidata_id != '' THEN lk.label ELSE lk.name END, ''),
			coalesce(gt.day, stt.day, ut.day, ''),
			coalesce((SELECT `+spansReadingCall("r")+` FROM readings r
				WHERE r.event_id = coalesce(rt.reading_id, sk.reading_id,
					CASE WHEN f.kind = 'missed' THEN `+newestLiveReading("f.source_id", "f.entry_id")+` END)), 0),
			coalesce((SELECT `+relatingCall("rl")+` FROM relatings rl WHERE rl.event_id = rel.relating_id), 0),
			coalesce((SELECT `+briefingCall("t")+` FROM told_briefings t
				WHERE t.event_id = coalesce(g.briefing_id, st.briefing_id, f.briefing_id)), 0),
			coalesce(ta.statement, ''), coalesce(tr.day, ''),
			coalesce((SELECT conversation_id FROM sunday_reads WHERE day = tr.day), 0)
		FROM feedback_given f
			LEFT JOIN refers_to rt ON rt.event_id = f.judgement_id
			LEFT JOIN readings rr ON rr.event_id = rt.reading_id
			LEFT JOIN speakers sk ON sk.event_id = f.judgement_id
			LEFT JOIN readings sr ON sr.event_id = sk.reading_id
			LEFT JOIN lookups lk ON lk.event_id = f.judgement_id
			LEFT JOIN relations rel ON rel.event_id = f.judgement_id
			LEFT JOIN gatherings g ON g.event_id = f.judgement_id
			LEFT JOIN told_briefings gt ON gt.event_id = g.briefing_id
			LEFT JOIN told_statements st ON st.event_id = f.judgement_id
			LEFT JOIN told_briefings stt ON stt.event_id = st.briefing_id
			LEFT JOIN told_briefings ut ON ut.event_id = f.briefing_id
			LEFT JOIN told_assessments ta ON ta.event_id = f.judgement_id
			LEFT JOIN told_reads tr ON tr.event_id = coalesce(ta.read_id, g.read_id, f.judgement_id)
		WHERE f.event_id = ?`, p.ID,
	).Scan(&at.SourceID, &at.EntryID, &at.Start, &at.End, &to.SourceID, &to.EntryID, &to.Start, &to.End,
		&relation, &statement, &k.Name, &k.NameKind, &k.Office, &k.On, &entity, &k.Day,
		&readCall, &relatedCall, &writtenCall, &assessment, &k.Read, &k.Conversation)
	if err != nil {
		return k, fmt.Errorf("feedback %d: %w", p.ID, err)
	}
	switch p.Judged {
	case feedback.Relation:
		k.What = relationWords[relation]
	case feedback.Statement:
		k.What = statement
	case feedback.Lookup:
		k.What = "“" + k.Name + "” is " + entity
	case feedback.Assessment:
		k.What = assessment
	case feedback.SundayRead:
		k.What = "the Sunday read"
	}
	for _, c := range []feedback.Call{{Act: "read", ID: readCall}, {Act: "related", ID: relatedCall}, {Act: "written", ID: writtenCall}} {
		if c.ID != 0 {
			k.Calls = append(k.Calls, c)
		}
	}
	return k, nil
}

// pieces are the pieces of Feedback where selects (SQL over feedback_given
// aliased f), newest first.
func (s *Store) pieces(ctx context.Context, where string, args ...any) ([]feedback.Piece, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.event_id, f.kind, coalesce(f.judgement_id, 0), coalesce(je.kind, ''),
			coalesce(f.source_id, ''), coalesce(f.entry_id, ''), coalesce(f.span_start, 0), coalesce(f.span_end, 0),
			coalesce(t.day, ''), e.at, coalesce(ue.at, '')
		FROM feedback_given f
			JOIN events e ON e.id = f.event_id
			LEFT JOIN events je ON je.id = f.judgement_id
			LEFT JOIN told_briefings t ON t.event_id = f.briefing_id
			LEFT JOIN feedback_undoings u ON u.feedback_id = f.event_id
			LEFT JOIN events ue ON ue.id = u.event_id
		`+where+`
		ORDER BY f.event_id DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("feedback given: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []feedback.Piece
	for rows.Next() {
		var p feedback.Piece
		var kind, judged, at, undone string
		if err := rows.Scan(&p.ID, &kind, &p.Judgement, &judged, &p.Span.SourceID, &p.Span.EntryID,
			&p.Span.Start, &p.Span.End, &p.Day, &at, &undone); err != nil {
			return nil, fmt.Errorf("feedback given: %w", err)
		}
		p.Kind, p.Judged, p.GivenAt = feedback.Kind(kind), feedback.Judged(judged), parseTime(at)
		if undone != "" {
			p.UndoneAt = parseTime(undone)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
