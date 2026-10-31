// Package register is the register: the status of every hypothesis of the
// end state, each derived from the Record by the reading rule the system
// design declares for it, never written by hand (system design, End state).
// Status lives here and nowhere else.
package register

import (
	"time"

	"github.com/samthorold/go-slow/internal/feedback"
)

// Status is where a hypothesis stands.
type Status string

// The four statuses.
const (
	Supported Status = "supported"
	Refuted   Status = "refuted"
	Undecided Status = "undecided"
	// Due is a hypothesis that needs a reading: it has none under its rule,
	// its review interval has passed, or what it was read on has changed.
	Due Status = "Due"
)

// Part is where a hypothesis hangs in the end state.
type Part string

// The parts of the end state.
const (
	Top          Part = "top"
	Necessary    Part = "necessary condition"
	FromFeedback Part = "read from Feedback"
)

// Hypothesis is one hypothesis as the register shows it: its status, its
// last reading, and why it is Due when it is.
type Hypothesis struct {
	Name   string
	Part   Part
	Status Status
	// Last is the latest reading, if there has been one.
	Last *Reading
	// Why says why the hypothesis is Due.
	Why string
}

// Reading is one reading: the window it covers, by its rule, and what it
// found.
type Reading struct {
	// From and To are the first and last days the reading covers
	// (YYYY-MM-DD); both empty for a reading of the Record as it stands.
	From, To string
	Status   Status
	// Found is what the reading counted, in words.
	Found []string
}

// Record is what the register reads from the Record. Days are YYYY-MM-DD,
// in the Owner's time zone.
type Record struct {
	// Today is the day the register is read on.
	Today string
	// TodayDue is whether today's outputs are past the time they are due
	// by.
	TodayDue bool
	// Location is the Owner's time zone, in which times fall on days.
	Location *time.Location
	// First is the first day a Briefing was written or tried, from which
	// the Briefing runs daily and the windows are counted; empty if none
	// ever was.
	First string
	// BriefingOpens are the days a Briefing was opened on.
	BriefingOpens []string
	// SundayReadOpens are the Sunday reads opened, and the days they were
	// opened on.
	SundayReadOpens []Open
	// Briefings are the days a Briefing was written for.
	Briefings []string
	// SundayReads are the days a Sunday read was written for.
	SundayReads []string
	// Feedback is every piece of Feedback, as given.
	Feedback []Given
	// Commits are the commits to go-slow.
	Commits []Commit
	// Untraced names each statement or Assessment that traces to nothing.
	Untraced []string
	// Sources are the Sources' names, and QuietSources those gone quiet
	// past their usual gap.
	Sources, QuietSources []string
}

// Open is an output opened: which (its day), and the day it was opened on.
type Open struct {
	Read, On string
}

// Given is a piece of Feedback as the register counts it.
type Given struct {
	Kind  feedback.Kind
	Level feedback.Level
	At    time.Time
	// SundayRead is the day of the Sunday read it was given on, if it was.
	SundayRead string
}

// Commit is a commit to go-slow, as git records it.
type Commit struct {
	Hash string
	At   time.Time
	// Editor is whether it changes the Editor, including how Feedback is
	// applied.
	Editor bool
}

// The hypotheses, as the system design names them.
const (
	Use       = "The Owner keeps reading the Briefing and the Sunday read."
	Appear    = "The outputs appear."
	Traces    = "Everything traces."
	NotQuiet  = "No Source goes quiet."
	Coverage  = "Coverage: the Editor misses less"
	Brevity   = "Brevity: the Editor includes less that isn't wanted"
	Accuracy  = "Accuracy: the Editor gets less wrong"
	Foresight = "Foresight: the Editor shows the Owner what they hadn't seen"
)

// Read derives the register from the Record.
func Read(r Record) []Hypothesis {
	return []Hypothesis{
		readUse(r),
		readAppear(r), readTraces(r), readNotQuiet(r),
		readFalling(r, Coverage, feedback.Missed), readFalling(r, Brevity, feedback.Unwanted), readFalling(r, Accuracy, feedback.Wrong),
		readForesight(r),
	}
}
