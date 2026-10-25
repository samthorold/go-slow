package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

// PlacingPrecedents are at most limit pieces of the Owner's Feedback on
// Developments whose Facts share an Entity with tp's Facts, or whose Items
// share a Topic with tp's Item: those sharing the most Entities and Topics
// first, then the newest. Feedback on a withdrawn Fact is never one: the
// acts never see what was withdrawn. Nor is Feedback the Owner undid.
func (s *Store) PlacingPrecedents(ctx context.Context, tp editor.ToPlace, limit int) ([]editor.Precedent, error) {
	names, err := json.Marshal(entityNamesOf(tp))
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, depth, given_at, story, statement FROM (
			SELECT feedback.id, feedback.kind, feedback.depth, feedback.given_at,
				coalesce(stories.title, '') AS story, facts.statement,
				(SELECT count(*) FROM fact_entities JOIN entities ON entities.id = fact_entities.entity_id
					WHERE fact_entities.fact_id = facts.id AND entities.name IN (SELECT value FROM json_each(?1)))
				+ (SELECT count(*) FROM item_topics theirs JOIN item_topics mine ON mine.topic_id = theirs.topic_id
					WHERE theirs.source_id = facts.source_id AND theirs.entry_id = facts.entry_id
						AND mine.source_id = ?2 AND mine.entry_id = ?3) AS shared
			FROM feedback
				JOIN facts ON facts.id = feedback.fact_id
				LEFT JOIN stories ON stories.id = feedback.story_id
			WHERE feedback.level = 'development' AND feedback.undone_at IS NULL AND facts.withdrawn_by IS NULL)
		WHERE shared > 0
		ORDER BY shared DESC, given_at DESC, id DESC
		LIMIT ?4`, string(names), tp.Item.SourceID, tp.Item.EntryID, limit)
	if err != nil {
		return nil, fmt.Errorf("placing precedents: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.Precedent
	for rows.Next() {
		var p editor.Precedent
		var at string
		if err := rows.Scan(&p.FeedbackID, &p.Kind, &p.Depth, &at, &p.Story, &p.Fact); err != nil {
			return nil, fmt.Errorf("placing precedents: %w", err)
		}
		p.GivenAt = parseTime(at)
		out = append(out, p)
	}
	return out, rows.Err()
}

// ReadingPrecedents are at most limit pieces of the Owner's Feedback that a
// Fact was wrong, on Facts drawn from Items sharing a Topic with it: those
// sharing the most Topics first, then the newest. An Item's Entities are
// known only once it is read, so a Topic is all reading can be matched by.
// The Facts are withdrawn, by that Feedback; they are shown as what the
// Editor got wrong, never as part of the Record. Feedback the Owner undid is
// never one.
func (s *Store) ReadingPrecedents(ctx context.Context, it editor.Item, limit int) ([]editor.Precedent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, depth, given_at, statement, quote FROM (
			SELECT feedback.id, feedback.kind, feedback.depth, feedback.given_at, facts.statement, facts.quote,
				(SELECT count(*) FROM item_topics theirs JOIN item_topics mine ON mine.topic_id = theirs.topic_id
					WHERE theirs.source_id = facts.source_id AND theirs.entry_id = facts.entry_id
						AND mine.source_id = ?1 AND mine.entry_id = ?2) AS shared
			FROM feedback JOIN facts ON facts.id = feedback.fact_id
			WHERE feedback.kind = 'wrong' AND feedback.level = 'fact' AND feedback.undone_at IS NULL)
		WHERE shared > 0
		ORDER BY shared DESC, given_at DESC, id DESC
		LIMIT ?3`, it.SourceID, it.EntryID, limit)
	if err != nil {
		return nil, fmt.Errorf("reading precedents: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []editor.Precedent
	for rows.Next() {
		var p editor.Precedent
		var at string
		if err := rows.Scan(&p.FeedbackID, &p.Kind, &p.Depth, &at, &p.Fact, &p.Quote); err != nil {
			return nil, fmt.Errorf("reading precedents: %w", err)
		}
		p.GivenAt = parseTime(at)
		out = append(out, p)
	}
	return out, rows.Err()
}

// keepPrecedents keeps, with an act's result on an Item, the Feedback it was
// shown as precedent, in the order shown.
func keepPrecedents(ctx context.Context, tx *sql.Tx, act, sourceID, entryID string, feedbackIDs []int64, at time.Time) error {
	for i, id := range feedbackIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO precedents_shown (act, source_id, entry_id, feedback_id, rank, shown_at)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			act, sourceID, entryID, id, i+1, formatTime(at)); err != nil {
			return fmt.Errorf("keep precedent %d of %s %s: %w", id, act, entryID, err)
		}
	}
	return nil
}

// PrecedentsShown are the ids of the Feedback act was shown as precedent
// when it judged an Item, in the order shown.
func (s *Store) PrecedentsShown(ctx context.Context, act, sourceID, entryID string) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT feedback_id FROM precedents_shown WHERE act = ? AND source_id = ? AND entry_id = ?
		ORDER BY rank`, act, sourceID, entryID)
	if err != nil {
		return nil, fmt.Errorf("precedents shown: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("precedents shown: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// entityNamesOf are the names of every Entity tp's Facts involve.
func entityNamesOf(tp editor.ToPlace) []string {
	out := []string{}
	for _, f := range tp.Facts {
		out = append(out, f.Entities...)
	}
	return out
}
