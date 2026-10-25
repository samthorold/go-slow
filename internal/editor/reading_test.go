package editor_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

// fakeModel stands in for a model behind the port: it answers every request
// with the same output, and keeps the requests it was sent.
type fakeModel struct {
	output string
	err    error
	sent   []editor.Request
}

func (f *fakeModel) Complete(_ context.Context, r editor.Request) (editor.Response, error) {
	f.sent = append(f.sent, r)
	if f.err != nil {
		return editor.Response{}, f.err
	}
	return editor.Response{Output: json.RawMessage(f.output), Model: "fake"}, nil
}

var speech = editor.Item{
	SourceID:  "govuk-number-10",
	EntryID:   "tag:www.gov.uk,2005:/government/speeches/pm-speech-in-scotland-16-october-2026",
	Title:     "PM speech in Scotland: 16 October 2026",
	Link:      "https://www.gov.uk/government/speeches/pm-speech-in-scotland-16-october-2026",
	Summary:   "Prime Minister Andy Burnham delivered a speech in Scotland today.",
	Content:   "<p>It’s good to be back in Scotland, to be here in Motherwell.</p><p>Scotland will get £2 billion.</p>",
	Published: time.Date(2026, time.October, 16, 15, 59, 24, 0, time.UTC),
}

const goodReading = `{
  "facts": [
    {"statement": "Andy Burnham gave a speech in Motherwell.", "as_at": "2026-10-16",
     "quote": "to be here in Motherwell", "entities": [
       {"name": "Andy Burnham", "kind": "person", "role": ""},
       {"name": "Motherwell", "kind": "place", "role": ""}]}
  ],
  "claims": [
    {"statement": "Scotland will get £2 billion.", "made_by": "Andy Burnham",
     "quote": "Scotland will get £2 billion.", "entities": [
       {"name": "Andy Burnham", "kind": "person", "role": "Prime Minister of the United Kingdom"},
       {"name": "Scotland", "kind": "place", "role": ""}]}
  ]
}`

func TestReadingGathersTheEntitiesItsFactsAndClaimsInvolveOncePerName(t *testing.T) {
	r, err := editor.Read(t.Context(), &fakeModel{output: goodReading}, speech)
	if err != nil {
		t.Fatal(err)
	}

	want := []editor.Entity{
		{Name: "Andy Burnham", Kind: "person", Role: "Prime Minister of the United Kingdom"},
		{Name: "Motherwell", Kind: "place"},
		{Name: "Scotland", Kind: "place"},
	}
	if !slices.Equal(r.Entities, want) {
		t.Errorf("Entities = %+v, want %+v", r.Entities, want)
	}
	if got := r.Facts[0].Entities; !slices.Equal(got, []string{"Andy Burnham", "Motherwell"}) {
		t.Errorf("the Fact involves %v", got)
	}
	if got := r.Claims[0].Entities; !slices.Equal(got, []string{"Andy Burnham", "Scotland"}) {
		t.Errorf("the Claim involves %v", got)
	}
}

func TestReadingAnItemDrawsFactsThatCarryTheItemAndADate(t *testing.T) {
	model := &fakeModel{output: goodReading}

	r, err := editor.Read(t.Context(), model, speech)
	if err != nil {
		t.Fatal(err)
	}

	if len(r.Facts) != 1 {
		t.Fatalf("Facts = %+v, want one", r.Facts)
	}
	f := r.Facts[0]
	if f.SourceID != speech.SourceID || f.EntryID != speech.EntryID {
		t.Errorf("Fact drawn from %s %s, want the speech", f.SourceID, f.EntryID)
	}
	if want := time.Date(2026, time.October, 16, 0, 0, 0, 0, time.UTC); !f.AsAt.Equal(want) {
		t.Errorf("Fact holds as at %v, want %v", f.AsAt, want)
	}
	if f.Statement != "Andy Burnham gave a speech in Motherwell." {
		t.Errorf("Fact = %q", f.Statement)
	}
}

func TestAnEntityNamedWithTwoRolesKeepsTheFirstGiven(t *testing.T) {
	output := strings.Replace(goodReading, `{"name": "Andy Burnham", "kind": "person", "role": ""}`,
		`{"name": "Andy Burnham", "kind": "person", "role": "Leader of the Labour Party"}`, 1)

	r, err := editor.Read(t.Context(), &fakeModel{output: output}, speech)
	if err != nil {
		t.Fatal(err)
	}

	if r.Entities[0].Name != "Andy Burnham" || r.Entities[0].Role != "Leader of the Labour Party" {
		t.Errorf("Entities = %+v, want Andy Burnham with the role the Fact gives", r.Entities)
	}
}

func TestReadingKeepsWhoMadeEachClaim(t *testing.T) {
	r, err := editor.Read(t.Context(), &fakeModel{output: goodReading}, speech)
	if err != nil {
		t.Fatal(err)
	}

	if len(r.Claims) != 1 || r.Claims[0].MadeBy != "Andy Burnham" {
		t.Fatalf("Claims = %+v, want one made by Andy Burnham", r.Claims)
	}
}

func TestAReadingThatFailsTheChecksIsRejected(t *testing.T) {
	for name, output := range map[string]string{
		"a Claim with no attribution":  strings.Replace(goodReading, `"made_by": "Andy Burnham"`, `"made_by": " "`, 1),
		"output that is not JSON":      `{"facts": [`,
		"output with an unknown field": strings.Replace(goodReading, `"claims"`, `"opinions"`, 1),
		"a Fact with no date":          strings.Replace(goodReading, `"as_at": "2026-10-16"`, `"as_at": "last week"`, 1),
		"a Fact with no statement":     strings.Replace(goodReading, `"statement": "Andy Burnham gave a speech in Motherwell."`, `"statement": ""`, 1),
		"one name given two kinds": strings.Replace(goodReading, `{"name": "Andy Burnham", "kind": "person", "role": "Prime Minister of the United Kingdom"}`,
			`{"name": "Andy Burnham", "kind": "organisation", "role": ""}`, 1),
		"an Entity of no known kind": strings.Replace(goodReading, `"kind": "place"`, `"kind": "vibe"`, 1),
		"an Entity with no name":     strings.Replace(goodReading, `"name": "Motherwell"`, `"name": " "`, 1),
		// Everything traces: a Fact rests on the Item's own words.
		"a Fact the Item does not say": strings.Replace(goodReading, `"quote": "to be here in Motherwell"`, `"quote": "to be here in Glasgow"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := editor.Read(t.Context(), &fakeModel{output: output}, speech)
			if !errors.Is(err, editor.ErrFailedChecks) {
				t.Errorf("err = %v, want ErrFailedChecks", err)
			}
		})
	}
}

func TestAFactsQuoteMatchesTheItemWhateverItsPunctuationAndMarkup(t *testing.T) {
	// The Item has a curly apostrophe and a paragraph break; the quote, a
	// straight one and a space.
	output := strings.Replace(goodReading, `"quote": "to be here in Motherwell"`,
		`"quote": "It's good to be back in Scotland, to be here in Motherwell. Scotland will"`, 1)

	if _, err := editor.Read(t.Context(), &fakeModel{output: output}, speech); err != nil {
		t.Fatal(err)
	}
}

func TestAnItemWithItsTextIsReadWithoutItsSummary(t *testing.T) {
	model := &fakeModel{output: goodReading}
	if _, err := editor.Read(t.Context(), model, speech); err != nil {
		t.Fatal(err)
	}

	if in := model.sent[0].Input; strings.Contains(in, speech.Summary) || strings.Contains(in, "Summary:") {
		t.Errorf("the input gives the summary of an Item with its text:\n%s", in)
	}
}

func TestAnItemWithNoTextIsReadFromItsSummary(t *testing.T) {
	noText := speech
	noText.Content = ""
	model := &fakeModel{output: `{"claims": [], "facts": [{"statement": "Andy Burnham gave a speech in Scotland.",
		"as_at": "2026-10-16", "quote": "delivered a speech in Scotland today", "entities": []}]}`}

	r, err := editor.Read(t.Context(), model, noText)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(model.sent[0].Input, "Summary: "+speech.Summary) || len(r.Facts) != 1 {
		t.Errorf("an Item with no text is not read from its summary: %+v\n%s", r.Facts, model.sent[0].Input)
	}
}

func TestAFactQuotingOnlyTheSummaryOfAnItemWithItsTextIsRefused(t *testing.T) {
	output := strings.Replace(goodReading, `"quote": "to be here in Motherwell"`,
		`"quote": "delivered a speech in Scotland today"`, 1)

	_, err := editor.Read(t.Context(), &fakeModel{output: output}, speech)

	if !errors.Is(err, editor.ErrFailedChecks) {
		t.Errorf("err = %v, want ErrFailedChecks", err)
	}
}

func TestAReadingShownNoPrecedentsIsToldNothingOfThem(t *testing.T) {
	model := &fakeModel{output: goodReading}
	if _, err := editor.Read(t.Context(), model, speech); err != nil {
		t.Fatal(err)
	}

	r := model.sent[0]
	if strings.Contains(strings.ToLower(r.Instructions), "precedent") || strings.Contains(r.Input, "Precedents") {
		t.Errorf("a reading shown no precedents is told of them:\n%s\n---\n%s", r.Instructions, r.Input)
	}
}

func TestAReadingShownPrecedentsIsToldOfThemAfterTheSameInstructions(t *testing.T) {
	without, with := &fakeModel{output: goodReading}, &fakeModel{output: goodReading}
	if _, err := editor.Read(t.Context(), without, speech); err != nil {
		t.Fatal(err)
	}
	precedent := editor.Precedent{FeedbackID: 1, Kind: "wrong", Fact: "Scotland will get £2 billion.", Quote: "Scotland will get £2 billion."}
	if _, err := editor.Read(t.Context(), with, speech, precedent); err != nil {
		t.Fatal(err)
	}

	base, shown := without.sent[0].Instructions, with.sent[0].Instructions
	if !strings.HasPrefix(shown, base) || !strings.Contains(shown[len(base):], "precedent") {
		t.Errorf("the precedent block does not follow the act's own instructions:\n%s", shown)
	}
	// A precedent is a case, never a rule: the block restates no standard.
	if strings.Contains(shown[len(base):], "A Fact is wrong when") {
		t.Errorf("the precedent block restates what a Fact is:\n%s", shown[len(base):])
	}
	if !strings.Contains(with.sent[0].Input, `"feedback"`) {
		t.Errorf("the precedent is not in the input:\n%s", with.sent[0].Input)
	}
}

func TestThereIsNoReadingWithoutAnItem(t *testing.T) {
	model := &fakeModel{output: goodReading}

	_, err := editor.Read(t.Context(), model, editor.Item{Title: "Untraceable"})

	if !errors.Is(err, editor.ErrFailedChecks) {
		t.Errorf("err = %v, want ErrFailedChecks", err)
	}
	if len(model.sent) != 0 {
		t.Error("the model was called with no Item to read")
	}
}

func TestTheModelIsGivenTheItemsTextAndTheActsSchema(t *testing.T) {
	model := &fakeModel{output: goodReading}
	if _, err := editor.Read(t.Context(), model, speech); err != nil {
		t.Fatal(err)
	}

	r := model.sent[0]
	if !strings.Contains(r.Input, "It’s good to be back in Scotland, to be here in Motherwell.") || strings.Contains(r.Input, "<p>") {
		t.Errorf("input is not the Item's text:\n%s", r.Input)
	}
	if !strings.Contains(r.Input, "2026-10-16") {
		t.Errorf("input does not give the Item's date:\n%s", r.Input)
	}
	if r.Schema["type"] != "object" || r.Instructions == "" {
		t.Errorf("request lacks the act's schema or instructions: %+v", r)
	}
}
