package editor

import (
	"context"
	"log/slog"
	"time"
)

// Store is the part of the Record the Editor reads and writes.
type Store interface {
	// ItemsToRead are the Items awaiting reading: tied to a Topic by the
	// lazy-reading check, and not yet read.
	ItemsToRead(ctx context.Context) ([]Item, error)
	// SaveReading writes a reading to the Record, all at once, and marks
	// its Item read at readAt.
	SaveReading(ctx context.Context, r Reading, readAt time.Time) error
	// FactsToPlace are the Facts awaiting placing, by Item, oldest Item
	// first.
	FactsToPlace(ctx context.Context) ([]ToPlace, error)
	// StoriesSharing are the Stories whose Developments involve any of the
	// named Entities.
	StoriesSharing(ctx context.Context, entities []string) ([]Story, error)
	// SavePlacing writes a placing to the Record, all at once, and marks its
	// Facts placed at placedAt.
	SavePlacing(ctx context.Context, p Placing, placedAt time.Time) error
}

// Deps are the Editor's dependencies.
type Deps struct {
	Store Store
	// Model is the model reading runs on, chosen by configuration.
	Model Model
	// PlacingModel is the model placing runs on, chosen by configuration.
	PlacingModel Model
	// Wikidata is where reading looks Entities up first.
	Wikidata Wikidata
	// Briefings is the part of the Record the Briefing is written from and
	// kept in, and BriefingModel the model it is written with.
	Briefings     BriefingStore
	BriefingModel Model
	Log           *slog.Logger
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
		r, err := Read(ctx, e.Model, it)
		if err == nil {
			r, err = resolve(ctx, e.Wikidata, r, it.Published, now)
		}
		if err != nil {
			e.Log.ErrorContext(ctx, "reading failed", "source", it.SourceID, "entry", it.EntryID, "error", err)
			continue
		}
		if err := e.Store.SaveReading(ctx, r, now); err != nil {
			return err
		}
		e.Log.InfoContext(ctx, "read", "source", it.SourceID, "entry", it.EntryID,
			"facts", len(r.Facts), "claims", len(r.Claims), "model", r.Model,
			"input_tokens", r.Usage.InputTokens, "output_tokens", r.Usage.OutputTokens)
	}
	return nil
}

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
func (e *Editor) keep(ctx context.Context, it Item, res Response, now time.Time) error {
	r, err := checkReading(it, res)
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
	// Topics are the names of the Topics the check tied the Item to.
	Topics []string
	Facts  []Fact
	Claims []Claim
}
