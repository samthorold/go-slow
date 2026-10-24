// Package collect is Collection: checking the Sources on a timetable and
// bringing their new Items into the Record. It makes no editorial judgement;
// fetching (and its politeness) and storage are behind the ports here.
package collect

import (
	"context"
	"log/slog"
	"time"
)

// OGL is the attribution the Open Government Licence v3.0 asks for.
const OGL = "Contains public sector information licensed under the Open Government Licence v3.0."

// Source is an outlet or publisher the Owner has chosen, read through one
// Atom feed.
type Source struct {
	ID          string
	Name        string
	FeedURL     string
	Attribution string
	// UsualGap is how long the Source normally goes between Items; silent
	// for longer, it is quiet.
	UsualGap time.Duration
}

// Item is one entry as Collection brought it in, kept in full.
type Item struct {
	SourceID string
	// EntryID is the feed's own id for the entry, unique within its Source.
	EntryID   string
	Title     string
	Link      string
	Summary   string
	Content   string
	Published time.Time
	Updated   time.Time
	// Raw is the entry's XML as the feed carried it.
	Raw         string
	Attribution string
	CollectedAt time.Time
}

// ListedItem is an Item as the Items page lists it.
type ListedItem struct {
	SourceName  string
	Title       string
	Link        string
	Summary     string
	Published   time.Time
	Attribution string
}

// Validators are what a server said about the feed last time, sent back to
// make the next request conditional.
type Validators struct {
	ETag         string
	LastModified string
}

// Fetched is the result of fetching a feed.
type Fetched struct {
	// NotModified is true when the server answered 304: nothing has changed.
	NotModified bool
	Body        []byte
	Validators  Validators
}

// Fetcher fetches a feed, conditionally on validators from last time.
type Fetcher interface {
	Fetch(ctx context.Context, url string, v Validators) (Fetched, error)
}

// Store is the part of the Record Collection writes to.
type Store interface {
	// Validators returns what was saved for the Source's feed last time.
	Validators(ctx context.Context, sourceID string) (Validators, error)
	SaveValidators(ctx context.Context, sourceID string, v Validators) error
	// AddItems stores the Items not already in the Record, and returns how
	// many were new.
	AddItems(ctx context.Context, items []Item) (int, error)
}

// Deps are the Collector's dependencies.
type Deps struct {
	Sources []Source
	Fetcher Fetcher
	Store   Store
	Log     *slog.Logger
}

// Collector runs Collection over its Sources.
type Collector struct {
	Deps
}

// New builds a Collector.
func New(d Deps) *Collector { return &Collector{Deps: d} }

// RunOn is Collection's timetable: a run at once, then one on every tick,
// each at the time now gives, until ctx is done. Runs never overlap. main
// passes a time.Ticker's channel; a tick that arrives during a run is
// dropped by the ticker, not queued.
func (c *Collector) RunOn(ctx context.Context, ticks <-chan time.Time, now func() time.Time) {
	c.Run(ctx, now())
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			c.Run(ctx, now())
		}
	}
}

// Run is one Collection run at now: each Source in turn. A failing Source is
// logged and does not stop the others.
func (c *Collector) Run(ctx context.Context, now time.Time) {
	for _, s := range c.Sources {
		n, err := c.collect(ctx, s, now)
		if err != nil {
			c.Log.ErrorContext(ctx, "collection failed", "source", s.ID, "error", err)
			continue
		}
		c.Log.InfoContext(ctx, "collected", "source", s.ID, "new_items", n)
	}
}

func (c *Collector) collect(ctx context.Context, s Source, now time.Time) (int, error) {
	v, err := c.Store.Validators(ctx, s.ID)
	if err != nil {
		return 0, err
	}
	f, err := c.Fetcher.Fetch(ctx, s.FeedURL, v)
	if err != nil {
		return 0, err
	}
	if f.NotModified {
		return 0, nil
	}
	items, err := parseAtom(f.Body)
	if err != nil {
		return 0, err
	}
	for i := range items {
		items[i].SourceID = s.ID
		items[i].Attribution = s.Attribution
		items[i].CollectedAt = now
	}
	n, err := c.Store.AddItems(ctx, items)
	if err != nil {
		return 0, err
	}
	return n, c.Store.SaveValidators(ctx, s.ID, f.Validators)
}
