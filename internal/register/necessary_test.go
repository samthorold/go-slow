package register_test

import (
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/register"
)

// appearing is a Record read on day 13, a Sunday, after the set time, with
// every Briefing and Sunday read written.
func appearing() register.Record {
	return register.Record{Today: day(13), TodayDue: true, Location: time.UTC, First: first,
		Briefings: days(0, 14), SundayReads: []string{day(6), day(13)}}
}

func TestTheOutputsAppearIsRefutedByAnyMissedDay(t *testing.T) {
	if h := hypothesis(t, register.Read(appearing()), register.Appear); h.Status != register.Supported {
		t.Errorf("every output written: status %s (%+v), want supported", h.Status, h.Last)
	}

	missedBriefing := appearing()
	missedBriefing.Briefings = append(days(0, 3), days(4, 10)...)
	h := hypothesis(t, register.Read(missedBriefing), register.Appear)
	if h.Status != register.Refuted || !strings.Contains(strings.Join(h.Last.Found, " "), day(3)) {
		t.Errorf("a missed Briefing: status %s (%+v), want refuted, naming the day", h.Status, h.Last)
	}

	missedRead := appearing()
	missedRead.SundayReads = []string{day(13)}
	h = hypothesis(t, register.Read(missedRead), register.Appear)
	if h.Status != register.Refuted || !strings.Contains(strings.Join(h.Last.Found, " "), day(6)) {
		t.Errorf("a missed Sunday read: status %s (%+v), want refuted, naming the Sunday", h.Status, h.Last)
	}
}

func TestTodaysOutputsAreNotMissedBeforeTheSetTime(t *testing.T) {
	r := appearing()
	r.TodayDue = false
	r.Briefings, r.SundayReads = days(0, 13), []string{day(6)}

	if h := hypothesis(t, register.Read(r), register.Appear); h.Status != register.Supported {
		t.Errorf("status %s (%+v), want supported: today's outputs are not due yet", h.Status, h.Last)
	}
}

func TestTheNecessaryConditionsAreDueWithNothingToRead(t *testing.T) {
	r := register.Record{Today: day(0), Location: time.UTC}
	for _, name := range []string{register.Appear, register.Traces, register.NotQuiet} {
		if h := hypothesis(t, register.Read(r), name); h.Status != register.Due {
			t.Errorf("%s: status %s, want Due", name, h.Status)
		}
	}
}

func TestEverythingTracesIsRefutedByAnyUntracedOne(t *testing.T) {
	r := appearing()
	if h := hypothesis(t, register.Read(r), register.Traces); h.Status != register.Supported {
		t.Errorf("nothing untraced: status %s, want supported", h.Status)
	}
	r.Untraced = []string{"Development 4 in the Briefing of " + day(2) + " traces to no Item."}
	if h := hypothesis(t, register.Read(r), register.Traces); h.Status != register.Refuted || len(h.Last.Found) != 1 {
		t.Errorf("one untraced: status %s (%+v), want refuted, naming it", h.Status, h.Last)
	}
}

func TestNoSourceGoesQuietIsRefutedByAnyQuietSource(t *testing.T) {
	r := appearing()
	r.Sources = []string{"GOV.UK news", "ONS releases"}
	if h := hypothesis(t, register.Read(r), register.NotQuiet); h.Status != register.Supported {
		t.Errorf("none quiet: status %s, want supported", h.Status)
	}
	r.QuietSources = []string{"ONS releases"}
	h := hypothesis(t, register.Read(r), register.NotQuiet)
	if h.Status != register.Refuted || !strings.Contains(strings.Join(h.Last.Found, " "), "ONS releases") {
		t.Errorf("one quiet: status %s (%+v), want refuted, naming it", h.Status, h.Last)
	}
}
