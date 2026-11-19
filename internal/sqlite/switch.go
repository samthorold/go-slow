package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Switch is when the Record came to keep only its Items and its events,
// dropping the old design's judgements, and which Items it held then.
type Switch struct {
	// At is when the Record changed.
	At time.Time
	// ItemsThrough is when the newest Item the Record held then was
	// collected: Items collected through it are hydrated only by re-judging,
	// never as work awaiting hydration.
	ItemsThrough time.Time
}

// Switch is the Record's switch, reporting whether it has one: a Record
// that held no Items when it changed has none, since all of it is the new
// design's.
func (s *Store) Switch(ctx context.Context) (Switch, bool, error) {
	var at, through string
	err := s.db.QueryRowContext(ctx, `
		SELECT events.at, record_switch.items_through
		FROM record_switch JOIN events ON events.id = record_switch.event_id`).Scan(&at, &through)
	if errors.Is(err, sql.ErrNoRows) {
		return Switch{}, false, nil
	}
	if err != nil {
		return Switch{}, false, fmt.Errorf("switch: %w", err)
	}
	return Switch{At: parseTime(at), ItemsThrough: parseTime(through)}, true, nil
}

// storySwitchShape is the shape of a switch to Stories' event, kind switch.
const storySwitchShape = 2

// storySwitch is SQL for the Record's switch to Stories: the through_cut of
// its newest switch event, or 0 for a Record with none. The newest, not the
// largest, so a switch is moved back as well as forward by keeping another,
// and none is rewritten.
const storySwitch = `coalesce((SELECT through_cut FROM story_switch ORDER BY event_id DESC LIMIT 1), 0)`

// KeepStorySwitch keeps a switch to Stories at at, as an event, through the
// Cut throughCut: from then on the Stories cut after it, and after the last
// weaving run written, await weaving (system design: The Record is recreated
// when the design of its Judgements changes, not translated).
func (s *Store) KeepStorySwitch(ctx context.Context, throughCut int64, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	id, err := addEvent(ctx, tx, "switch", storySwitchShape, at)
	if err != nil {
		return fmt.Errorf("keep switch to stories: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO story_switch (event_id, through_cut) VALUES (?, ?)`, id, throughCut); err != nil {
		return fmt.Errorf("keep switch to stories: %w", err)
	}
	return tx.Commit()
}

// CutBefore is the newest Cut of an Item collected before day, reporting
// whether there is one: the switch to Stories that makes the Stories cut of
// Items collected on and after day await weaving.
func (s *Store) CutBefore(ctx context.Context, day time.Time) (int64, bool, error) {
	var id sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT max(cuts.event_id) FROM cuts JOIN items USING (source_id, entry_id)
		WHERE items.collected_at < ?`, formatTime(day)).Scan(&id)
	if err != nil {
		return 0, false, fmt.Errorf("cut before %s: %w", day, err)
	}
	return id.Int64, id.Valid, nil
}

// afterSwitch is SQL true for an Item, aliased items, collected after the
// switch, or for any Item if the Record has none.
const afterSwitch = `NOT EXISTS (SELECT 1 FROM record_switch sw WHERE items.collected_at <= sw.items_through)`
