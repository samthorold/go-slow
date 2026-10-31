// Package feedback is the Owner's Feedback: what they tell the Editor.
// Feedback on judgements and Spans is kept as events (Piece, Pieces: system
// design: Feedback lands on a judgement, so the judge to fix is known;
// Feedback is kept as given, and undoing it is Feedback too). Feedback on a
// Sunday read or its Assessments is kept as it was before the Record became
// events (Feedback, Service), until it lands on judgements too.
package feedback

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Kind is what the Owner says.
type Kind string

// The four kinds of Feedback.
const (
	// Missed: a Span should have been told.
	Missed Kind = "missed"
	// Unwanted: a Span told shouldn't have been.
	Unwanted Kind = "unwanted"
	// Wrong: a judgement is mistaken.
	Wrong Kind = "wrong"
	// NewToMe: something told showed the Owner what they hadn't seen.
	NewToMe Kind = "new to me"
)

// Level is what a piece of the Feedback kept before the Record became
// events is about.
type Level string

// The levels that Feedback was given at. Feedback is no longer given on a
// Story, a Development, a Fact or a Connection, but the Record keeps what
// was given on them, and the register still reads it.
const (
	Story       Level = "story"
	Development Level = "development"
	Fact        Level = "fact"
	Connection  Level = "connection"
	// Assessment is an Assessment a Sunday read reached, and SundayRead the
	// read as a whole.
	Assessment Level = "assessment"
	SundayRead Level = "sunday read"
)

// allowed are the kinds each level still takes. An Assessment is a
// judgement, so it can be wrong; the Sunday read as a whole is new to the
// Owner or not, which is what Foresight reads.
var allowed = map[Level][]Kind{
	Assessment: {Wrong, NewToMe},
	SundayRead: {NewToMe},
}

// Feedback is one piece of the Sunday read's Feedback, as given.
type Feedback struct {
	ID    int64
	Kind  Kind
	Level Level
	// AssessmentID is the Assessment it is about, and ReadID the Sunday
	// read.
	AssessmentID, ReadID int64
	// Day is the Sunday read it was given on (YYYY-MM-DD).
	Day     string
	GivenAt time.Time
	// UndoneAt is when the Owner undid it, or zero. An undone piece is kept
	// as given, and fixes the Record no more.
	UndoneAt time.Time
}

// Undone reports whether the Owner has undone f.
func (f Feedback) Undone() bool { return !f.UndoneAt.IsZero() }

// Kept is a piece of Feedback as the Record keeps it, with what it is about
// named as it stands in the Record.
type Kept struct {
	Feedback
	// Assessment is the Assessment's statement, and ReadDay the Sunday of
	// the read it, or the read the Feedback is about, belongs to.
	Assessment, ReadDay string
	// Conversation is the kept conversation behind an Assessment or a
	// Sunday read, or zero.
	Conversation int64
}

// Draft is the Sunday read's Feedback as the Owner's form sent it, not yet
// validated.
type Draft struct {
	Kind, Level      string
	Assessment, Read string
	Day              string
}

// Errors are validation failures, by field; "" is for the form as a whole.
type Errors map[string]string

// ErrNotInRecord is the error for Feedback about something not in the
// Record.
var ErrNotInRecord = errors.New("not in the Record")

// Store is the part of the Record that keeps the Sunday read's Feedback and
// that it fixes.
type Store interface {
	// About reports whether what f is about is in the Record.
	About(ctx context.Context, f Feedback) (bool, error)
	// Give keeps f as given and applies e to the Record, all at once,
	// returning f's id.
	Give(ctx context.Context, f Feedback, e Effect) (int64, error)
	// Feedback is the piece with id, as given, reporting whether it is in
	// the Record.
	Feedback(ctx context.Context, id int64) (Feedback, bool, error)
	// Undo marks f undone at at and lifts the marks e made, all at once. A
	// mark another piece not undone also makes stays.
	Undo(ctx context.Context, f Feedback, e Effect, at time.Time) error
}

// Service is the Owner's Feedback commands on a Sunday read.
type Service struct {
	store Store
}

// New builds the Service over its store. Nothing here calls a model.
func New(store Store) *Service { return &Service{store: store} }

// Give keeps the Owner's Feedback as given, at now, and fixes the Record by
// it. A Draft that fails validation keeps nothing and returns its Errors;
// one about something not in the Record is ErrNotInRecord.
func (s *Service) Give(ctx context.Context, d Draft, now time.Time) (Feedback, Errors, error) {
	f, errs := validate(d)
	if len(errs) > 0 {
		return f, errs, nil
	}
	f.GivenAt = now
	found, err := s.store.About(ctx, f)
	if err != nil {
		return f, nil, err
	}
	if !found {
		return f, nil, ErrNotInRecord
	}
	f.ID, err = s.store.Give(ctx, f, EffectOf(f))
	return f, nil, err
}

// Undo undoes the piece of Feedback with id, at now: it stays kept as
// given, marked undone with when, and the marks it made on the Record are
// lifted. Undoing a piece already undone changes nothing. A piece not in
// the Record is ErrNotInRecord.
func (s *Service) Undo(ctx context.Context, id int64, now time.Time) (Feedback, error) {
	f, found, err := s.store.Feedback(ctx, id)
	if err != nil {
		return f, err
	}
	if !found {
		return f, ErrNotInRecord
	}
	if f.Undone() {
		return f, nil
	}
	f.UndoneAt = now
	return f, s.store.Undo(ctx, f, EffectOf(f), now)
}

func validate(d Draft) (Feedback, Errors) {
	errs := Errors{}
	f := Feedback{Kind: Kind(d.Kind), Level: Level(d.Level), Day: d.Day}
	kinds, ok := allowed[f.Level]
	if !ok || !slices.Contains(kinds, f.Kind) {
		errs[""] = "That Feedback can't be given on that."
		return f, errs
	}
	id := func(field, s string) int64 {
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil || n <= 0 {
			errs[field] = "Choose one."
		}
		return n
	}
	switch f.Level {
	case Assessment:
		f.AssessmentID = id("assessment", d.Assessment)
	case SundayRead:
		f.ReadID = id("read", d.Read)
	}
	return f, errs
}

// Effect is how a piece of the Sunday read's Feedback fixes the Record. It
// is worked out from the Feedback alone, so a changed mechanism can be run
// again over all the Feedback kept.
type Effect struct {
	// WithdrawAssessment is a wrong Assessment, withdrawn with the
	// statements of the reads that cite it.
	WithdrawAssessment int64
}

// EffectOf is how f fixes the Record. *New to me* changes nothing.
func EffectOf(f Feedback) Effect {
	var e Effect
	if f.Kind == Wrong && f.Level == Assessment {
		e.WithdrawAssessment = f.AssessmentID
	}
	return e
}
