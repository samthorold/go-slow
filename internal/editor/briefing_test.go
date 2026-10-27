package editor_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
)

// The day's Developments in two Stories, as writing the Briefing is given
// them.
var (
	offerFact = editor.Fact{ID: 7, SourceID: speech.SourceID, EntryID: "offer", AsAt: oct16,
		Statement: "Andy Burnham made an offer to the Scottish Government."}
	speechFact = editor.Fact{ID: 11, SourceID: speech.SourceID, EntryID: speech.EntryID, AsAt: oct16,
		Statement: "Andy Burnham gave a speech in Motherwell."}
	steelFact = editor.Fact{ID: 12, SourceID: speech.SourceID, EntryID: speech.EntryID, AsAt: oct16,
		Statement: "The steelworks in Motherwell will stay open."}

	scotlandToday = editor.ToBrief{
		Story:  editor.Story{ID: 3, Title: "The Prime Minister's offer to Scotland"},
		Today:  []editor.Development{{Fact: offerFact, ItemTitle: "Offer to Holyrood"}, {Fact: speechFact, ItemTitle: speech.Title}},
		Topics: []string{"Scotland"},
		Depth:  interest.Following,
	}
	steelToday = editor.ToBrief{
		Story:  editor.Story{ID: 4, Title: "Steel in Motherwell"},
		Today:  []editor.Development{{Fact: steelFact, ItemTitle: speech.Title}},
		Topics: []string{"Industry"},
		Depth:  interest.Passing,
	}
)

var briefingDay = time.Date(2026, time.October, 24, 7, 0, 0, 0, time.UTC)

func TestWritingTheBriefingTurnsTheDaysDevelopmentsIntoStatementsCitingThem(t *testing.T) {
	model := &fakeModel{output: `{"stories": [
		{"story": "S4", "statements": [{"text": "The Motherwell steelworks stays open.", "developments": ["D3"]}]},
		{"story": "S3", "statements": [
			{"text": "The Prime Minister offered Holyrood a deal and took it to Motherwell.", "developments": ["D1", "D2"]}]}
	]}`}

	b, err := editor.WriteBriefing(t.Context(), model, briefingDay, []editor.ToBrief{scotlandToday, steelToday})
	if err != nil {
		t.Fatal(err)
	}

	in := model.sent[0].Input
	for _, want := range []string{scotlandToday.Story.Title, "D1", offerFact.Statement, "D3", steelFact.Statement, "passing", "following", "Saturday 24 October 2026"} {
		if !strings.Contains(in, want) {
			t.Errorf("the model is not shown %q:\n%s", want, in)
		}
	}
	if len(b.Stories) != 2 || b.Stories[0].Story.ID != 4 || b.Stories[1].Story.ID != 3 {
		t.Fatalf("Stories = %+v, want Steel then Scotland, in the model's order", b.Stories)
	}
	st := b.Stories[1].Statements
	if len(st) != 1 || st[0].Text != "The Prime Minister offered Holyrood a deal and took it to Motherwell." {
		t.Fatalf("statements = %+v", st)
	}
	if len(st[0].Developments) != 2 || st[0].Developments[0].ID != offerFact.ID || st[0].Developments[1].ID != speechFact.ID {
		t.Errorf("the statement does not cite its Developments: %+v", st[0].Developments)
	}
	if b.Day != "2026-10-24" || b.Model != "fake" {
		t.Errorf("day %q, model %q", b.Day, b.Model)
	}
}

func TestABriefingWithAnUntracedStatementIsRejected(t *testing.T) {
	for name, output := range map[string]string{
		"a statement citing nothing": `{"stories": [{"story": "S3", "statements": [
			{"text": "Scotland is the true north.", "developments": []},
			{"text": "An offer and a speech.", "developments": ["D1", "D2"]}]}]}`,
		"a Development not given": `{"stories": [{"story": "S3", "statements": [
			{"text": "An offer and a speech.", "developments": ["D1", "D2", "D9"]}]}]}`,
		"another Story's Development": `{"stories": [
			{"story": "S3", "statements": [{"text": "An offer, a speech and steel.", "developments": ["D1", "D2", "D3"]}]}]}`,
		"a Story not given": `{"stories": [{"story": "S3", "statements": [{"text": "An offer and a speech.", "developments": ["D1", "D2"]}]},
			{"story": "S8", "statements": [{"text": "Elsewhere.", "developments": ["D1"]}]}]}`,
		"a statement with no text": `{"stories": [{"story": "S3", "statements": [{"text": " ", "developments": ["D1", "D2"]}]}]}`,
		"a Story twice": `{"stories": [{"story": "S3", "statements": [{"text": "An offer.", "developments": ["D1"]}]},
			{"story": "S3", "statements": [{"text": "A speech.", "developments": ["D2"]}]}]}`,
		"not the schema": `{"stories": [{"story": "S3", "summary": "x"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			model := &fakeModel{output: output}

			_, err := editor.WriteBriefing(t.Context(), model, briefingDay, []editor.ToBrief{scotlandToday, steelToday})

			if !errors.Is(err, editor.ErrFailedChecks) {
				t.Errorf("err = %v, want ErrFailedChecks", err)
			}
		})
	}
}

func TestAtFollowingDepthOrCloserEveryDevelopmentIsInTheBriefing(t *testing.T) {
	// The Scotland Story is followed: leaving out its speech, or the whole
	// Story, is refused. The steel Story is passing, so leaving it out is
	// the Editor's call.
	for name, output := range map[string]string{
		"a Development left out": `{"stories": [{"story": "S3", "statements": [{"text": "An offer.", "developments": ["D1"]}]}]}`,
		"the Story left out":     `{"stories": [{"story": "S4", "statements": [{"text": "Steel.", "developments": ["D3"]}]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := editor.WriteBriefing(t.Context(), &fakeModel{output: output}, briefingDay, []editor.ToBrief{scotlandToday, steelToday})
			if !errors.Is(err, editor.ErrFailedChecks) {
				t.Errorf("err = %v, want ErrFailedChecks", err)
			}
		})
	}

	b, err := editor.WriteBriefing(t.Context(), &fakeModel{output: `{"stories": [{"story": "S3", "statements": [
		{"text": "An offer.", "developments": ["D1"]}, {"text": "A speech.", "developments": ["D2"]}]}]}`},
		briefingDay, []editor.ToBrief{scotlandToday, steelToday})
	if err != nil || len(b.Stories) != 1 {
		t.Errorf("leaving out a passing Story: %+v, %v", b, err)
	}
}

func TestABriefingWithNoDevelopmentsSaysSoWithNoModelCall(t *testing.T) {
	model := &fakeModel{output: `{}`}

	b, err := editor.WriteBriefing(t.Context(), model, briefingDay, nil)

	if err != nil {
		t.Fatal(err)
	}
	if len(model.sent) != 0 {
		t.Errorf("the model was called %d times for a day with nothing to write", len(model.sent))
	}
	if b.Day != "2026-10-24" || len(b.Stories) != 0 {
		t.Errorf("Briefing = %+v, want an empty one for the day", b)
	}
}

func TestTheSchemaOffersOnlyTheStoriesAndDevelopmentsGiven(t *testing.T) {
	model := &fakeModel{output: `{"stories": []}`}
	_, _ = editor.WriteBriefing(t.Context(), model, briefingDay, []editor.ToBrief{steelToday})

	s := model.sent[0]
	if s.Act != "briefing" {
		t.Errorf("act = %q", s.Act)
	}
	stories := s.Schema["properties"].(map[string]any)["stories"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	if enum := stories["story"].(map[string]any)["enum"].([]string); len(enum) != 1 || enum[0] != "S4" {
		t.Errorf("story enum = %v", enum)
	}
	devs := stories["statements"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["developments"].(map[string]any)["items"].(map[string]any)
	if enum := devs["enum"].([]string); len(enum) != 1 || enum[0] != "D1" {
		t.Errorf("development enum = %v", enum)
	}
}
