package sqlite

import (
	"slices"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/register"
)

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
