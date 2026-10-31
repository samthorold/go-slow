package register

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
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

// readFalling reads a part whose Feedback of kind should fall.
func readFalling(r Record, name string, kind feedback.Kind) Hypothesis {
	h := Hypothesis{Name: name, Part: FromFeedback, Status: Due}
	ws := windows(fallingFrom(r), r.Today, feedbackDays)
	if len(ws) < 2 {
		h.Why = "No reading yet: the first 8 weeks since the Briefing began have not ended."
		if !r.Switch.IsZero() {
			h.Why = "No reading yet: the first 8 weeks wholly after the switch to the event-based Record have not ended."
		}
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
	if h.Last != nil && h.Status != Due {
		if editorChangedOn(r, h.Last.To) {
			h.Status, h.Why = Due, "The Editor has changed since its last reading."
		} else if j, ok := newJudgeOn(r, h.Last.To); ok {
			h.Status, h.Why = Due, "A new Judge, "+j.String()+", has judged since its last reading."
		}
	}
	return h
}

// newJudgeOn is the first Judge that began judging after the day last, if
// one has.
func newJudgeOn(r Record, last string) (Judge, bool) {
	for _, j := range r.Judges {
		if dayIn(j.From, r.Location) > last {
			return j, true
		}
	}
	return Judge{}, false
}

// fallingFrom is the first day of the falling parts' windows: the first
// Briefing's, or the first whole day after the switch if that is later,
// since each kind of Feedback lands on different things either side of it.
func fallingFrom(r Record) string {
	if r.Switch.IsZero() || r.First == "" {
		return r.First
	}
	d, _ := date(dayIn(r.Switch, r.Location))
	after := dayString(d.AddDate(0, 0, 1))
	return max(after, r.First)
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

// counted is Feedback counted, in all and by what it was given on.
type counted struct {
	total int
	byOn  map[string]int
}

// String is the count, then each part of it, the largest first.
func (c counted) String() string {
	if c.total == 0 {
		return "none"
	}
	ons := slices.Collect(maps.Keys(c.byOn))
	slices.SortFunc(ons, func(a, b string) int {
		return cmp.Or(cmp.Compare(c.byOn[b], c.byOn[a]), cmp.Compare(a, b))
	})
	parts := make([]string, len(ons))
	for i, on := range ons {
		parts[i] = fmt.Sprintf("%s %d", on, c.byOn[on])
	}
	return fmt.Sprintf("%d (%s)", c.total, strings.Join(parts, ", "))
}

// countFeedback counts the Feedback of kind given in w.
func countFeedback(r Record, kind feedback.Kind, w window) counted {
	c := counted{byOn: map[string]int{}}
	for _, g := range r.Feedback {
		if g.Kind == kind && w.holds(dayIn(g.At, r.Location)) {
			c.total++
			c.byOn[g.On]++
		}
	}
	return c
}
