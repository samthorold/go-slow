package sqlite_test

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/feedback"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
)

var saturday = time.Date(2026, time.October, 24, 7, 30, 0, 0, time.UTC)

// speechItem is the Item threeFacts keeps, which mentions Andy Burnham.
var speechItem = editor.Item{SourceID: "number-10", EntryID: "speech"}

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
		return editor.Fact{Statement: s, AsAt: saturday, Quote: s}
	}
	if err := store.SaveFactReading(t.Context(), editor.FactReading{SourceID: src.ID, EntryID: "speech", Model: "fake",
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

	stories, err := store.CandidateStories(ctx, speechItem, 20)
	if err != nil || len(stories) != 1 || len(stories[0].Timeline) != 1 || stories[0].Timeline[0].ID != 2 {
		t.Errorf("placing's candidates hold a withdrawn Development: %+v %v", stories, err)
	}
	if toPlace, err := store.FactsToPlace(ctx); err != nil || len(toPlace) != 0 {
		t.Errorf("a withdrawn Fact awaits placing: %+v %v", toPlace, err)
	}
	near, err := store.FactsNear(ctx, editor.ToConnect{Item: speechItem, Facts: []editor.Fact{{ID: 2, AsAt: saturday}}}, 10)
	if err != nil || len(near) != 0 {
		t.Errorf("connecting is shown a withdrawn Fact: %+v %v", near, err)
	}
	toBrief, err := store.ToBrief(ctx, saturday.AddDate(0, 0, -1), saturday)
	if err != nil || len(toBrief) != 1 || len(toBrief[0].Today) != 1 || toBrief[0].Today[0].ID != 2 {
		t.Errorf("the Briefing is given a withdrawn Development: %+v %v", toBrief, err)
	}
}

// The Facts the Owner may say a Story missed are found by what their Items
// mention, as placing finds candidates: no Fact here is connected, so none
// involves Andy Burnham, yet the third's Item mentions him.
func TestTheFactsToAddToAStoryAreThoseWhoseItemsMentionAnEntityItsItemsMention(t *testing.T) {
	store := threeFacts(t)

	facts, err := store.FactsToAdd(t.Context(), 1, 20)

	if err != nil || len(facts) != 1 || facts[0].ID != 3 {
		t.Errorf("facts to add = %+v %v, want the third", facts, err)
	}
}

// give gives the Owner's Feedback, returning its id.
func give(t *testing.T, store *sqlite.Store, d feedback.Draft) int64 {
	t.Helper()
	f, errs, err := feedback.New(store).Give(t.Context(), d, saturday)
	if err != nil || errs != nil {
		t.Fatalf("give Feedback %+v: %v %v", d, errs, err)
	}
	return f.ID
}

// undo undoes a piece of the Owner's Feedback, an hour after it was given.
func undo(t *testing.T, store *sqlite.Store, id int64) {
	t.Helper()
	if _, err := feedback.New(store).Undo(t.Context(), id, saturday.Add(time.Hour)); err != nil {
		t.Fatalf("undo Feedback %d: %v", id, err)
	}
}

// briefed are the ids of the Facts the Briefing would be given today.
func briefed(t *testing.T, store *sqlite.Store) []int64 {
	t.Helper()
	toBrief, err := store.ToBrief(t.Context(), saturday.AddDate(0, 0, -1), saturday)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, tb := range toBrief {
		for _, d := range tb.Today {
			ids = append(ids, d.ID)
		}
	}
	return ids
}

func TestUndoingAWrongFactRestoresItAndKeepsThePieceMarkedUndone(t *testing.T) {
	store := threeFacts(t)
	id := give(t, store, feedback.Draft{Kind: "wrong", Level: "fact", Fact: "1"})

	undo(t, store, id)

	if got := briefed(t, store); !slices.Equal(got, []int64{1, 2}) {
		t.Errorf("the Briefing is given %v, want the undone Fact back with the other: [1 2]", got)
	}
	kept, err := store.KeptFeedback(t.Context())
	if err != nil || len(kept) != 1 {
		t.Fatalf("the undone piece is not kept: %+v %v", kept, err)
	}
	if !kept[0].UndoneAt.Equal(saturday.Add(time.Hour)) || kept[0].Kind != feedback.Wrong || kept[0].FactID != 1 {
		t.Errorf("the piece is not kept as given, marked undone with when: %+v", kept[0])
	}
}

func TestKeptFeedbackOnAFactCarriesTheFactsItem(t *testing.T) {
	store := threeFacts(t)
	give(t, store, feedback.Draft{Kind: "new to me", Level: "fact", Fact: "3"})
	give(t, store, feedback.Draft{Kind: "unwanted", Level: "story", Story: "1"})

	kept, err := store.KeptFeedback(t.Context())
	if err != nil || len(kept) != 2 {
		t.Fatalf("kept Feedback: %+v %v", kept, err)
	}
	type item struct{ SourceID, EntryID string }
	for _, k := range kept {
		want := item{}
		if k.Level == feedback.Fact {
			want = item{SourceID: "number-10", EntryID: "speech"}
		}
		if got := (item{SourceID: k.SourceID, EntryID: k.EntryID}); got != want {
			t.Errorf("%s Feedback carries Item %+v, want %+v", k.Level, got, want)
		}
	}
}

func TestUndoingUnwantedPutsTheStoryOrDevelopmentBackInTheBriefing(t *testing.T) {
	for _, d := range []feedback.Draft{
		{Kind: "unwanted", Level: "story", Story: "1"},
		{Kind: "unwanted", Level: "development", Story: "1", Fact: "1"},
	} {
		store := threeFacts(t)
		id := give(t, store, d)
		if got := briefed(t, store); slices.Contains(got, 1) {
			t.Fatalf("%s: the unwanted is still briefed: %v", d.Level, got)
		}

		undo(t, store, id)

		if got := briefed(t, store); !slices.Equal(got, []int64{1, 2}) {
			t.Errorf("%s: the Briefing is given %v, want it wanted again: [1 2]", d.Level, got)
		}
		st, _, err := store.Story(t.Context(), 1)
		if err != nil || st.Unwanted || st.Timeline[0].Unwanted {
			t.Errorf("%s: the Record still marks it unwanted: %+v %v", d.Level, st, err)
		}
	}
}

// timeline are the ids of the Facts on the Story's Timeline, as its page
// shows it and as the acts see it.
func timeline(t *testing.T, store *sqlite.Store, story int64) (page, acts []int64) {
	t.Helper()
	st, _, err := store.Story(t.Context(), story)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range st.Timeline {
		page = append(page, d.ID)
	}
	seen, err := store.CandidateStories(t.Context(), speechItem, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range seen {
		if s.ID == story {
			for _, d := range s.Timeline {
				acts = append(acts, d.ID)
			}
		}
	}
	return page, acts
}

func TestUndoingAMissedDevelopmentTakesItOutAndItCanBeAddedAgain(t *testing.T) {
	store := threeFacts(t)
	missed := feedback.Draft{Kind: "missed", Level: "development", Story: "1", Fact: "3"}
	id := give(t, store, missed)

	undo(t, store, id)

	if page, acts := timeline(t, store, 1); !slices.Equal(page, []int64{1, 2}) || !slices.Equal(acts, []int64{1, 2}) {
		t.Errorf("the undone Development is still added: page %v, acts %v, want [1 2]", page, acts)
	}
	give(t, store, missed)
	if page, _ := timeline(t, store, 1); !slices.Equal(page, []int64{1, 2, 3}) {
		t.Errorf("giving it again did not add it: %v", page)
	}
}

func TestUndoingAMissedStoryTakesItOut(t *testing.T) {
	store := threeFacts(t)
	id := give(t, store, feedback.Draft{Kind: "missed", Level: "story", Title: "Travels", Facts: []string{"3"}})

	undo(t, store, id)

	if _, acts := timeline(t, store, 2); len(acts) != 0 {
		t.Errorf("the acts still see the undone Story: %v", acts)
	}
	list, err := store.ListStories(t.Context())
	if err != nil || len(list) != 1 || list[0].ID != 1 {
		t.Errorf("the undone Story is still listed: %+v %v", list, err)
	}
}

func TestAMarkAnotherPieceAlsoMadeStaysUntilBothAreUndone(t *testing.T) {
	store := threeFacts(t)
	first := give(t, store, feedback.Draft{Kind: "wrong", Level: "fact", Fact: "1"})
	second := give(t, store, feedback.Draft{Kind: "wrong", Level: "fact", Fact: "1"})

	undo(t, store, first)
	if got := briefed(t, store); !slices.Equal(got, []int64{2}) {
		t.Errorf("after undoing one of two wrong pieces the Briefing is given %v, want the Fact still withdrawn: [2]", got)
	}
	undo(t, store, second)
	if got := briefed(t, store); !slices.Equal(got, []int64{1, 2}) {
		t.Errorf("after undoing both the Briefing is given %v, want [1 2]", got)
	}
}
