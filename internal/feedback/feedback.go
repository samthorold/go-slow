// Package feedback is the Owner's Feedback: what they tell the Editor about
// a Story, Development, Fact or Connection, or a Sunday read or its
// Assessments. Each piece is kept as given
// (what was said, about what, at what Depth, when) and then fixes the
// Record (system design: Feedback is kept as given, apart from how it is
// applied; Feedback fixes the Record and becomes precedent).
package feedback

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/samthorold/go-slow/internal/interest"
)

// Kind is what the Owner says.
type Kind string

// The four kinds of Feedback.
const (
	// Missed: it should be there.
	Missed Kind = "missed"
	// Unwanted: it shouldn't.
	Unwanted Kind = "unwanted"
	// Wrong: it is mistaken.
	Wrong Kind = "wrong"
	// NewToMe: it showed the Owner something they hadn't seen.
	NewToMe Kind = "new to me"
)

// Level is what the Feedback is about.
type Level string

// The levels Feedback is given at.
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

// allowed are the kinds each level takes. *Wrong* is about a proposition
// (a Fact, or a Connection between Facts), so a Story or a Development is
// never wrong: a Development that should not be one is unwanted. *Missed*
// adds a Story or a Development; a missed Fact or Connection would need the
// Owner to write it, which is the Editor's work. Only a Story or a
// Development is in the Briefing to be unwanted. An Assessment is a
// judgement, so it can be wrong; the Sunday read as a whole is new to the
// Owner or not, which is what Foresight reads, and its statements are
// mended through what they cite.
var allowed = map[Level][]Kind{
	Story:       {Missed, Unwanted, NewToMe},
	Development: {Missed, Unwanted, NewToMe},
	Fact:        {Wrong, NewToMe},
	Connection:  {Wrong, NewToMe},
	Assessment:  {Wrong, NewToMe},
	SundayRead:  {NewToMe},
}

// Feedback is one piece of Feedback, as given.
type Feedback struct {
	ID    int64
	Kind  Kind
	Level Level
	// StoryID is the Story it is about, or the Story a Development is in.
	// It is zero for a missed Story, which is not in the Record yet.
	StoryID int64
	// FactID is the Fact it is about, a Development's Fact, or a
	// Connection's cause.
	FactID int64
	// ToFactID is a Connection's effect.
	ToFactID int64
	// AssessmentID is the Assessment it is about, and ReadID the Sunday
	// read.
	AssessmentID, ReadID int64
	// Title is a missed Story's, as the Owner typed it.
	Title string
	// FactIDs are the Facts a missed Story gathers, as its Developments.
	FactIDs []int64
	// Day is the Briefing it was given on (YYYY-MM-DD), if any.
	Day string
	// Depth is the closest Depth of the Topics that tie what it is about, as
	// it was when the Feedback was given; zero if no Topic ties it.
	Depth   interest.Depth
	GivenAt time.Time
	// UndoneAt is when the Owner undid it, or zero. An undone piece is kept
	// as given, and fixes the Record no more.
	UndoneAt time.Time
}

// Undone reports whether the Owner has undone f.
func (f Feedback) Undone() bool { return !f.UndoneAt.IsZero() }

// Kept is a piece of Feedback as the Record keeps it, with what it is about
// named as it stands in the Record: the Story's title, and each Fact's
// statement.
type Kept struct {
	Feedback
	Story, Fact, ToFact string
	// Assessment is the Assessment's statement, and ReadDay the Sunday of
	// the read it, or the read the Feedback is about, belongs to.
	Assessment, ReadDay string
}

// Draft is Feedback as the Owner's form sent it, not yet validated.
type Draft struct {
	Kind, Level string
	// Story, Fact and ToFact are ids, as typed.
	Story, Fact, ToFact string
	Assessment, Read    string
	Title               string
	Facts               []string
	Day                 string
}

// Errors are a Draft's validation failures, by field; "" is for the form as
// a whole.
type Errors map[string]string

// ErrNotInRecord is the error for Feedback about something not in the
// Record.
var ErrNotInRecord = errors.New("not in the Record")

// Store is the part of the Record that keeps Feedback and that Feedback
// fixes.
type Store interface {
	// About reports whether what f is about is in the Record (for a missed
	// Story, whether each of its Facts is), whether a missed Development
	// is already one, and the closest Depth of the Topics that tie it.
	About(ctx context.Context, f Feedback) (About, error)
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

// About is what the Record holds of what Feedback is about.
type About struct {
	Found bool
	// Developed is whether a missed Development's Fact already is one in
	// its Story.
	Developed bool
	Depth     interest.Depth
}

// Service is the Owner's Feedback commands.
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
	about, err := s.store.About(ctx, f)
	if err != nil {
		return f, nil, err
	}
	if !about.Found {
		return f, nil, ErrNotInRecord
	}
	if f.Kind == Missed && f.Level == Development && about.Developed {
		return f, Errors{"fact": "That Fact is already a Development in this Story."}, nil
	}
	f.Depth = about.Depth
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
	f := Feedback{Kind: Kind(d.Kind), Level: Level(d.Level), Title: strings.TrimSpace(d.Title), Day: d.Day}
	kinds, ok := allowed[f.Level]
	if !ok || !slices.Contains(kinds, f.Kind) {
		errs[""] = "That Feedback can't be given on that."
		return f, errs
	}
	id := func(field, s string, need bool) int64 {
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if (err != nil || n <= 0) && (need || strings.TrimSpace(s) != "") {
			errs[field] = "Choose one."
		}
		return n
	}
	switch f.Level {
	case Assessment:
		f.AssessmentID = id("assessment", d.Assessment, true)
		return f, errs
	case SundayRead:
		f.ReadID = id("read", d.Read, true)
		return f, errs
	}
	newStory := f.Kind == Missed && f.Level == Story
	f.StoryID = id("story", d.Story, (f.Level == Story && !newStory) || f.Level == Development)
	f.FactID = id("fact", d.Fact, f.Level != Story)
	if f.Level == Connection {
		f.ToFactID = id("to_fact", d.ToFact, true)
	}
	if newStory {
		f.StoryID = 0
		if f.Title == "" {
			errs["title"] = "Give the missed Story a title."
		}
		for _, s := range d.Facts {
			if n := id("facts", s, true); n > 0 && !slices.Contains(f.FactIDs, n) {
				f.FactIDs = append(f.FactIDs, n)
			}
		}
	}
	return f, errs
}

// Effect is how a piece of Feedback fixes the Record. It is worked out from
// the Feedback alone, so a changed mechanism can be run again over all the
// Feedback kept.
type Effect struct {
	// NewStory is a missed Story to start, with its Developments.
	NewStory *NewStory
	// AddDevelopment is a missed Development: a Fact in a Story.
	AddDevelopment *Pair
	// UnwantedStory and UnwantedDevelopment are taken out of the Briefing.
	UnwantedStory       int64
	UnwantedDevelopment *Pair
	// WithdrawFact is a wrong Fact, withdrawn with whatever was built on it:
	// its Developments and its Connections.
	WithdrawFact int64
	// WithdrawConnection is a wrong Connection: cause, then effect.
	WithdrawConnection *Pair
	// WithdrawAssessment is a wrong Assessment, withdrawn with the
	// statements of the reads that cite it.
	WithdrawAssessment int64
}

// NewStory is a Story the Owner said was missed.
type NewStory struct {
	Title   string
	FactIDs []int64
}

// Pair is two ids: a Story and a Fact, or two Facts.
type Pair struct{ A, B int64 }

// EffectOf is how f fixes the Record. *New to me* changes nothing.
func EffectOf(f Feedback) Effect {
	var e Effect
	switch {
	case f.Kind == Missed && f.Level == Story:
		e.NewStory = &NewStory{Title: f.Title, FactIDs: f.FactIDs}
	case f.Kind == Missed && f.Level == Development:
		e.AddDevelopment = &Pair{f.StoryID, f.FactID}
	case f.Kind == Unwanted && f.Level == Story:
		e.UnwantedStory = f.StoryID
	case f.Kind == Unwanted && f.Level == Development:
		e.UnwantedDevelopment = &Pair{f.StoryID, f.FactID}
	case f.Kind == Wrong && f.Level == Fact:
		e.WithdrawFact = f.FactID
	case f.Kind == Wrong && f.Level == Connection:
		e.WithdrawConnection = &Pair{f.FactID, f.ToFactID}
	case f.Kind == Wrong && f.Level == Assessment:
		e.WithdrawAssessment = f.AssessmentID
	}
	return e
}
