package editor_test

import (
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// twoStories is a Record holding the speech, in the Story it started, and
// the later readout, placed by placing.
func twoStories(t *testing.T, placing string) *sqlite.Store {
	t.Helper()
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	readLater(t, store)
	ed, _ := placer(store, &fakeModel{output: placing})
	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	return store
}

// inANewStory places the readout's Fact in a Story of its own.
const inANewStory = `{"new_stories": [{"ref": "N1", "title": "Relations with Holyrood"}],
  "facts": [{"fact": "F1", "developments_in": ["N1"]}]}`

func connected(t *testing.T, store *sqlite.Store, id int64) []editor.StoryConnection {
	t.Helper()
	cs, err := store.ConnectedStories(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func TestTwoStoriesWhoseFactsInvolveAnEntityInTheSameRoleAreConnectedOnEachSide(t *testing.T) {
	store := twoStories(t, inANewStory)
	if cs := connected(t, store, 1); len(cs) != 0 {
		t.Fatalf("before connecting gives the Entities their roles, the Stories are connected: %+v", cs)
	}
	ed, _ := connector(store, &inTurn{outputs: []string{speechRoles, readoutCausedBySpeech}})
	if err := ed.ConnectWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	for _, side := range []struct {
		id, other   int64
		here, there string
	}{{1, 2, speech.EntryID, laterItem.EntryID}, {2, 1, laterItem.EntryID, speech.EntryID}} {
		cs := connected(t, store, side.id)
		if len(cs) != 1 || cs[0].ID != side.other {
			t.Fatalf("Story %d is connected to %+v, want Story %d", side.id, cs, side.other)
		}
		if len(cs[0].Shared) != 0 || len(cs[0].Entities) != 1 {
			t.Fatalf("Story %d: reasons %+v, want the one shared Entity", side.id, cs[0])
		}
		e := cs[0].Entities[0]
		if e.Entity != "Andy Burnham" || e.Role != "actor" || e.Here.EntryID != side.here || e.There.EntryID != side.there ||
			e.Here.ItemTitle == "" || e.There.ItemTitle == "" {
			t.Errorf("Story %d: the reason is not Andy Burnham as actor, shown by each side's Fact: %+v", side.id, e)
		}
	}
}

func TestAnEntityInADifferentRoleDoesNotConnectTwoStories(t *testing.T) {
	store := twoStories(t, inANewStory)
	affected := `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "affected"}, {"entity": "Edinburgh", "role": "place"}]}], "causes": []}`
	ed, _ := connector(store, &inTurn{outputs: []string{speechRoles, affected}})
	if err := ed.ConnectWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if cs := connected(t, store, 1); len(cs) != 0 {
		t.Errorf("Stories connected by an Entity in different roles: %+v", cs)
	}
}

func TestAnInvolvementConnectingAddsConnectsStoriesByItsEntityAndRole(t *testing.T) {
	// The speech's Fact did not list Scotland, which its reading gave in a
	// Claim; a later Item's Fact, in a Story of its own, lists it.
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	visit := collect.Item{SourceID: number10.ID, EntryID: "visit", Title: "PM visit to Glasgow",
		Link: "https://www.gov.uk/visit", Published: laterItem.Published, Attribution: collect.OGL, CollectedAt: saturday}
	if _, err := store.AddItems(t.Context(), []collect.Item{visit}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveReading(t.Context(), editor.Reading{
		SourceID: visit.SourceID, EntryID: visit.EntryID, Model: "fake",
		Entities: []editor.Entity{{Name: "Scotland", Kind: "place"}},
		Facts: []editor.Fact{{Statement: "Scotland's ferries were funded.", AsAt: oct16.AddDate(0, 0, 1),
			Quote: "ferries were funded", Entities: []string{"Scotland"}}},
	}, saturday); err != nil {
		t.Fatal(err)
	}
	ed, _ := placer(store, &fakeModel{output: `{"new_stories": [{"ref": "N1", "title": "Scottish ferries"}],
	  "facts": [{"fact": "F1", "developments_in": ["N1"]}]}`})
	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	visitRoles := `{"facts": [{"fact": "F1", "roles": [{"entity": "Scotland", "role": "place"}]}], "causes": []}`
	ed, logs := connector(store, &inTurn{outputs: []string{speechRolesWithScotland, visitRoles}})

	if err := ed.ConnectWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	cs := connected(t, store, 1)
	if len(cs) != 1 || cs[0].ID != 2 || len(cs[0].Entities) != 1 ||
		cs[0].Entities[0].Entity != "Scotland" || cs[0].Entities[0].Role != "place" {
		t.Errorf("the speech's Story is connected to %+v, want the ferries' Story by Scotland as place\n%s", cs, logs)
	}
}

func TestARoleGivenToANameOfAnotherItemsReadingIsRefused(t *testing.T) {
	// Scotland is in the Record, from the speech's reading, but the readout's
	// reading never gave it.
	store := twoStories(t, inANewStory)
	readoutWithScotland := `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"},
	  {"entity": "Edinburgh", "role": "place"}, {"entity": "Scotland", "role": "place"}]}], "causes": []}`
	ed, logs := connector(store, &inTurn{outputs: []string{speechRoles, readoutWithScotland}})

	if err := ed.ConnectWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	waiting, err := store.FactsToConnect(t.Context())
	if err != nil || len(waiting) != 1 || waiting[0].Item.EntryID != laterItem.EntryID ||
		!strings.Contains(logs.String(), `F1 gives a role to \"Scotland\"`) {
		t.Errorf("awaiting connecting: %+v %v, want the readout refused for Scotland\n%s", waiting, err, logs)
	}
}

func TestTwoStoriesSharingADevelopmentAreConnectedTheMomentItIsPlaced(t *testing.T) {
	// A third Item's Fact moves on both Stories: no connecting has run, and
	// nothing is stored between the Stories.
	store := twoStories(t, inANewStory)
	third := collect.Item{SourceID: number10.ID, EntryID: "visit", Title: "PM visit to Glasgow",
		Link: "https://www.gov.uk/visit", Published: laterItem.Published.Add(time.Hour), Attribution: collect.OGL, CollectedAt: saturday}
	if _, err := store.AddItems(t.Context(), []collect.Item{third}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveReading(t.Context(), editor.Reading{
		SourceID: third.SourceID, EntryID: third.EntryID, Model: "fake",
		Entities: []editor.Entity{{Name: "Andy Burnham", Kind: "person"}},
		Facts: []editor.Fact{{Statement: "Andy Burnham visited Glasgow.", AsAt: oct16.AddDate(0, 0, 2),
			Quote: "visited Glasgow", Entities: []string{"Andy Burnham"}}},
	}, saturday); err != nil {
		t.Fatal(err)
	}
	ed, logs := placer(store, &fakeModel{output: `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": ["S1", "S2"]}]}`})
	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	for _, side := range [][2]int64{{1, 2}, {2, 1}} {
		cs := connected(t, store, side[0])
		if len(cs) != 1 || cs[0].ID != side[1] || len(cs[0].Shared) != 1 ||
			cs[0].Shared[0].EntryID != third.EntryID || cs[0].Title == "" {
			t.Errorf("Story %d: connected %+v, want Story %d through the visit's Fact\n%s", side[0], cs, side[1], logs)
		}
	}
}
