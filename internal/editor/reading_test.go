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
     "quote": "to be here in Motherwell"}
  ],
  "claims": [
    {"statement": "Scotland will get £2 billion.", "made_by": "Andy Burnham",
     "quote": "Scotland will get £2 billion."}
  ],
  "mentions": [
    {"name": "Andy Burnham", "kind": "person", "role": "Prime Minister of the United Kingdom"},
    {"name": "Motherwell", "kind": "place", "role": ""},
    {"name": "Scotland", "kind": "place", "role": ""}
  ]
}`

// legacyReading is goodReading in reading's earlier schema, which gave the
// Entities each Fact and Claim involved, inline, and no mentions.
const legacyReading = `{
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

func TestReadingAsksForTheItemsMentionsOnceAndNoEntitiesPerStatement(t *testing.T) {
	model := &fakeModel{output: goodReading}
	if _, err := editor.Read(t.Context(), model, speech); err != nil {
		t.Fatal(err)
	}
	req := model.sent[0]

	props := req.Schema["properties"].(map[string]any)
	mentions, ok := props["mentions"].(map[string]any)
	if !ok || mentions["type"] != "array" {
		t.Fatalf("the schema asks for no mentions: %+v", props)
	}
	item := mentions["items"].(map[string]any)["properties"].(map[string]any)
	for _, f := range []string{"name", "kind", "role"} {
		if _, ok := item[f]; !ok {
			t.Errorf("a mention has no %s: %+v", f, item)
		}
	}
	if !slices.Contains(req.Schema["required"].([]string), "mentions") {
		t.Errorf("mentions are not required: %v", req.Schema["required"])
	}
	for _, statements := range []string{"facts", "claims"} {
		fields := props[statements].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
		if _, ok := fields["entities"]; ok {
			t.Errorf("each of the %s still lists its Entities: %+v", statements, fields)
		}
	}
	// Involvement is connecting's judgement: reading no longer defines it.
	if strings.Contains(req.Instructions, "involve") {
		t.Errorf("reading's instructions still say what a statement involves:\n%s", req.Instructions)
	}
}

func TestReadingGivesTheItemsMentionsOnceAndTiesNoEntityToItsFacts(t *testing.T) {
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
	// Which Entities a Fact involves is connecting's: reading ties none.
	if got := r.Facts[0].Entities; len(got) != 0 {
		t.Errorf("the Fact is tied to %v, want none", got)
	}
}

func TestAReadingInTheEarlierSchemaIsRefused(t *testing.T) {
	_, err := editor.Read(t.Context(), &fakeModel{output: legacyReading}, speech)
	if !errors.Is(err, editor.ErrFailedChecks) {
		t.Errorf("err = %v, want ErrFailedChecks", err)
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
	output := strings.Replace(goodReading, `"mentions": [`,
		`"mentions": [{"name": "Andy Burnham", "kind": "person", "role": ""},
		{"name": "Andy Burnham", "kind": "person", "role": "Leader of the Labour Party"},`, 1)

	r, err := editor.Read(t.Context(), &fakeModel{output: output}, speech)
	if err != nil {
		t.Fatal(err)
	}

	if r.Entities[0].Name != "Andy Burnham" || r.Entities[0].Role != "Leader of the Labour Party" {
		t.Errorf("Entities = %+v, want Andy Burnham with the first role given", r.Entities)
	}
	if len(r.Entities) != 3 {
		t.Errorf("Entities = %+v, want Andy Burnham once", r.Entities)
	}
}

func TestOnlyAPersonKeepsTheRoleReadingGivesIt(t *testing.T) {
	output := `{
  "facts": [
    {"statement": "Andy Burnham gave a speech at the Labour conference in Motherwell.", "as_at": "2026-10-16",
     "quote": "to be here in Motherwell"}
  ],
  "claims": [],
  "mentions": [
    {"name": "Andy Burnham", "kind": "person", "role": "Prime Minister of the United Kingdom"},
    {"name": "Labour Party", "kind": "organisation", "role": "host"},
    {"name": "Motherwell", "kind": "place", "role": "venue"},
    {"name": "Labour conference", "kind": "occasion", "role": "setting"}]
}`

	r, err := editor.Read(t.Context(), &fakeModel{output: output}, speech)
	if err != nil {
		t.Fatal(err)
	}

	want := []editor.Entity{
		{Name: "Andy Burnham", Kind: "person", Role: "Prime Minister of the United Kingdom"},
		{Name: "Labour Party", Kind: "organisation"},
		{Name: "Motherwell", Kind: "place"},
		{Name: "Labour conference", Kind: "occasion"},
	}
	if !slices.Equal(r.Entities, want) {
		t.Errorf("Entities = %+v, want %+v", r.Entities, want)
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
		"one name given two kinds": strings.Replace(goodReading, `"mentions": [`,
			`"mentions": [{"name": "Motherwell", "kind": "organisation", "role": ""},`, 1),
		"a reading that gives no mentions": `{"facts": [], "claims": []}`,
		"a Fact with Entities of its own beside the mentions": strings.Replace(goodReading, `"quote": "to be here in Motherwell"`,
			`"quote": "to be here in Motherwell", "entities": []`, 1),
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

// walkedIntoDowning is an Item whose quote a junk statement can rest on.
var walkedIntoDowning = editor.Item{
	SourceID:  speech.SourceID,
	EntryID:   speech.EntryID,
	Title:     speech.Title,
	Content:   "<p>I have said this since when I walked into Downing Street in July.</p>",
	Published: speech.Published,
}

func TestAStatementSharingNoContentWordWithItsQuoteIsRefused(t *testing.T) {
	for name, output := range map[string]string{
		// #119's recording: a genuine quote under a statement that says nothing
		// it says.
		"a Claim": `{"facts": [], "claims": [{"statement": "No, placeholder removed.",
			"made_by": "the Prime Minister", "quote": "when I walked into Downing Street in July"}], "mentions": []}`,
		"a Fact": `{"claims": [], "facts": [{"statement": "No, placeholder removed.", "as_at": "2026-10-16",
			"quote": "when I walked into Downing Street in July"}], "mentions": []}`,
		// Words like "in" and "the" carry no content, so sharing them is not
		// sharing a word.
		"a statement sharing only stopwords": `{"facts": [], "claims": [{"statement": "It is in the past, as I have said.",
			"made_by": "the Prime Minister", "quote": "when I walked into Downing Street in July"}], "mentions": []}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := editor.Read(t.Context(), &fakeModel{output: output}, walkedIntoDowning)
			if !errors.Is(err, editor.ErrFailedChecks) {
				t.Errorf("err = %v, want ErrFailedChecks", err)
			}
		})
	}
}

func TestAStatementSharingAStemWithItsQuoteButNotAWordPasses(t *testing.T) {
	// "walks" and "walked" share a stem; no other content word is shared.
	output := `{"facts": [], "claims": [{"statement": "The Prime Minister walks to work.",
		"made_by": "the Prime Minister", "quote": "when I walked into Downing Street in July"}], "mentions": []}`

	if _, err := editor.Read(t.Context(), &fakeModel{output: output}, walkedIntoDowning); err != nil {
		t.Fatal(err)
	}
}

func TestAStatementWhoseOnlySharedWordIsAnInitialismPasses(t *testing.T) {
	item := walkedIntoDowning
	item.Content = "<p>I took a clear message to the UN. The United Nations General Assembly heard it.</p>"
	for name, fact := range map[string]string{
		// The Commons statement's sample 5, from #119's recordings.
		"the quote's initialism": `"statement": "The Prime Minister spoke to the United Nations General Assembly in New York.",
			"quote": "I took a clear message to the UN"`,
		"the statement's initialism": `"statement": "The PM addressed the UN.",
			"quote": "The United Nations General Assembly heard it"`,
	} {
		t.Run(name, func(t *testing.T) {
			output := `{"claims": [], "mentions": [], "facts": [{"as_at": "2026-10-12", ` + fact + `}]}`
			if _, err := editor.Read(t.Context(), &fakeModel{output: output}, item); err != nil {
				t.Fatal(err)
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
		"as_at": "2026-10-16", "quote": "delivered a speech in Scotland today"}], "mentions": []}`}

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

// A Fact is held true as at a date, and no one can corroborate what
// something will do, cause or be worth: reading is told such a statement is
// never a Fact, but a Claim, attributed.
func TestReadingIsToldAProjectedEffectIsNeverAFact(t *testing.T) {
	model := &fakeModel{output: goodReading}
	if _, err := editor.Read(t.Context(), model, speech); err != nil {
		t.Fatal(err)
	}
	instructions := model.sent[0].Instructions

	for _, want := range []string{
		"(an estimate, a forecast, a projected effect) can't be established yet, so it is never a Fact",
		"as a Claim, made by whoever the Item attributes it to, or, if it attributes it to no one, by the Item's publisher",
	} {
		if !strings.Contains(instructions, want) {
			t.Errorf("reading's instructions do not say %q:\n%s", want, instructions)
		}
	}
}

// An arrangement and a declared intention can be corroborated as at the
// Item's date, so each is a Fact, worded as what it is, never as the act
// it arranges or intends (system design: An arrangement is a Fact; A
// declared intention is a Fact, as an intention). A promise is a declared
// intention, so it is no longer listed among Claims.
func TestReadingIsToldAnArrangementAndADeclaredIntentionAreFacts(t *testing.T) {
	model := &fakeModel{output: goodReading}
	if _, err := editor.Read(t.Context(), model, speech); err != nil {
		t.Fatal(err)
	}
	instructions := model.sent[0].Instructions

	for _, want := range []string{
		"That something has been arranged is a Fact, worded as the arrangement as at its date",
		"never as the event it arranges",
		"That someone has declared what they will do is a Fact, whoever declares it and however the Item reports it (said, confirmed, assured), worded as their intention",
		"never as the act done",
		"what something will do, cause or be worth beyond what anyone intends (an estimate, a forecast, a projected effect) can't be established yet, so it is never a Fact",
	} {
		if !strings.Contains(instructions, want) {
			t.Errorf("reading's instructions do not say %q:\n%s", want, instructions)
		}
	}
	if _, claims, _ := strings.Cut(instructions, "- Claims:"); strings.Contains(strings.SplitN(claims, "\n", 2)[0], "promises") {
		t.Errorf("reading's instructions list promises among Claims:\n%s", instructions)
	}
	// The standard is how a Fact is drawn, not a list of words: "is set to
	// be built" words a present arrangement, which is a Fact.
	if strings.Contains(instructions, "never worded with") {
		t.Errorf("reading's instructions ban words from Facts:\n%s", instructions)
	}
}

// Drawing what was said must not replace that it was said, nor what it
// answers (system design: A declared intention is a Fact, as an intention):
// reading is told, inside its Facts definition, that a saying and the event
// it responds to are Facts too.
func TestReadingIsToldASayingAndWhatItRespondsToAreFacts(t *testing.T) {
	model := &fakeModel{output: goodReading}
	if _, err := editor.Read(t.Context(), model, speech); err != nil {
		t.Fatal(err)
	}
	instructions := model.sent[0].Instructions

	want := "When the Item reports someone saying or responding to something, the saying and the event it responds to are Facts too, beside the Facts and Claims drawn from what was said."
	_, facts, _ := strings.Cut(instructions, "- Facts:")
	if !strings.Contains(strings.SplitN(facts, "\n", 2)[0], want) {
		t.Errorf("reading's Facts definition does not say %q:\n%s", want, instructions)
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
