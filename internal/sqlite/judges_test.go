package sqlite

import (
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// readingBy is the release's reading by a judge on model, at.
func readingBy(t *testing.T, c cut.Cut, model string, at time.Time) editor.Reading {
	t.Helper()
	r := releaseReading(t, c)
	r.Judge.Model, r.Judge.RequestHash, r.At = model, "hash-"+model, at
	return r
}

// newestModel is the model of the judge whose reading of the release a view
// reads, or "" for none.
func newestModel(t *testing.T, s *Store) string {
	t.Helper()
	r, found, err := s.NewestReading(t.Context(), "number-10", "jobs")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		return ""
	}
	return r.Judge.Model
}

func TestAViewReadsTheNewestLiveJudgeThatHasJudgedAnItem(t *testing.T) {
	s, c := cutRelease(t)
	for _, model := range []string{"claude-older", "claude-newer"} {
		if err := s.KeepReading(t.Context(), readingBy(t, c, model, cutAt.Add(time.Hour))); err != nil {
			t.Fatal(err)
		}
	}

	if got := newestModel(t, s); got != "claude-newer" {
		t.Errorf("a view reads %q, want the newer judge's reading", got)
	}
}

// newCandidate is a candidate reading judge on model, kept in s.
func newCandidate(t *testing.T, s *Store, model string) int64 {
	t.Helper()
	id, err := s.NewCandidate(t.Context(), editor.Candidate{
		Act: "reading", Version: 1, Model: model, At: cutAt.Add(2 * time.Hour),
		Range: editor.Range{From: cutAt.Add(-24 * time.Hour), To: cutAt.Add(24 * time.Hour)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestACandidatesReadingIsKeptAndReadByNoView(t *testing.T) {
	s, c := cutRelease(t)
	cand := newCandidate(t, s, "claude-candidate")
	r := readingBy(t, c, "claude-candidate", cutAt.Add(2*time.Hour))
	r.Candidate = cand

	if err := s.KeepReading(t.Context(), r); err != nil {
		t.Fatal(err)
	}

	if n := count(t, s, "readings"); n != 1 {
		t.Errorf("%d readings kept, want the candidate's", n)
	}
	if got := newestModel(t, s); got != "" {
		t.Errorf("a view reads %q's reading, want none: only a candidate has read it", got)
	}
	if waiting, err := s.ItemsToRead(t.Context()); err != nil || len(waiting) != 1 {
		t.Errorf("awaiting reading %+v (%v), want the release still: a candidate's reading is not the live one", waiting, err)
	}
	if err := s.KeepReading(t.Context(), readingBy(t, c, "claude-live", cutAt.Add(3*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if got := newestModel(t, s); got != "claude-live" {
		t.Errorf("a view reads %q, want the live judge's", got)
	}
}

func TestChoosingACandidateMakesItTheLiveJudgeFromThenOnAndRewritesNoEvent(t *testing.T) {
	s, c := cutRelease(t)
	cand := newCandidate(t, s, "claude-candidate")
	r := readingBy(t, c, "claude-candidate", cutAt.Add(2*time.Hour))
	r.Candidate = cand
	if err := s.KeepReading(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	// A live reading made after the candidate's, before it is chosen.
	if err := s.KeepReading(t.Context(), readingBy(t, c, "claude-live", cutAt.Add(3*time.Hour))); err != nil {
		t.Fatal(err)
	}
	events := count(t, s, "events")

	if err := s.ChooseCandidate(t.Context(), cand, cutAt.Add(4*time.Hour)); err != nil {
		t.Fatal(err)
	}

	if got := newestModel(t, s); got != "claude-candidate" {
		t.Errorf("a view reads %q, want the chosen candidate's: it is the live judge from its choice", got)
	}
	if n := count(t, s, "events"); n != events+1 {
		t.Errorf("%d events after choosing, want %d: the choice is one event, and nothing else is written", n, events+1)
	}
	if n := count(t, s, "readings"); n != 2 {
		t.Errorf("%d readings, want both kept", n)
	}
	cands, err := s.Candidates(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].ID != cand || cands[0].Model != "claude-candidate" ||
		!cands[0].ChosenAt.Equal(cutAt.Add(4*time.Hour)) {
		t.Errorf("candidates = %+v, want the one chosen", cands)
	}
	if err := s.ChooseCandidate(t.Context(), cand, cutAt.Add(5*time.Hour)); err == nil {
		t.Error("a candidate was chosen twice")
	}
	if err := s.ChooseCandidate(t.Context(), cand+100, cutAt.Add(5*time.Hour)); err == nil {
		t.Error("a candidate not in the Record was chosen")
	}

	// A live judge judging after the choice is newer still.
	if err := s.KeepReading(t.Context(), readingBy(t, c, "claude-later", cutAt.Add(6*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if got := newestModel(t, s); got != "claude-later" {
		t.Errorf("a view reads %q, want the reading made after the choice", got)
	}
}
