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
	// first. Work marked failed awaits nothing, and nor do the Items
	// collected before the Record kept only events, which only re-judging
	// reads.
	ItemsToRead(ctx context.Context) ([]ToRead, error)
	// KeepReading keeps a reading, each of its judgements as its own event
	// with the reading's Judge, all at once.
	KeepReading(ctx context.Context, r Reading) error
	CallKeeper
}

// Deps are the Editor's dependencies.
type Deps struct {
	Store Store
	// Model is the model reading runs on, chosen by configuration.
	Model Model
	// Weavings is the part of the Record weaving reads and keeps its
	// Stories in; weaving runs in batches, on the batch interface's models.
	Weavings WeavingStore
	// Namings is the part of the Record naming reads the Stories the Editor
	// made from and keeps their Names in; naming runs in batches too.
	Namings NamingStore
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
		call, err := e.callNow(ctx, e.Model, itemKey(tr.Item), req, now, func(res Response) (err error) {
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

// KeptItem is an Item as its page shows it: what was collected, with its
// Source's name and attribution. What was judged of it is in its events.
type KeptItem struct {
	Item
	SourceName  string
	Attribution string
}
