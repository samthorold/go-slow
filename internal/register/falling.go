package register

import (
	"fmt"
	"strings"

	"github.com/samthorold/go-slow/internal/feedback"
)

// The shared rule's declared values (system design, Read from Feedback):
// the last 4 weeks against the 4 before, Due every 4 weeks; supported by a
// fall of at least a quarter.
const (
	feedbackWeeks = 4
	feedbackDays  = 7 * feedbackWeeks
)

// levels are the levels each count is broken down by, in the design's
// order.
var levels = []feedback.Level{feedback.Story, feedback.Development, feedback.Fact, feedback.Connection}

// readFalling reads a part whose Feedback of kind should fall.
func readFalling(r Record, name string, kind feedback.Kind) Hypothesis {
	h := Hypothesis{Name: name, Part: FromFeedback, Status: Due}
	ws := windows(r.First, r.Today, feedbackDays)
	if len(ws) < 2 {
		h.Why = "No reading yet: the first 8 weeks since the Briefing began have not ended."
		return h
	}
	// Each review point is read unless the top stands refuted there; the
	// status is the latest point's reading.
	for i := 1; i < len(ws); i++ {
		before, after := ws[i-1], ws[i]
		h.Status, h.Why = Due, ""
		switch {
		case useAt(r, after.to) == Refuted:
			h.Why = "Not read while the top is refuted."
		case countDays(before, r.Briefings) == 0 || countDays(after, r.Briefings) == 0:
			h.Why = "Not read: a window has no Briefings to count Feedback per."
		default:
			last := fallingReading(r, kind, before, after)
			h.Last, h.Status = &last, last.Status
		}
	}
	if h.Last != nil && h.Status != Due && editorChangedOn(r, h.Last.To) {
		h.Status, h.Why = Due, "The Editor has changed since its last reading."
	}
	return h
}

// editorChangedOn reports whether a commit changed the Editor after the day
// last.
func editorChangedOn(r Record, last string) bool {
	for _, c := range r.Commits {
		if c.Editor && dayIn(c.At, r.Location) > last {
			return true
		}
	}
	return false
}

// fallingReading reads kind's rate per Briefing over after against before.
func fallingReading(r Record, kind feedback.Kind, before, after window) Reading {
	was, is := countFeedback(r, kind, before), countFeedback(r, kind, after)
	wasBriefings, isBriefings := countDays(before, r.Briefings), countDays(after, r.Briefings)
	rd := Reading{From: dayString(before.from), To: after.last(), Status: Undecided}
	// Rates compared as fractions: is/isBriefings against was/wasBriefings.
	switch {
	case is.total*wasBriefings > was.total*isBriefings:
		rd.Status = Refuted
	case was.total > 0 && 4*is.total*wasBriefings <= 3*was.total*isBriefings:
		rd.Status = Supported
	}
	rd.Found = []string{
		fmt.Sprintf("Last 4 weeks: %s on %d Briefings.", is, isBriefings),
		fmt.Sprintf("The 4 before: %s on %d Briefings.", was, wasBriefings),
	}
	return rd
}

// counted is Feedback counted, in all and by level.
type counted struct {
	total   int
	byLevel map[feedback.Level]int
}

func (c counted) String() string {
	var parts []string
	for _, l := range levels {
		if n := c.byLevel[l]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", l, n))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return fmt.Sprintf("%d (%s)", c.total, strings.Join(parts, ", "))
}

// countFeedback counts the Feedback of kind given in w.
func countFeedback(r Record, kind feedback.Kind, w window) counted {
	c := counted{byLevel: map[feedback.Level]int{}}
	for _, g := range r.Feedback {
		if g.Kind == kind && w.holds(dayIn(g.At, r.Location)) {
			c.total++
			c.byLevel[g.Level]++
		}
	}
	return c
}
