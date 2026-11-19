package sqlite

import (
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// hydrationBy is the release's hydration by a judge on model, at.
func hydrationBy(t *testing.T, c cut.Cut, model string, at time.Time) editor.Hydration {
	t.Helper()
	r := releaseHydration(t, c)
	r.Judge.Model, r.Judge.RequestHash, r.At = model, "hash-"+model, at
	return r
}

// newestModel is the model of the judge whose hydration of the release a view
// reads, or "" for none.
func newestModel(t *testing.T, s *Store) string {
	t.Helper()
	r, found, err := s.NewestHydration(t.Context(), "number-10", "jobs")
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
		if err := s.KeepHydration(t.Context(), hydrationBy(t, c, model, cutAt.Add(time.Hour))); err != nil {
			t.Fatal(err)
		}
	}

	if got := newestModel(t, s); got != "claude-newer" {
		t.Errorf("a view reads %q, want the newer judge's hydration", got)
	}
}

// newCandidate is a candidate hydration judge on model, kept in s.
func newCandidate(t *testing.T, s *Store, model string) int64 {
	t.Helper()
	id, err := s.NewCandidate(t.Context(), editor.Candidate{
		Act: "hydration", Version: 1, Model: model, At: cutAt.Add(2 * time.Hour),
		Range: editor.Range{From: cutAt.Add(-24 * time.Hour), To: cutAt.Add(24 * time.Hour)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestACandidatesHydrationIsKeptAndReadByNoView(t *testing.T) {
	s, c := cutRelease(t)
	cand := newCandidate(t, s, "claude-candidate")
	r := hydrationBy(t, c, "claude-candidate", cutAt.Add(2*time.Hour))
	r.Candidate = cand

	if err := s.KeepHydration(t.Context(), r); err != nil {
		t.Fatal(err)
	}

	if n := count(t, s, "hydrations"); n != 1 {
		t.Errorf("%d hydrations kept, want the candidate's", n)
	}
	if got := newestModel(t, s); got != "" {
		t.Errorf("a view reads %q's hydration, want none: only a candidate has hydrated it", got)
	}
	if waiting, err := s.ItemsToHydrate(t.Context()); err != nil || len(waiting) != 1 {
		t.Errorf("awaiting hydration %+v (%v), want the release still: a candidate's hydration is not the live one", waiting, err)
	}
	if err := s.KeepHydration(t.Context(), hydrationBy(t, c, "claude-live", cutAt.Add(3*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if got := newestModel(t, s); got != "claude-live" {
		t.Errorf("a view reads %q, want the live judge's", got)
	}
}

func TestChoosingACandidateMakesItTheLiveJudgeFromThenOnAndRewritesNoEvent(t *testing.T) {
	s, c := cutRelease(t)
	cand := newCandidate(t, s, "claude-candidate")
	r := hydrationBy(t, c, "claude-candidate", cutAt.Add(2*time.Hour))
	r.Candidate = cand
	if err := s.KeepHydration(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	// A live hydration made after the candidate's, before it is chosen.
	if err := s.KeepHydration(t.Context(), hydrationBy(t, c, "claude-live", cutAt.Add(3*time.Hour))); err != nil {
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
	if n := count(t, s, "hydrations"); n != 2 {
		t.Errorf("%d hydrations, want both kept", n)
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
	if err := s.KeepHydration(t.Context(), hydrationBy(t, c, "claude-later", cutAt.Add(6*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if got := newestModel(t, s); got != "claude-later" {
		t.Errorf("a view reads %q, want the hydration made after the choice", got)
	}
}
