package sqlite_test

import (
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// stories is a constructed Record of Items, each read with the Entities it
// mentions and one Fact tied to none of them, so what finds a Story can only
// be what its Items mention.
type stories struct {
	t     *testing.T
	store *sqlite.Store
	facts int64
	n     int
}

func newStories(t *testing.T) *stories {
	t.Helper()
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "go-slow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	src := collect.Source{ID: "number-10", Name: "Number 10", FeedURL: "https://example.org/feed", Attribution: collect.OGL, UsualGap: time.Hour}
	if err := store.SyncSources(t.Context(), []collect.Source{src}, saturday.AddDate(0, -1, 0)); err != nil {
		t.Fatal(err)
	}
	return &stories{t: t, store: store}
}

// item reads an Item published on day, mentioning the Entities named, with
// one Fact as at day: the Item and its Fact's id.
func (s *stories) item(day time.Time, mentions ...string) (editor.Item, int64) {
	s.t.Helper()
	s.n++
	entry := "item-" + strconv.Itoa(s.n)
	it := editor.Item{SourceID: "number-10", EntryID: entry, Title: entry, Published: day}
	if _, err := s.store.AddItems(s.t.Context(), []collect.Item{{SourceID: it.SourceID, EntryID: entry, Title: entry,
		Link: "https://example.org/" + entry, Published: day, Updated: day, Attribution: collect.OGL, CollectedAt: day}}); err != nil {
		s.t.Fatal(err)
	}
	var es []editor.Entity
	for _, m := range mentions {
		es = append(es, editor.Entity{Name: m, Kind: "organisation"})
	}
	if err := s.store.SaveReading(s.t.Context(), editor.Reading{SourceID: it.SourceID, EntryID: entry, Model: "fake",
		Entities: es, Facts: []editor.Fact{{Statement: entry + " happened.", AsAt: day, Quote: entry}}}, day); err != nil {
		s.t.Fatal(err)
	}
	s.facts++
	return it, s.facts
}

// story starts a Story whose one Development is the Fact of an Item
// published on day mentioning the Entities named: the Story's id.
func (s *stories) story(day time.Time, mentions ...string) int64 {
	s.t.Helper()
	it, fact := s.item(day, mentions...)
	if err := s.store.SavePlacing(s.t.Context(), editor.Placing{SourceID: it.SourceID, EntryID: it.EntryID, Model: "fake",
		Facts: []int64{fact}, NewStories: []editor.NewStory{{Title: it.EntryID, FactIDs: []int64{fact}}}}, day); err != nil {
		s.t.Fatal(err)
	}
	ls, err := s.store.ListStories(s.t.Context())
	if err != nil {
		s.t.Fatal(err)
	}
	var latest int64
	for _, l := range ls {
		latest = max(latest, l.ID)
	}
	return latest
}

// candidates are the ids of the Stories placing weighs its Facts against,
// in rank order.
func (s *stories) candidates(it editor.Item, limit int) []int64 {
	s.t.Helper()
	got, err := s.store.CandidateStories(s.t.Context(), it, limit)
	if err != nil {
		s.t.Fatal(err)
	}
	var ids []int64
	for _, st := range got {
		ids = append(ids, st.ID)
	}
	return ids
}

func TestTheFactsNearAnItemsAreThoseWhoseItemsMentionAnEntityItMentionsOrInItsStories(t *testing.T) {
	r := newStories(t)
	_, clyde := r.item(saturday.AddDate(0, 0, -3), "Clyde")
	r.item(saturday.AddDate(0, 0, -2), "County Tyrone")
	it, mine := r.item(saturday, "Clyde", "Glasgow")
	// A Fact sharing nothing the Item mentions, in a Story with the Item's
	// Fact.
	other, ferries := r.item(saturday.AddDate(0, 0, -1), "Arran")
	if err := r.store.SavePlacing(t.Context(), editor.Placing{SourceID: it.SourceID, EntryID: it.EntryID, Model: "fake",
		Facts: []int64{mine}, NewStories: []editor.NewStory{{Title: "Ferries", FactIDs: []int64{mine}}}}, saturday); err != nil {
		t.Fatal(err)
	}
	if err := r.store.SavePlacing(t.Context(), editor.Placing{SourceID: other.SourceID, EntryID: other.EntryID, Model: "fake",
		Facts: []int64{ferries}, Developments: []editor.Placement{{FactID: ferries, StoryID: 1}}}, saturday); err != nil {
		t.Fatal(err)
	}

	near, err := r.store.FactsNear(t.Context(), editor.ToConnect{Item: it, Facts: []editor.Fact{{ID: mine, AsAt: saturday}}}, 30)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, f := range near {
		ids = append(ids, f.ID)
	}
	if !slices.Equal(ids, []int64{ferries, clyde}) {
		t.Errorf("Facts near = %v, want the Story's %d, then %d, whose Item mentions the Clyde, nearest first", ids, ferries, clyde)
	}
}

func TestTheCandidatesAreTheStoriesWhoseItemsMentionAnEntityTheItemMentions(t *testing.T) {
	r := newStories(t)
	railways := r.story(saturday.AddDate(0, 0, -3), "Great British Railways")
	r.story(saturday.AddDate(0, 0, -2), "County Tyrone")
	it, _ := r.item(saturday, "Great British Railways", "Derby")

	if got := r.candidates(it, 20); !slices.Equal(got, []int64{railways}) {
		t.Errorf("candidates = %v, want only the Story whose Item mentions Great British Railways (%d)", got, railways)
	}
}

func TestARareSharedEntityOutranksACommonOne(t *testing.T) {
	r := newStories(t)
	day := saturday.AddDate(0, 0, -1)
	pm := r.story(day, "Prime Minister")
	r.story(day, "Prime Minister", "County Tyrone")
	r.story(day, "Prime Minister", "Scottish Government")
	railways := r.story(day, "Great British Railways")
	it, _ := r.item(saturday, "Prime Minister", "Great British Railways")

	if got := r.candidates(it, 20); len(got) != 4 || got[0] != railways || !slices.Contains(got[1:], pm) {
		t.Errorf("candidates = %v, want all four, the Story sharing Great British Railways (%d), in one Story, first", got, railways)
	}
}

func TestAQuietStorySharingARareEntityOutranksARecentOneSharingOnlyACommonOne(t *testing.T) {
	r := newStories(t)
	quiet := r.story(saturday.AddDate(0, -3, 0), "Great British Railways", "Prime Minister")
	r.story(saturday.AddDate(0, 0, -5), "Prime Minister", "County Tyrone")
	recent := r.story(saturday.AddDate(0, 0, -1), "Prime Minister")
	it, _ := r.item(saturday, "Prime Minister", "Great British Railways")

	if got := r.candidates(it, 2); !slices.Equal(got, []int64{quiet, recent}) {
		t.Errorf("candidates = %v, want the quiet Story sharing Great British Railways (%d) first, then the most recent (%d)",
			got, quiet, recent)
	}
}

func TestCandidatesSharingAsMuchGoNewestFirst(t *testing.T) {
	r := newStories(t)
	older := r.story(saturday.AddDate(0, 0, -9), "Scottish Government")
	newer := r.story(saturday.AddDate(0, 0, -2), "Scottish Government")
	it, _ := r.item(saturday, "Scottish Government")

	if got := r.candidates(it, 20); !slices.Equal(got, []int64{newer, older}) {
		t.Errorf("candidates = %v, want the newer Story (%d) before the older (%d)", got, newer, older)
	}
}

func TestPlacingIsShownAtMostTheCapOfCandidatesEachWithWhatItShares(t *testing.T) {
	r := newStories(t)
	for i := range 25 {
		r.story(saturday.AddDate(0, 0, -30+i), "Prime Minister")
	}
	railways := r.story(saturday.AddDate(0, 0, -60), "Great British Railways", "Derby")
	it, _ := r.item(saturday, "Prime Minister", "Great British Railways", "Derby")

	got, err := r.store.CandidateStories(t.Context(), it, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 20 {
		t.Fatalf("%d candidates, want the cap of 20", len(got))
	}
	if got[0].ID != railways || !slices.Equal(got[0].Shares, []string{"Derby", "Great British Railways"}) {
		t.Errorf("first candidate %d sharing %v, want %d sharing Derby and Great British Railways", got[0].ID, got[0].Shares, railways)
	}
	if !slices.Equal(got[1].Shares, []string{"Prime Minister"}) || len(got[1].Timeline) != 1 {
		t.Errorf("second candidate %+v, want one sharing the Prime Minister, with its Timeline", got[1])
	}
}
