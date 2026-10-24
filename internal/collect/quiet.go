package collect

import "time"

// SourceStatus is how a Source stands for the necessary condition "No Source
// goes quiet": every Source yields new Items within its usual gap.
type SourceStatus struct {
	ID       string
	Name     string
	UsualGap time.Duration
	// Since is when go-slow began collecting the Source.
	Since time.Time
	// Newest is when the Source's newest Item was published; zero if it has
	// yielded none.
	Newest time.Time
}

// Quiet reports whether, at now, the Source has gone longer than its usual
// gap without a new Item. A Source that has yielded nothing counts its
// silence from when collecting it began.
func (s SourceStatus) Quiet(now time.Time) bool {
	last := s.Newest
	if last.IsZero() {
		last = s.Since
	}
	if last.IsZero() {
		return false
	}
	return now.Sub(last) > s.UsualGap
}
