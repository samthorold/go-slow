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

// The versions of the shapes of a Briefing's events this build writes.
const (
	briefingShape  = 1
	statementShape = 1
	gatheringShape = 1
)

// SpansToTell are the Items first cut after after and up to upTo, in the
// order they were published, each with the Spans the Briefing is shown: of
// an Item read, those of the Cut its newest reading was shown that it did
// not reject, as it reads them; of an Item only cut, every Span of its
// newest Cut, as written. Each Span comes with the relations judged of it,
// either way, so far (system design: The Briefing doesn't wait for
// construction).
func (s *Store) SpansToTell(ctx context.Context, after, upTo time.Time) ([]editor.ItemToTell, error) {
	keys, err := s.itemsCutIn(ctx, after, upTo)
	if err != nil {
		return nil, fmt.Errorf("spans to tell: %w", err)
	}
	out := make([]editor.ItemToTell, 0, len(keys))
	for _, k := range keys {
		tt, err := s.itemToTell(ctx, k[0], k[1])
		if err != nil {
			return nil, fmt.Errorf("spans to tell: %w", err)
		}
		out = append(out, tt)
	}
	return out, nil
}

// itemsCutIn are the Source and entry of each Item first cut after after and
// up to upTo, in the order they were published.
func (s *Store) itemsCutIn(ctx context.Context, after, upTo time.Time) ([][2]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT source_id, entry_id FROM (
			SELECT items.source_id, items.entry_id, items.published,
				(SELECT min(e.at) FROM cuts c JOIN events e ON e.id = c.event_id
					WHERE c.source_id = items.source_id AND c.entry_id = items.entry_id) AS cut_at
			FROM items)
		WHERE cut_at > ? AND cut_at <= ?
		ORDER BY published, source_id, entry_id`, formatTime(after), formatTime(upTo))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var keys [][2]string
	for rows.Next() {
		var k [2]string
		if err := rows.Scan(&k[0], &k[1]); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// itemToTell is the Item with its Spans to tell, and their relations.
func (s *Store) itemToTell(ctx context.Context, sourceID, entryID string) (editor.ItemToTell, error) {
	it, _, err := s.itemAt(ctx, sourceID, entryID)
	if err != nil {
		return editor.ItemToTell{}, err
	}
	r, read, err := s.NewestReading(ctx, sourceID, entryID)
	if err != nil {
		return editor.ItemToTell{}, err
	}
	var spans []cut.Span
	if read {
		if spans, err = s.cutSpans(ctx, r.CutID); err != nil {
			return editor.ItemToTell{}, err
		}
	} else {
		c, _, _, err := s.NewestCut(ctx, sourceID, entryID)
		if err != nil {
			return editor.ItemToTell{}, err
		}
		spans = c.Spans
	}
	tt := editor.ItemToTell{Item: it, Read: read}
	for i, sp := range spans {
		if slices.ContainsFunc(r.Rejected, func(a editor.Address) bool { return a.Span == sp }) {
			continue
		}
		o := otherSpan(it, r, sp, i+1, nil)
		rels, err := s.RelationsOf(ctx, o.At)
		if err != nil {
			return editor.ItemToTell{}, err
		}
		tt.Spans = append(tt.Spans, editor.SpanToTell{OtherSpan: o, Relations: rels})
	}
	return tt, nil
}

// KeepBriefing keeps a Briefing for its day as an event, with its Judge,
// and each statement, with the Spans it cites, and each Story it gathered as
// an event of its own, tied to it, all at once.
func (s *Store) KeepBriefing(ctx context.Context, b editor.Briefing) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	at := formatTime(b.WrittenAt)
	fail := func(what string, err error) error { return fmt.Errorf("keep briefing %s: %s: %w", b.Day, what, err) }
	event := func(kind string, shape int) (int64, error) {
		res, err := tx.ExecContext(ctx, `INSERT INTO events (kind, shape, at) VALUES (?, ?, ?)`, kind, shape, at)
		if err != nil {
			return 0, fail(kind, err)
		}
		return res.LastInsertId()
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO briefings (day, written_at, model) VALUES (?, ?, ?)`,
		b.Day, at, b.Judge.Model); err != nil {
		return fail("day", err)
	}
	id, err := event("briefing", briefingShape)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO told_briefings (event_id, day, act, version, model, request_hash) VALUES (?, ?, ?, ?, ?, ?)`,
		id, b.Day, b.Judge.Act, b.Judge.Version, b.Judge.Model, b.Judge.RequestHash); err != nil {
		return fail("briefing", err)
	}
	for i, st := range b.Statements {
		sid, err := event("statement", statementShape)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO told_statements (event_id, briefing_id, position, text) VALUES (?, ?, ?, ?)`,
			sid, id, i, st.Text); err != nil {
			return fail("statement", err)
		}
		for j, c := range st.Cites {
			var reading any
			if c.ReadingID != 0 {
				reading = c.ReadingID
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO told_citations (statement_id, position, source_id, entry_id, span_start, span_end, reading_id)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				sid, j, c.At.SourceID, c.At.EntryID, c.At.Span.Start, c.At.Span.End, reading); err != nil {
				return fail("citation", err)
			}
		}
	}
	for _, g := range b.Gathered {
		gid, err := event("gathering", gatheringShape)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO gatherings (event_id, briefing_id, source_id, entry_id, span_start, span_end) VALUES (?, ?, ?, ?, ?, ?)`,
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

// briefingCall is the id of the applied call that wrote the Briefing
// aliased t, or 0: the one on its day answering its request.
func briefingCall(t string) string {
	return `coalesce((SELECT max(c.id) FROM model_calls c
		WHERE c.act = 'briefing' AND c.outcome = 'applied' AND c.request_hash = ` + t + `.request_hash
			AND c.work_key = ` + t + `.day), 0)`
}

// Briefing is the day's Briefing over Spans, reporting whether one was
// written in that shape: its Judge and the call that wrote it, its
// statements in order, each with the Spans it cites as their Items' newest
// readings read them, and the Stories it gathered, as it was written.
func (s *Store) Briefing(ctx context.Context, day string) (editor.Briefing, bool, error) {
	b := editor.Briefing{Day: day}
	var at string
	err := s.db.QueryRowContext(ctx, `
		SELECT t.event_id, b.written_at, t.act, t.version, t.model, t.request_hash, `+briefingCall("t")+`
		FROM told_briefings t JOIN briefings b USING (day) WHERE t.day = ?`, day,
	).Scan(&b.ID, &at, &b.Judge.Act, &b.Judge.Version, &b.Judge.Model, &b.Judge.RequestHash, &b.Call)
	if errors.Is(err, sql.ErrNoRows) {
		return b, false, nil
	}
	if err != nil {
		return b, false, fmt.Errorf("briefing %s: %w", day, err)
	}
	b.WrittenAt = parseTime(at)
	if b.Statements, err = s.toldStatements(ctx, b.ID); err != nil {
		return b, false, fmt.Errorf("briefing %s: %w", day, err)
	}
	if b.Gathered, err = s.gatherings(ctx, `g.briefing_id = ?`, b.ID); err != nil {
		return b, false, fmt.Errorf("briefing %s: %w", day, err)
	}
	return b, true, nil
}

// toldStatements are the Briefing's statements in order, each with the
// Spans it cites.
func (s *Store) toldStatements(ctx context.Context, briefingID int64) ([]editor.Statement, error) {
	out, err := s.statementsCiting(ctx, briefingID)
	if err != nil {
		return nil, err
	}
	for i, st := range out {
		for j, c := range st.Cites {
			o, err := s.SpanAt(ctx, c.At)
			if err != nil {
				return nil, err
			}
			o.At, o.ReadingID = c.At, c.ReadingID
			out[i].Cites[j] = o
		}
	}
	return out, nil
}

// statementsCiting are the Briefing's statements in order, each citing its
// Spans by address and the reading each was shown as, and nothing more.
func (s *Store) statementsCiting(ctx context.Context, briefingID int64) ([]editor.Statement, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT st.event_id, st.text, c.source_id, c.entry_id, c.span_start, c.span_end, coalesce(c.reading_id, 0)
		FROM told_statements st JOIN told_citations c ON c.statement_id = st.event_id
		WHERE st.briefing_id = ?
		ORDER BY st.position, c.position`, briefingID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []editor.Statement
	for rows.Next() {
		var id int64
		var text string
		var c editor.OtherSpan
		a := &c.At
		if err := rows.Scan(&id, &text, &a.SourceID, &a.EntryID, &a.Span.Start, &a.Span.End, &c.ReadingID); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].ID != id {
			out = append(out, editor.Statement{ID: id, Text: text})
		}
		out[len(out)-1].Cites = append(out[len(out)-1].Cites, c)
	}
	return out, rows.Err()
}

// LatestGathering is the latest Story gathered from the Span at a, reporting
// whether one was: with the Briefing that gathered it, its Judge and call,
// and the Spans it holds as their Items' newest readings read them.
func (s *Store) LatestGathering(ctx context.Context, a editor.Address) (editor.GatheredStory, bool, error) {
	gs, err := s.gatherings(ctx, `g.event_id = (SELECT max(event_id) FROM gatherings
		WHERE source_id = ? AND entry_id = ? AND span_start = ? AND span_end = ?)`,
		a.SourceID, a.EntryID, a.Span.Start, a.Span.End)
	if err != nil || len(gs) == 0 {
		return editor.GatheredStory{}, false, err
	}
	g := editor.GatheredStory{Gathering: gs[0]}
	var at string
	if err := s.db.QueryRowContext(ctx, `
		SELECT t.day, e.at, t.act, t.version, t.model, t.request_hash, `+briefingCall("t")+`
		FROM gatherings g JOIN told_briefings t ON t.event_id = g.briefing_id JOIN events e ON e.id = g.event_id
		WHERE g.event_id = ?`, g.ID,
	).Scan(&g.Day, &at, &g.Judge.Act, &g.Judge.Version, &g.Judge.Model, &g.Judge.RequestHash, &g.Call); err != nil {
		return g, false, fmt.Errorf("latest gathering: %w", err)
	}
	g.At = parseTime(at)
	for _, o := range g.Story {
		sp, err := s.SpanAt(ctx, o)
		if err != nil {
			return g, false, err
		}
		g.Spans = append(g.Spans, sp)
	}
	return g, true, nil
}

// BriefingTrail is each Span the day's Briefing over Spans cites, once, in
// the order it first cites them, as it was written, with the calls behind
// it: the reading call that made the reading it was shown as, and the
// relating calls on its Item made before the Briefing.
func (s *Store) BriefingTrail(ctx context.Context, day string) ([]editor.CitedSpan, error) {
	out, err := s.citedSpans(ctx, day)
	if err != nil {
		return nil, fmt.Errorf("briefing trail %s: %w", day, err)
	}
	for i, c := range out {
		sp, err := s.SpanAt(ctx, c.Span.At)
		if err != nil {
			return nil, err
		}
		sp.ReadingID = c.Span.ReadingID
		out[i].Span = sp
	}
	return out, nil
}

// citedSpans are the Spans the day's Briefing cites, each once, by address
// and the reading it was shown as, with the calls behind it.
func (s *Store) citedSpans(ctx context.Context, day string) ([]editor.CitedSpan, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.source_id, c.entry_id, c.span_start, c.span_end, coalesce(c.reading_id, 0),
			coalesce((SELECT `+spansReadingCall("r")+` FROM readings r WHERE r.event_id = c.reading_id), 0),
			coalesce((SELECT json_group_array(call) FROM (SELECT `+relatingCall("rl")+` AS call FROM relatings rl
				WHERE rl.source_id = c.source_id AND rl.entry_id = c.entry_id AND rl.event_id < t.event_id
				ORDER BY rl.event_id) WHERE call != 0), '[]')
		FROM told_briefings t
			JOIN told_statements st ON st.briefing_id = t.event_id
			JOIN told_citations c ON c.statement_id = st.event_id
		WHERE t.day = ?
		ORDER BY st.position, c.position`, day)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []editor.CitedSpan
	for rows.Next() {
		var c editor.CitedSpan
		var relatings string
		a := &c.Span.At
		if err := rows.Scan(&a.SourceID, &a.EntryID, &a.Span.Start, &a.Span.End, &c.Span.ReadingID,
			&c.ReadingCall, &relatings); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(relatings), &c.RelatingCalls); err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(out, func(o editor.CitedSpan) bool { return o.Span.At == c.Span.At }) {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

// gatherings are the Stories gathered that match where (SQL over the
// gatherings aliased g), in the order they were gathered.
func (s *Store) gatherings(ctx context.Context, where string, args ...any) ([]editor.Gathering, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.event_id, g.source_id, g.entry_id, g.span_start, g.span_end,
			gs.source_id, gs.entry_id, gs.span_start, gs.span_end
		FROM gatherings g LEFT JOIN gathered_spans gs ON gs.gathering_id = g.event_id
		WHERE `+where+`
		ORDER BY g.event_id, gs.position`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []editor.Gathering
	for rows.Next() {
		var id int64
		var from editor.Address
		var src, entry sql.NullString
		var start, end sql.NullInt64
		if err := rows.Scan(&id, &from.SourceID, &from.EntryID, &from.Span.Start, &from.Span.End,
			&src, &entry, &start, &end); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].ID != id {
			out = append(out, editor.Gathering{ID: id, From: from})
		}
		if src.Valid {
			g := &out[len(out)-1]
			g.Story = append(g.Story, editor.Address{SourceID: src.String, EntryID: entry.String,
				Span: cut.Span{Start: int(start.Int64), End: int(end.Int64)}})
		}
	}
	return out, rows.Err()
}
