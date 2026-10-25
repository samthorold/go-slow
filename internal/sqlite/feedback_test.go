package sqlite_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/feedback"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
)

var saturday = time.Date(2026, time.October, 24, 7, 30, 0, 0, time.UTC)

// threeFacts keeps an Item tied to a Topic with three Facts about Andy
// Burnham: the first two placed together in a new Story, the third not yet
// placed.
func threeFacts(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "go-slow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	src := collect.Source{ID: "number-10", Name: "Number 10", FeedURL: "https://example.org/feed", Attribution: collect.OGL, UsualGap: time.Hour}
	if err := store.SyncSources(t.Context(), []collect.Source{src}, saturday.AddDate(0, 0, -7)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddItems(t.Context(), []collect.Item{{SourceID: src.ID, EntryID: "speech", Title: "Speech",
		Link: "https://example.org/speech", Published: saturday.Add(-time.Hour), Updated: saturday.Add(-time.Hour),
		Attribution: collect.OGL, CollectedAt: saturday.Add(-time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	if _, errs, err := interest.New(store).Add(t.Context(), interest.Draft{Name: "Burnham", Depth: "close", Terms: "Speech"}); err != nil || errs != nil {
		t.Fatalf("add Topic: %v %v", errs, err)
	}
	fact := func(s string) editor.Fact {
		return editor.Fact{Statement: s, AsAt: saturday, Quote: s, Entities: []string{"Andy Burnham"}}
	}
	if err := store.SaveReading(t.Context(), editor.Reading{SourceID: src.ID, EntryID: "speech", Model: "fake",
		Entities: []editor.Entity{{Name: "Andy Burnham", Kind: "person"}},
		Facts:    []editor.Fact{fact("One."), fact("Two."), fact("Three.")}}, saturday.Add(-50*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlacing(t.Context(), editor.Placing{SourceID: src.ID, EntryID: "speech", Model: "fake",
		Facts: []int64{1, 2}, NewStories: []editor.NewStory{{Title: "Burnham", FactIDs: []int64{1, 2}}}},
		saturday.Add(-40*time.Minute)); err != nil {
		t.Fatal(err)
	}
	return store
}

func wrong(t *testing.T, store *sqlite.Store, fact string) {
	t.Helper()
	if _, errs, err := feedback.New(store).Give(t.Context(),
		feedback.Draft{Kind: "wrong", Level: "fact", Fact: fact}, saturday); err != nil || errs != nil {
		t.Fatalf("give Feedback: %v %v", errs, err)
	}
}

func TestTheActsNeverSeeAWithdrawnFact(t *testing.T) {
	store := threeFacts(t)
	wrong(t, store, "1")
	wrong(t, store, "3")
	ctx := t.Context()

	stories, err := store.StoriesSharing(ctx, []string{"Andy Burnham"})
	if err != nil || len(stories) != 1 || len(stories[0].Timeline) != 1 || stories[0].Timeline[0].ID != 2 {
		t.Errorf("placing's candidates hold a withdrawn Development: %+v %v", stories, err)
	}
	if toPlace, err := store.FactsToPlace(ctx); err != nil || len(toPlace) != 0 {
		t.Errorf("a withdrawn Fact awaits placing: %+v %v", toPlace, err)
	}
	near, err := store.FactsNear(ctx, []editor.Fact{{ID: 2, AsAt: saturday}}, 10)
	if err != nil || len(near) != 0 {
		t.Errorf("connecting is shown a withdrawn Fact: %+v %v", near, err)
	}
	toBrief, err := store.ToBrief(ctx, saturday.AddDate(0, 0, -1), saturday)
	if err != nil || len(toBrief) != 1 || len(toBrief[0].Today) != 1 || toBrief[0].Today[0].ID != 2 {
		t.Errorf("the Briefing is given a withdrawn Development: %+v %v", toBrief, err)
	}
}
