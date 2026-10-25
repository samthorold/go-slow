package sqlite_test

import (
	"slices"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/feedback"
	"github.com/samthorold/go-slow/internal/register"
)

func TestTheRegisterCountsFeedbackAsGiven(t *testing.T) {
	store := threeFacts(t)
	wrong(t, store, "1")
	if _, errs, err := feedback.New(store).Give(t.Context(),
		feedback.Draft{Kind: "new to me", Level: "story", Story: "1"}, saturday.Add(time.Hour)); err != nil || errs != nil {
		t.Fatalf("give Feedback: %v %v", errs, err)
	}

	r, err := store.RegisterRecord(t.Context(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}

	want := []register.Given{
		{Kind: feedback.Wrong, Level: feedback.Fact, At: saturday},
		{Kind: feedback.NewToMe, Level: feedback.Story, At: saturday.Add(time.Hour)},
	}
	if !slices.EqualFunc(r.Feedback, want, func(a, b register.Given) bool {
		return a.Kind == b.Kind && a.Level == b.Level && a.At.Equal(b.At) && a.SundayRead == b.SundayRead
	}) {
		t.Errorf("Feedback %+v, want %+v", r.Feedback, want)
	}
	if len(r.Untraced) != 0 {
		t.Errorf("untraced %v, want none: every Fact is from its Item", r.Untraced)
	}
}

func TestCommitsAreKeptOnceHoweverOftenTheyAreRecorded(t *testing.T) {
	store := threeFacts(t)
	commits := []register.Commit{
		{Hash: "a1", At: saturday.Add(-48 * time.Hour), Editor: true},
		{Hash: "b2", At: saturday.Add(-24 * time.Hour)},
	}

	if n, err := store.KeepCommits(t.Context(), commits); err != nil || n != 2 {
		t.Fatalf("kept %d (%v), want 2", n, err)
	}
	if n, err := store.KeepCommits(t.Context(), append(commits, register.Commit{Hash: "c3", At: saturday})); err != nil || n != 1 {
		t.Fatalf("kept %d new (%v), want 1", n, err)
	}

	r, err := store.RegisterRecord(t.Context(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	want := append(commits, register.Commit{Hash: "c3", At: saturday})
	if !slices.EqualFunc(r.Commits, want, func(a, b register.Commit) bool {
		return a.Hash == b.Hash && a.At.Equal(b.At) && a.Editor == b.Editor
	}) {
		t.Errorf("commits %+v, want %+v", r.Commits, want)
	}
}
