package register

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// The necessary conditions are read on the Record as it stands, on every
// view of the register: any failure refutes, and passing confirms nothing
// about the top.

// readAppear reads "The outputs appear": a Briefing every day and a Sunday
// read every Sunday since the first Briefing, by the set time. Refuted by
// any missed day.
func readAppear(r Record) Hypothesis {
	h := Hypothesis{Name: Appear, Part: Necessary, Status: Due}
	start, ok := date(r.First)
	end, ok2 := date(r.Today)
	if !ok || !ok2 {
		h.Why = "No reading yet: no Briefing has been written or tried."
		return h
	}
	if !r.TodayDue {
		end = end.AddDate(0, 0, -1)
	}
	var briefings, reads []string
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		s := dayString(d)
		if !slices.Contains(r.Briefings, s) {
			briefings = append(briefings, s)
		}
		if d.Weekday() == time.Sunday && !slices.Contains(r.SundayReads, s) {
			reads = append(reads, s)
		}
	}
	rd := Reading{Status: Supported, Found: []string{"Every Briefing and Sunday read appeared since " + r.First + "."}}
	if len(briefings) > 0 || len(reads) > 0 {
		rd.Status, rd.Found = Refuted, nil
		if len(briefings) > 0 {
			rd.Found = append(rd.Found, "No Briefing on "+strings.Join(briefings, ", ")+".")
		}
		if len(reads) > 0 {
			rd.Found = append(rd.Found, "No Sunday read on "+strings.Join(reads, ", ")+".")
		}
	}
	h.Last, h.Status = &rd, rd.Status
	return h
}

// readTraces reads "Everything traces": every statement of a Briefing cites
// a Span in its Item's text (or, in a Briefing over Stories, a Development
// tracing to an Item), and every Fact traces to an Item or a Reference
// source. Refuted by any untraced one.
func readTraces(r Record) Hypothesis {
	h := Hypothesis{Name: Traces, Part: Necessary, Status: Due}
	if len(r.Briefings) == 0 {
		h.Why = "No reading yet: no Briefing has been written."
		return h
	}
	rd := Reading{Status: Supported, Found: []string{"Every statement of a Briefing, Development and Fact traces."}}
	if len(r.Untraced) > 0 {
		rd.Status, rd.Found = Refuted, r.Untraced
	}
	h.Last, h.Status = &rd, rd.Status
	return h
}

// readNotQuiet reads "No Source goes quiet": every Source yields new Items
// within its usual gap. Refuted by any Source silent past it.
func readNotQuiet(r Record) Hypothesis {
	h := Hypothesis{Name: NotQuiet, Part: Necessary, Status: Due}
	if len(r.Sources) == 0 {
		h.Why = "No reading yet: there are no Sources."
		return h
	}
	rd := Reading{Status: Supported, Found: []string{fmt.Sprintf("None of the %d Sources is quiet.", len(r.Sources))}}
	if len(r.QuietSources) > 0 {
		rd.Status, rd.Found = Refuted, []string{"Quiet past its usual gap: " + strings.Join(r.QuietSources, ", ") + "."}
	}
	h.Last, h.Status = &rd, rd.Status
	return h
}
