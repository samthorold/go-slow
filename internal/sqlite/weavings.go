package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// The versions of the shapes of a weaving run's events this build writes.
const (
	weavingShape = 1
	storyShape   = 1
)

// StoriesToWeave are the Stories cut since the last weaving run whose result
// was written (every Cut after its through_cut), of Items collected after the
// switch, oldest Cut first, each with its Item and its Spans.
func (s *Store) StoriesToWeave(ctx context.Context) ([]editor.CutStory, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT cuts.event_id, items.source_id, items.entry_id, items.title, items.link, items.summary,
			items.content, items.published
		FROM cuts JOIN items USING (source_id, entry_id)
		WHERE cuts.event_id > coalesce((SELECT max(through_cut) FROM weavings), 0)
			AND `+afterSwitch+`
		ORDER BY cuts.event_id`)
	if err != nil {
		return nil, fmt.Errorf("stories to weave: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.CutStory
	for rows.Next() {
		var c editor.CutStory
		var published string
		if err := rows.Scan(&c.CutID, &c.Item.SourceID, &c.Item.EntryID, &c.Item.Title, &c.Item.Link,
			&c.Item.Summary, &c.Item.Content, &published); err != nil {
			return nil, fmt.Errorf("stories to weave: %w", err)
		}
		c.Item.Published = parseTime(published)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stories to weave: %w", err)
	}
	_ = rows.Close()
	for i := range out {
		if out[i].Spans, err = s.cutSpans(ctx, out[i].CutID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// WeavingRan reports whether the weaving run keyed key has had its result
// written or been marked failed.
func (s *Store) WeavingRan(ctx context.Context, key string) (bool, error) {
	var ran bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM weavings WHERE work_key = ?)
			OR EXISTS (SELECT 1 FROM work_attempts WHERE act = 'weaving' AND work_key = ? AND failed = 1)`,
		key, key).Scan(&ran)
	if err != nil {
		return false, fmt.Errorf("weaving %s ran: %w", key, err)
	}
	return ran, nil
}

// KeepWeaving keeps a weaving run as an event, with its Judge, and each Story
// it made as an event of its own, tied to it, with its Editor's Spans and
// what they cite, all at once.
func (s *Store) KeepWeaving(ctx context.Context, w editor.Weaving) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	id, err := addEvent(ctx, tx, "weaving", weavingShape, w.At)
	if err != nil {
		return fmt.Errorf("keep weaving: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO weavings (event_id, work_key, through_cut, act, version, model, request_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, w.Key, w.ThroughCut, w.Judge.Act, w.Judge.Version, w.Judge.Model, w.Judge.RequestHash); err != nil {
		return fmt.Errorf("keep weaving: %w", err)
	}
	for _, st := range w.Stories {
		sid, err := addEvent(ctx, tx, "story", storyShape, w.At)
		if err != nil {
			return fmt.Errorf("keep weaving: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO woven_stories (event_id, weaving_id) VALUES (?, ?)`, sid, id); err != nil {
			return fmt.Errorf("keep weaving: %w", err)
		}
		for n, es := range st.Spans {
			if _, err := tx.ExecContext(ctx, `INSERT INTO editor_spans (story_id, n, body) VALUES (?, ?, ?)`,
				sid, n+1, es.Body); err != nil {
				return fmt.Errorf("keep weaving: %w", err)
			}
			for m, c := range es.Cites {
				if !c.Of.IsZero() {
					if _, err := tx.ExecContext(ctx, `
						INSERT INTO editor_span_cites_editor (story_id, n, m, cited_story_id, cited_n)
						VALUES (?, ?, ?, ?, ?)`,
						sid, n+1, m+1, c.Of.Story, c.Of.N); err != nil {
						return fmt.Errorf("keep weaving: %w", err)
					}
					continue
				}
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO editor_span_cites (story_id, n, m, source_id, entry_id, span_start, span_end)
					VALUES (?, ?, ?, ?, ?, ?, ?)`,
					sid, n+1, m+1, c.Address.SourceID, c.Address.EntryID, c.Address.Span.Start, c.Address.Span.End); err != nil {
					return fmt.Errorf("keep weaving: %w", err)
				}
			}
		}
	}
	return tx.Commit()
}

// addEvent adds an event of kind and shape at at, returning its id.
func addEvent(ctx context.Context, tx *sql.Tx, kind string, shape int, at time.Time) (int64, error) {
	res, err := tx.ExecContext(ctx, `INSERT INTO events (kind, shape, at) VALUES (?, ?, ?)`, kind, shape, formatTime(at))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// storyColumns are a Story's columns as scanStory reads them: its event,
// its run, its Judge, when it was made and the call that made it (the
// applied weaving call on its run's work answering its request); then its
// newest Name, if any, likewise, its naming's work keyed by the Story.
const storyColumns = `
	s.event_id, s.weaving_id, w.act, w.version, w.model, w.request_hash, events.at,
	coalesce((SELECT max(c.id) FROM model_calls c
		WHERE c.act = 'weaving' AND c.outcome = 'applied' AND c.request_hash = w.request_hash
			AND c.work_key = w.work_key), 0),
	coalesce(n.name, ''), coalesce(n.act, ''), coalesce(n.version, 0), coalesce(n.model, ''),
	coalesce(n.request_hash, ''), coalesce(named.at, ''),
	coalesce((SELECT max(c.id) FROM model_calls c
		WHERE c.act = 'naming' AND c.outcome = 'applied' AND c.request_hash = n.request_hash
			AND c.work_key = CAST(s.event_id AS TEXT)), 0)
	FROM woven_stories s JOIN weavings w ON w.event_id = s.weaving_id
		JOIN events ON events.id = s.event_id
		LEFT JOIN story_names n ON n.event_id = (SELECT max(event_id) FROM story_names WHERE story_id = s.event_id)
		LEFT JOIN events named ON named.id = n.event_id`

// StoriesMade are the Stories the Editor made, newest first, each with its
// Editor's Spans in the order written and what each cites.
func (s *Store) StoriesMade(ctx context.Context) ([]editor.Story, error) {
	return s.stories(ctx, `ORDER BY s.event_id DESC`)
}

// stories are the Stories the Editor made, as StoriesMade reads them, chosen
// and ordered by the query's tail.
func (s *Store) stories(ctx context.Context, tail string) ([]editor.Story, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+storyColumns+` `+tail)
	if err != nil {
		return nil, fmt.Errorf("stories made: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.Story
	for rows.Next() {
		st, err := scanStory(rows)
		if err != nil {
			return nil, fmt.Errorf("stories made: %w", err)
		}
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stories made: %w", err)
	}
	_ = rows.Close()
	for i := range out {
		if out[i].Spans, err = s.editorSpans(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// StoryMade is a Story the Editor made, by its event, reporting whether the
// Record holds one.
func (s *Store) StoryMade(ctx context.Context, id int64) (editor.Story, bool, error) {
	st, err := scanStory(s.db.QueryRowContext(ctx, `SELECT `+storyColumns+` WHERE s.event_id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return editor.Story{}, false, nil
	}
	if err != nil {
		return editor.Story{}, false, fmt.Errorf("story %d: %w", id, err)
	}
	if st.Spans, err = s.editorSpans(ctx, id); err != nil {
		return editor.Story{}, false, err
	}
	return st, true, nil
}

func scanStory(row scanner) (editor.Story, error) {
	var st editor.Story
	var at, namedAt string
	n := &st.Name
	if err := row.Scan(&st.ID, &st.Weaving, &st.Judge.Act, &st.Judge.Version, &st.Judge.Model,
		&st.Judge.RequestHash, &at, &st.Call,
		&n.Text, &n.Judge.Act, &n.Judge.Version, &n.Judge.Model, &n.Judge.RequestHash, &namedAt, &n.Call); err != nil {
		return st, err
	}
	st.At = parseTime(at)
	if n.Text != "" {
		n.Story, n.At = st.ID, parseTime(namedAt)
	}
	return st, nil
}

// editorSpans are a Story's Editor's Spans, in the order written, each with
// the Spans it cites, in the order cited, and their words: a Source's Span's
// read from its Item's text, an earlier Editor's Span's from the Judgement
// that wrote it.
func (s *Store) editorSpans(ctx context.Context, storyID int64) ([]editor.EditorSpan, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.n, e.body, c.m, c.source_id, c.entry_id, c.span_start, c.span_end, items.content, items.title, 0, 0
		FROM editor_spans e
			JOIN editor_span_cites c ON c.story_id = e.story_id AND c.n = e.n
			JOIN items ON items.source_id = c.source_id AND items.entry_id = c.entry_id
		WHERE e.story_id = ?
		UNION ALL
		SELECT e.n, e.body, c.m, '', '', 0, 0, cited.body, '', c.cited_story_id, c.cited_n
		FROM editor_spans e
			JOIN editor_span_cites_editor c ON c.story_id = e.story_id AND c.n = e.n
			JOIN editor_spans cited ON cited.story_id = c.cited_story_id AND cited.n = c.cited_n
		WHERE e.story_id = ?
		ORDER BY 1, 3`, storyID, storyID)
	if err != nil {
		return nil, fmt.Errorf("editor's spans of story %d: %w", storyID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.EditorSpan
	last := 0
	for rows.Next() {
		var n, m int
		var body, text, title string
		var a editor.Address
		var of editor.EditorSpanAt
		if err := rows.Scan(&n, &body, &m, &a.SourceID, &a.EntryID, &a.Span.Start, &a.Span.End, &text, &title,
			&of.Story, &of.N); err != nil {
			return nil, fmt.Errorf("editor's spans of story %d: %w", storyID, err)
		}
		if n != last {
			out = append(out, editor.EditorSpan{Body: body})
			last = n
		}
		es := &out[len(out)-1]
		if !of.IsZero() {
			es.Cites = append(es.Cites, editor.Cited{Of: of, Words: text})
			continue
		}
		es.Cites = append(es.Cites, editor.Cited{Address: a, Words: cut.Words(text, a.Span), Title: title})
	}
	return out, rows.Err()
}
