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
}

// Deps are the Editor's dependencies.
type Deps struct {
	Store Store
	// Model is the model reading runs on, chosen by configuration.
	Model Model
	// Wikidata is where reading looks Entities up first.
	Wikidata Wikidata
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
