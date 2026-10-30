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

// RetrySundayRead is how long after a failure the Sunday read is tried
// again, and SundayReadTries how many times it is tried on a Sunday. A read
// is a long agentic run that costs far more than a Briefing, and one that
// failed its checks or hit its bounds is likely to again, so it is tried a
// few times, an hour apart, rather than all day.
const (
	RetrySundayRead = time.Hour
	SundayReadTries = 3
)

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
	// HasSundayRead reports whether the day's Sunday read has been written.
	HasSundayRead(ctx context.Context, day string) (bool, error)
	// SundayReadFailures are the failures to write the day's Sunday read,
	// the earliest first.
	SundayReadFailures(ctx context.Context, day string) ([]SundayReadFailure, error)
}

// ScheduleDeps are the schedule's dependencies.
type ScheduleDeps struct {
	Store ScheduleStore
	// At is the time of day the Owner sets for the Briefing, such as 7h.
	At time.Duration
	// Brief writes the Briefing at now (Editor.Brief).
	Brief func(ctx context.Context, now time.Time) error
	// SundayRead writes the Sunday read at now (Editor.SundayRead): on a
	// Sunday, at the Briefing's time, just after the Briefing. Nil writes
	// none.
	SundayRead func(ctx context.Context, now time.Time) error
	Log        *slog.Logger
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

// Tick writes the day's Briefing if it is due at now, not yet written and
// has not failed in the last RetryBriefing; then, on a Sunday, the Sunday
// read likewise. The Briefing never waits for the work collected before it:
// it is written over what the Record holds at the set time (system design:
// The Briefing doesn't wait for construction).
func (s *Schedule) Tick(ctx context.Context, now time.Time) {
	if now.Before(DueAt(now, s.At)) {
		return
	}
	s.tickBriefing(ctx, now)
	if s.SundayRead != nil && now.Weekday() == time.Sunday {
		s.tickSundayRead(ctx, now)
	}
}

// tickSundayRead writes the Sunday read if it is not yet written, has failed
// fewer than SundayReadTries times today, and not in the last
// RetrySundayRead.
func (s *Schedule) tickSundayRead(ctx context.Context, now time.Time) {
	day := now.Format(time.DateOnly)
	written, err := s.Store.HasSundayRead(ctx, day)
	if err != nil || written {
		if err != nil {
			s.Log.ErrorContext(ctx, "writing the Sunday read failed", "error", err)
		}
		return
	}
	fs, err := s.Store.SundayReadFailures(ctx, day)
	if err != nil {
		s.Log.ErrorContext(ctx, "writing the Sunday read failed", "error", err)
		return
	}
	if len(fs) >= SundayReadTries || len(fs) > 0 && now.Sub(fs[len(fs)-1].At) < RetrySundayRead {
		return
	}
	if err := s.SundayRead(ctx, now); err != nil {
		s.Log.ErrorContext(ctx, "writing the Sunday read failed", "error", err)
	}
}

// tickBriefing writes the day's Briefing if it is due.
func (s *Schedule) tickBriefing(ctx context.Context, now time.Time) {
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
	if err := s.Brief(ctx, now); err != nil {
		s.Log.ErrorContext(ctx, "writing the Briefing failed", "error", err)
	}
}
