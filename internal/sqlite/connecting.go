package sqlite

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

// FactsToConnect are the Facts placed and not yet connected, by Item, in the
// order the Items were published. Connecting waits for placing, since the
// Facts a Fact is weighed against include those in its Stories.
func (s *Store) FactsToConnect(ctx context.Context) ([]editor.ToConnect, error) {
	out, err := s.toConnect(ctx, `EXISTS (SELECT 1 FROM fact_placings p WHERE p.fact_id = facts.id)
		AND NOT EXISTS (SELECT 1 FROM fact_connectings c WHERE c.fact_id = facts.id)
		AND `+notFailed("connecting"))
	if err != nil {
		return nil, fmt.Errorf("facts to connect: %w", err)
	}
	return out, nil
}

// FactsPlacedSince are the Facts placed at or after since, by Item, in the
// order the Items were published: the day's Facts, for connecting to run
// again over before the Briefing.
func (s *Store) FactsPlacedSince(ctx context.Context, since time.Time) ([]editor.ToConnect, error) {
	out, err := s.toConnect(ctx, `EXISTS (SELECT 1 FROM fact_placings p WHERE p.fact_id = facts.id AND p.placed_at >= ?)`,
		formatTime(since))
	if err != nil {
		return nil, fmt.Errorf("facts placed since: %w", err)
	}
	return out, nil
}

func (s *Store) toConnect(ctx context.Context, where string, args ...any) ([]editor.ToConnect, error) {
	byItem, err := s.factsByItem(ctx, where, args...)
	if err != nil {
		return nil, err
	}
	out := make([]editor.ToConnect, 0, len(byItem))
	for _, tp := range byItem {
		out = append(out, editor.ToConnect{Item: tp.Item, Facts: tp.Facts, Mentions: tp.Mentions})
	}
	return out, nil
}

// mentions are the Entities an Item mentions, as its reading gave them, by
// name.
func (s *Store) mentions(ctx context.Context, sourceID, entryID string) ([]editor.Mention, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT entities.name, entities.kind
		FROM item_mentions m JOIN entities ON entities.id = m.entity_id
		WHERE m.source_id = ? AND m.entry_id = ?
		ORDER BY entities.name`, sourceID, entryID)
	if err != nil {
		return nil, fmt.Errorf("entities of the reading of %s: %w", entryID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.Mention
	for rows.Next() {
		var m editor.Mention
		if err := rows.Scan(&m.Name, &m.Kind); err != nil {
			return nil, fmt.Errorf("entities of the reading of %s: %w", entryID, err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// FactsNear are at most limit Facts other than tc's whose Items mention an
// Entity tc's Item mentions, or that are Developments in a Story with one of
// tc's Facts, those nearest the latest of tc's Facts in date first.
func (s *Store) FactsNear(ctx context.Context, tc editor.ToConnect, limit int) ([]editor.Fact, error) {
	var ids []int64
	var latest time.Time
	for _, f := range tc.Facts {
		ids = append(ids, f.ID)
		if f.AsAt.After(latest) {
			latest = f.AsAt
		}
	}
	mine, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH mine(id) AS (SELECT value FROM json_each(?1)),
		near(id) AS (
			SELECT facts.id FROM item_mentions ours
				JOIN item_mentions theirs ON theirs.entity_id = ours.entity_id
				JOIN facts ON facts.source_id = theirs.source_id AND facts.entry_id = theirs.entry_id
			WHERE ours.source_id = ?4 AND ours.entry_id = ?5
			UNION
			SELECT theirs.fact_id FROM developments ours
				JOIN developments theirs ON theirs.story_id = ours.story_id
			WHERE ours.fact_id IN mine
		)
		SELECT facts.id, facts.source_id, facts.entry_id, facts.statement, facts.as_at, facts.quote
		FROM facts
		WHERE facts.id IN near AND facts.id NOT IN mine AND facts.withdrawn_by IS NULL
		ORDER BY abs(julianday(facts.as_at) - julianday(?2)), facts.id DESC
		LIMIT ?3`, string(mine), latest.Format(time.DateOnly), limit, tc.Item.SourceID, tc.Item.EntryID)
	if err != nil {
		return nil, fmt.Errorf("facts near: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.Fact
	for rows.Next() {
		var f editor.Fact
		var asAt string
		if err := rows.Scan(&f.ID, &f.SourceID, &f.EntryID, &f.Statement, &asAt, &f.Quote); err != nil {
			return nil, fmt.Errorf("facts near: %w", err)
		}
		if err := scanFact(&f, asAt); err != nil {
			return nil, fmt.Errorf("facts near: %w", err)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("facts near: %w", err)
	}
	return out, nil
}

// SaveConnecting gives each Entity a Fact involves its role in it, the one
// tie between a Fact and an Entity (an Entity given no role is not
// involved, though its Item mentions it), keeps the cause Connections, and
// marks every Fact connected, all at once. A Connection already kept is
// kept once.
func (s *Store) SaveConnecting(ctx context.Context, c editor.Connecting, connectedAt time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Connecting decides which Entities each Fact involves, so connecting a
	// Fact again replaces what it decided before. The Entities stay
	// mentioned by the Fact's Item.
	for _, f := range c.Facts {
		if _, err := tx.ExecContext(ctx, `DELETE FROM fact_entities WHERE fact_id = ?`, f); err != nil {
			return fmt.Errorf("roles in fact %d: %w", f, err)
		}
	}
	for _, r := range c.Roles {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO fact_entities (fact_id, entity_id, role)
			SELECT ?, id, ? FROM entities WHERE name = ?
			ON CONFLICT (fact_id, entity_id) DO UPDATE SET role = excluded.role`,
			r.FactID, r.Role, r.Entity); err != nil {
			return fmt.Errorf("role of %q in fact %d: %w", r.Entity, r.FactID, err)
		}
	}
	for _, cs := range c.Causes {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO fact_connections (from_fact_id, to_fact_id, kind, made_at, model, made_on)
			VALUES (?, ?, 'cause', ?, ?, ?) ON CONFLICT DO NOTHING`,
			cs.CauseID, cs.EffectID, formatTime(connectedAt), c.Model, c.SourceID+"\n"+c.EntryID); err != nil {
			return fmt.Errorf("cause from fact %d to %d: %w", cs.CauseID, cs.EffectID, err)
		}
	}
	for _, f := range c.Facts {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO fact_connectings (fact_id, connected_at, model) VALUES (?, ?, ?)
			ON CONFLICT (fact_id) DO UPDATE SET connected_at = excluded.connected_at, model = excluded.model`,
			f, formatTime(connectedAt), c.Model); err != nil {
			return fmt.Errorf("mark fact %d connected: %w", f, err)
		}
	}
	return tx.Commit()
}

// Involvements are the Entities each Fact of an Item involves, each in its
// role, by Fact and then Entity: what connecting decided of them.
func (s *Store) Involvements(ctx context.Context, sourceID, entryID string) ([]editor.Role, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT fe.fact_id, entities.name, fe.role
		FROM fact_entities fe JOIN facts f ON f.id = fe.fact_id JOIN entities ON entities.id = fe.entity_id
		WHERE f.source_id = ? AND f.entry_id = ? AND fe.role IS NOT NULL
		ORDER BY fe.fact_id, entities.name`, sourceID, entryID)
	if err != nil {
		return nil, fmt.Errorf("involvements of %s: %w", entryID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.Role
	for rows.Next() {
		var r editor.Role
		if err := rows.Scan(&r.FactID, &r.Entity, &r.Role); err != nil {
			return nil, fmt.Errorf("involvements of %s: %w", entryID, err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// traced selects a Fact, with the title of its Item, from the facts table
// aliased as f joined to items as i.
func traced(f, i string) string {
	return f + `.id, ` + f + `.source_id, ` + f + `.entry_id, ` + f + `.statement, ` + f + `.as_at, ` + f + `.quote,
	` + i + `.title`
}

// commonEntityShare is the share of Stories an Entity may be in and still
// connect Stories: one in more of them (the government, actor in nearly
// everything a government source announces) connects none, since being in
// both is then no reason the two are related (system design: Connections
// are made between Facts and derived between Stories). Half is "most".
const commonEntityShare = 0.5

// liveDevelopments selects each live Development (story_id, fact_id) with
// its Fact's Item (source_id, entry_id), as the acts see a Story.
const liveDevelopments = `
	SELECT developments.story_id, developments.fact_id, facts.source_id, facts.entry_id
	FROM developments JOIN facts ON facts.id = developments.fact_id
	WHERE developments.withdrawn_by IS NULL AND facts.withdrawn_by IS NULL AND NOT ` + addedByUndone

// ConnectedStories are the Stories connected to the Story id, derived from
// what they share: Developments, Items they have Developments from, and
// Entities their Facts involve in the same role. For each Entity and role,
// the latest pair of Facts shows why.
func (s *Store) ConnectedStories(ctx context.Context, id int64) ([]editor.StoryConnection, error) {
	var out []editor.StoryConnection
	at := map[int64]int{}
	story := func(id int64, title string) *editor.StoryConnection {
		n, ok := at[id]
		if !ok {
			n = len(out)
			at[id] = n
			out = append(out, editor.StoryConnection{ID: id, Title: title})
		}
		return &out[n]
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH live AS (`+liveDevelopments+`)
		SELECT other.story_id, stories.title, `+traced("f", "i")+`
		FROM live mine
			JOIN live other ON other.fact_id = mine.fact_id AND other.story_id != mine.story_id
			JOIN stories ON stories.id = other.story_id
			JOIN facts f ON f.id = mine.fact_id JOIN items i ON i.source_id = f.source_id AND i.entry_id = f.entry_id
		WHERE mine.story_id = ?
		ORDER BY other.story_id, f.as_at, f.id`, id)
	if err != nil {
		return nil, fmt.Errorf("connected stories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var other int64
		var title string
		var d editor.Development
		var asAt string
		if err := rows.Scan(&other, &title, &d.ID, &d.SourceID, &d.EntryID, &d.Statement, &asAt, &d.Quote, &d.ItemTitle); err != nil {
			return nil, fmt.Errorf("connected stories: %w", err)
		}
		if err := scanFact(&d.Fact, asAt); err != nil {
			return nil, fmt.Errorf("connected stories: %w", err)
		}
		c := story(other, title)
		c.Shared = append(c.Shared, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("connected stories: %w", err)
	}
	_ = rows.Close()

	// Stories with Developments from the same Item, each by a Fact of its
	// own: a shared Development is its own reason.
	rows, err = s.db.QueryContext(ctx, `
		WITH live AS (`+liveDevelopments+`)
		SELECT DISTINCT other.story_id, stories.title, i.source_id, i.entry_id, i.title
		FROM live mine
			JOIN live other ON other.source_id = mine.source_id AND other.entry_id = mine.entry_id
				AND other.story_id != mine.story_id AND other.fact_id != mine.fact_id
			JOIN stories ON stories.id = other.story_id
			JOIN items i ON i.source_id = mine.source_id AND i.entry_id = mine.entry_id
		WHERE mine.story_id = ?
		ORDER BY other.story_id, i.published, i.source_id, i.entry_id`, id)
	if err != nil {
		return nil, fmt.Errorf("connected stories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var other int64
		var title string
		var it editor.SharedItem
		if err := rows.Scan(&other, &title, &it.SourceID, &it.EntryID, &it.Title); err != nil {
			return nil, fmt.Errorf("connected stories: %w", err)
		}
		c := story(other, title)
		c.Items = append(c.Items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("connected stories: %w", err)
	}
	_ = rows.Close()

	// Entities in the same role, only between Facts of different Items: two
	// Facts of one Item already connect their Stories by the Item.
	rows, err = s.db.QueryContext(ctx, `
		WITH live AS (`+liveDevelopments+`),
		involved AS (
			SELECT DISTINCT fe.entity_id, live.story_id
			FROM live JOIN fact_entities fe ON fe.fact_id = live.fact_id AND fe.role IS NOT NULL
		)
		SELECT other.story_id, stories.title, entities.name, here.role,
			(SELECT count(*) FROM involved WHERE involved.entity_id = here.entity_id),
			(SELECT count(DISTINCT story_id) FROM live),
			`+traced("f", "i")+`, `+traced("g", "j")+`
		FROM live mine
			JOIN fact_entities here ON here.fact_id = mine.fact_id AND here.role IS NOT NULL
			JOIN fact_entities there ON there.entity_id = here.entity_id AND there.role = here.role
				AND there.fact_id != here.fact_id
			JOIN live other ON other.fact_id = there.fact_id AND other.story_id != mine.story_id
				AND NOT (other.source_id = mine.source_id AND other.entry_id = mine.entry_id)
			JOIN stories ON stories.id = other.story_id
			JOIN entities ON entities.id = here.entity_id
			JOIN facts f ON f.id = here.fact_id JOIN items i ON i.source_id = f.source_id AND i.entry_id = f.entry_id
			JOIN facts g ON g.id = there.fact_id JOIN items j ON j.source_id = g.source_id AND j.entry_id = g.entry_id
		WHERE mine.story_id = ?
		ORDER BY other.story_id, entities.name, here.role, f.as_at DESC, g.as_at DESC, f.id DESC, g.id DESC`, id)
	if err != nil {
		return nil, fmt.Errorf("connected stories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	type reason struct {
		story        int64
		entity, role string
	}
	seen := map[reason]bool{}
	var stories int // how many Stories there are
	for rows.Next() {
		var other int64
		var title string
		var e editor.SharedEntity
		var asAt, asAt2 string
		if err := rows.Scan(&other, &title, &e.Entity, &e.Role, &e.Stories, &stories,
			&e.Here.ID, &e.Here.SourceID, &e.Here.EntryID, &e.Here.Statement, &asAt, &e.Here.Quote, &e.Here.ItemTitle,
			&e.There.ID, &e.There.SourceID, &e.There.EntryID, &e.There.Statement, &asAt2, &e.There.Quote, &e.There.ItemTitle,
		); err != nil {
			return nil, fmt.Errorf("connected stories: %w", err)
		}
		if float64(e.Stories) > commonEntityShare*float64(stories) {
			continue
		}
		if r := (reason{other, e.Entity, e.Role}); seen[r] {
			continue
		} else {
			seen[r] = true
		}
		if err := scanFact(&e.Here.Fact, asAt); err != nil {
			return nil, fmt.Errorf("connected stories: %w", err)
		}
		if err := scanFact(&e.There.Fact, asAt2); err != nil {
			return nil, fmt.Errorf("connected stories: %w", err)
		}
		c := story(other, title)
		c.Entities = append(c.Entities, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("connected stories: %w", err)
	}
	_ = rows.Close()
	rankConnections(out, stories)
	for i := range out {
		for j := range out[i].Shared {
			if err := s.withInvolves(ctx, &out[i].Shared[j].Fact); err != nil {
				return nil, fmt.Errorf("connected stories: %w", err)
			}
		}
		for j := range out[i].Entities {
			if err := s.withInvolves(ctx, &out[i].Entities[j].Here.Fact, &out[i].Entities[j].There.Fact); err != nil {
				return nil, fmt.Errorf("connected stories: %w", err)
			}
		}
	}
	return out, nil
}

// rankConnections orders each connection's Entities by how few Stories they
// are in, and the connections themselves: those sharing a Development or an
// Item first, then by their Entities weighted by rarity over the given
// count of Stories, as placing weighs its candidates (log(1 + N/n) for an
// Entity in n of N Stories), ties to the earlier Story.
func rankConnections(cs []editor.StoryConnection, stories int) {
	score := func(c editor.StoryConnection) float64 {
		var s float64
		for _, e := range c.Entities {
			s += math.Log1p(float64(stories) / float64(e.Stories))
		}
		return s
	}
	for i := range cs {
		slices.SortStableFunc(cs[i].Entities, func(a, b editor.SharedEntity) int {
			return cmp.Compare(a.Stories, b.Stories)
		})
	}
	slices.SortStableFunc(cs, func(a, b editor.StoryConnection) int {
		aFirm, bFirm := len(a.Shared)+len(a.Items) > 0, len(b.Shared)+len(b.Items) > 0
		if aFirm != bFirm {
			if aFirm {
				return -1
			}
			return 1
		}
		if c := cmp.Compare(score(b), score(a)); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
}

// storyCauses are the cause Connections to or from the Developments of the
// Story id, in the order of their effects' dates.
func (s *Store) storyCauses(ctx context.Context, id int64) ([]editor.CauseConnection, error) {
	return s.causes(ctx, `EXISTS (SELECT 1 FROM developments d
		WHERE d.story_id = ? AND d.fact_id IN (c.from_fact_id, c.to_fact_id))`, id)
}

// Causes are every cause Connection in the Record, each Fact traced to its
// Item, in the order of their effects' dates.
func (s *Store) Causes(ctx context.Context) ([]editor.CauseConnection, error) {
	return s.causes(ctx, `1`)
}

func (s *Store) causes(ctx context.Context, where string, args ...any) ([]editor.CauseConnection, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+traced("f", "i")+`, `+traced("g", "j")+`, c.withdrawn_by IS NOT NULL, `+connectingCall("c")+`
		FROM fact_connections c
			JOIN facts f ON f.id = c.from_fact_id JOIN items i ON i.source_id = f.source_id AND i.entry_id = f.entry_id
			JOIN facts g ON g.id = c.to_fact_id JOIN items j ON j.source_id = g.source_id AND j.entry_id = g.entry_id
		WHERE c.kind = 'cause' AND `+where+`
		ORDER BY g.as_at, f.as_at, g.id, f.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("causes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.CauseConnection
	for rows.Next() {
		var c editor.CauseConnection
		var asAt, asAt2 string
		if err := rows.Scan(
			&c.Cause.ID, &c.Cause.SourceID, &c.Cause.EntryID, &c.Cause.Statement, &asAt, &c.Cause.Quote, &c.Cause.ItemTitle,
			&c.Effect.ID, &c.Effect.SourceID, &c.Effect.EntryID, &c.Effect.Statement, &asAt2, &c.Effect.Quote, &c.Effect.ItemTitle,
			&c.Withdrawn, &c.Call,
		); err != nil {
			return nil, fmt.Errorf("causes: %w", err)
		}
		if err := scanFact(&c.Cause.Fact, asAt); err != nil {
			return nil, fmt.Errorf("causes: %w", err)
		}
		if err := scanFact(&c.Effect.Fact, asAt2); err != nil {
			return nil, fmt.Errorf("causes: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
