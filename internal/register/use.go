package register

import (
	"fmt"
	"time"
)

// The top's declared values (system design, End state): a window of 8
// weeks, in which a Briefing opened on fewer than 2 days in 7, or 3 or fewer
// of its 8 Sunday reads opened, refutes; and in a quiet window (commits on no
// more than 1 day in 7), a Briefing opened on at least 4 days in 7 and at
// least 6 of the 8 Sunday reads opened supports. Averages over the window
// are counts over its 56 days.
const (
	useWeeks          = 8
	useDays           = 7 * useWeeks
	useRefutedBelow   = 2 * useWeeks // days with a Briefing opened
	useRefutedReadsAt = 3            // Sunday reads opened, or fewer
	useSupportedFrom  = 4 * useWeeks // days with a Briefing opened
	useSupportedReads = 6            // Sunday reads opened, or more
	quietCommitDays   = 1 * useWeeks // days with a commit, or fewer
)

func readUse(r Record) Hypothesis {
	h := Hypothesis{Name: Use, Part: Top, Status: Due}
	ws := windows(r.First, r.Today, useDays)
	if len(ws) == 0 {
		h.Why = "No reading yet: the first 8-week window since the Briefing began has not ended."
		return h
	}
	last := useReading(r, ws[len(ws)-1])
	h.Last, h.Status = &last, last.Status
	return h
}

// useAt is the top's status as it stood at the start of the day at: its
// reading over the latest window ended by then, or Due if none has.
func useAt(r Record, at time.Time) Status {
	ws := windows(r.First, dayString(at), useDays)
	if len(ws) == 0 {
		return Due
	}
	return useReading(r, ws[len(ws)-1]).Status
}

// useReading reads the top over w.
func useReading(r Record, w window) Reading {
	opened := countDays(w, r.BriefingOpens)
	reads := map[string]bool{}
	for _, o := range r.SundayReadOpens {
		if sunday(o.Read) && w.holds(o.Read) && o.On < dayString(w.to) {
			reads[o.Read] = true
		}
	}
	var commitDays []string
	for _, c := range r.Commits {
		commitDays = append(commitDays, dayIn(c.At, r.Location))
	}
	commits := countDays(w, commitDays)
	quiet := commits <= quietCommitDays

	rd := Reading{From: dayString(w.from), To: w.last(), Status: Undecided}
	switch {
	case opened < useRefutedBelow || len(reads) <= useRefutedReadsAt:
		rd.Status = Refuted
	case quiet && opened >= useSupportedFrom && len(reads) >= useSupportedReads:
		rd.Status = Supported
	}
	window := "busy"
	if quiet {
		window = "quiet"
	}
	rd.Found = []string{
		fmt.Sprintf("A Briefing opened on %d of %d days.", opened, useDays),
		fmt.Sprintf("%d of the %d Sunday reads opened.", len(reads), useWeeks),
		fmt.Sprintf("Commits on %d days: a %s window.", commits, window),
	}
	return rd
}

func sunday(day string) bool {
	d, ok := date(day)
	return ok && d.Weekday() == time.Sunday
}
