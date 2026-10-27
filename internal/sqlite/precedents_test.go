package sqlite_test

import (
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/feedback"
)

// unwanted starts a Story whose one Development is the Fact of an Item
// published on day mentioning the Entities named, and gives the Owner's
// Feedback that the Development was unwanted, at saturday: the Feedback's id.
func (s *stories) unwanted(day time.Time, mentions ...string) int64 {
	s.t.Helper()
	story := s.story(day, mentions...)
	f, errs, err := feedback.New(s.store).Give(s.t.Context(), feedback.Draft{
		Kind: "unwanted", Level: "development",
		Story: strconv.FormatInt(story, 10), Fact: strconv.FormatInt(s.facts, 10),
	}, saturday)
	if err != nil || errs != nil {
		s.t.Fatalf("give Feedback: %v %v", errs, err)
	}
	return f.ID
}

func TestPlacingsPrecedentsAreFoundByTheEntitiesTheItemsMention(t *testing.T) {
	r := newStories(t)
	// The Facts are tied to no Entity, so only what their Items mention can
	// find a precedent.
	railways := r.unwanted(saturday.AddDate(0, 0, -3), "Great British Railways")
	both := r.unwanted(saturday.AddDate(0, 0, -5), "Great British Railways", "Derby")
	r.unwanted(saturday.AddDate(0, 0, -2), "County Tyrone")
	it, fact := r.item(saturday, "Great British Railways", "Derby")

	got, err := r.store.PlacingPrecedents(t.Context(), editor.ToPlace{Item: it, Facts: []editor.Fact{{ID: fact, AsAt: saturday}}}, 8)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, p := range got {
		ids = append(ids, p.FeedbackID)
	}
	if !slices.Equal(ids, []int64{both, railways}) {
		t.Errorf("precedents = %v, want the Feedback whose Item mentions both Entities (%d), then the one mentioning one (%d)",
			ids, both, railways)
	}
}
