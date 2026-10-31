package sqlite

import (
	"maps"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/feedback"
	"github.com/samthorold/go-slow/internal/register"
)

func TestTheRegisterCountsTheFeedbackStandingByWhatItWasGivenOn(t *testing.T) {
	ctx := t.Context()
	s, c := cutRelease(t)
	if err := s.KeepReading(ctx, releaseReading(t, c)); err != nil {
		t.Fatal(err)
	}
	reading, _, err := s.NewestReading(ctx, "number-10", "jobs")
	if err != nil {
		t.Fatal(err)
	}
	s2 := at(t, c, 2, "")
	if err := s.KeepBriefing(ctx, editor.Briefing{Day: "2026-10-30", WrittenAt: cutAt.Add(2 * time.Hour),
		Judge:      editor.Judge{Act: "briefing", Version: editor.BriefingVersion, Model: "fake"},
		Statements: []editor.Statement{{Text: "Derby gets jobs.", Cites: []editor.OtherSpan{{At: s2}}}},
	}); err != nil {
		t.Fatal(err)
	}
	var statement int64
	if err := s.db.QueryRowContext(ctx, `SELECT event_id FROM told_statements`).Scan(&statement); err != nil {
		t.Fatal(err)
	}
	pieces := feedback.NewPieces(s)
	span := strconv.Itoa(s2.Span.Start) + "-" + strconv.Itoa(s2.Span.End)
	given := cutAt.Add(3 * time.Hour)
	give := func(a feedback.Ask) feedback.Piece {
		t.Helper()
		p, errs, err := pieces.Give(ctx, a, given)
		if err != nil || errs != nil {
			t.Fatalf("give %+v: %v %v", a, errs, err)
		}
		return p
	}
	give(feedback.Ask{Kind: "wrong", Judgement: strconv.FormatInt(reading.RefersTo[0].ID, 10)})
	give(feedback.Ask{Kind: "unwanted", SourceID: "number-10", EntryID: "jobs", Span: span, Day: "2026-10-30"})
	give(feedback.Ask{Kind: "new to me", Judgement: strconv.FormatInt(statement, 10)})
	undone := give(feedback.Ask{Kind: "missed", SourceID: "number-10", EntryID: "jobs", Span: span})
	if _, err := pieces.Undo(ctx, undone.ID, given.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	r, err := s.RegisterRecord(ctx, time.UTC)
	if err != nil {
		t.Fatal(err)
	}

	got := map[feedback.Kind]string{}
	for _, g := range r.Feedback {
		got[g.Kind] = g.On
		if !g.At.Equal(given) {
			t.Errorf("%s given at %v, want %v", g.Kind, g.At, given)
		}
	}
	want := map[feedback.Kind]string{feedback.Wrong: "reading", feedback.Unwanted: "number-10", feedback.NewToMe: "briefing"}
	if len(r.Feedback) != 3 || !maps.Equal(got, want) {
		t.Errorf("feedback %+v, want each standing piece by what it was given on, %v, and the undone missed left out", r.Feedback, want)
	}
}

func TestTheRegisterReadsEachLiveJudgeAsItBeganJudging(t *testing.T) {
	ctx := t.Context()
	s, c := cutRelease(t)
	reading := func(model string, at time.Time) {
		t.Helper()
		r := releaseReading(t, c)
		r.Judge.Model, r.At = model, at
		if err := s.KeepReading(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	reading("m1", cutAt.Add(time.Hour))
	reading("m1", cutAt.Add(2*time.Hour))
	reading("m2", cutAt.Add(3*time.Hour))
	// Going back to an earlier Judge is a new Judge again.
	reading("m1", cutAt.Add(4*time.Hour))
	// A Briefing written empty, with no call, has no Judge.
	if err := s.KeepBriefing(ctx, editor.Briefing{Day: "2026-10-30", WrittenAt: cutAt.Add(5 * time.Hour),
		Judge: editor.Judge{Act: "briefing", Version: editor.BriefingVersion}}); err != nil {
		t.Fatal(err)
	}
	if err := s.KeepBriefing(ctx, editor.Briefing{Day: "2026-10-31", WrittenAt: cutAt.Add(29 * time.Hour),
		Judge: editor.Judge{Act: "briefing", Version: editor.BriefingVersion, Model: "fake"}}); err != nil {
		t.Fatal(err)
	}

	r, err := s.RegisterRecord(ctx, time.UTC)
	if err != nil {
		t.Fatal(err)
	}

	want := []register.Judge{
		{Act: "reading", Version: 1, Model: "m1", From: cutAt.Add(time.Hour)},
		{Act: "reading", Version: 1, Model: "m2", From: cutAt.Add(3 * time.Hour)},
		{Act: "reading", Version: 1, Model: "m1", From: cutAt.Add(4 * time.Hour)},
		{Act: "briefing", Version: editor.BriefingVersion, Model: "fake", From: cutAt.Add(29 * time.Hour)},
	}
	if !slices.EqualFunc(r.Judges, want, func(a, b register.Judge) bool {
		return a.Act == b.Act && a.Version == b.Version && a.Model == b.Model && a.From.Equal(b.From)
	}) {
		t.Errorf("judges\n%+v\nwant\n%+v", r.Judges, want)
	}
}

func TestTheRegisterReadsTheSwitch(t *testing.T) {
	ctx := t.Context()
	db := oldRecord(t)
	if err := migrateTo(ctx, db, -1); err != nil {
		t.Fatal(err)
	}
	s := &Store{db: db}
	sw, _, err := s.Switch(ctx)
	if err != nil {
		t.Fatal(err)
	}

	r, err := s.RegisterRecord(ctx, time.UTC)
	if err != nil {
		t.Fatal(err)
	}

	if r.Switch.IsZero() || !r.Switch.Equal(sw.At) {
		t.Errorf("the register reads the switch as at %v, want %v", r.Switch, sw.At)
	}
	if r, err := openTestStore(t).RegisterRecord(ctx, time.UTC); err != nil || !r.Switch.IsZero() {
		t.Errorf("a Record with no switch reads one at %v (%v)", r.Switch, err)
	}
}

func TestCommitsAreKeptOnceHoweverOftenTheyAreRecorded(t *testing.T) {
	s := withItem(t, "<p>Work starts in May.</p>")
	commits := []register.Commit{
		{Hash: "a1", At: cutAt.Add(-48 * time.Hour), Editor: true},
		{Hash: "b2", At: cutAt.Add(-24 * time.Hour)},
	}

	if n, err := s.KeepCommits(t.Context(), commits); err != nil || n != 2 {
		t.Fatalf("kept %d (%v), want 2", n, err)
	}
	if n, err := s.KeepCommits(t.Context(), append(commits, register.Commit{Hash: "c3", At: cutAt})); err != nil || n != 1 {
		t.Fatalf("kept %d new (%v), want 1", n, err)
	}

	r, err := s.RegisterRecord(t.Context(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	want := append(commits, register.Commit{Hash: "c3", At: cutAt})
	if !slices.EqualFunc(r.Commits, want, func(a, b register.Commit) bool {
		return a.Hash == b.Hash && a.At.Equal(b.At) && a.Editor == b.Editor
	}) {
		t.Errorf("commits %+v, want %+v", r.Commits, want)
	}
}
