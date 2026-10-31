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
	// collected: Items collected through it are read only by re-judging,
	// never as work awaiting reading.
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

// afterSwitch is SQL true for an Item, aliased items, collected after the
// switch, or for any Item if the Record has none.
const afterSwitch = `NOT EXISTS (SELECT 1 FROM record_switch sw WHERE items.collected_at <= sw.items_through)`
