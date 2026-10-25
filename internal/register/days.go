package register

import "time"

// date is a day as a time at midnight UTC, so that adding days never meets
// a change of clocks.
func date(s string) (time.Time, bool) {
	d, err := time.Parse(time.DateOnly, s)
	return d, err == nil
}

func dayString(d time.Time) string { return d.Format(time.DateOnly) }

// window is a run of whole days, [from, to).
type window struct{ from, to time.Time }

func (w window) holds(day string) bool {
	d, ok := date(day)
	return ok && !d.Before(w.from) && d.Before(w.to)
}

// last is the last day the window covers.
func (w window) last() string { return dayString(w.to.AddDate(0, 0, -1)) }

// windows are the windows of n days from first, back to back, that have
// ended by today, oldest first.
func windows(first, today string, n int) []window {
	start, ok := date(first)
	end, ok2 := date(today)
	if !ok || !ok2 {
		return nil
	}
	var out []window
	for to := start.AddDate(0, 0, n); !to.After(end); to = to.AddDate(0, 0, n) {
		out = append(out, window{to.AddDate(0, 0, -n), to})
	}
	return out
}

// countDays is how many distinct days in w the days hold.
func countDays(w window, days []string) int {
	seen := map[string]bool{}
	for _, d := range days {
		if w.holds(d) {
			seen[d] = true
		}
	}
	return len(seen)
}

// dayIn is the day t falls on in loc.
func dayIn(t time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	return t.In(loc).Format(time.DateOnly)
}
