package collect_test

import (
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
)

func TestASourceSilentPastItsUsualGapIsQuiet(t *testing.T) {
	gap := 4 * 24 * time.Hour
	longAgo := now.AddDate(0, -1, 0)
	cases := []struct {
		name   string
		since  time.Time
		newest time.Time
		quiet  bool
	}{
		{"an Item within the gap", longAgo, now.Add(-gap + time.Hour), false},
		{"its newest Item exactly a gap ago", longAgo, now.Add(-gap), false},
		{"nothing new past the gap", longAgo, now.Add(-gap - time.Hour), true},
		{"no Items yet, newly added", now.Add(-time.Hour), time.Time{}, false},
		{"no Items in a gap since it was added", longAgo, time.Time{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := collect.SourceStatus{Name: "Number 10", UsualGap: gap, Since: tc.since, Newest: tc.newest}
			if got := s.Quiet(now); got != tc.quiet {
				t.Errorf("Quiet = %v, want %v", got, tc.quiet)
			}
		})
	}
}
