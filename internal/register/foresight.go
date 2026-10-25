package register

import (
	"fmt"

	"github.com/samthorold/go-slow/internal/feedback"
)

// Foresight's declared values (system design, Foresight): each 4-week
// window, Due every 4 weeks; supported by some new to me Feedback in at
// least 3 of its 4 Sunday reads.
const foresightSupportedReads = 3

func readForesight(r Record) Hypothesis {
	h := Hypothesis{Name: Foresight, Part: FromFeedback, Status: Due}
	ws := windows(r.First, r.Today, feedbackDays)
	if len(ws) == 0 {
		h.Why = "No reading yet: the first 4 weeks since the Briefing began have not ended."
		return h
	}
	for _, w := range ws {
		h.Status, h.Why = Due, ""
		switch {
		case useAt(r, w.to) == Refuted:
			h.Why = "Not read while the top is refuted."
		case countDays(w, r.SundayReads) == 0:
			h.Why = "Not read: the window has no Sunday read."
		default:
			last := foresightReading(r, w)
			h.Last, h.Status = &last, last.Status
		}
	}
	return h
}

// foresightReading reads foresight over w.
func foresightReading(r Record, w window) Reading {
	all := countFeedback(r, feedback.NewToMe, w)
	onReads := map[string]bool{}
	for _, g := range r.Feedback {
		if g.Kind == feedback.NewToMe && g.SundayRead != "" && w.holds(g.SundayRead) && w.holds(dayIn(g.At, r.Location)) {
			onReads[g.SundayRead] = true
		}
	}
	rd := Reading{From: dayString(w.from), To: w.last(), Status: Undecided}
	switch {
	case all.total == 0:
		rd.Status = Refuted
	case len(onReads) >= foresightSupportedReads:
		rd.Status = Supported
	}
	rd.Found = []string{
		fmt.Sprintf("New to me: %s.", all),
		fmt.Sprintf("Some on %d of the %d Sunday reads written.", len(onReads), countDays(w, r.SundayReads)),
	}
	return rd
}
