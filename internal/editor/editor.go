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
	// StandingWrongOnRelations is the wrong standing on relations, each with
	// the Spans at its two ends, newest first: what relating is shown as
	// precedent.
	StandingWrongOnRelations(ctx context.Context) ([]Precedent, error)
	CallKeeper
}

// Deps are the Editor's dependencies.
type Deps struct {
	Store Store
	// Model is the model reading runs on, chosen by configuration.
	Model Model
	// RelatingModel is the model relating runs on, chosen by configuration.
	RelatingModel Model
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

// KeptItem is an Item as its page shows it: what was collected, with its
// Source's name and attribution. What was judged of it is in its events.
type KeptItem struct {
	Item
	SourceName  string
	Attribution string
}
