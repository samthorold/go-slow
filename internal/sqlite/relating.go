package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// The versions of the shapes of a relating's events this build writes.
const (
	relatingShape = 1
	relationShape = 1
)

// firstRead is the event id of the first live reading of the Item aliased
// items, or NULL: the order Items were read in, which decides which of two
// Items judges the pair of their Spans (system design: Relating a Span
// judges once per pair, when the second of the two is read). A candidate's
// reading, read by no view, reads nothing in. Re-judging an Item makes a
// newer reading, never an earlier first one, so it moves no Item's turn.
const firstRead = `(SELECT min(fr.event_id) FROM readings fr
	LEFT JOIN candidate_readings fcr ON fcr.reading_id = fr.event_id
	LEFT JOIN candidate_choices fch ON fch.candidate_id = fcr.candidate_id
	WHERE fr.source_id = items.source_id AND fr.entry_id = items.entry_id
		AND (fcr.reading_id IS NULL OR fch.event_id IS NOT NULL))`

// newestReadingOf is the event id of the newest live reading of the Item
// aliased items: the one a view reads (newestLiveReading).
var newestReadingOf = newestLiveReading("items.source_id", "items.entry_id")

// readItem is an Item with a reading, as relating reads it.
type readItem struct {
	item      editor.Item
	readOrder int64
}

// readItems are the Items with a reading that match where (SQL over items),
// in the order they were first read.
func (s *Store) readItems(ctx context.Context, where string, args ...any) ([]readItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT items.source_id, items.entry_id, items.title, items.link, items.summary, items.content,
			items.published, `+firstRead+` AS read_order
		FROM items
		WHERE `+firstRead+` IS NOT NULL AND (`+where+`)
		ORDER BY read_order`, args...)
	if err != nil {
		return nil, fmt.Errorf("read items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []readItem
	for rows.Next() {
		var r readItem
		var published string
		if err := rows.Scan(&r.item.SourceID, &r.item.EntryID, &r.item.Title, &r.item.Link, &r.item.Summary,
			&r.item.Content, &published, &r.readOrder); err != nil {
			return nil, fmt.Errorf("read items: %w", err)
		}
		r.item.Published = parseTime(published)
		out = append(out, r)
	}
	return out, rows.Err()
}

// spansRead is an Item's newest reading, the Spans of the Cut it was read
// as that it did not reject, and what the Record's look-ups have each of
// its names as.
type spansRead struct {
	reading editor.Reading
	spans   []cut.Span
	numbers []int
	is      map[editor.LookupKey]editor.NamedEntity
}

// readSpans is the Item's newest reading and its Spans not rejected.
func (s *Store) readSpans(ctx context.Context, it editor.Item) (spansRead, error) {
	r, _, err := s.NewestReading(ctx, it.SourceID, it.EntryID)
	if err != nil {
		return spansRead{}, err
	}
	all, err := s.cutSpans(ctx, r.CutID)
	if err != nil {
		return spansRead{}, err
	}
	is, err := s.EntitiesNamed(ctx, r.Keys(it.Published))
	if err != nil {
		return spansRead{}, err
	}
	out := spansRead{reading: r, is: is}
	for i, sp := range all {
		if slices.ContainsFunc(r.Rejected, func(a editor.Address) bool { return a.Span == sp }) {
			continue
		}
		out.spans = append(out.spans, sp)
		out.numbers = append(out.numbers, i+1)
	}
	return out, nil
}

// cutSpans are the Spans the Cut lists, in order.
func (s *Store) cutSpans(ctx context.Context, cutID int64) ([]cut.Span, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT span_start, span_end FROM cut_spans WHERE cut_id = ? ORDER BY n`, cutID)
	if err != nil {
		return nil, fmt.Errorf("spans of cut %d: %w", cutID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []cut.Span
	for rows.Next() {
		var sp cut.Span
		if err := rows.Scan(&sp.Start, &sp.End); err != nil {
			return nil, fmt.Errorf("spans of cut %d: %w", cutID, err)
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// ItemsToRelate are the Items awaiting relating, in the order they were
// first read: every Item read and not yet related whose newest reading's
// names have all been looked up, since the Entities they are found to be
// are what finds its candidates. Each comes with its newest reading and the
// Spans of the Cut it was read as that it did not reject, each with the
// Entities it mentions; its candidates are the Editor's to choose. Work
// marked failed awaits nothing.
func (s *Store) ItemsToRelate(ctx context.Context) ([]editor.ToRelate, error) {
	items, err := s.readItems(ctx, `
		NOT EXISTS (SELECT 1 FROM relatings rl WHERE rl.source_id = items.source_id AND rl.entry_id = items.entry_id)
		AND NOT EXISTS (SELECT 1 FROM span_names n
			WHERE n.reading_id = `+newestReadingOf+`
				AND NOT EXISTS (SELECT 1 FROM lookups l
					WHERE l.name = n.name AND l.kind = n.kind AND l.office = n.office AND l.on_date = `+lookupOn+`))
		AND `+notFailed("relating"))
	if err != nil {
		return nil, fmt.Errorf("items to relate: %w", err)
	}
	out := make([]editor.ToRelate, 0, len(items))
	for _, ri := range items {
		sr, err := s.readSpans(ctx, ri.item)
		if err != nil {
			return nil, fmt.Errorf("items to relate: %w", err)
		}
		tr := editor.ToRelate{Item: ri.item, Reading: sr.reading, ReadOrder: ri.readOrder}
		for i, sp := range sr.spans {
			tr.Spans = append(tr.Spans, editor.SpanToRelate{N: sr.numbers[i], Span: sp,
				Entities: sr.reading.Entities(sp, ri.item.Published, sr.is)})
		}
		out = append(out, tr)
	}
	return out, nil
}

// namesRead are the keys of every name the newest readings of the Items
// first read op (SQL "<" or "<=") readBefore judge, each once.
func (s *Store) namesRead(ctx context.Context, op string, readBefore int64) ([]editor.LookupKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT n.name, n.kind, n.office, `+lookupOn+`
		FROM items JOIN span_names n ON n.reading_id = `+newestReadingOf+`
		WHERE `+firstRead+` `+op+` ?`, readBefore)
	if err != nil {
		return nil, fmt.Errorf("names read: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var keys []editor.LookupKey
	for rows.Next() {
		var k editor.LookupKey
		if err := rows.Scan(&k.Name, &k.Kind, &k.Office, &k.On); err != nil {
			return nil, fmt.Errorf("names read: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// itemsNaming are the Items first read at or before readBefore (before it,
// if strictly) whose newest reading names one of the Entities es, as the
// Record's look-ups have each name now.
func (s *Store) itemsNaming(ctx context.Context, es []editor.NamedEntity, readBefore int64, strictly bool) ([]readItem, error) {
	if len(es) == 0 {
		return nil, nil
	}
	op := "<="
	if strictly {
		op = "<"
	}
	keys, err := s.namesRead(ctx, op, readBefore)
	if err != nil {
		return nil, err
	}
	// What each is now.
	is, err := s.EntitiesNamed(ctx, keys)
	if err != nil {
		return nil, err
	}
	var where []string
	var args []any
	for k, e := range is {
		if slices.Contains(es, e) {
			where = append(where, `(n.name = ? AND n.kind = ? AND n.office = ? AND `+lookupOn+` = ?)`)
			args = append(args, k.Name, k.Kind, k.Office, k.On)
		}
	}
	if len(where) == 0 {
		return nil, nil
	}
	cond := `EXISTS (SELECT 1 FROM span_names n WHERE n.reading_id = ` + newestReadingOf + ` AND (`
	for i, w := range where {
		if i > 0 {
			cond += " OR "
		}
		cond += w
	}
	cond += `)) AND ` + firstRead + ` ` + op + ` ?`
	return s.readItems(ctx, cond, append(args, readBefore)...)
}

// SpansMentioning are the Spans of Items first read before readBefore that
// mention one of the Entities es, by their Items' newest readings, and that
// those readings did not reject, in the order their Items were read: the
// Spans relating finds a Span's candidates among. Each comes with its Item,
// its hydrated reading, whose words it is and every Entity it mentions.
func (s *Store) SpansMentioning(ctx context.Context, es []editor.NamedEntity, readBefore int64) ([]editor.OtherSpan, error) {
	items, err := s.itemsNaming(ctx, es, readBefore, true)
	if err != nil {
		return nil, fmt.Errorf("spans mentioning: %w", err)
	}
	var out []editor.OtherSpan
	for _, ri := range items {
		sr, err := s.readSpans(ctx, ri.item)
		if err != nil {
			return nil, fmt.Errorf("spans mentioning: %w", err)
		}
		for i, sp := range sr.spans {
			mentions := sr.reading.Entities(sp, ri.item.Published, sr.is)
			if !slices.ContainsFunc(mentions, func(e editor.NamedEntity) bool { return slices.Contains(es, e) }) {
				continue
			}
			out = append(out, otherSpan(ri.item, sr.reading, sp, sr.numbers[i], mentions))
		}
	}
	return out, nil
}

// otherSpan is the Span sp, numbered n, of it as r reads it.
func otherSpan(it editor.Item, r editor.Reading, sp cut.Span, n int, mentions []editor.NamedEntity) editor.OtherSpan {
	return editor.OtherSpan{
		At: editor.Address{SourceID: it.SourceID, EntryID: it.EntryID, Span: sp}, N: n, Item: it,
		Words: r.HydratedWords(it.Content, sp), Speaker: r.SpeakerOf(it.Content, sp), Entities: mentions,
		ReadingID: r.ID,
	}
}

// ItemsMentioning is how many Items first read at or before readBefore
// mention each of the Entities es, by their newest readings: how rarely each
// is mentioned, as at the Item read then.
func (s *Store) ItemsMentioning(ctx context.Context, es []editor.NamedEntity, readBefore int64) (map[editor.NamedEntity]int, error) {
	out := map[editor.NamedEntity]int{}
	for _, e := range es {
		items, err := s.itemsNaming(ctx, []editor.NamedEntity{e}, readBefore, false)
		if err != nil {
			return nil, fmt.Errorf("items mentioning: %w", err)
		}
		out[e] = len(items)
	}
	return out, nil
}

// KeepRelating keeps a relating as an event, with its Judge, and each of
// its relations as an event of its own, tied to it, all at once. A relation
// between two Spans of one Item, or with neither Span of the relating's
// Item, is refused, and nothing is kept.
func (s *Store) KeepRelating(ctx context.Context, r editor.Relating) error {
	for _, rel := range r.Relations {
		own := func(a editor.Address) bool { return a.SourceID == r.SourceID && a.EntryID == r.EntryID }
		switch {
		case rel.From.SourceID == rel.To.SourceID && rel.From.EntryID == rel.To.EntryID:
			return fmt.Errorf("keep relating of %s: a relation between two Spans of one Item", r.EntryID)
		case !own(rel.From) && !own(rel.To):
			return fmt.Errorf("keep relating of %s: a relation with no Span of its Item", r.EntryID)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	at := formatTime(r.At)
	event := func(kind string, shape int) (int64, error) {
		res, err := tx.ExecContext(ctx, `INSERT INTO events (kind, shape, at) VALUES (?, ?, ?)`, kind, shape, at)
		if err != nil {
			return 0, fmt.Errorf("keep relating of %s: %s: %w", r.EntryID, kind, err)
		}
		return res.LastInsertId()
	}
	id, err := event("relating", relatingShape)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO relatings (event_id, source_id, entry_id, reading_id, act, version, model, request_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, r.SourceID, r.EntryID, r.ReadingID, r.Judge.Act, r.Judge.Version, r.Judge.Model, r.Judge.RequestHash); err != nil {
		return fmt.Errorf("keep relating of %s: %w", r.EntryID, err)
	}
	if err := keepShown(ctx, tx, id, r.Precedents); err != nil {
		return fmt.Errorf("keep relating of %s: %w", r.EntryID, err)
	}
	for _, rel := range r.Relations {
		eid, err := event("relation", relationShape)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO relations (event_id, relating_id, kind, from_source_id, from_entry_id, from_start, from_end,
				to_source_id, to_entry_id, to_start, to_end)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			eid, id, rel.Kind, rel.From.SourceID, rel.From.EntryID, rel.From.Span.Start, rel.From.Span.End,
			rel.To.SourceID, rel.To.EntryID, rel.To.Span.Start, rel.To.Span.End); err != nil {
			return fmt.Errorf("keep relating of %s: relation: %w", r.EntryID, err)
		}
	}
	return tx.Commit()
}

// relatingCall is the id of the applied relating call that made the
// relating aliased r, or 0: the one on its Item answering its request.
func relatingCall(r string) string {
	return `coalesce((SELECT max(c.id) FROM model_calls c
		WHERE c.act = 'relating' AND c.outcome = 'applied' AND c.request_hash = ` + r + `.request_hash
			AND c.work_key = ` + r + `.source_id || char(10) || ` + r + `.entry_id), 0)`
}

// RelationsOf are the relations judged of the Span at a, either way, each
// with the Span at its other end as its Item's newest reading reads it, and
// the relating that judged it, with its Judge and call, in the order they
// were judged.
func (s *Store) RelationsOf(ctx context.Context, a editor.Address) ([]editor.SpanRelation, error) {
	out, err := s.relationsAt(ctx, a)
	if err != nil {
		return nil, err
	}
	for i, sr := range out {
		o := sr.Relation.To
		if o == a {
			o = sr.Relation.From
		}
		other, err := s.SpanAt(ctx, o)
		if err != nil {
			return nil, err
		}
		out[i].Other = other
	}
	return out, nil
}

// relationsAt are the relations judged of the Span at a, either way, with
// their relatings, in the order they were judged.
func (s *Store) relationsAt(ctx context.Context, a editor.Address) ([]editor.SpanRelation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT rel.event_id, rel.kind, rel.from_source_id, rel.from_entry_id, rel.from_start, rel.from_end,
			rel.to_source_id, rel.to_entry_id, rel.to_start, rel.to_end,
			rl.event_id, rl.act, rl.version, rl.model, rl.request_hash, `+relatingCall("rl")+`
		FROM relations rel JOIN relatings rl ON rl.event_id = rel.relating_id
		WHERE ((rel.from_source_id = ? AND rel.from_entry_id = ? AND rel.from_start = ? AND rel.from_end = ?)
			OR (rel.to_source_id = ? AND rel.to_entry_id = ? AND rel.to_start = ? AND rel.to_end = ?))
			AND NOT `+wrongStanding("rel.event_id")+`
		ORDER BY rel.event_id`,
		a.SourceID, a.EntryID, a.Span.Start, a.Span.End, a.SourceID, a.EntryID, a.Span.Start, a.Span.End)
	if err != nil {
		return nil, fmt.Errorf("relations of a Span: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.SpanRelation
	for rows.Next() {
		var sr editor.SpanRelation
		f, t := &sr.Relation.From, &sr.Relation.To
		if err := rows.Scan(&sr.ID, &sr.Relation.Kind, &f.SourceID, &f.EntryID, &f.Span.Start, &f.Span.End,
			&t.SourceID, &t.EntryID, &t.Span.Start, &t.Span.End,
			&sr.RelatingID, &sr.Judge.Act, &sr.Judge.Version, &sr.Judge.Model, &sr.Judge.RequestHash, &sr.Call); err != nil {
			return nil, fmt.Errorf("relations of a Span: %w", err)
		}
		out = append(out, sr)
	}
	return out, rows.Err()
}

// SpanAt is the Span at a as its Item's newest reading reads it: its words,
// whose words it is and what it mentions, with its Item. A Span its Item has
// no reading of reads as its words; one at no address of the Item's Cuts
// has no number.
func (s *Store) SpanAt(ctx context.Context, a editor.Address) (editor.OtherSpan, error) {
	it, found, err := s.itemAt(ctx, a.SourceID, a.EntryID)
	if err != nil || !found {
		return editor.OtherSpan{}, err
	}
	r, read, err := s.NewestReading(ctx, it.SourceID, it.EntryID)
	if err != nil {
		return editor.OtherSpan{}, err
	}
	var mentions []editor.NamedEntity
	n := 0
	if read {
		is, err := s.EntitiesNamed(ctx, r.Keys(it.Published))
		if err != nil {
			return editor.OtherSpan{}, err
		}
		mentions = r.Entities(a.Span, it.Published, is)
		spans, err := s.cutSpans(ctx, r.CutID)
		if err != nil {
			return editor.OtherSpan{}, err
		}
		n = slices.Index(spans, a.Span) + 1
	}
	return otherSpan(it, r, a.Span, n, mentions), nil
}

// itemAt is the Item with the Source and entry, reporting whether it is in
// the Record.
func (s *Store) itemAt(ctx context.Context, sourceID, entryID string) (editor.Item, bool, error) {
	var it editor.Item
	var published string
	err := s.db.QueryRowContext(ctx, `
		SELECT source_id, entry_id, title, link, summary, content, published FROM items
		WHERE source_id = ? AND entry_id = ?`, sourceID, entryID,
	).Scan(&it.SourceID, &it.EntryID, &it.Title, &it.Link, &it.Summary, &it.Content, &published)
	if errors.Is(err, sql.ErrNoRows) {
		return editor.Item{}, false, nil
	}
	if err != nil {
		return editor.Item{}, false, fmt.Errorf("item %s: %w", entryID, err)
	}
	it.Published = parseTime(published)
	return it, true, nil
}
