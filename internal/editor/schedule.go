package editor

import (
	"context"
	"log/slog"
	"time"
)

// RetryBriefing is how long after a failure writing the Briefing is tried
// again: soon enough that a passing fault costs the Owner minutes, not the
// day, and seldom enough that a lasting one is not a call every minute.
const RetryBriefing = 15 * time.Minute

// DueAt is when the Briefing for the day that holds day, in day's location,
// is due: at, as a time of day (7h is 07:00), on that day's clock.
func DueAt(day time.Time, at time.Duration) time.Time {
	y, m, d := day.Date()
	return time.Date(y, m, d, int(at/time.Hour), int(at%time.Hour/time.Minute), 0, 0, day.Location())
}

// ScheduleStore is the part of the Record the schedule reads.
type ScheduleStore interface {
	// HasBriefing reports whether the Briefing for the day that holds t has
	// been written.
	HasBriefing(ctx context.Context, t time.Time) (bool, error)
	// BriefingFailure is the latest failure to write the day's Briefing.
	BriefingFailure(ctx context.Context, day string) (BriefingFailure, bool, error)
}

// ScheduleDeps are the schedule's dependencies.
type ScheduleDeps struct {
	Store ScheduleStore
	// At is the time of day the Owner sets for the Briefing, such as 7h.
	At time.Duration
	// Before are the steps run just before the Briefing is written, such as
	// connecting over the day's Facts (system design: The acts run as Items
	// arrive). One that fails is logged, and the Briefing is still written:
	// a Briefing on what the Record holds beats none.
	Before []func(ctx context.Context, now time.Time) error
	// Brief writes the Briefing at now (Editor.Brief).
	Brief func(ctx context.Context, now time.Time) error
	Log   *slog.Logger
}

// Schedule writes the Briefing in real time, once a day at the time the
// Owner sets, apart from Collection's timetable (system design: The daily
// acts run in batches; the Briefing is written in real time).
type Schedule struct {
	ScheduleDeps
}

// NewSchedule builds the schedule.
func NewSchedule(d ScheduleDeps) *Schedule { return &Schedule{ScheduleDeps: d} }

// RunOn is the schedule's clock: a tick at once, then one on every tick,
// each at the time now gives, until ctx is done.
func (s *Schedule) RunOn(ctx context.Context, ticks <-chan time.Time, now func() time.Time) {
	s.Tick(ctx, now())
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			s.Tick(ctx, now())
		}
	}
}

// Tick writes the day's Briefing if it is due at now and not yet written,
// and has not failed in the last RetryBriefing.
func (s *Schedule) Tick(ctx context.Context, now time.Time) {
	if now.Before(DueAt(now, s.At)) {
		return
	}
	written, err := s.Store.HasBriefing(ctx, now)
	if err != nil {
		s.Log.ErrorContext(ctx, "writing the Briefing failed", "error", err)
		return
	}
	if written {
		return
	}
	f, failed, err := s.Store.BriefingFailure(ctx, now.Format(time.DateOnly))
	if err != nil {
		s.Log.ErrorContext(ctx, "writing the Briefing failed", "error", err)
		return
	}
	if failed && now.Sub(f.At) < RetryBriefing {
		return
	}
	for _, step := range s.Before {
		if err := step(ctx, now); err != nil {
			s.Log.ErrorContext(ctx, "a step before the Briefing failed", "error", err)
		}
	}
	if err := s.Brief(ctx, now); err != nil {
		s.Log.ErrorContext(ctx, "writing the Briefing failed", "error", err)
	}
}
