// Package feedback is the Owner's Feedback: what they tell the Editor,
// kept as events (Piece, Pieces: system design: Feedback lands on a
// judgement, so the judge to fix is known; Feedback is kept as given, and
// undoing it is Feedback too).
package feedback

import (
	"errors"
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

// Level is what the register breaks Feedback down by. The first four are
// the old design's, on which no Feedback is given and the Record keeps none
// now; the register counts nothing at them until it is restated over the
// events.
type Level string

// The levels the register counts Feedback at.
const (
	Story       Level = "story"
	Development Level = "development"
	Fact        Level = "fact"
	Connection  Level = "connection"
	// OnAssessment is an Assessment a Sunday read reached, and OnSundayRead
	// the read as a whole: Feedback on the read given before it told Spans,
	// and, as the register counts it, Feedback on the read given since.
	OnAssessment Level = "assessment"
	OnSundayRead Level = "sunday read"
)

// Errors are validation failures, by field; "" is for the form as a whole.
type Errors map[string]string

// ErrNotInRecord is the error for Feedback about something not in the
// Record.
var ErrNotInRecord = errors.New("not in the Record")
