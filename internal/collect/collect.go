// Package collect is Collection: checking the Sources on a timetable and
// bringing their new Items into the Record. It makes no editorial judgement;
// fetching (and its politeness) and storage are behind the ports here.
package collect

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
)

// OGL is the attribution the Open Government Licence v3.0 asks for.
const OGL = "Contains public sector information licensed under the Open Government Licence v3.0."

// OPL is the attribution the Open Parliament Licence v3.0 asks for.
const OPL = "Contains Parliamentary information licensed under the Open Parliament Licence v3.0."

// Keep is how much of an Item a Source's terms let the Record keep (system
// design: The record keeps everything collected).
type Keep int

const (
	// KeepUndeclared is a Source whose terms have not been declared. It is
	// never collected: the Record keeps nothing it has no licence for.
	KeepUndeclared Keep = iota
	// KeepFullText keeps the Item as collected: its full text and its raw
	// entry.
	KeepFullText
	// KeepSummary keeps the Item's title, link, summary and dates, but not
	// its full text or raw entry.
	KeepSummary
)

// Source is an outlet or publisher the Owner has chosen, read through one
// feed or API endpoint.
type Source struct {
	ID   string
	Name string
	// ShortName is how a citation names the Source: who is speaking, not
	// where it is published ("No 10", not "GOV.UK"). Sources from one
	// speaker share it. For the pages only; no act is shown it.
	ShortName string
	FeedURL   string
	Format    Format
	// Text is how the Source's entries get their full text, where the feed
	// carries only a summary or an opening.
	Text Text
	// Licence names the terms the Source is collected under; Attribution is
	// the statement they ask for, carried by every Item; Keeps is how much
	// of an Item they let the Record keep.
	Licence     string
	Attribution string
	Keeps       Keep
	// UsualGap is how long the Source normally goes between Items; silent
	// for longer, it is quiet.
	UsualGap time.Duration
	// MinInterval is the Source's own rate limit: the least time between two
	// fetches of it, however often Collection runs. A feed that answers
	// conditional requests can be asked every run (zero); one that cannot
	// is asked no more often than it changes.
	MinInterval time.Duration
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
	// Due is when a scheduled event, such as a release on a statistics
	// body's release calendar, is to happen; zero for any other Item. A
	// scheduled event is upcoming until it is due.
	Due time.Time
	// Raw is the entry as the feed carried it.
	Raw         string
	Attribution string
	CollectedAt time.Time
}

// ListedItem is an Item as the Items page lists it.
type ListedItem struct {
	SourceID    string
	EntryID     string
	SourceName  string
	Title       string
	Link        string
	Summary     string
	Published   time.Time
	Due         time.Time
	Attribution string
	// Checked is whether the lazy-reading check has looked at the Item yet.
	Checked bool
	// Topics are the names of the Topics the check tied the Item to: it
	// awaits reading for each. None, once Checked, is an Item that touches
	// no Topic.
	Topics []string
	// HasText is whether the Item has its text, and Cut whether a Cut lists
	// its Spans.
	HasText, Cut bool
	// Read is whether the Editor has read the Item's Spans; Names, Speakers
	// and Rejected are how many of each its newest reading judged.
	Read                      bool
	Names, Speakers, Rejected int
}

// Checker is the lazy-reading check, run over the Items Collection has just
// brought in. It is plain code, not a model call.
type Checker interface {
	CheckNew(ctx context.Context) error
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
	// Known reports which of the Source's entries are already in the
	// Record.
	Known(ctx context.Context, sourceID string, entryIDs []string) (map[string]bool, error)
}

// Deps are the Collector's dependencies.
type Deps struct {
	Sources []Source
	Fetcher Fetcher
	Store   Store
	// Cut, if set, runs after every run, before the check: it cuts the
	// Items with text that have no Cut yet into Spans (system design: The
	// grain belongs to the judgement, not to the Span).
	Cut Cutter
	// Check runs after every run, over the Items not yet checked.
	Check Checker
	// Read, if set, runs after the check: the Editor reads the Items
	// awaiting reading (system design: The acts run as Items arrive), at
	// once or by submitting them in a batch and applying the results of
	// earlier ones (The daily acts run in batches).
	Read Reader
	// LookUp, if set, runs after reading: plain code looks up in Wikidata
	// what the readings name, as its own step, so a look-up refused or
	// delayed holds up no reading (system design: Entities are looked up by
	// name, not by Span).
	LookUp Looker
	Log    *slog.Logger
}

// Looker is the look-up step, run over the names not yet looked up. A
// look-up that fails is left for the next run.
type Looker interface {
	LookUpWaiting(ctx context.Context, now time.Time) error
}

// Cutter cuts the Items not yet cut into Spans, at now, in plain code.
type Cutter interface {
	CutNew(ctx context.Context, now time.Time) error
}

// Reader is the Editor's reading, run over the Items awaiting it. A reading
// that fails leaves its Item awaiting reading, for the next run.
type Reader interface {
	ReadWaiting(ctx context.Context, now time.Time) error
}

// Collector runs Collection over its Sources.
type Collector struct {
	Deps
	// fetched is when each Source was last fetched, for its rate limit.
	// Runs never overlap, so it needs no lock. It is not kept across
	// restarts: a restart fetches each Source once at most.
	fetched map[string]time.Time
}

// New builds a Collector.
func New(d Deps) *Collector { return &Collector{Deps: d, fetched: map[string]time.Time{}} }

// RunOn is Collection's timetable: a run at once, then one on every tick,
// each at the time now gives, until ctx is done. Runs never overlap. main
// passes a time.Ticker's channel; a tick that arrives during a run is
// dropped by the ticker, not queued.
func (c *Collector) RunOn(ctx context.Context, ticks <-chan time.Time, now func() time.Time) {
	c.Run(ctx, now())
	c.RunEvery(ctx, ticks, now)
}

// RunEvery is RunOn without the run at once: a run on every tick until ctx
// is done. main runs the first run itself, so that the Briefing's schedule
// starts only once it has ended.
func (c *Collector) RunEvery(ctx context.Context, ticks <-chan time.Time, now func() time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			c.Run(ctx, now())
		}
	}
}

// Run is one Collection run at now: each Source in turn, then the
// lazy-reading check over what is new. A failing Source is logged and does
// not stop the others.
func (c *Collector) Run(ctx context.Context, now time.Time) {
	for _, s := range c.Sources {
		if last, ok := c.fetched[s.ID]; ok && now.Sub(last) < s.MinInterval {
			continue
		}
		n, err := c.collect(ctx, s, now)
		if err != nil {
			c.Log.ErrorContext(ctx, "collection failed", "source", s.ID, "error", err)
			continue
		}
		c.Log.InfoContext(ctx, "collected", "source", s.ID, "new_items", n)
	}
	// Items left uncut by a failure here are cut after the next run.
	if c.Cut != nil {
		if err := c.Cut.CutNew(ctx, now); err != nil {
			c.Log.ErrorContext(ctx, "cutting failed", "error", err)
		}
	}
	// Items left unchecked by a failure here are checked after the next run.
	if err := c.Check.CheckNew(ctx); err != nil {
		c.Log.ErrorContext(ctx, "lazy-reading check failed", "error", err)
	}
	if c.Read != nil {
		if err := c.Read.ReadWaiting(ctx, now); err != nil {
			c.Log.ErrorContext(ctx, "reading failed", "error", err)
		}
	}
	if c.LookUp != nil {
		if err := c.LookUp.LookUpWaiting(ctx, now); err != nil {
			c.Log.ErrorContext(ctx, "looking up failed", "error", err)
		}
	}
}

// blockedHosts are publishers' sites that block scripted clients, so
// Collection never fetches them, whatever their robots.txt says: UK
// Parliament's main site, Hansard, and the Commons Library and Developer Hub
// sites, all of which answer a scripted client with a challenge page.
// Parliament is read only through its APIs.
var blockedHosts = map[string]bool{
	"parliament.uk":                true,
	"www.parliament.uk":            true,
	"hansard.parliament.uk":        true,
	"commonslibrary.parliament.uk": true,
	"developer.parliament.uk":      true,
}

// neverFetched reports whether rawURL is on a blocked host. A URL that does
// not parse is never fetched either.
func neverFetched(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	return blockedHosts[strings.ToLower(u.Hostname())]
}

func (c *Collector) collect(ctx context.Context, s Source, now time.Time) (int, error) {
	if s.Keeps == KeepUndeclared {
		return 0, errors.New("the Source declares no terms, so nothing of it may be kept")
	}
	if neverFetched(s.FeedURL) {
		return 0, fmt.Errorf("%s is on a host that blocks scripted clients; only its publisher's APIs are fetched", s.FeedURL)
	}
	v, err := c.Store.Validators(ctx, s.ID)
	if err != nil {
		return 0, err
	}
	c.fetched[s.ID] = now
	f, err := c.Fetcher.Fetch(ctx, s.FeedURL, v)
	if err != nil {
		return 0, err
	}
	if f.NotModified {
		return 0, nil
	}
	items, err := s.Format.parse(f.Body)
	if err != nil {
		return 0, err
	}
	for i := range items {
		items[i].SourceID = s.ID
		items[i].Attribution = s.Attribution
		items[i].CollectedAt = now
		if s.Keeps == KeepSummary {
			items[i].Content, items[i].Raw = "", ""
		}
	}
	items, whole, err := c.withText(ctx, s, items)
	if err != nil {
		return 0, err
	}
	n, err := c.Store.AddItems(ctx, items)
	if err != nil {
		return 0, err
	}
	if !whole {
		// The feed is asked in full next time, so the entries left out
		// are still in what it answers.
		return n, nil
	}
	return n, c.Store.SaveValidators(ctx, s.ID, f.Validators)
}

// withText gives each of the Source's entries its full text, where the
// Source declares one is fetched and its terms let it be kept. An entry
// whose text can't be fetched is left out of this run, to be collected on
// a later one; whole is false if any was. Only new entries are fetched:
// those already in the Record are left out, as AddItems would leave them.
func (c *Collector) withText(ctx context.Context, s Source, items []Item) (kept []Item, whole bool, err error) {
	if !s.fetchesText() {
		return items, true, nil
	}
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.EntryID
	}
	known, err := c.Store.Known(ctx, s.ID, ids)
	if err != nil {
		return nil, false, err
	}
	whole = true
	for _, it := range items {
		if known[it.EntryID] {
			continue
		}
		text, err := fetchText(ctx, c.Fetcher, s, it)
		if err != nil {
			c.Log.WarnContext(ctx, "entry's text not fetched; left for a later run",
				"source", s.ID, "entry", it.EntryID, "error", err)
			whole = false
			continue
		}
		it.Content = text
		kept = append(kept, it)
	}
	return kept, whole, nil
}
