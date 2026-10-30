package editor

import (
	"time"

	"github.com/samthorold/go-slow/internal/interest"
)

// Precedent is a piece of the Owner's past Feedback, shown to an act when it
// judges a case like the one the Feedback was about (system design: Feedback
// fixes the Record and becomes precedent).
type Precedent struct {
	// FeedbackID is the Feedback's in the Record.
	FeedbackID int64
	// Kind is what the Owner said: missed, unwanted or new to me.
	Kind string
	// Story is the title of the Story the Development is in, if it is on
	// one, and Fact the statement of the Fact it is about.
	Story, Fact string
	// Quote is the Item's own words the Fact was drawn from.
	Quote string
	// Depth is the Depth it was given at; zero if no Topic tied it.
	Depth   interest.Depth
	GivenAt time.Time
}
