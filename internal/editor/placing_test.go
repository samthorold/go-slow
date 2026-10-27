package editor_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

var oct16 = time.Date(2026, time.October, 16, 0, 0, 0, 0, time.UTC)

// speechFacts are the speech's Facts as reading drew them, with the ids the
// Record gave them.
var toPlace = editor.ToPlace{
	Item: speech,
	Facts: []editor.Fact{
		{ID: 11, SourceID: speech.SourceID, EntryID: speech.EntryID, AsAt: oct16,
			Statement: "Andy Burnham gave a speech in Motherwell.", Quote: "to be here in Motherwell"},
	},
}

// Two Stories that share the speech Fact's Entities, and one that does not.
var (
	scotland = editor.Story{ID: 3, Title: "The Prime Minister's offer to Scotland",
		Entities: []string{"Andy Burnham", "Scottish Government"},
		Timeline: []editor.Development{{ItemTitle: "Offer to Holyrood", Fact: editor.Fact{ID: 7, AsAt: oct16.AddDate(0, 0, -1),
			Statement: "Andy Burnham made an offer to the Scottish Government."}}}}
	lanarkshire = editor.Story{ID: 4, Title: "Steel in Motherwell", Entities: []string{"Motherwell"}}
	tyrone      = editor.Story{ID: 5, Title: "Lieutenancy of County Tyrone", Entities: []string{"County Tyrone"}}
)

func TestEveryFactOfAnItemIsWeighedAgainstTheItemsCandidatesGivenOnce(t *testing.T) {
	two := toPlace
	two.Facts = append(slices.Clone(toPlace.Facts), editor.Fact{ID: 12, SourceID: speech.SourceID, EntryID: speech.EntryID,
		AsAt: oct16, Statement: "The steelworks will stay open."})
	sharing := scotland
	sharing.Shares = []string{"Andy Burnham"}
	steel := lanarkshire
	steel.Shares = []string{"Motherwell"}
	model := &fakeModel{output: `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": ["S3"]},
		{"fact": "F2", "developments_in": ["S4"]}]}`}

	p, err := editor.Place(t.Context(), model, two, []editor.Story{sharing, steel})
	if err != nil {
		t.Fatal(err)
	}
	if want := []editor.Placement{{FactID: 11, StoryID: 3}, {FactID: 12, StoryID: 4}}; !slices.Equal(p.Developments, want) {
		t.Errorf("Developments = %+v, want %+v: a Fact may be placed in any of its Item's candidates", p.Developments, want)
	}

	in := model.sent[0].Input
	for _, s := range []string{scotland.Title, steel.Title, "Andy Burnham made an offer to the Scottish Government."} {
		if strings.Count(in, s) != 1 {
			t.Errorf("the model is shown %q %d times, want once:\n%s", s, strings.Count(in, s), in)
		}
	}
	if !strings.Contains(in, "Shares: Andy Burnham\n") || !strings.Contains(in, "Shares: Motherwell\n") {
		t.Errorf("the model is not shown what each candidate shares with the Item:\n%s", in)
	}
	if strings.Contains(in, "Entities:") {
		t.Errorf("the model is shown Entities per Fact, which no longer decide its candidates:\n%s", in)
	}

	// A Development in a Story that is not a candidate is refused.
	model.output = `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": ["S5"]}, {"fact": "F2", "developments_in": []}]}`
	if _, err := editor.Place(t.Context(), model, two, []editor.Story{sharing, steel}); !errors.Is(err, editor.ErrFailedChecks) {
		t.Errorf("err = %v, want ErrFailedChecks for a Development in a Story that is not a candidate", err)
	}
}

func TestAFactCanBeADevelopmentInSeveralStoriesOrInNone(t *testing.T) {
	stories := []editor.Story{scotland, lanarkshire}
	model := &fakeModel{output: `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": ["S3", "S4"]}]}`}

	p, err := editor.Place(t.Context(), model, toPlace, stories)
	if err != nil {
		t.Fatal(err)
	}
	want := []editor.Placement{{FactID: 11, StoryID: 3}, {FactID: 11, StoryID: 4}}
	if !slices.Equal(p.Developments, want) {
		t.Errorf("Developments = %+v, want %+v", p.Developments, want)
	}

	model.output = `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": []}]}`
	p, err = editor.Place(t.Context(), model, toPlace, stories)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Developments)+len(p.NewStories) != 0 || !slices.Equal(p.Facts, []int64{11}) {
		t.Errorf("a Fact in no Story: %+v, want it placed with no Developments", p)
	}
}

func TestAPlacingThatFailsTheChecksIsRejected(t *testing.T) {
	two := toPlace
	two.Facts = append(slices.Clone(toPlace.Facts), editor.Fact{ID: 12, SourceID: speech.SourceID, EntryID: speech.EntryID,
		AsAt: oct16, Statement: "Scotland will get £2 billion."})
	for name, output := range map[string]string{
		"output that is not JSON": `{"facts": [`,
		"a Fact left unplaced":    `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": []}]}`,
		"a Fact placed twice": `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": []},
			{"fact": "F1", "developments_in": []}, {"fact": "F2", "developments_in": []}]}`,
		"a Fact not given to place": `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": []},
			{"fact": "F2", "developments_in": []}, {"fact": "F3", "developments_in": []}]}`,
		"a Story that does not exist": `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": ["S99"]},
			{"fact": "F2", "developments_in": []}]}`,
		"a new Story with no title": `{"new_stories": [{"ref": "N1", "title": " "}], "facts": [
			{"fact": "F1", "developments_in": []}, {"fact": "F2", "developments_in": ["N1"]}]}`,
		"a new Story no Fact starts": `{"new_stories": [{"ref": "N1", "title": "Scotland's money"}], "facts": [
			{"fact": "F1", "developments_in": []}, {"fact": "F2", "developments_in": []}]}`,
		"two new Stories with one ref": `{"new_stories": [{"ref": "N1", "title": "A"}, {"ref": "N1", "title": "B"}], "facts": [
			{"fact": "F1", "developments_in": ["N1"]}, {"fact": "F2", "developments_in": ["N1"]}]}`,
		// A new Story is started only when no candidate fits.
		"a new Story started only by Facts that fit a candidate": `{"new_stories": [{"ref": "N1", "title": "A"}], "facts": [
			{"fact": "F1", "developments_in": ["S3", "N1"]}, {"fact": "F2", "developments_in": []}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := editor.Place(t.Context(), &fakeModel{output: output}, two, []editor.Story{scotland})
			if !errors.Is(err, editor.ErrFailedChecks) {
				t.Errorf("err = %v, want ErrFailedChecks", err)
			}
		})
	}
}

func TestTheSchemaOffersOnlyTheFactsToPlaceAndTheItemsCandidates(t *testing.T) {
	// A model answering a Fact by its statement rather than its ref fails
	// the checks; the schema rules it out instead.
	model := &fakeModel{output: `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": []}]}`}
	if _, err := editor.Place(t.Context(), model, toPlace, []editor.Story{scotland, tyrone}); err != nil {
		t.Fatal(err)
	}

	item := func(m any, path ...string) any {
		for _, p := range path {
			m = m.(map[string]any)[p]
		}
		return m
	}
	facts := item(model.sent[0].Schema, "properties", "facts", "items", "properties")
	if got := item(facts, "fact", "enum"); !slices.Equal(got.([]string), []string{"F1"}) {
		t.Errorf("fact refs = %v, want [F1]", got)
	}
	if got := item(facts, "developments_in", "items", "enum"); !slices.Equal(got.([]string), []string{"S3", "S5", "N1"}) {
		t.Errorf("Story refs = %v, want the candidates S3 and S5 and one new Story per Fact", got)
	}
}

func TestAFactThatFitsNoCandidateCanStartANewStory(t *testing.T) {
	model := &fakeModel{output: `{
	  "new_stories": [{"ref": "N1", "title": "The Prime Minister's offer to Scotland"}],
	  "facts": [{"fact": "F1", "developments_in": ["N1"]}]
	}`}

	p, err := editor.Place(t.Context(), model, toPlace, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(p.NewStories) != 1 || p.NewStories[0].Title != "The Prime Minister's offer to Scotland" ||
		len(p.NewStories[0].FactIDs) != 1 || p.NewStories[0].FactIDs[0] != 11 {
		t.Errorf("new Stories = %+v, want one started by Fact 11", p.NewStories)
	}
	if len(p.Developments) != 0 {
		t.Errorf("Developments in existing Stories = %+v, want none", p.Developments)
	}
	if len(p.Facts) != 1 || p.Facts[0] != 11 {
		t.Errorf("placed Facts = %v, want [11]", p.Facts)
	}
}
