package sqlite

import (
	"slices"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/feedback"
	"github.com/samthorold/go-slow/internal/register"
)

// keptBefore keeps a piece of Feedback as the Record kept it before it
// became events, when Feedback was still given on Facts and Stories: the
// register counts what was given so.
func keptBefore(t *testing.T, s *Store, kind, level string, at time.Time, undone bool) {
	t.Helper()
	var undoneAt any
	if undone {
		undoneAt = formatTime(at.Add(time.Minute))
	}
	if _, err := s.db.ExecContext(t.Context(), `
		INSERT INTO feedback (kind, level, depth, given_at, undone_at) VALUES (?, ?, 0, ?, ?)`,
		kind, level, formatTime(at), undoneAt); err != nil {
		t.Fatal(err)
	}
}

func TestTheRegisterCountsTheFeedbackKeptBeforeTheRecordBecameEventsAsGivenAndNotUndone(t *testing.T) {
	s := withItem(t, "<p>Work starts in May.</p>")
	keptBefore(t, s, "wrong", "fact", cutAt, false)
	keptBefore(t, s, "unwanted", "story", cutAt.Add(time.Minute), true)
	keptBefore(t, s, "new to me", "story", cutAt.Add(time.Hour), false)

	r, err := s.RegisterRecord(t.Context(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}

	want := []register.Given{
		{Kind: feedback.Wrong, Level: feedback.Fact, At: cutAt},
		{Kind: feedback.NewToMe, Level: feedback.Story, At: cutAt.Add(time.Hour)},
	}
	if !slices.EqualFunc(r.Feedback, want, func(a, b register.Given) bool {
		return a.Kind == b.Kind && a.Level == b.Level && a.At.Equal(b.At) && a.SundayRead == b.SundayRead
	}) {
		t.Errorf("Feedback %+v, want %+v", r.Feedback, want)
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
