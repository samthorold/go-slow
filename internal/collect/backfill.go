package collect

import (
	"context"
	"log/slog"
)

// TextStore is the part of the Record backfill writes to.
type TextStore interface {
	// Untexted lists the Source's Items that have no text and are not yet
	// read.
	Untexted(ctx context.Context, sourceID string) ([]Item, error)
	// GiveText gives an unread Item with no text its text, and leaves it
	// for the lazy-reading check to look at again. It reports whether it
	// did: an Item read meanwhile is left as it was read.
	GiveText(ctx context.Context, sourceID, entryID, text string) (bool, error)
}

// BackfillDeps are what Backfill needs.
type BackfillDeps struct {
	Sources []Source
	Fetcher Fetcher
	Store   TextStore
	Check   Checker
	Log     *slog.Logger
}

// Backfill gives their text to the Items collected before their Source's
// text was fetched, while they are unread (system design: An Item is
// collected with its full text, or not yet), then runs the lazy-reading
// check over them again. Nothing has been drawn from an unread Item, so its
// text changes no Fact; an Item already read stays as it was read. An Item
// whose text can't be fetched is logged and left as it was, for the next
// backfill. It returns how many Items were given their text.
func Backfill(ctx context.Context, d BackfillDeps) (int, error) {
	given := 0
	for _, s := range d.Sources {
		if !s.fetchesText() {
			continue
		}
		items, err := d.Store.Untexted(ctx, s.ID)
		if err != nil {
			return given, err
		}
		for _, it := range items {
			text, err := fetchText(ctx, d.Fetcher, s, it)
			if err != nil {
				d.Log.WarnContext(ctx, "Item's text not fetched; left as it was",
					"source", s.ID, "entry", it.EntryID, "error", err)
				continue
			}
			if text == "" {
				continue
			}
			ok, err := d.Store.GiveText(ctx, s.ID, it.EntryID, text)
			if err != nil {
				return given, err
			}
			if ok {
				given++
			}
		}
	}
	return given, d.Check.CheckNew(ctx)
}
