package register_test

import (
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/feedback"
	"github.com/samthorold/go-slow/internal/register"
)

// given is n pieces of Feedback of the kind, given on on (the judgement's
// act, or the Span's Source), on day d (at noon).
func given(n int, kind feedback.Kind, on, d string) []register.Given {
	at, _ := time.Parse(time.DateOnly, d)
	var out []register.Given
	for range n {
		out = append(out, register.Given{Kind: kind, On: on, At: at.Add(12 * time.Hour)})
	}
	return out
}

// feedbackRecord is a Record read on day 56 with a Briefing written and
// opened every day, so that the top's first window does not refute.
func feedbackRecord() register.Record {
	r := register.Record{Today: day(56), Location: time.UTC, First: first, Briefings: days(0, 56)}
	opened(&r, 0, 56)
	return r
}

func TestTheFeedbackPartsThatShouldFallAreReadByTheSharedRule(t *testing.T) {
	for _, c := range []struct {
		name          string
		before, after int
		// afterBriefings is how many Briefings the last 4 weeks had, of 28.
		afterBriefings int
		want           register.Status
	}{
		{"a rate that rose refutes", 4, 5, 28, register.Refuted},
		{"a rate that held is undecided", 4, 4, 28, register.Undecided},
		{"a fall by a quarter supports", 4, 3, 28, register.Supported},
		{"a fall by less than a quarter is undecided", 5, 4, 28, register.Undecided},
		{"none in either is undecided", 0, 0, 28, register.Undecided},
		{"some after none refutes", 0, 1, 28, register.Refuted},
		{"the rate is per Briefing", 4, 3, 14, register.Refuted},
	} {
		for _, part := range []struct {
			name string
			kind feedback.Kind
		}{{register.Coverage, feedback.Missed}, {register.Brevity, feedback.Unwanted}, {register.Accuracy, feedback.Wrong}} {
			t.Run(c.name+"/"+part.name, func(t *testing.T) {
				r := feedbackRecord()
				r.Briefings = append(days(0, 28), days(28, c.afterBriefings)...)
				r.Feedback = append(given(c.before, part.kind, "reading", day(10)),
					given(c.after, part.kind, "reading", day(40))...)
				// Feedback of the other kinds counts for none of this part.
				r.Feedback = append(r.Feedback, given(9, feedback.NewToMe, "briefing", day(40))...)

				h := hypothesis(t, register.Read(r), part.name)

				if h.Status != c.want || h.Last == nil {
					t.Fatalf("status %s, last %+v; want %s", h.Status, h.Last, c.want)
				}
				if h.Last.From != day(0) || h.Last.To != day(55) {
					t.Errorf("read over %s to %s, want the last 4 weeks against the 4 before, %s to %s",
						h.Last.From, h.Last.To, day(0), day(55))
				}
			})
		}
	}
}

func TestEachFeedbackCountIsBrokenDownByWhatItWasGivenOn(t *testing.T) {
	r := feedbackRecord()
	r.Feedback = append(given(1, feedback.Wrong, "relating", day(30)), given(2, feedback.Wrong, "reading", day(31))...)
	r.Feedback = append(r.Feedback, given(1, feedback.Missed, "parliament", day(30))...)
	r.Feedback = append(r.Feedback, given(2, feedback.Missed, "gov-uk", day(31))...)

	for _, c := range []struct{ part, want string }{
		{register.Accuracy, "3 (reading 2, relating 1)"},
		{register.Coverage, "3 (gov-uk 2, parliament 1)"},
	} {
		h := hypothesis(t, register.Read(r), c.part)
		if found := strings.Join(h.Last.Found, "\n"); !strings.Contains(found, c.want) {
			t.Errorf("%s: want the count broken down as %q:\n%s", c.part, c.want, found)
		}
	}
}

// opened has a Briefing opened every day and every Sunday read opened, over
// the days [from, from+n).
func opened(r *register.Record, from, n int) {
	r.BriefingOpens = append(r.BriefingOpens, days(from, n)...)
	for _, d := range days(from, n) {
		if t, _ := time.Parse(time.DateOnly, d); t.Weekday() == time.Sunday {
			r.SundayReadOpens = append(r.SundayReadOpens, register.Open{Read: d, On: d})
		}
	}
}

var fallingParts = []string{register.Coverage, register.Brevity, register.Accuracy}

func TestTheFeedbackPartsAreNotReadWhileTheTopIsRefuted(t *testing.T) {
	// The top's first window, days 0 to 55, refutes: nothing was opened.
	r := feedbackRecord()
	r.Today = day(84)
	r.Briefings = days(0, 84)
	r.BriefingOpens, r.SundayReadOpens = nil, nil

	for _, name := range fallingParts {
		h := hypothesis(t, register.Read(r), name)
		if h.Status != register.Due || h.Last != nil || !strings.Contains(h.Why, "refuted") {
			t.Errorf("%s: status %s (%s), last %+v; want Due, not read while the top is refuted", name, h.Status, h.Why, h.Last)
		}
	}
}

func TestAFeedbackPartKeepsItsLastReadingAndIsDueOnceTheTopIsRefuted(t *testing.T) {
	// The top's first window supports; its second, days 56 to 111, refutes.
	r := feedbackRecord()
	r.Today = day(112)
	r.Briefings = days(0, 112)

	h := hypothesis(t, register.Read(r), register.Coverage)

	if h.Status != register.Due || h.Last == nil || h.Last.To != day(83) {
		t.Errorf("status %s, last %+v; want Due, with the reading of days 28 to 83 kept", h.Status, h.Last)
	}
}

func TestTheSharedRuleIsDueOnAChangeToTheEditorSinceItsReading(t *testing.T) {
	r := feedbackRecord()
	at := func(n int) time.Time { d, _ := time.Parse(time.DateOnly, day(n)); return d.Add(9 * time.Hour) }
	// A change to anything else, and a change to the Editor before the
	// reading, leave the reading standing.
	r.Commits = []register.Commit{{Hash: "a", At: at(56)}, {Hash: "b", At: at(55), Editor: true}}
	for _, name := range fallingParts {
		if h := hypothesis(t, register.Read(r), name); h.Status == register.Due {
			t.Errorf("%s is Due (%s) with no change to the Editor since its reading", name, h.Why)
		}
	}

	r.Commits = append(r.Commits, register.Commit{Hash: "c", At: at(56), Editor: true})
	for _, name := range fallingParts {
		if h := hypothesis(t, register.Read(r), name); h.Status != register.Due || h.Last == nil {
			t.Errorf("%s: status %s, last %+v; want Due after a change to the Editor, its reading kept", name, h.Status, h.Last)
		}
	}
	if h := hypothesis(t, register.Read(r), register.Foresight); strings.Contains(h.Why, "Editor") {
		t.Errorf("foresight is Due on a change to the Editor, which its rule does not declare: %s", h.Why)
	}
}

func TestTheSharedRuleIsDueOnANewJudgeSinceItsReading(t *testing.T) {
	r := feedbackRecord()
	at := func(n int) time.Time { d, _ := time.Parse(time.DateOnly, day(n)); return d.Add(9 * time.Hour) }
	// Judges that began before the reading's last day leave it standing.
	r.Judges = []register.Judge{
		{Act: "reading", Version: 1, Model: "m1", From: at(0)},
		{Act: "reading", Version: 2, Model: "m1", From: at(55)},
	}
	for _, name := range fallingParts {
		if h := hypothesis(t, register.Read(r), name); h.Status == register.Due {
			t.Errorf("%s is Due (%s) with no new Judge since its reading", name, h.Why)
		}
	}

	// A new model, made only through config, shows in no commit.
	r.Judges = append(r.Judges, register.Judge{Act: "reading", Version: 2, Model: "m2", From: at(56)})
	for _, name := range fallingParts {
		h := hypothesis(t, register.Read(r), name)
		if h.Status != register.Due || h.Last == nil || !strings.Contains(h.Why, "m2") {
			t.Errorf("%s: status %s (%s), last %+v; want Due on the new Judge, naming it, its reading kept",
				name, h.Status, h.Why, h.Last)
		}
	}
	if h := hypothesis(t, register.Read(r), register.Foresight); strings.Contains(h.Why, "Judge") {
		t.Errorf("foresight is Due on a new Judge, which its rule does not declare: %s", h.Why)
	}
}

func TestTheSharedRuleIsDueWithNoBriefingsToCountPer(t *testing.T) {
	r := feedbackRecord()
	r.Briefings = days(0, 28)

	if h := hypothesis(t, register.Read(r), register.Coverage); h.Status != register.Due {
		t.Errorf("status %s, want Due: the last 4 weeks have no Briefings", h.Status)
	}
}

// onSundayRead is a piece of new to me Feedback given on the Sunday read of
// day d, that day.
func onSundayRead(d string) register.Given {
	g := given(1, feedback.NewToMe, "briefing", d)[0]
	g.SundayRead = d
	return g
}

func TestForesightIsReadOverEachFourWeekWindowByItsRule(t *testing.T) {
	// Read on day 28, over days 0 to 27, whose Sundays are days 6, 13, 20
	// and 27.
	sundays := []string{day(6), day(13), day(20), day(27)}
	for _, c := range []struct {
		name     string
		feedback []register.Given
		want     register.Status
	}{
		{"no new to me refutes", given(5, feedback.Missed, "briefing", day(3)), register.Refuted},
		{"some in 3 of the 4 Sunday reads supports",
			[]register.Given{onSundayRead(day(6)), onSundayRead(day(13)), onSundayRead(day(27))}, register.Supported},
		{"some in only 2 is undecided",
			append([]register.Given{onSundayRead(day(6)), onSundayRead(day(13)), onSundayRead(day(13))},
				given(3, feedback.NewToMe, "relating", day(15))...), register.Undecided},
		{"new to me in the window but on no Sunday read is undecided",
			given(1, feedback.NewToMe, "relating", day(27)), register.Undecided},
		{"new to me only outside the window refutes",
			[]register.Given{onSundayRead(day(34))}, register.Refuted},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := register.Record{Today: day(28), Location: time.UTC, First: first, Briefings: days(0, 28),
				SundayReads: sundays, Feedback: c.feedback}

			h := hypothesis(t, register.Read(r), register.Foresight)

			if h.Status != c.want || h.Last == nil || h.Last.From != day(0) || h.Last.To != day(27) {
				t.Fatalf("status %s (%s), last %+v; want %s over days 0 to 27", h.Status, h.Why, h.Last, c.want)
			}
		})
	}
}

func TestForesightIsDueWithNoSundayReadsToReadOn(t *testing.T) {
	r := register.Record{Today: day(28), Location: time.UTC, First: first, Briefings: days(0, 28)}
	if h := hypothesis(t, register.Read(r), register.Foresight); h.Status != register.Due {
		t.Errorf("status %s, want Due, not refuted: there are no Sunday reads yet", h.Status)
	}
	r.Today = day(27)
	r.SundayReads = []string{day(6), day(13), day(20)}
	if h := hypothesis(t, register.Read(r), register.Foresight); h.Status != register.Due || h.Last != nil {
		t.Errorf("status %s, last %+v; want Due until the first 4 weeks have ended", h.Status, h.Last)
	}
}

func TestForesightIsNotReadWhileTheTopIsRefuted(t *testing.T) {
	// Nothing was opened in the top's first window, days 0 to 55.
	r := register.Record{Today: day(56), Location: time.UTC, First: first, Briefings: days(0, 56),
		SundayReads: days(0, 56), Feedback: []register.Given{onSundayRead(day(48))}}

	h := hypothesis(t, register.Read(r), register.Foresight)

	if h.Status != register.Due || !strings.Contains(h.Why, "refuted") || h.Last == nil || h.Last.To != day(27) {
		t.Errorf("status %s (%s), last %+v; want Due, keeping the reading of days 0 to 27", h.Status, h.Why, h.Last)
	}
}

func TestTheFallingPartsReadOnlyWindowsWhollyAfterTheSwitch(t *testing.T) {
	// The Record became events at noon on day 10: the first window wholly
	// after it starts on day 11, and the first reading comes 8 weeks on.
	r := feedbackRecord()
	r.Briefings = days(0, 80)
	opened(&r, 56, 24)
	switched, _ := time.Parse(time.DateOnly, day(10))
	r.Switch = switched.Add(12 * time.Hour)

	r.Today = day(66)
	for _, name := range fallingParts {
		if h := hypothesis(t, register.Read(r), name); h.Status != register.Due || h.Last != nil || !strings.Contains(h.Why, "switch") {
			t.Errorf("%s on day 66: status %s (%s), last %+v; want Due with no reading until 8 weeks after the switch",
				name, h.Status, h.Why, h.Last)
		}
	}

	r.Today = day(67)
	for _, name := range fallingParts {
		h := hypothesis(t, register.Read(r), name)
		if h.Last == nil || h.Last.From != day(11) || h.Last.To != day(66) {
			t.Errorf("%s on day 67: last %+v; want a reading over days 11 to 66", name, h.Last)
		}
	}
}

func TestTheFeedbackPartsAreDueUntilEightWeeksOfBriefings(t *testing.T) {
	r := feedbackRecord()
	r.Today = day(55)

	for _, name := range []string{register.Coverage, register.Brevity, register.Accuracy} {
		if h := hypothesis(t, register.Read(r), name); h.Status != register.Due || h.Last != nil {
			t.Errorf("%s: status %s, last %+v; want Due with no reading", name, h.Status, h.Last)
		}
	}
}
