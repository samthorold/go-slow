package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
)

// factEntities is a subquery giving a Fact's Entities as a JSON array.
const factEntities = `(SELECT json_group_array(entities.name ORDER BY entities.name)
	FROM fact_entities JOIN entities ON entities.id = fact_entities.entity_id
	WHERE fact_entities.fact_id = facts.id)`

// FactsToPlace are the Facts not yet placed, by Item, in the order the
// Items were published.
func (s *Store) FactsToPlace(ctx context.Context) ([]editor.ToPlace, error) {
	out, err := s.factsByItem(ctx, `NOT EXISTS (SELECT 1 FROM fact_placings p WHERE p.fact_id = facts.id)
		AND `+notFailed("placing"))
	if err != nil {
		return nil, fmt.Errorf("facts to place: %w", err)
	}
	return out, nil
}

// factsByItem are the Facts matching where, by Item, in the order the Items
// were published.
func (s *Store) factsByItem(ctx context.Context, where string, args ...any) ([]editor.ToPlace, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT facts.id, facts.source_id, facts.entry_id, facts.statement, facts.as_at, facts.quote,
			`+factEntities+`, items.title, items.link, items.published,
			(SELECT coalesce(max(topics.depth), 0) FROM item_topics JOIN topics ON topics.id = item_topics.topic_id
				WHERE item_topics.source_id = items.source_id AND item_topics.entry_id = items.entry_id)
		FROM facts JOIN items USING (source_id, entry_id)
		WHERE facts.withdrawn_by IS NULL AND (`+where+`)
		ORDER BY items.published, items.collected_at, facts.source_id, facts.entry_id, facts.id`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []editor.ToPlace
	for rows.Next() {
		var f editor.Fact
		var it editor.Item
		var asAt, ents, published string
		var depth interest.Depth
		if err := rows.Scan(&f.ID, &f.SourceID, &f.EntryID, &f.Statement, &asAt, &f.Quote, &ents,
			&it.Title, &it.Link, &published, &depth); err != nil {
			return nil, err
		}
		if err := scanFact(&f, asAt, ents); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].Item.SourceID != f.SourceID || out[n-1].Item.EntryID != f.EntryID {
			it.SourceID, it.EntryID, it.Published = f.SourceID, f.EntryID, parseTime(published)
			out = append(out, editor.ToPlace{Item: it, Depth: depth})
		}
		out[len(out)-1].Facts = append(out[len(out)-1].Facts, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()
	for i, tp := range out {
		if out[i].Mentions, err = s.mentions(ctx, tp.Item.SourceID, tp.Item.EntryID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func scanFact(f *editor.Fact, asAt, ents string) error {
	t, err := time.Parse(time.DateOnly, asAt)
	if err != nil {
		return fmt.Errorf("as at: %w", err)
	}
	f.AsAt = t
	if err := json.Unmarshal([]byte(ents), &f.Entities); err != nil {
		return fmt.Errorf("entities: %w", err)
	}
	return nil
}

// CandidateStories are the Stories placing weighs an Item's Facts against,
// with their Timelines, best first: at most limit of the Stories with a
// Development whose Item mentions an Entity it mentions (system design:
// Placing weighs a Fact against the Stories its Item's Entities are in).
// Each is ranked by the Entities it shares with the Item, each weighted by
// how few Stories it is in, and by its newest Development where they tie.
// Each carries the names of the Entities it shares, as Shares.
func (s *Store) CandidateStories(ctx context.Context, it editor.Item, limit int) ([]editor.Story, error) {
	shared, err := s.sharedMentions(ctx, it)
	if err != nil {
		return nil, fmt.Errorf("candidate stories: %w", err)
	}
	ranked := rankCandidates(shared)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	out := make([]editor.Story, 0, len(ranked))
	for _, c := range ranked {
		st, _, err := s.story(ctx, c.story, false)
		if err != nil {
			return nil, err
		}
		st.Shares = c.shares
		out = append(out, st)
	}
	return out, nil
}

// sharedMention is an Entity an Item mentions that a Story's Items mention
// too, with what ranking it needs: how many Stories mention it, how many
// Stories there are, and the date of the Story's newest Development.
type sharedMention struct {
	story   int64
	entity  string
	in      int
	stories int
	latest  string
}

// sharedMentions are the Entities it mentions that Stories' Items mention
// too, one row per Story and Entity. Only live Developments count, as the
// acts see a Story.
func (s *Store) sharedMentions(ctx context.Context, it editor.Item) ([]sharedMention, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH live AS (
			SELECT developments.story_id, facts.source_id, facts.entry_id, facts.as_at
			FROM developments JOIN facts ON facts.id = developments.fact_id
			WHERE developments.withdrawn_by IS NULL AND facts.withdrawn_by IS NULL AND NOT `+addedByUndone+`
		),
		latest AS (SELECT story_id, max(as_at) AS as_at FROM live GROUP BY story_id),
		mentioned AS (
			SELECT DISTINCT live.story_id, m.entity_id
			FROM live JOIN item_mentions m ON m.source_id = live.source_id AND m.entry_id = live.entry_id
		),
		shared AS (
			SELECT mentioned.story_id, mentioned.entity_id FROM mentioned
			WHERE mentioned.entity_id IN (SELECT entity_id FROM item_mentions WHERE source_id = ? AND entry_id = ?)
		)
		SELECT shared.story_id, entities.name,
			(SELECT count(*) FROM mentioned m WHERE m.entity_id = shared.entity_id),
			(SELECT count(*) FROM latest),
			latest.as_at
		FROM shared JOIN entities ON entities.id = shared.entity_id
			JOIN latest ON latest.story_id = shared.story_id
		ORDER BY shared.story_id, entities.name`, it.SourceID, it.EntryID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []sharedMention
	for rows.Next() {
		var m sharedMention
		if err := rows.Scan(&m.story, &m.entity, &m.in, &m.stories, &m.latest); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// candidate is a Story ranked as a candidate: its score, the date of its
// newest Development and the Entities it shares.
type candidate struct {
	story  int64
	score  float64
	latest string
	shares []string
}

// rankCandidates ranks the Stories the shared mentions are in, best first:
// by the sum of each shared Entity's weight, an inverse document frequency
// over Stories, log(1 + N/n) for an Entity n of the N Stories mention, so
// an Entity in nearly every Story still counts a little; ties go to the
// Story with the newest Development, then the newest Story.
func rankCandidates(shared []sharedMention) []candidate {
	var out []candidate
	at := map[int64]int{}
	for _, m := range shared {
		n, ok := at[m.story]
		if !ok {
			n = len(out)
			at[m.story] = n
			out = append(out, candidate{story: m.story, latest: m.latest})
		}
		out[n].score += math.Log1p(float64(m.stories) / float64(m.in))
		out[n].shares = append(out[n].shares, m.entity)
	}
	slices.SortStableFunc(out, func(a, b candidate) int {
		if c := cmp.Compare(b.score, a.score); c != 0 {
			return c
		}
		if c := cmp.Compare(b.latest, a.latest); c != 0 {
			return c
		}
		return cmp.Compare(b.story, a.story)
	})
	return out
}

// Story is one Story with its Timeline, reporting whether it is in the
// Record. What Feedback withdrew is on the Timeline, marked.
func (s *Store) Story(ctx context.Context, id int64) (editor.Story, bool, error) {
	return s.story(ctx, id, true)
}

// story is one Story, with what Feedback withdrew if withdrawn is set, and
// otherwise without it, as the acts see the Story.
func (s *Store) story(ctx context.Context, id int64, withdrawn bool) (editor.Story, bool, error) {
	st := editor.Story{ID: id}
	err := s.db.QueryRowContext(ctx, `SELECT title, unwanted_by IS NOT NULL, missed_by IS NOT NULL AND NOT `+storyAddedByUndone+`
		FROM stories WHERE id = ?`, id).Scan(&st.Title, &st.Unwanted, &st.Missed)
	if errors.Is(err, sql.ErrNoRows) {
		return st, false, nil
	}
	if err != nil {
		return st, false, fmt.Errorf("story %d: %w", id, err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT facts.id, facts.source_id, facts.entry_id, facts.statement, facts.as_at, facts.quote,
			`+factEntities+`, items.title,
			developments.withdrawn_by IS NOT NULL, developments.unwanted_by IS NOT NULL, developments.missed_by IS NOT NULL,
			CASE WHEN developments.missed_by IS NULL THEN `+placingCall("facts")+` ELSE 0 END
		FROM developments JOIN facts ON facts.id = developments.fact_id
			JOIN items USING (source_id, entry_id)
		WHERE developments.story_id = ? AND (? OR developments.withdrawn_by IS NULL) AND NOT `+addedByUndone+`
		ORDER BY facts.as_at, items.published, facts.id`, id, withdrawn)
	if err != nil {
		return st, false, fmt.Errorf("story %d: %w", id, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var d editor.Development
		var asAt, ents string
		if err := rows.Scan(&d.ID, &d.SourceID, &d.EntryID, &d.Statement, &asAt, &d.Quote, &ents, &d.ItemTitle,
			&d.Withdrawn, &d.Unwanted, &d.Missed, &d.PlacingCall); err != nil {
			return st, false, fmt.Errorf("story %d: %w", id, err)
		}
		if err := scanFact(&d.Fact, asAt, ents); err != nil {
			return st, false, fmt.Errorf("story %d: %w", id, err)
		}
		for _, e := range d.Entities {
			if !slices.Contains(st.Entities, e) {
				st.Entities = append(st.Entities, e)
			}
		}
		st.Timeline = append(st.Timeline, d)
	}
	if err := rows.Err(); err != nil {
		return st, false, fmt.Errorf("story %d: %w", id, err)
	}
	slices.Sort(st.Entities)
	if st.Causes, err = s.storyCauses(ctx, id); err != nil {
		return st, false, fmt.Errorf("story %d: %w", id, err)
	}
	if !withdrawn {
		st.Causes = slices.DeleteFunc(st.Causes, func(c editor.CauseConnection) bool { return c.Withdrawn })
	}
	return st, true, nil
}

// ListStories lists the Stories, the one moved on most recently first.
func (s *Store) ListStories(ctx context.Context) ([]editor.StoryListing, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT stories.id, stories.title, count(facts.id), coalesce(max(facts.as_at), '')
		FROM stories LEFT JOIN developments ON developments.story_id = stories.id
			LEFT JOIN facts ON facts.id = developments.fact_id
		WHERE NOT `+storyAddedByUndone+`
		GROUP BY stories.id
		ORDER BY max(facts.as_at) DESC, stories.id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list stories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.StoryListing
	for rows.Next() {
		var l editor.StoryListing
		var latest string
		if err := rows.Scan(&l.ID, &l.Title, &l.Developments, &latest); err != nil {
			return nil, fmt.Errorf("list stories: %w", err)
		}
		if latest != "" {
			if l.Latest, err = time.Parse(time.DateOnly, latest); err != nil {
				return nil, fmt.Errorf("list stories: %w", err)
			}
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list stories: %w", err)
	}
	return out, nil
}

// SavePlacing starts the placing's new Stories, makes its Facts
// Developments, and marks every Fact it placed, all at once.
func (s *Store) SavePlacing(ctx context.Context, p editor.Placing, placedAt time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	develop := func(storyID, factID int64) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO developments (story_id, fact_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
			storyID, factID); err != nil {
			return fmt.Errorf("development of fact %d in story %d: %w", factID, storyID, err)
		}
		return nil
	}
	for _, ns := range p.NewStories {
		var id int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO stories (title, started_at) VALUES (?, ?) RETURNING id`,
			ns.Title, formatTime(placedAt)).Scan(&id); err != nil {
			return fmt.Errorf("start story %q: %w", ns.Title, err)
		}
		for _, f := range ns.FactIDs {
			if err := develop(id, f); err != nil {
				return err
			}
		}
	}
	for _, d := range p.Developments {
		if err := develop(d.StoryID, d.FactID); err != nil {
			return err
		}
	}
	for _, f := range p.Facts {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO fact_placings (fact_id, placed_at, model) VALUES (?, ?, ?)`,
			f, formatTime(placedAt), p.Model); err != nil {
			return fmt.Errorf("mark fact %d placed: %w", f, err)
		}
	}
	if err := keepPrecedents(ctx, tx, "placing", p.SourceID, p.EntryID, p.Precedents, placedAt); err != nil {
		return err
	}
	return tx.Commit()
}
