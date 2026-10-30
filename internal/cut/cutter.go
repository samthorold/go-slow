package cut

import (
	"context"
	"fmt"
	"time"
)

// Judge is what made a Cut: the cutter and its version. A new version of a
// cutter is a new Judge, and adds Cuts beside its earlier version's.
type Judge struct {
	Cutter  string
	Version int
}

// SentenceCutter is Sentences, as the Judge its Cuts name. Its version
// changes whenever what Sentences lists of a text could.
var SentenceCutter = Judge{Cutter: "sentences", Version: 1}

// Text is an Item's text as kept, which its Spans address.
type Text struct {
	SourceID, EntryID string
	Text              string
}

// Cut is the Judgement listing an Item's Spans, in order. It is kept as an
// event and never changed.
type Cut struct {
	// ID is the event's identifier in the Record; zero until kept.
	ID                int64
	SourceID, EntryID string
	Judge             Judge
	At                time.Time
	Spans             []Span
}

// Store is the part of the Record cutting reads and writes.
type Store interface {
	// Uncut lists the Items with text that no Cut lists the Spans of.
	Uncut(ctx context.Context) ([]Text, error)
	// KeepCut keeps a Cut as an event.
	KeepCut(ctx context.Context, c Cut) error
}

// Cutter cuts the Items in the Record.
type Cutter struct {
	store Store
	judge Judge
	cut   func(string) []Span
}

// New is the sentence cutter over the Record.
func New(store Store) *Cutter {
	return &Cutter{store: store, judge: SentenceCutter, cut: Sentences}
}

// CutNew cuts, at now, every Item with text that has no Cut yet: those just
// collected, and on its first run those already in the Record. An Item
// already cut is left to its Cut, whatever cutter made it: a new cutter
// judges forward, and re-cutting what is kept is a deliberate run of its
// own (system design: Every judgement carries its judge).
func (c *Cutter) CutNew(ctx context.Context, now time.Time) error {
	texts, err := c.store.Uncut(ctx)
	if err != nil {
		return err
	}
	for _, t := range texts {
		cut := Cut{SourceID: t.SourceID, EntryID: t.EntryID, Judge: c.judge, At: now, Spans: c.cut(t.Text)}
		if err := c.store.KeepCut(ctx, cut); err != nil {
			return fmt.Errorf("cut %s %s: %w", t.SourceID, t.EntryID, err)
		}
	}
	return nil
}
