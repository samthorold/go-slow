package feedback_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/feedback"
)

// fakeStore keeps what it is given, and answers About with found. Undo
// marks a kept piece undone, keeping the effect it lifted.
type fakeStore struct {
	found   bool
	given   []feedback.Feedback
	effects []feedback.Effect
	lifted  []feedback.Effect
}

func (s *fakeStore) Feedback(_ context.Context, id int64) (feedback.Feedback, bool, error) {
	if id < 1 || int(id) > len(s.given) {
		return feedback.Feedback{}, false, nil
	}
	return s.given[id-1], true, nil
}

func (s *fakeStore) Undo(_ context.Context, f feedback.Feedback, e feedback.Effect, at time.Time) error {
	s.given[f.ID-1].UndoneAt = at
	s.lifted = append(s.lifted, e)
	return nil
}

func (s *fakeStore) About(context.Context, feedback.Feedback) (bool, error) { return s.found, nil }

func (s *fakeStore) Give(_ context.Context, f feedback.Feedback, e feedback.Effect) (int64, error) {
	f.ID = int64(len(s.given) + 1)
	s.given, s.effects = append(s.given, f), append(s.effects, e)
	return f.ID, nil
}

var now = time.Date(2026, time.October, 24, 8, 0, 0, 0, time.UTC)

func TestTheSundayReadAndItsAssessmentsTakeFeedback(t *testing.T) {
	for _, tc := range []struct {
		d    feedback.Draft
		want feedback.Effect
	}{
		// A wrong Assessment is withdrawn; new to me changes nothing.
		{feedback.Draft{Kind: "wrong", Level: "assessment", Assessment: "4"}, feedback.Effect{WithdrawAssessment: 4}},
		{feedback.Draft{Kind: "new to me", Level: "assessment", Assessment: "4"}, feedback.Effect{}},
		{feedback.Draft{Kind: "new to me", Level: "sunday read", Read: "2"}, feedback.Effect{}},
	} {
		store := &fakeStore{found: true}
		f, errs, err := feedback.New(store).Give(t.Context(), tc.d, now)
		if err != nil || errs != nil {
			t.Fatalf("%+v: %v %v", tc.d, errs, err)
		}
		if len(store.given) != 1 || store.effects[0] != tc.want || !f.GivenAt.Equal(now) {
			t.Errorf("%+v: kept %+v with effect %+v, want %+v", tc.d, store.given, store.effects, tc.want)
		}
	}
	for _, d := range []feedback.Draft{
		{Kind: "unwanted", Level: "assessment", Assessment: "4"},
		{Kind: "wrong", Level: "sunday read", Read: "2"},
		{Kind: "wrong", Level: "assessment"},
		// The levels Feedback was given at before the Record became events
		// take it no more.
		{Kind: "wrong", Level: "fact"},
		{Kind: "unwanted", Level: "story"},
		{Kind: "missed", Level: "development"},
		{Kind: "new to me", Level: "connection"},
	} {
		store := &fakeStore{found: true}
		if _, errs, err := feedback.New(store).Give(t.Context(), d, now); err != nil || len(errs) == 0 || len(store.given) != 0 {
			t.Errorf("%+v: errs %v, err %v, kept %d", d, errs, err, len(store.given))
		}
	}
}

func TestTheSundayReadsFeedbackIsUndoneLiftingTheMarksItMadeOnce(t *testing.T) {
	store := &fakeStore{found: true}
	s := feedback.New(store)
	given, _, _ := s.Give(t.Context(), feedback.Draft{Kind: "wrong", Level: "assessment", Assessment: "4"}, now)

	undone, err := s.Undo(t.Context(), given.ID, now.Add(time.Hour))
	if err != nil || !undone.UndoneAt.Equal(now.Add(time.Hour)) || store.lifted[0] != store.effects[0] {
		t.Fatalf("not undone, lifting its marks: %+v %+v %v", undone, store.lifted, err)
	}
	again, err := s.Undo(t.Context(), given.ID, now.Add(2*time.Hour))
	if err != nil || len(store.lifted) != 1 || !again.UndoneAt.Equal(now.Add(time.Hour)) {
		t.Errorf("undoing again changed something: %+v, lifted %d, %v", again, len(store.lifted), err)
	}
	if _, err := s.Undo(t.Context(), 9, now); !errors.Is(err, feedback.ErrNotInRecord) {
		t.Errorf("undoing what is not in the Record: err %v, want ErrNotInRecord", err)
	}
	if _, _, err := s.Give(t.Context(), feedback.Draft{Kind: "new to me", Level: "sunday read", Read: "9"}, now); err != nil {
		t.Fatal(err)
	}
	store.found = false
	if _, _, err := s.Give(t.Context(), feedback.Draft{Kind: "new to me", Level: "sunday read", Read: "9"}, now); !errors.Is(err, feedback.ErrNotInRecord) {
		t.Errorf("Feedback on what is not in the Record: err %v, want ErrNotInRecord", err)
	}
}

// pieceStore keeps the pieces it is given, and holds the judgements, the
// Spans and what each day's Briefing told that it was built with.
type pieceStore struct {
	judgements map[int64]feedback.Judged
	spans      []feedback.Span
	told       map[string][]feedback.Span
	given      []feedback.Piece
}

func (s *pieceStore) JudgementKind(_ context.Context, id int64) (feedback.Judged, bool, error) {
	k, ok := s.judgements[id]
	return k, ok, nil
}

func (s *pieceStore) SpanInItem(_ context.Context, sp feedback.Span) (bool, error) {
	for _, in := range s.spans {
		if in == sp {
			return true, nil
		}
	}
	return false, nil
}

func (s *pieceStore) ToldIn(_ context.Context, day string, sp feedback.Span) (bool, error) {
	for _, t := range s.told[day] {
		if t == sp {
			return true, nil
		}
	}
	return false, nil
}

func (s *pieceStore) GivePiece(_ context.Context, p feedback.Piece) (int64, error) {
	p.ID = int64(len(s.given) + 1)
	s.given = append(s.given, p)
	return p.ID, nil
}

func (s *pieceStore) Piece(_ context.Context, id int64) (feedback.Piece, bool, error) {
	if id < 1 || int(id) > len(s.given) {
		return feedback.Piece{}, false, nil
	}
	return s.given[id-1], true, nil
}

func (s *pieceStore) UndoPiece(_ context.Context, id int64, at time.Time) error {
	s.given[id-1].UndoneAt = at
	return nil
}

// record is a pieceStore holding a judgement of each kind (ids 1 to 6, in
// the order of feedback's constants), a Span, and the Briefing for
// 2026-10-24 telling it.
func record() *pieceStore {
	sp := feedback.Span{SourceID: "number-10", EntryID: "release", Start: 3, End: 20}
	return &pieceStore{
		judgements: map[int64]feedback.Judged{1: feedback.RefersTo, 2: feedback.Lookup, 3: feedback.Speaker,
			4: feedback.Relation, 5: feedback.Gathering, 6: feedback.Statement},
		spans: []feedback.Span{sp},
		told:  map[string][]feedback.Span{"2026-10-24": {sp}},
	}
}

func TestEachKindIsGivenOnlyWhereTheDesignGivesIt(t *testing.T) {
	const span = "3-20"
	on := func(kind, judgement string) feedback.Ask { return feedback.Ask{Kind: kind, Judgement: judgement} }
	onSpan := func(kind, day string) feedback.Ask {
		return feedback.Ask{Kind: kind, SourceID: "number-10", EntryID: "release", Span: span, Day: day}
	}
	for _, a := range []feedback.Ask{
		on("wrong", "1"), on("wrong", "2"), on("wrong", "3"), on("wrong", "4"), on("wrong", "5"), on("wrong", "6"),
		on("new to me", "5"), on("new to me", "6"),
		onSpan("unwanted", "2026-10-24"), onSpan("missed", ""),
	} {
		store := record()
		p, errs, err := feedback.NewPieces(store).Give(t.Context(), a, now)
		if err != nil || errs != nil || len(store.given) != 1 || !p.GivenAt.Equal(now) {
			t.Errorf("%+v: refused: %v %v", a, errs, err)
		}
	}
	for _, a := range []feedback.Ask{
		on("new to me", "1"), on("new to me", "3"), on("unwanted", "6"), on("missed", "6"), on("liked", "6"),
		on("wrong", ""),
		onSpan("wrong", ""),
		onSpan("unwanted", ""),           // not from a Briefing's page
		onSpan("unwanted", "2026-10-23"), // not told that day
		onSpan("missed", "2026-10-24"),   // from a Briefing's page
	} {
		store := record()
		if _, errs, err := feedback.NewPieces(store).Give(t.Context(), a, now); err != nil || len(errs) == 0 || len(store.given) != 0 {
			t.Errorf("%+v: errs %v, err %v, kept %d", a, errs, err, len(store.given))
		}
	}
	for _, a := range []feedback.Ask{on("wrong", "9"), {Kind: "missed", SourceID: "number-10", EntryID: "release", Span: "3-21"}} {
		if _, _, err := feedback.NewPieces(record()).Give(t.Context(), a, now); !errors.Is(err, feedback.ErrNotInRecord) {
			t.Errorf("%+v: err %v, want ErrNotInRecord", a, err)
		}
	}
}

func TestUndoingAPieceKeepsItMarkedUndoneOnce(t *testing.T) {
	store := record()
	s := feedback.NewPieces(store)
	p, _, _ := s.Give(t.Context(), feedback.Ask{Kind: "wrong", Judgement: "1"}, now)

	undone, err := s.Undo(t.Context(), p.ID, now.Add(time.Hour))
	if err != nil || !undone.UndoneAt.Equal(now.Add(time.Hour)) || !store.given[0].Undone() {
		t.Fatalf("not undone with when: %+v %v", undone, err)
	}
	again, err := s.Undo(t.Context(), p.ID, now.Add(2*time.Hour))
	if err != nil || !again.UndoneAt.Equal(now.Add(time.Hour)) {
		t.Errorf("undoing again changed something: %+v %v", again, err)
	}
	if _, err := s.Undo(t.Context(), 9, now); !errors.Is(err, feedback.ErrNotInRecord) {
		t.Errorf("err %v, want ErrNotInRecord", err)
	}
}
