package editor_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/editor"
)

// speechMentions are the Entities the speech's reading gave, anywhere: its
// Fact listed Andy Burnham and Motherwell, its Claim Scotland.
var speechMentions = []editor.Mention{
	{Name: "Andy Burnham", Kind: "person"}, {Name: "Motherwell", Kind: "place"}, {Name: "Scotland", Kind: "place"},
}

// toConnect is the speech's Fact, placed and awaiting connecting.
var toConnect = editor.ToConnect{Item: speech, Facts: toPlace.Facts, Mentions: speechMentions}

func TestConnectingIsShownTheItemsMentionsOnceAndNoEntitiesListedForAFact(t *testing.T) {
	model := &fakeModel{output: connectsTheSpeech}
	if _, err := editor.Connect(t.Context(), model, toConnect, []editor.Fact{offer}); err != nil {
		t.Fatal(err)
	}

	in := model.sent[0].Input
	for _, m := range []string{"Andy Burnham (person)", "Motherwell (place)", "Scotland (place)"} {
		if strings.Count(in, m) != 1 {
			t.Errorf("the input shows %q %d times, want once:\n%s", m, strings.Count(in, m), in)
		}
	}
	if strings.Contains(in, "Entities:") {
		t.Errorf("the input shows a Fact's Entities as reading listed them:\n%s", in)
	}
}

// offer is an earlier Fact that shares Andy Burnham with the speech's.
var offer = editor.Fact{ID: 7, SourceID: speech.SourceID, EntryID: "offer", AsAt: oct16.AddDate(0, 0, -1),
	Statement: "Andy Burnham made an offer to the Scottish Government.", Quote: "an offer",
	Entities: []string{"Andy Burnham", "Scottish Government"}}

const connectsTheSpeech = `{
  "facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}, {"entity": "Motherwell", "role": "place"}]}],
  "causes": [{"cause": "C1", "effect": "F1"}]
}`

func TestConnectingMakesACauseBetweenFactsAndGivesEachEntityItsRole(t *testing.T) {
	model := &fakeModel{output: connectsTheSpeech}

	c, err := editor.Connect(t.Context(), model, toConnect, []editor.Fact{offer})
	if err != nil {
		t.Fatal(err)
	}

	if len(c.Causes) != 1 || c.Causes[0] != (editor.Cause{CauseID: offer.ID, EffectID: 11}) {
		t.Errorf("Causes = %+v, want the offer causing the speech", c.Causes)
	}
	want := []editor.Role{{FactID: 11, Entity: "Andy Burnham", Role: "actor"}, {FactID: 11, Entity: "Motherwell", Role: "place"}}
	if len(c.Roles) != 2 || c.Roles[0] != want[0] || c.Roles[1] != want[1] {
		t.Errorf("Roles = %+v, want %+v", c.Roles, want)
	}
	if len(c.Facts) != 1 || c.Facts[0] != 11 || c.SourceID != speech.SourceID || c.EntryID != speech.EntryID {
		t.Errorf("the connecting does not carry its Item and Facts: %+v", c)
	}
}

func TestTheConnectingSchemaOffersOnlyTheFactsShownTheItemsMentionsAndTheRoles(t *testing.T) {
	model := &fakeModel{output: connectsTheSpeech}
	if _, err := editor.Connect(t.Context(), model, toConnect, []editor.Fact{offer}); err != nil {
		t.Fatal(err)
	}

	b, err := json.Marshal(model.sent[0].Schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"enum":["F1"]`, `"enum":["F1","C1"]`, `"enum":["Andy Burnham","Motherwell","Scotland"]`,
		`"enum":["actor","undergoer","place","occasion"]`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("the schema lacks %s:\n%s", want, b)
		}
	}
}

func TestARoleGivenToAnEntityOfTheItemsReadingThatTheFactDidNotListIsKept(t *testing.T) {
	// The speech's reading gave Scotland in its Claim; its Fact did not list it.
	output := `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}, {"entity": "Motherwell", "role": "place"},
	  {"entity": "Scotland", "role": "undergoer"}]}], "causes": []}`

	c, err := editor.Connect(t.Context(), &fakeModel{output: output}, toConnect, nil)
	if err != nil {
		t.Fatal(err)
	}

	if want := (editor.Role{FactID: 11, Entity: "Scotland", Role: "undergoer"}); !slices.Contains(c.Roles, want) {
		t.Errorf("Roles = %+v, want %+v kept", c.Roles, want)
	}
}

func TestARoleGivenToANameTheItemsReadingNeverGaveIsRefusedNamingTheFactAndTheName(t *testing.T) {
	output := `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}, {"entity": "Motherwell", "role": "place"},
	  {"entity": "Edinburgh", "role": "place"}]}], "causes": []}`

	_, err := editor.Connect(t.Context(), &fakeModel{output: output}, toConnect, nil)
	if !errors.Is(err, editor.ErrFailedChecks) || !strings.Contains(err.Error(), `F1`) || !strings.Contains(err.Error(), `"Edinburgh"`) {
		t.Errorf("err = %v, want ErrFailedChecks naming F1 and Edinburgh", err)
	}
}

func TestAFactMayInvolveFewerOfTheItemsMentionsThanReadingListedForItOrNone(t *testing.T) {
	// Reading listed Andy Burnham and Motherwell for the Fact.
	for name, output := range map[string]string{
		"fewer": `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}]}], "causes": []}`,
		"none":  `{"facts": [{"fact": "F1", "roles": []}], "causes": []}`,
	} {
		t.Run(name, func(t *testing.T) {
			c, err := editor.Connect(t.Context(), &fakeModel{output: output}, toConnect, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(c.Facts) != 1 || c.Facts[0] != 11 || slices.ContainsFunc(c.Roles, func(r editor.Role) bool { return r.Entity == "Motherwell" }) {
				t.Errorf("connecting = %+v, want F1 connected, involving no Motherwell", c)
			}
		})
	}
}

func TestAConnectingThatFailsTheChecksIsRejected(t *testing.T) {
	const roles = `[{"entity": "Andy Burnham", "role": "actor"}, {"entity": "Motherwell", "role": "place"}]`
	for name, output := range map[string]string{
		"not the schema":                `{"facts": "none"}`,
		"a Fact left unconnected":       `{"facts": [], "causes": []}`,
		"a Fact connected twice":        `{"facts": [{"fact": "F1", "roles": ` + roles + `}, {"fact": "F1", "roles": ` + roles + `}], "causes": []}`,
		"a Fact not given":              `{"facts": [{"fact": "F2", "roles": []}], "causes": []}`,
		"an Entity given two roles":     `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}, {"entity": "Andy Burnham", "role": "undergoer"}, {"entity": "Motherwell", "role": "place"}]}], "causes": []}`,
		"a name the reading never gave": `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}, {"entity": "Motherwell", "role": "place"}, {"entity": "Glasgow", "role": "place"}]}], "causes": []}`,
		"a role not known":              `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "hero"}, {"entity": "Motherwell", "role": "place"}]}], "causes": []}`,
		"a cause of itself":             `{"facts": [{"fact": "F1", "roles": ` + roles + `}], "causes": [{"cause": "F1", "effect": "F1"}]}`,
		"a cause between other Facts":   `{"facts": [{"fact": "F1", "roles": ` + roles + `}], "causes": [{"cause": "C1", "effect": "C2"}]}`,
		"a cause to a Fact not shown":   `{"facts": [{"fact": "F1", "roles": ` + roles + `}], "causes": [{"cause": "C3", "effect": "F1"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			visit := editor.Fact{ID: 8, SourceID: speech.SourceID, EntryID: "visit", AsAt: oct16,
				Statement: "Andy Burnham visited Glasgow.", Entities: []string{"Andy Burnham"}}
			_, err := editor.Connect(t.Context(), &fakeModel{output: output}, toConnect, []editor.Fact{offer, visit})
			if !errors.Is(err, editor.ErrFailedChecks) {
				t.Errorf("err = %v, want ErrFailedChecks", err)
			}
		})
	}
}
