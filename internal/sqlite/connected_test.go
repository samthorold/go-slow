package sqlite_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
)

// read is an Item published on day, read into n Facts as at day and
// mentioning the Entities named: the Item and its Facts' ids.
func (s *stories) read(day time.Time, n int, mentions ...string) (editor.Item, []int64) {
	s.t.Helper()
	s.n++
	entry := "item-" + strconv.Itoa(s.n)
	it := editor.Item{SourceID: "number-10", EntryID: entry, Title: "Item " + strconv.Itoa(s.n), Published: day}
	if _, err := s.store.AddItems(s.t.Context(), []collect.Item{{SourceID: it.SourceID, EntryID: entry, Title: it.Title,
		Link: "https://example.org/" + entry, Published: day, Updated: day, Attribution: collect.OGL, CollectedAt: day}}); err != nil {
		s.t.Fatal(err)
	}
	var es []editor.Entity
	for _, m := range mentions {
		es = append(es, editor.Entity{Name: m, Kind: "organisation"})
	}
	var fs []editor.Fact
	var ids []int64
	for i := range n {
		fs = append(fs, editor.Fact{Statement: entry + " fact " + strconv.Itoa(i) + ".", AsAt: day, Quote: entry})
		s.facts++
		ids = append(ids, s.facts)
	}
	if err := s.store.SaveReading(s.t.Context(), editor.Reading{SourceID: it.SourceID, EntryID: entry, Model: "fake",
		Entities: es, Facts: fs}, day); err != nil {
		s.t.Fatal(err)
	}
	return it, ids
}

// place starts a Story of its own for each of the Item's Facts given: the
// Stories' ids, in order.
func (s *stories) place(it editor.Item, facts ...int64) []int64 {
	s.t.Helper()
	var ns []editor.NewStory
	for _, f := range facts {
		ns = append(ns, editor.NewStory{Title: it.Title + " story " + strconv.FormatInt(f, 10), FactIDs: []int64{f}})
	}
	if err := s.store.SavePlacing(s.t.Context(), editor.Placing{SourceID: it.SourceID, EntryID: it.EntryID, Model: "fake",
		Facts: facts, NewStories: ns}, it.Published); err != nil {
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
	var ids []int64
	for i := range facts {
		ids = append(ids, latest-int64(len(facts)-1-i))
	}
	return ids
}

// involve gives the Item's Fact its Entities in their roles, given as
// pairs: an Entity's name, then its role.
func (s *stories) involve(it editor.Item, fact int64, entityRoles ...string) {
	s.t.Helper()
	var rs []editor.Role
	for i := 0; i+1 < len(entityRoles); i += 2 {
		rs = append(rs, editor.Role{FactID: fact, Entity: entityRoles[i], Role: entityRoles[i+1]})
	}
	if err := s.store.SaveConnecting(s.t.Context(), editor.Connecting{SourceID: it.SourceID, EntryID: it.EntryID, Model: "fake",
		Facts: []int64{fact}, Roles: rs}, it.Published); err != nil {
		s.t.Fatal(err)
	}
}

func (s *stories) connected(id int64) []editor.StoryConnection {
	s.t.Helper()
	cs, err := s.store.ConnectedStories(s.t.Context(), id)
	if err != nil {
		s.t.Fatal(err)
	}
	return cs
}

func TestStoriesWithDevelopmentsFromTheSameItemAreConnectedByThatItem(t *testing.T) {
	r := newStories(t)
	update, facts := r.read(saturday, 2)
	ids := r.place(update, facts...)

	for _, side := range [][2]int64{{ids[0], ids[1]}, {ids[1], ids[0]}} {
		cs := r.connected(side[0])
		if len(cs) != 1 || cs[0].ID != side[1] {
			t.Fatalf("Story %d is connected to %+v, want Story %d", side[0], cs, side[1])
		}
		if len(cs[0].Items) != 1 || cs[0].Items[0].EntryID != update.EntryID || cs[0].Items[0].Title != update.Title {
			t.Errorf("Story %d: the reason is %+v, want the Item %q they were announced together in", side[0], cs[0], update.Title)
		}
		if len(cs[0].Shared) != 0 || len(cs[0].Entities) != 0 {
			t.Errorf("Story %d: reasons beyond the Item: %+v", side[0], cs[0])
		}
	}
}

// storyOf is a Story whose one Development is the Fact of its own Item,
// published on day, involving the Entities given in their roles, as pairs:
// the Story's id.
func (s *stories) storyOf(day time.Time, entityRoles ...string) int64 {
	s.t.Helper()
	var mentions []string
	for i := 0; i+1 < len(entityRoles); i += 2 {
		mentions = append(mentions, entityRoles[i])
	}
	it, facts := s.read(day, 1, mentions...)
	id := s.place(it, facts...)[0]
	s.involve(it, facts[0], entityRoles...)
	return id
}

func TestAnEntityInMoreThanTheCutOffShareOfStoriesConnectsNone(t *testing.T) {
	r := newStories(t)
	day := saturday.AddDate(0, 0, -1)
	// The government is in three of the four Stories, the minister in two.
	reform := r.storyOf(day, "UK Government", "actor", "Pat McFadden", "actor")
	allowance := r.storyOf(day, "UK Government", "actor", "Pat McFadden", "actor")
	ferries := r.storyOf(day, "UK Government", "actor")
	r.storyOf(day, "County Tyrone", "place")

	cs := r.connected(reform)
	if len(cs) != 1 || cs[0].ID != allowance || len(cs[0].Entities) != 1 || cs[0].Entities[0].Entity != "Pat McFadden" {
		t.Errorf("connected %+v, want Story %d by Pat McFadden alone, and not Story %d", cs, allowance, ferries)
	}
}

func TestEntityTiesAreRankedByHowFewStoriesTheEntityIsIn(t *testing.T) {
	r := newStories(t)
	day := saturday.AddDate(0, 0, -1)
	// The Cabinet Office is in three of the six Stories, Zoe Smith in two.
	mine := r.storyOf(day, "Cabinet Office", "actor", "Zoe Smith", "actor")
	common := r.storyOf(day, "Cabinet Office", "actor")
	rare := r.storyOf(day, "Cabinet Office", "actor", "Zoe Smith", "actor")
	for range 3 {
		r.storyOf(day, "County Tyrone", "place")
	}

	cs := r.connected(mine)
	if len(cs) != 2 || cs[0].ID != rare || cs[1].ID != common {
		t.Fatalf("connected %+v, want Story %d, sharing the rarer Entity, before Story %d", cs, rare, common)
	}
	if es := cs[0].Entities; len(es) != 2 || es[0].Entity != "Zoe Smith" || es[1].Entity != "Cabinet Office" {
		t.Errorf("Story %d's Entities %+v, want Zoe Smith, in fewer Stories, before the Cabinet Office", rare, es)
	}
}

func TestAnEntityInTheSameRoleConnectsNothingBetweenFactsOfTheSameItem(t *testing.T) {
	r := newStories(t)
	update, facts := r.read(saturday, 2, "Department for Work and Pensions")
	ids := r.place(update, facts...)
	for _, f := range facts {
		r.involve(update, f, "Department for Work and Pensions", "actor")
	}

	cs := r.connected(ids[0])
	if len(cs) != 1 || len(cs[0].Items) != 1 || len(cs[0].Entities) != 0 {
		t.Errorf("connected %+v, want Story %d by the Item alone, not by the Entity its Facts share", cs, ids[1])
	}
}
