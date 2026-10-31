package sqlite

import (
	"strconv"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/feedback"
)

func TestWrongOnAJudgementIsReadByNoViewWhileItStandsAndUndoingItIsAnEventOfItsOwn(t *testing.T) {
	s, c := cutRelease(t)
	if err := s.KeepReading(t.Context(), releaseReading(t, c)); err != nil {
		t.Fatal(err)
	}
	r, _, err := s.NewestReading(t.Context(), "number-10", "jobs")
	if err != nil || len(r.RefersTo) != 1 || len(r.Speakers) != 1 {
		t.Fatalf("reading %+v, %v", r, err)
	}
	pieces := feedback.NewPieces(s)
	given := cutAt.Add(2 * time.Hour)
	p, errs, err := pieces.Give(t.Context(), feedback.Ask{Kind: "wrong", Judgement: strconv.FormatInt(r.RefersTo[0].ID, 10)}, given)
	if err != nil || errs != nil {
		t.Fatalf("give: %v %v", errs, err)
	}

	r, _, err = s.NewestReading(t.Context(), "number-10", "jobs")
	if err != nil || len(r.RefersTo) != 0 || len(r.Speakers) != 1 {
		t.Errorf("under wrong, the reading reads %+v (%v), want its reference left out and nothing else", r, err)
	}

	if _, err := pieces.Undo(t.Context(), p.ID, given.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if r, _, _ = s.NewestReading(t.Context(), "number-10", "jobs"); len(r.RefersTo) != 1 {
		t.Errorf("once undone, the reading reads %+v, want its reference back", r.RefersTo)
	}
	for kind, want := range map[string]int{"feedback": 1, "undoing": 1} {
		var n int
		if err := s.db.QueryRowContext(t.Context(), `SELECT count(*) FROM events WHERE kind = ?`, kind).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Errorf("%d %s events, want %d", n, kind, want)
		}
	}
	kept, err := s.KeptPieces(t.Context())
	if err != nil || len(kept) != 1 {
		t.Fatalf("kept %+v, %v", kept, err)
	}
	k := kept[0]
	if !k.GivenAt.Equal(given) || !k.UndoneAt.Equal(given.Add(time.Hour)) || k.Judged != feedback.RefersTo ||
		k.What != "“the city” refers to “Derby”" {
		t.Errorf("kept %+v, want the piece as given, marked undone", k)
	}
	if _, err := s.db.ExecContext(t.Context(), `DELETE FROM feedback_undoings`); err == nil {
		t.Error("an undoing was removed: events are never removed")
	}
}
