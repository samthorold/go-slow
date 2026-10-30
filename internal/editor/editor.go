package editor

import (
	"context"
	"log/slog"
	"time"
)

// Store is the part of the Record the Editor reads and writes.
type Store interface {
	// ItemsToRead are the Items awaiting reading: every Item cut and not
	// yet read, whatever it is about, each with its newest Cut, oldest
	// first. Work marked failed awaits nothing.
	ItemsToRead(ctx context.Context) ([]ToRead, error)
	// KeepReading keeps a reading, each of its judgements as its own event
	// with the reading's Judge, all at once.
	KeepReading(ctx context.Context, r Reading) error
	// ItemsToRelate are the Items awaiting relating, in the order they were
	// first read: read, not yet related, and what their readings name
	// looked up. Each comes with its Spans not rejected, and what each
	// mentions; work marked failed awaits nothing.
	ItemsToRelate(ctx context.Context) ([]ToRelate, error)
	// SpansMentioning are the Spans not rejected, of Items first read before
	// readBefore, that mention one of es.
	SpansMentioning(ctx context.Context, es []NamedEntity, readBefore int64) ([]OtherSpan, error)
	// ItemsMentioning is how many Items first read at or before readBefore
	// mention each of es.
	ItemsMentioning(ctx context.Context, es []NamedEntity, readBefore int64) (map[NamedEntity]int, error)
	// KeepRelating keeps a relating, each of its relations as its own event
	// with the relating's Judge, all at once.
	KeepRelating(ctx context.Context, r Relating) error
	// FactsToPlace are the Facts awaiting placing, by Item, oldest Item
	// first.
	FactsToPlace(ctx context.Context) ([]ToPlace, error)
	// CandidateStories are at most limit Stories with a Development whose
	// Item mentions an Entity it mentions, best first: ranked by the
	// Entities each shares, each weighted by how few Stories it is in, then
	// by the newest Development. Each carries what it shares as Shares.
	CandidateStories(ctx context.Context, it Item, limit int) ([]Story, error)
	// PlacingPrecedents are at most limit pieces of the Owner's Feedback on
	// Developments whose Facts' Items mention an Entity tp's Item mentions,
	// or share a Topic with it: those sharing the most first, then the
	// newest. Feedback on a withdrawn Fact is never a precedent.
	PlacingPrecedents(ctx context.Context, tp ToPlace, limit int) ([]Precedent, error)
	// SavePlacing writes a placing to the Record, all at once, and marks its
	// Facts placed at placedAt.
	SavePlacing(ctx context.Context, p Placing, placedAt time.Time) error
	// FactsToConnect are the Facts placed and awaiting connecting, by Item,
	// oldest Item first.
	FactsToConnect(ctx context.Context) ([]ToConnect, error)
	// FactsPlacedSince are the Facts placed at or after since, by Item,
	// oldest Item first.
	FactsPlacedSince(ctx context.Context, since time.Time) ([]ToConnect, error)
	// FactsNear are at most limit Facts other than tc's whose Items mention
	// an Entity tc's Item mentions, or that are Developments in a Story with
	// one of tc's Facts, those nearest them in date first.
	FactsNear(ctx context.Context, tc ToConnect, limit int) ([]Fact, error)
	// SaveConnecting writes a connecting to the Record, all at once, and
	// marks its Facts connected at connectedAt.
	SaveConnecting(ctx context.Context, c Connecting, connectedAt time.Time) error
	CallKeeper
}

// Deps are the Editor's dependencies.
type Deps struct {
	Store Store
	// Model is the model reading runs on, chosen by configuration.
	Model Model
	// RelatingModel is the model relating runs on, chosen by configuration.
	RelatingModel Model
	// PlacingModel is the model placing runs on, chosen by configuration.
	PlacingModel Model
	// ConnectingModel is the model connecting runs on, chosen by
	// configuration.
	ConnectingModel Model
	// Wikidata is the Reference source the Sunday read's tools look up.
	Wikidata Wikidata
	// Briefings is the part of the Record the Briefing is written from and
	// kept in, and BriefingModel the model it is written with.
	Briefings     BriefingStore
	BriefingModel Model
	// SundayReads is the part of the Record the Sunday read is written from
	// and kept in, and SundayReadModel the model it is written with, as an
	// agent with tools.
	SundayReads     SundayStore
	SundayReadModel Conversation
	// ModelFor is the model an act's calls run on, as the model adapter
	// chooses it, kept with each call; nil leaves it unknown.
	ModelFor func(act string) string
	Log      *slog.Logger
}

// Editor does the editorial work over the Record.
type Editor struct {
	Deps
}

// New builds the Editor.
func New(d Deps) *Editor { return &Editor{Deps: d} }

// ReadWaiting reads every Item awaiting reading, at now, in real time: the
// synchronous path, for when no batch interface is available. A reading
// that fails (a failed call, a result that fails the checks) is logged and
// writes nothing, so its Item stays awaiting reading and is read on the next
// run; it does not stop the others. Only a failing Record is an error.
func (e *Editor) ReadWaiting(ctx context.Context, now time.Time) error {
	waiting, err := e.Store.ItemsToRead(ctx)
	if err != nil {
		return err
	}
	for _, tr := range waiting {
		req := readingRequest(tr)
		var r Reading
		call, err := e.callNow(ctx, e.Model, itemKey(tr.Item), req, nil, now, func(res Response) (err error) {
			r, err = checkReading(tr, req, modelOf(e.ModelFor, req.Act), res)
			return err
		})
		if err != nil {
			if err := e.Store.KeepCall(ctx, call); err != nil {
				return err
			}
			e.Log.ErrorContext(ctx, "reading failed", "source", tr.Item.SourceID, "entry", tr.Item.EntryID, "error", err)
			continue
		}
		if err := e.keepReading(ctx, r, now); err != nil {
			return err
		}
		if err := e.Store.KeepCall(ctx, call); err != nil {
			return err
		}
	}
	return nil
}

// keepReading keeps a reading judged at now, and logs it.
func (e *Editor) keepReading(ctx context.Context, r Reading, now time.Time) error {
	r.At = now
	if err := e.Store.KeepReading(ctx, r); err != nil {
		return err
	}
	e.Log.InfoContext(ctx, "read", "source", r.SourceID, "entry", r.EntryID,
		"refers_to", len(r.RefersTo), "names", len(r.Names), "speakers", len(r.Speakers),
		"rejected", len(r.Rejected), "model", r.Judge.Model,
		"input_tokens", r.Usage.InputTokens, "output_tokens", r.Usage.OutputTokens)
	return nil
}

// itemKey is the key an act's work on an Item is kept under: its Source and
// entry, a line apart.
func itemKey(it Item) string { return it.SourceID + "\n" + it.EntryID }

// PlaceWaiting places every Fact awaiting placing, an Item at a time in the
// order the Items were published, so a later Fact meets the Stories earlier
// ones started (system design: The acts run as Items arrive). A placing that
// fails is logged and writes nothing, so its Facts await placing on the next
// run. Only a failing Record is an error.
func (e *Editor) PlaceWaiting(ctx context.Context, now time.Time) error {
	waiting, err := e.Store.FactsToPlace(ctx)
	if err != nil {
		return err
	}
	for _, tp := range waiting {
		tp, err := e.withPrecedents(ctx, tp)
		if err != nil {
			return err
		}
		stories, err := e.Store.CandidateStories(ctx, tp.Item, candidatesShown)
		if err != nil {
			return err
		}
		var p Placing
		call, err := e.callNow(ctx, e.PlacingModel, itemKey(tp.Item), placingRequest(tp, stories),
			feedbackIDs(tp.Precedents), now, func(res Response) (err error) {
				p, err = checkPlacing(tp, stories, res)
				return err
			})
		if err != nil {
			if err := e.Store.KeepCall(ctx, call); err != nil {
				return err
			}
			e.Log.ErrorContext(ctx, "placing failed", "source", tp.Item.SourceID, "entry", tp.Item.EntryID, "error", err)
			continue
		}
		if err := e.Store.SavePlacing(ctx, p, now); err != nil {
			return err
		}
		if err := e.Store.KeepCall(ctx, call); err != nil {
			return err
		}
		e.Log.InfoContext(ctx, "placed", "source", tp.Item.SourceID, "entry", tp.Item.EntryID,
			"facts", len(p.Facts), "developments", len(p.Developments), "new_stories", len(p.NewStories),
			"model", p.Model, "input_tokens", p.Usage.InputTokens, "output_tokens", p.Usage.OutputTokens)
	}
	return nil
}

// ConnectWaiting connects every Fact awaiting connecting, an Item at a time
// in the order the Items were published. A connecting that fails is logged
// and writes nothing, so its Facts await connecting on the next run. Only a
// failing Record is an error.
func (e *Editor) ConnectWaiting(ctx context.Context, now time.Time) error {
	waiting, err := e.Store.FactsToConnect(ctx)
	if err != nil {
		return err
	}
	return e.connectEach(ctx, waiting, now)
}

// Reconnect runs connecting again at now, in real time, over the day's
// Facts: those placed in the day before now, an Item at a time. It is a step
// the Briefing's Schedule runs just before writing (ScheduleDeps.Before); a
// day rather than since midnight, since the Briefing covers what was placed
// since the previous one, a day earlier. A Fact connected early in the
// day was weighed against the Facts in the Record then; by the time the
// Briefing is written, later Items have brought in Facts it may be connected
// to (system design: The acts run as Items arrive). It only adds: a
// Connection made earlier stays. It runs just before the Briefing is
// written, which is due at once, so it does not wait on a batch.
func (e *Editor) Reconnect(ctx context.Context, now time.Time) error {
	day, err := e.Store.FactsPlacedSince(ctx, now.Add(-24*time.Hour))
	if err != nil {
		return err
	}
	return e.connectEach(ctx, day, now)
}

// connectEach connects each Item's Facts in turn, in real time.
func (e *Editor) connectEach(ctx context.Context, waiting []ToConnect, now time.Time) error {
	for _, tc := range waiting {
		others, err := e.Store.FactsNear(ctx, tc, factsShown)
		if err != nil {
			return err
		}
		var c Connecting
		call, err := e.callNow(ctx, e.ConnectingModel, itemKey(tc.Item), connectingRequest(tc, others),
			nil, now, func(res Response) (err error) {
				c, err = checkConnecting(tc, others, res)
				return err
			})
		if err != nil {
			if err := e.Store.KeepCall(ctx, call); err != nil {
				return err
			}
			e.Log.ErrorContext(ctx, "connecting failed", "source", tc.Item.SourceID, "entry", tc.Item.EntryID, "error", err)
			continue
		}
		if err := e.Store.SaveConnecting(ctx, c, now); err != nil {
			return err
		}
		if err := e.Store.KeepCall(ctx, call); err != nil {
			return err
		}
		e.Log.InfoContext(ctx, "connected", "source", tc.Item.SourceID, "entry", tc.Item.EntryID,
			"facts", len(c.Facts), "causes", len(c.Causes),
			"model", c.Model, "input_tokens", c.Usage.InputTokens, "output_tokens", c.Usage.OutputTokens)
	}
	return nil
}

// StoryListing is a Story as the Stories page lists it.
type StoryListing struct {
	ID    int64
	Title string
	// Developments is how many it has, and Latest the date of the latest.
	Developments int
	Latest       time.Time
}

// KnownEntity is an Entity as the Record holds it.
type KnownEntity struct {
	Name string
	Kind string
	// WikidataID is the Wikidata item it is, or empty for the Editor's own.
	WikidataID string
	// Aliases are the names the Record's Items have called it by.
	Aliases []string
	// Facts are the Facts about it drawn from Reference sources.
	Facts []Fact
}

// ReadItem is an Item as its page shows it: what was collected, and what
// reading drew from it.
type ReadItem struct {
	Item
	SourceName  string
	Attribution string
	// Read is whether the Item has been read; ReadAt and Model say when and
	// by which model.
	Read   bool
	ReadAt time.Time
	Model  string
	// ReadingCall is the kept call whose reading drew its Facts; zero if
	// none was kept.
	ReadingCall int64
	// Topics are the names of the Topics the check tied the Item to.
	Topics []string
	// Mentions are the Entities the Item mentions, each once, by name.
	Mentions []Entity
	// Facts carry the Entities each involves (Fact.Involves).
	Facts  []Fact
	Claims []Claim
}
