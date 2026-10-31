package feedback

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Judged is the kind of judgement a piece of Feedback is about: the kind of
// its event in the Record.
type Judged string

// The judgements Feedback is given on (system design: Feedback lands on a
// judgement, so the judge to fix is known). A name and a Rejected are
// judgements too, but take no Feedback: what a name is is its look-up, and
// a Rejected Span is never told.
const (
	RefersTo  Judged = "refers_to"
	Lookup    Judged = "lookup"
	Speaker   Judged = "speaker"
	Relation  Judged = "relation"
	Gathering Judged = "gathering"
	Statement Judged = "statement"
)

// judgedTakes are the judgements each kind given on a judgement lands on:
// wrong on any of them, new to me only on what is told (a statement, or a
// Story gathered to tell it).
var judgedTakes = map[Kind][]Judged{
	Wrong:   {RefersTo, Lookup, Speaker, Relation, Gathering, Statement},
	NewToMe: {Gathering, Statement},
}

// Span is a Span of an Item, by its address: its Source and entry, and
// where it starts and ends in the Item's text as kept.
type Span struct {
	SourceID, EntryID string
	Start, End        int
}

// Piece is one piece of Feedback on the Record as events: what the Owner
// said, about what, and when, and nothing of the state it was given
// against (system design: Feedback is kept as given, and undoing it is
// Feedback too).
type Piece struct {
	ID   int64
	Kind Kind
	// Judgement is the event the piece is about, for wrong and new to me,
	// and Judged its kind.
	Judgement int64
	Judged    Judged
	// Span is the Span it is about, for unwanted and missed.
	Span Span
	// Day is the Briefing (YYYY-MM-DD) an unwanted Span was told in.
	Day     string
	GivenAt time.Time
	// UndoneAt is when it was undone, or zero while it stands.
	UndoneAt time.Time
}

// Undone reports whether p was undone.
func (p Piece) Undone() bool { return !p.UndoneAt.IsZero() }

// KeptPiece is a piece of Feedback as the Record keeps it, with what it is
// about worked out from the Record by the same lookups the things' own
// pages use, never stored with the piece.
type KeptPiece struct {
	Piece
	// What names what it is about: a judgement's words ("“the city” refers
	// to “Derby”"), a statement's text, or a Span's words.
	What string
	// At is the Span it lands on: the Span judged (a reference's, a
	// speaker's, a relation's later-published, a gathering's), or the Span
	// itself; zero for a statement and a look-up. To is a relation's other
	// Span, and a reference's or a speaker's other stretch.
	At, To Span
	// Name, NameKind, Office and On are the key a look-up was made for.
	Name, NameKind, Office, On string
	// Calls are the kept calls behind what it is about: the reading behind
	// a reference, a speaker or a Span missed; the relating behind a
	// relation; the Briefing behind a statement, a gathering or a Span told
	// unwanted. A look-up is plain code, with no call.
	Calls []Call
}

// Call is a kept model call, by the act that made it.
type Call struct {
	Act string
	ID  int64
}

// Ask is a piece of Feedback as a page's form sends it, not yet validated:
// the kind and the judgement's id, or the Span ("start-end") of the Item
// (Source and entry); Day is the Briefing the page shows, if it shows one.
type Ask struct {
	Kind, Judgement   string
	SourceID, EntryID string
	Span, Day         string
}

// PieceStore is the part of the Record that keeps Feedback as events.
type PieceStore interface {
	// JudgementKind is the kind of the event id, reporting whether the
	// Record holds it.
	JudgementKind(ctx context.Context, id int64) (Judged, bool, error)
	// SpanInItem reports whether s lies within its Item's text.
	SpanInItem(ctx context.Context, s Span) (bool, error)
	// ToldIn reports whether the day's Briefing has a statement citing s.
	ToldIn(ctx context.Context, day string, s Span) (bool, error)
	// GivePiece keeps p as given, returning its id.
	GivePiece(ctx context.Context, p Piece) (int64, error)
	// Piece is the piece with id, reporting whether it is in the Record.
	Piece(ctx context.Context, id int64) (Piece, bool, error)
	// UndoPiece keeps the undoing of the piece id, at at.
	UndoPiece(ctx context.Context, id int64, at time.Time) error
}

// Pieces is the Owner's Feedback commands on judgements and Spans.
type Pieces struct {
	store PieceStore
}

// NewPieces builds the commands over their store. Nothing here calls a
// model.
func NewPieces(store PieceStore) *Pieces { return &Pieces{store: store} }

// Refusals for Feedback the design doesn't give where it was asked for.
const (
	refusedJudged   = "That Feedback can't be given on that."
	refusedUnwanted = "Unwanted is given on what a Briefing told, from its page."
	refusedMissed   = "Missed is given on a Span, from its page or its Item's."
)

// Give keeps the Owner's Feedback as given, at now. Feedback the design
// doesn't give where it was asked for keeps nothing and returns its Errors;
// a judgement or a Span not in the Record is ErrNotInRecord.
func (s *Pieces) Give(ctx context.Context, a Ask, now time.Time) (Piece, Errors, error) {
	p := Piece{Kind: Kind(a.Kind), Day: a.Day, GivenAt: now}
	switch p.Kind {
	case Wrong, NewToMe:
		id, err := strconv.ParseInt(strings.TrimSpace(a.Judgement), 10, 64)
		if err != nil || id <= 0 {
			return p, Errors{"": refusedJudged}, nil
		}
		kind, found, err := s.store.JudgementKind(ctx, id)
		if err != nil {
			return p, nil, err
		}
		if !found {
			return p, nil, ErrNotInRecord
		}
		if !slices.Contains(judgedTakes[p.Kind], kind) {
			return p, Errors{"": refusedJudged}, nil
		}
		p.Judgement, p.Judged, p.Day = id, kind, ""
	case Unwanted, Missed:
		sp, ok := spanOf(a)
		switch {
		case !ok:
			return p, Errors{"": refusedJudged}, nil
		case p.Kind == Unwanted && a.Day == "":
			return p, Errors{"": refusedUnwanted}, nil
		case p.Kind == Missed && a.Day != "":
			return p, Errors{"": refusedMissed}, nil
		}
		p.Span = sp
		in, err := s.store.SpanInItem(ctx, sp)
		if err != nil {
			return p, nil, err
		}
		if !in {
			return p, nil, ErrNotInRecord
		}
		if p.Kind == Unwanted {
			told, err := s.store.ToldIn(ctx, a.Day, sp)
			if err != nil {
				return p, nil, err
			}
			if !told {
				return p, Errors{"": refusedUnwanted}, nil
			}
		}
	default:
		return p, Errors{"": refusedJudged}, nil
	}
	id, err := s.store.GivePiece(ctx, p)
	p.ID = id
	return p, nil, err
}

// spanOf is the Span a names, if it names one.
func spanOf(a Ask) (Span, bool) {
	start, end, ok := strings.Cut(a.Span, "-")
	if !ok || a.SourceID == "" || a.EntryID == "" {
		return Span{}, false
	}
	s, err1 := strconv.Atoi(start)
	e, err2 := strconv.Atoi(end)
	if err1 != nil || err2 != nil || s < 0 || s >= e {
		return Span{}, false
	}
	return Span{SourceID: a.SourceID, EntryID: a.EntryID, Start: s, End: e}, true
}

// Undo undoes the piece id, at now, keeping the undoing as an event of its
// own: the piece stays, marked undone, and stops standing. Undoing a piece
// already undone changes nothing; a piece not in the Record is
// ErrNotInRecord.
func (s *Pieces) Undo(ctx context.Context, id int64, now time.Time) (Piece, error) {
	p, found, err := s.store.Piece(ctx, id)
	if err != nil {
		return p, err
	}
	if !found {
		return p, ErrNotInRecord
	}
	if p.Undone() {
		return p, nil
	}
	if err := s.store.UndoPiece(ctx, id, now); err != nil {
		return p, err
	}
	p.UndoneAt = now
	return p, nil
}
