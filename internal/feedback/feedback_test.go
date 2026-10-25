package feedback_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/feedback"
	"github.com/samthorold/go-slow/internal/interest"
)

// fakeStore keeps what it is given, and answers About with about.
type fakeStore struct {
	about   feedback.About
	given   []feedback.Feedback
	effects []feedback.Effect
}

func (s *fakeStore) About(context.Context, feedback.Feedback) (feedback.About, error) {
	return s.about, nil
}

func (s *fakeStore) Give(_ context.Context, f feedback.Feedback, e feedback.Effect) (int64, error) {
	s.given, s.effects = append(s.given, f), append(s.effects, e)
	return int64(len(s.given)), nil
}

var now = time.Date(2026, time.October, 24, 8, 0, 0, 0, time.UTC)

func TestNewToMeIsKeptAndChangesNothing(t *testing.T) {
	for _, d := range []feedback.Draft{
		{Kind: "new to me", Level: "story", Story: "1"},
		{Kind: "new to me", Level: "development", Story: "1", Fact: "2"},
		{Kind: "new to me", Level: "fact", Fact: "2"},
		{Kind: "new to me", Level: "connection", Fact: "2", ToFact: "3"},
	} {
		store := &fakeStore{about: feedback.About{Found: true, Developed: true, Depth: interest.Close}}
		f, errs, err := feedback.New(store).Give(t.Context(), d, now)
		if err != nil || errs != nil {
			t.Fatalf("%+v: %v %v", d, errs, err)
		}
		if len(store.given) != 1 || f.Depth != interest.Close || !f.GivenAt.Equal(now) {
			t.Errorf("%+v: not kept as given, with its Depth and when: %+v", d, store.given)
		}
		if store.effects[0] != (feedback.Effect{}) {
			t.Errorf("%+v: new to me changed the Record: %+v", d, store.effects[0])
		}
	}
}

func TestFeedbackTheLevelDoesNotTakeIsRefusedAndKeepsNothing(t *testing.T) {
	for _, d := range []feedback.Draft{
		{Kind: "wrong", Level: "story", Story: "1"},
		{Kind: "wrong", Level: "development", Story: "1", Fact: "2"},
		{Kind: "unwanted", Level: "fact", Fact: "2"},
		{Kind: "missed", Level: "connection", Fact: "2", ToFact: "3"},
		{Kind: "liked", Level: "story", Story: "1"},
		{Kind: "unwanted", Level: "development", Story: "1"},
	} {
		store := &fakeStore{about: feedback.About{Found: true, Developed: true}}
		_, errs, err := feedback.New(store).Give(t.Context(), d, now)
		if err != nil || len(errs) == 0 || len(store.given) != 0 {
			t.Errorf("%+v: errs %v, err %v, kept %d", d, errs, err, len(store.given))
		}
	}
}

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
		store := &fakeStore{about: feedback.About{Found: true}}
		f, errs, err := feedback.New(store).Give(t.Context(), tc.d, now)
		if err != nil || errs != nil {
			t.Fatalf("%+v: %v %v", tc.d, errs, err)
		}
		if len(store.given) != 1 || store.effects[0] != tc.want {
			t.Errorf("%+v: kept %+v with effect %+v, want %+v", tc.d, store.given, store.effects, tc.want)
		}
		if f.AssessmentID == 0 && f.ReadID == 0 {
			t.Errorf("%+v: not about the read or an Assessment: %+v", tc.d, f)
		}
	}
	for _, d := range []feedback.Draft{
		{Kind: "unwanted", Level: "assessment", Assessment: "4"},
		{Kind: "wrong", Level: "sunday read", Read: "2"},
		{Kind: "wrong", Level: "assessment"},
	} {
		store := &fakeStore{about: feedback.About{Found: true}}
		if _, errs, err := feedback.New(store).Give(t.Context(), d, now); err != nil || len(errs) == 0 || len(store.given) != 0 {
			t.Errorf("%+v: errs %v, err %v, kept %d", d, errs, err, len(store.given))
		}
	}
}

func TestFeedbackOnWhatIsNotInTheRecordIsAnError(t *testing.T) {
	store := &fakeStore{}
	_, _, err := feedback.New(store).Give(t.Context(), feedback.Draft{Kind: "wrong", Level: "fact", Fact: "9"}, now)
	if !errors.Is(err, feedback.ErrNotInRecord) || len(store.given) != 0 {
		t.Errorf("err %v, kept %d", err, len(store.given))
	}
}
