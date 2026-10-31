package collect

import (
	"context"
	"log/slog"
)

// TextStore is the part of the Record backfill writes to.
type TextStore interface {
	// Untexted lists the Source's Items that have no text and are not yet
	// cut.
	Untexted(ctx context.Context, sourceID string) ([]Item, error)
	// GiveText gives an uncut Item with no text its text. It reports
	// whether it did: an Item cut meanwhile is left as it was.
	GiveText(ctx context.Context, sourceID, entryID, text string) (bool, error)
}

// BackfillDeps are what Backfill needs.
type BackfillDeps struct {
	Sources []Source
	Fetcher Fetcher
	Store   TextStore
	Log     *slog.Logger
}

// Backfill gives their text to the Items collected before their Source's
// text was fetched, while they are uncut (system design: An Item is
// collected with its full text, or not yet). Nothing has been judged of an
// uncut Item, so its text moves no address; the next Collection run cuts
// it. An Item whose text can't be fetched is logged and left as it was, for
// the next backfill. It returns how many Items were given their text.
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
	return given, nil
}
