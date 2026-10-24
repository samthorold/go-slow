package editor_test

import (
	"context"
	"encoding/json"
	"errors"
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
  "entities": [
    {"name": "Andy Burnham", "kind": "person"},
    {"name": "Motherwell", "kind": "place"},
    {"name": "Scotland", "kind": "place"}
  ],
  "facts": [
    {"statement": "Andy Burnham gave a speech in Motherwell.", "as_at": "2026-10-16",
     "quote": "to be here in Motherwell", "entities": ["Andy Burnham", "Motherwell"]}
  ],
  "claims": [
    {"statement": "Scotland will get £2 billion.", "made_by": "Andy Burnham",
     "quote": "Scotland will get £2 billion.", "entities": ["Andy Burnham", "Scotland"]}
  ]
}`

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
		"an Entity not listed":         strings.Replace(goodReading, `["Andy Burnham", "Motherwell"]`, `["Andy Burnham", "Glasgow"]`, 1),
		"an Entity of no known kind":   strings.Replace(goodReading, `"kind": "place"`, `"kind": "vibe"`, 1),
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
