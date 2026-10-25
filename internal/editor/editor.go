package editor

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Store is the part of the Record the Editor reads and writes.
type Store interface {
	// ItemsToRead are the Items awaiting reading: tied to a Topic by the
	// lazy-reading check, and not yet read.
	ItemsToRead(ctx context.Context) ([]Item, error)
	// ReadingPrecedents are at most limit pieces of the Owner's Feedback
	// that a Fact was wrong, on Facts from Items sharing a Topic with it:
	// those sharing the most Topics first, then the newest.
	ReadingPrecedents(ctx context.Context, it Item, limit int) ([]Precedent, error)
	// SaveReading writes a reading to the Record, all at once, and marks
	// its Item read at readAt.
	SaveReading(ctx context.Context, r Reading, readAt time.Time) error
	// FactsToPlace are the Facts awaiting placing, by Item, oldest Item
	// first.
	FactsToPlace(ctx context.Context) ([]ToPlace, error)
	// StoriesSharing are the Stories whose Developments involve any of the
	// named Entities.
	StoriesSharing(ctx context.Context, entities []string) ([]Story, error)
	// PlacingPrecedents are at most limit pieces of the Owner's Feedback on
	// Developments whose Facts share an Entity with tp's Facts, or whose
	// Items share a Topic with tp's Item: those sharing the most first, then
	// the newest. Feedback on a withdrawn Fact is never a precedent.
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
	// FactsNear are at most limit other Facts that share an Entity or a
	// Story with any of facts, those nearest them in date first.
	FactsNear(ctx context.Context, facts []Fact, limit int) ([]Fact, error)
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
	// PlacingModel is the model placing runs on, chosen by configuration.
	PlacingModel Model
	// ConnectingModel is the model connecting runs on, chosen by
	// configuration.
	ConnectingModel Model
	// Wikidata is where reading looks Entities up first.
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

// ReadWaiting reads every Item awaiting reading, at now. A reading that fails
// (a failed call, a result that fails the checks) is logged and writes
// nothing, so its Item stays awaiting reading and is read on the next run;
// it does not stop the others. Only a failing Record is an error.
func (e *Editor) ReadWaiting(ctx context.Context, now time.Time) error {
	items, err := e.Store.ItemsToRead(ctx)
	if err != nil {
		return err
	}
	for _, it := range items {
		ps, err := e.Store.ReadingPrecedents(ctx, it, precedentsShown)
		if err != nil {
			return err
		}
		r, call, err := e.readNow(ctx, it, ps, now)
		if err != nil {
			if call != nil {
				if err := e.Store.KeepCall(ctx, *call); err != nil {
					return err
				}
			}
			e.Log.ErrorContext(ctx, "reading failed", "source", it.SourceID, "entry", it.EntryID, "error", err)
			continue
		}
		if err := e.Store.SaveReading(ctx, r, now); err != nil {
			return err
		}
		if err := e.Store.KeepCall(ctx, *call); err != nil {
			return err
		}
		e.Log.InfoContext(ctx, "read", "source", it.SourceID, "entry", it.EntryID,
			"facts", len(r.Facts), "claims", len(r.Claims), "model", r.Model,
			"input_tokens", r.Usage.InputTokens, "output_tokens", r.Usage.OutputTokens)
	}
	return nil
}

// readNow reads an Item in real time, shown the precedents, at now: the
// reading, ready to write, and its call, ended as it came out. The call is
// nil only when no request could be made. A reading whose call failed, whose
// result fails the checks or whose Entities could not be looked up is an
// error, and its call says which.
func (e *Editor) readNow(ctx context.Context, it Item, ps []Precedent, now time.Time) (Reading, *Call, error) {
	req, err := readingRequest(it, ps)
	if err != nil {
		return Reading{}, nil, err
	}
	call := newCall(req.Act, itemKey(it), "", modelOf(e.ModelFor, req.Act), req, feedbackIDs(ps), now)
	res, err := e.Model.Complete(ctx, req)
	call = call.answered(res)
	if err != nil {
		err = fmt.Errorf("reading %s: %w", it.EntryID, err)
		call = call.ended(Failed, err)
		return Reading{}, &call, err
	}
	r, err := checkReading(it, ps, res)
	if err != nil {
		call = call.ended(Refused, err)
		return Reading{}, &call, err
	}
	if r, err = resolve(ctx, e.Wikidata, r, it.Published, now); err != nil {
		call = call.ended(Failed, fmt.Errorf("not written: %w", err))
		return Reading{}, &call, err
	}
	call = call.ended(Applied, nil)
	return r, &call, nil
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
		stories, err := e.Store.StoriesSharing(ctx, entityNames(tp))
		if err != nil {
			return err
		}
		p, err := Place(ctx, e.PlacingModel, tp, stories)
		if err != nil {
			e.Log.ErrorContext(ctx, "placing failed", "source", tp.Item.SourceID, "entry", tp.Item.EntryID, "error", err)
			continue
		}
		if err := e.Store.SavePlacing(ctx, p, now); err != nil {
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
		others, err := e.Store.FactsNear(ctx, tc.Facts, factsShown)
		if err != nil {
			return err
		}
		c, err := Connect(ctx, e.ConnectingModel, tc, others)
		if err != nil {
			e.Log.ErrorContext(ctx, "connecting failed", "source", tc.Item.SourceID, "entry", tc.Item.EntryID, "error", err)
			continue
		}
		if err := e.Store.SaveConnecting(ctx, c, now); err != nil {
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

// keep checks a model's answer on an Item, looks its Entities up in
// Wikidata and writes the reading to the Record, at now. A result that fails
// the checks is ErrFailedChecks and writes nothing. A failed lookup writes
// nothing either and is an error like the Record's: in a batch, the batch
// stays pending and its results are applied once Wikidata answers, with no
// new model call.
func (e *Editor) keep(ctx context.Context, it Item, precedents []Precedent, res Response, now time.Time) error {
	r, err := checkReading(it, precedents, res)
	if err != nil {
		return err
	}
	if r, err = resolve(ctx, e.Wikidata, r, it.Published, now); err != nil {
		return err
	}
	return e.Store.SaveReading(ctx, r, now)
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
	Facts  []Fact
	Claims []Claim
}
