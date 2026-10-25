package editor_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// schedule writes the Briefing at 07:00 with writer, after the steps in
// before, and returns what happened in order.
func schedule(store *sqlite.Store, writer *fakeModel, before ...string) (*editor.Schedule, *[]string) {
	var happened []string
	ed, _ := briefer(store, writer)
	var steps []func(context.Context, time.Time) error
	for _, name := range before {
		steps = append(steps, func(context.Context, time.Time) error {
			happened = append(happened, name)
			return errors.New(name + " failed")
		})
	}
	return editor.NewSchedule(editor.ScheduleDeps{
		Store:  store,
		At:     7 * time.Hour,
		Before: steps,
		Brief: func(ctx context.Context, now time.Time) error {
			happened = append(happened, "brief")
			return ed.Brief(ctx, now)
		},
		Log: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
	}), &happened
}

var (
	sixFiftyNine = time.Date(2026, time.October, 24, 6, 59, 0, 0, time.UTC)
	seven        = time.Date(2026, time.October, 24, 7, 0, 0, 0, time.UTC)
)

func TestTheBriefingIsWrittenOnceADayAtTheSetTimeAfterTheStepsBeforeIt(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	writer := &fakeModel{output: spokeInMotherwell}
	s, happened := schedule(store, writer, "connecting")

	s.Tick(t.Context(), sixFiftyNine)
	if len(*happened) != 0 {
		t.Fatalf("before the set time: %v", *happened)
	}
	s.Tick(t.Context(), seven)
	s.Tick(t.Context(), seven.Add(time.Minute))

	// A step before writing that fails is logged; the Briefing is still
	// written.
	if want := []string{"connecting", "brief"}; !slices.Equal(*happened, want) {
		t.Errorf("happened = %v, want %v", *happened, want)
	}
	if _, found, err := store.Briefing(t.Context(), "2026-10-24"); err != nil || !found {
		t.Errorf("no Briefing written at the set time: %v %v", found, err)
	}

	// The next day's is written at its own set time.
	s.Tick(t.Context(), seven.AddDate(0, 0, 1))
	if _, found, _ := store.Briefing(t.Context(), "2026-10-25"); !found {
		t.Error("no Briefing written on the next day")
	}
}

func TestABriefingThatFailedIsTriedAgainLater(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	writer := &fakeModel{err: errors.New("overloaded")}
	s, happened := schedule(store, writer)

	s.Tick(t.Context(), seven)
	writer.err, writer.output = nil, spokeInMotherwell
	s.Tick(t.Context(), seven.Add(5*time.Minute))
	if len(*happened) != 1 {
		t.Fatalf("tried again at once after failing: %v", *happened)
	}
	s.Tick(t.Context(), seven.Add(editor.RetryBriefing))

	if _, found, _ := store.Briefing(t.Context(), "2026-10-24"); !found {
		t.Error("the Briefing was not written on trying again")
	}
}

// sundaySchedule is schedule with the Sunday read written by agent, and
// what happened, in order.
func sundaySchedule(t *testing.T, store *sqlite.Store, agent *fakeAgent) (*editor.Schedule, *[]string) {
	t.Helper()
	s, happened := schedule(store, &fakeModel{output: spokeInMotherwell})
	ed, _ := reader(store, agent)
	s.SundayRead = func(ctx context.Context, now time.Time) error {
		*happened = append(*happened, "sunday read")
		return ed.SundayRead(ctx, now)
	}
	return s, happened
}

func TestTheSundayReadIsWrittenOnSundayAfterTheBriefing(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	s, happened := sundaySchedule(t, store, &fakeAgent{turns: []editor.Turn{submits(citesTheSpeech)}})
	sunday := seven.AddDate(0, 0, 1)

	s.Tick(t.Context(), seven) // Saturday: the Briefing only
	s.Tick(t.Context(), sunday.Add(-time.Minute))
	s.Tick(t.Context(), sunday)
	s.Tick(t.Context(), sunday.Add(time.Minute))

	if want := []string{"brief", "brief", "sunday read"}; !slices.Equal(*happened, want) {
		t.Errorf("happened = %v, want %v", *happened, want)
	}
	if _, found, err := store.SundayRead(t.Context(), "2026-10-25"); err != nil || !found {
		t.Errorf("no Sunday read written: %v %v", found, err)
	}
}

func TestASundayReadThatFailedIsTriedAgainAFewTimes(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	s, happened := sundaySchedule(t, store, &fakeAgent{}) // never submits
	sunday := seven.AddDate(0, 0, 1)

	for _, at := range []time.Duration{0, 30 * time.Minute, time.Hour, 2 * time.Hour, 3 * time.Hour, 4 * time.Hour} {
		s.Tick(t.Context(), sunday.Add(at))
	}

	var tries int
	for _, h := range *happened {
		if h == "sunday read" {
			tries++
		}
	}
	if tries != editor.SundayReadTries {
		t.Errorf("tried %d times, want %d, each %v after the last: %v", tries, editor.SundayReadTries, editor.RetrySundayRead, *happened)
	}
}

func TestTheSetTimeIsInTheClocksTimeZone(t *testing.T) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	// 06:30 UTC is 07:30 in London (BST): past the set time there.
	day := time.Date(2026, time.October, 24, 6, 30, 0, 0, time.UTC).In(london)
	if due := editor.DueAt(day, 7*time.Hour); !due.Equal(time.Date(2026, time.October, 24, 6, 0, 0, 0, time.UTC)) {
		t.Errorf("due at %v, want 07:00 BST", due)
	}
}
