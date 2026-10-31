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

// Errors are validation failures, by field; "" is for the form as a whole.
type Errors map[string]string

// ErrNotInRecord is the error for Feedback about something not in the
// Record.
var ErrNotInRecord = errors.New("not in the Record")
