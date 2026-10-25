package register_test

import (
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/register"
)

// first is the first day a Briefing was written: a Monday, so each 8-week
// window from it holds 8 Sundays, the first on day 6.
const first = "2026-01-05"

// day is the day n days after first.
func day(n int) string {
	d, _ := time.Parse(time.DateOnly, first)
	return d.AddDate(0, 0, n).Format(time.DateOnly)
}

// days are the n days from day from on, one each.
func days(from, n int) []string {
	var out []string
	for i := range n {
		out = append(out, day(from+i))
	}
	return out
}

// hypothesis is the named hypothesis in the register.
func hypothesis(t *testing.T, hs []register.Hypothesis, name string) register.Hypothesis {
	t.Helper()
	for _, h := range hs {
		if h.Name == name {
			return h
		}
	}
	t.Fatalf("the register has no %q", name)
	return register.Hypothesis{}
}

// useRecord is a Record read on today, with a Briefing opened on each of
// the first opened days of the first window, the first sundays of its
// Sunday reads opened on their own day, and commits on the first commits
// days.
func useRecord(today, opened, sundays, commits int) register.Record {
	r := register.Record{Today: day(today), Location: time.UTC, First: first, BriefingOpens: days(0, opened)}
	for i := range sundays {
		sunday := day(6 + 7*i)
		r.SundayReadOpens = append(r.SundayReadOpens, register.Open{Read: sunday, On: sunday})
	}
	for _, d := range days(0, commits) {
		at, _ := time.Parse(time.DateOnly, d)
		r.Commits = append(r.Commits, register.Commit{Hash: d, At: at.Add(12 * time.Hour)})
	}
	return r
}

func TestUseIsDueUntilTheFirstEightWeeksHaveEnded(t *testing.T) {
	if h := hypothesis(t, register.Read(useRecord(55, 56, 8, 0)), register.Use); h.Status != register.Due || h.Last != nil {
		t.Errorf("on the window's last day: status %s, last %+v; want Due with no reading", h.Status, h.Last)
	}
	if h := hypothesis(t, register.Read(register.Record{Today: day(400)}), register.Use); h.Status != register.Due {
		t.Errorf("with no Briefing ever: status %s, want Due", h.Status)
	}
}

func TestAFreshWindowIsJudgedAlone(t *testing.T) {
	// The first window supports; the second, with nothing opened, refutes.
	r := useRecord(112, 56, 8, 0)
	// A Sunday read of the first window opened only in the second counts for
	// neither.
	r.SundayReadOpens = append(r.SundayReadOpens, register.Open{Read: day(55 - 1), On: day(60)})

	h := hypothesis(t, register.Read(r), register.Use)

	if h.Status != register.Refuted || h.Last.From != day(56) || h.Last.To != day(111) {
		t.Errorf("status %s over %s to %s, want refuted over the second window", h.Status, h.Last.From, h.Last.To)
	}
}

func TestCommitsFallOnDaysInTheOwnersTimeZone(t *testing.T) {
	auckland, err := time.LoadLocation("Pacific/Auckland")
	if err != nil {
		t.Fatal(err)
	}
	// 8 commits on 8 days in UTC fall on 9 days in Auckland: each at 11:00
	// UTC is the next day there, and one more at 09:00 UTC on the first day
	// is still that day there.
	r := useRecord(56, 40, 8, 8)
	r.Location = auckland
	r.Commits = append(r.Commits, register.Commit{Hash: "x", At: time.Date(2026, time.January, 5, 9, 0, 0, 0, time.UTC)})
	for i := range r.Commits[:8] {
		r.Commits[i].At = r.Commits[i].At.Add(-time.Hour)
	}

	if h := hypothesis(t, register.Read(r), register.Use); h.Status != register.Undecided {
		t.Errorf("status %s, want undecided: 9 commit days in Auckland is a busy window", h.Status)
	}
	r.Location = time.UTC
	if h := hypothesis(t, register.Read(r), register.Use); h.Status != register.Supported {
		t.Errorf("status %s, want supported: 8 commit days in UTC is a quiet window", h.Status)
	}
}

func TestUseIsReadOverEachEightWeekWindowByItsDeclaredRule(t *testing.T) {
	for _, c := range []struct {
		name                     string
		opened, sundays, commits int
		want                     register.Status
	}{
		{"fewer than 2 days in 7 refutes", 15, 8, 0, register.Refuted},
		{"2 days in 7 does not refute", 16, 8, 0, register.Undecided},
		{"3 of the 8 Sunday reads refutes", 40, 3, 0, register.Refuted},
		{"4 of the 8 Sunday reads does not refute", 40, 4, 0, register.Undecided},
		{"4 days in 7 and 6 Sunday reads in a quiet window supports", 32, 6, 8, register.Supported},
		{"just under 4 days in 7 does not support", 31, 8, 0, register.Undecided},
		{"5 of the 8 Sunday reads does not support", 40, 5, 0, register.Undecided},
		{"a busy window can't support", 56, 8, 9, register.Undecided},
		{"a busy window can refute", 15, 8, 56, register.Refuted},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := hypothesis(t, register.Read(useRecord(56, c.opened, c.sundays, c.commits)), register.Use)

			if h.Status != c.want || h.Last == nil || h.Last.Status != c.want {
				t.Fatalf("status %s (last %+v), want %s", h.Status, h.Last, c.want)
			}
			if h.Last.From != day(0) || h.Last.To != day(55) {
				t.Errorf("read over %s to %s, want the 8 weeks %s to %s", h.Last.From, h.Last.To, day(0), day(55))
			}
		})
	}
}
