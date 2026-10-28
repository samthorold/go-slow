package sqlite_test

import (
	"slices"
	"testing"
)

// toAdd are the ids of the Facts offered as a Development the Story missed,
// in the order offered.
func (s *stories) toAdd(story int64, limit int) []int64 {
	s.t.Helper()
	got, err := s.store.FactsToAdd(s.t.Context(), story, limit)
	if err != nil {
		s.t.Fatal(err)
	}
	var ids []int64
	for _, f := range got {
		ids = append(ids, f.ID)
	}
	return ids
}

func TestTheFactsOfferedAreCappedAndThoseSharingAsMuchGoNewestFirst(t *testing.T) {
	r := newStories(t)
	story := r.story(saturday.AddDate(0, 0, -30), "Prime Minister")
	var facts []int64
	for i := range 5 {
		_, f := r.item(saturday.AddDate(0, 0, -10+i), "Prime Minister")
		facts = append(facts, f)
	}

	if got := r.toAdd(story, 3); !slices.Equal(got, []int64{facts[4], facts[3], facts[2]}) {
		t.Errorf("facts to add = %v, want the cap of three, newest first: %v", got, []int64{facts[4], facts[3], facts[2]})
	}
}

func TestAFactSharingARareEntityWithTheStoryIsOfferedAheadOfOneSharingOnlyACommonOne(t *testing.T) {
	r := newStories(t)
	day := saturday.AddDate(0, 0, -10)
	story := r.story(day, "Prime Minister", "Great British Railways")
	r.story(day, "Prime Minister", "County Tyrone")
	r.story(day, "Prime Minister", "Scottish Government")
	_, rare := r.item(saturday.AddDate(0, 0, -5), "Great British Railways")
	_, common := r.item(saturday, "Prime Minister")

	got := r.toAdd(story, 20)

	if len(got) != 4 || got[0] != rare || !slices.Contains(got, common) {
		t.Errorf("facts to add = %v, want four, the older Fact sharing Great British Railways (%d) first", got, rare)
	}
}
