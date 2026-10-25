package editor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Item is what reading looks at of a collected Item.
type Item struct {
	SourceID  string
	EntryID   string
	Title     string
	Link      string
	Summary   string
	Content   string
	Published time.Time
}

// Entity is a thing in the world a Fact or Claim involves. For now every
// Entity is go-slow's own, made by reading; Wikidata comes later.
type Entity struct {
	Name string
	Kind string
}

// The kinds of Entity.
var entityKinds = []string{"person", "organisation", "place", "occasion"}

// Fact is a proposition drawn from an Item, holding as at a date. It carries
// the Item it was drawn from.
type Fact struct {
	SourceID  string
	EntryID   string
	Statement string
	AsAt      time.Time
	// Quote is the Item's own words the Fact rests on.
	Quote string
	// Entities are the names of the Entities it involves.
	Entities []string
}

// Claim is a proposition attributed to whoever made it, not corroborated.
// It carries the Item it was found in.
type Claim struct {
	SourceID  string
	EntryID   string
	Statement string
	// MadeBy is who made the Claim.
	MadeBy   string
	Quote    string
	Entities []string
}

// Reading is the result of reading an Item: its Facts and Claims, and the
// Entities they involve.
type Reading struct {
	SourceID string
	EntryID  string
	Entities []Entity
	Facts    []Fact
	Claims   []Claim
	// Model is the model that read the Item.
	Model string
	Usage Usage
}

// ErrFailedChecks is the error for a model result that fails the act's
// checks. Nothing of it is written to the Record.
var ErrFailedChecks = errors.New("the result fails the act's checks")

func failed(it Item, format string, args ...any) error {
	return fmt.Errorf("reading %s: %w: %s", it.EntryID, ErrFailedChecks, fmt.Sprintf(format, args...))
}

// readingInstructions are the reading act's own instructions.
const readingInstructions = `You are the Editor of a private slow-news briefing. You are reading one Item: a piece of material collected from a Source, such as a government press release or speech.

Draw from the Item:
- Facts: propositions the Item itself establishes as having happened or being so, in no one's voice. That a person said something, announced something, met someone or visited somewhere is a Fact. Each Fact holds as at a date (YYYY-MM-DD): the date the Item gives for it, otherwise the Item's publication date.
- Claims: propositions someone asserts that the Item does not itself establish: promises, forecasts, judgements, accusations, figures given in a speaker's own voice. Each Claim keeps who made it, by name if the Item gives one.
- Entities: every person, organisation, place or named occasion (such as a summit or a Budget) a Fact or Claim involves. Name each Entity once, in its fullest form in the Item, and refer to it by exactly that name.

For each Fact and Claim, give a quote: a short passage copied word for word from the Item that it rests on. Never paraphrase in a quote.

Keep to what the Item says. Do not add background knowledge, and do not draw a Fact from anything but the Item. An Item with nothing worth recording has no Facts or Claims.`

// readingSchema is the reading act's output schema.
var readingSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"entities": array(object(map[string]any{
			"name": str(),
			"kind": map[string]any{"type": "string", "enum": entityKinds},
		})),
		"facts": array(object(map[string]any{
			"statement": str(),
			"as_at":     map[string]any{"type": "string", "format": "date"},
			"quote":     str(),
			"entities":  array(str()),
		})),
		"claims": array(object(map[string]any{
			"statement": str(),
			"made_by":   str(),
			"quote":     str(),
			"entities":  array(str()),
		})),
	},
	"required":             []string{"entities", "facts", "claims"},
	"additionalProperties": false,
}

func str() map[string]any { return map[string]any{"type": "string"} }

func array(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

func object(props map[string]any) map[string]any {
	required := make([]string, 0, len(props))
	for k := range props {
		required = append(required, k)
	}
	slices.Sort(required)
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

// output is the reading act's output as the model gives it.
type output struct {
	Entities []struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
	} `json:"entities"`
	Facts []struct {
		Statement string   `json:"statement"`
		AsAt      string   `json:"as_at"`
		Quote     string   `json:"quote"`
		Entities  []string `json:"entities"`
	} `json:"facts"`
	Claims []struct {
		Statement string   `json:"statement"`
		MadeBy    string   `json:"made_by"`
		Quote     string   `json:"quote"`
		Entities  []string `json:"entities"`
	} `json:"claims"`
}

// Read is the act of reading an Item: a model draws its Facts and Claims and
// the Entities they involve. A result that fails the act's checks is
// ErrFailedChecks, and none of it is returned.
func Read(ctx context.Context, m Model, it Item) (Reading, error) {
	req, err := readingRequest(it)
	if err != nil {
		return Reading{}, err
	}
	res, err := m.Complete(ctx, req)
	if err != nil {
		return Reading{}, fmt.Errorf("reading %s: %w", it.EntryID, err)
	}
	return checkReading(it, res)
}

// readingRequest is the reading act's call on a model for an Item.
func readingRequest(it Item) (Request, error) {
	if it.SourceID == "" || it.EntryID == "" {
		return Request{}, failed(it, "no Item to draw from")
	}
	return Request{
		Act:          "reading",
		Instructions: readingInstructions,
		Input:        input(it),
		Schema:       readingSchema,
	}, nil
}

// checkReading decodes the model's answer on an Item and runs the act's
// checks over it. A result that fails them is ErrFailedChecks.
func checkReading(it Item, res Response) (Reading, error) {
	var out output
	dec := json.NewDecoder(bytes.NewReader(res.Output))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return Reading{}, failed(it, "output does not match the schema: %v", err)
	}
	r := Reading{SourceID: it.SourceID, EntryID: it.EntryID, Model: res.Model, Usage: res.Usage}
	named := map[string]bool{}
	for _, e := range out.Entities {
		name := strings.TrimSpace(e.Name)
		if name == "" || !slices.Contains(entityKinds, e.Kind) {
			return Reading{}, failed(it, "an Entity with no name or kind: %+v", e)
		}
		if !named[name] {
			named[name] = true
			r.Entities = append(r.Entities, Entity{Name: name, Kind: e.Kind})
		}
	}
	involves := func(names []string) ([]string, error) {
		var out []string
		for _, n := range names {
			n = strings.TrimSpace(n)
			if !named[n] {
				return nil, failed(it, "an Entity not listed: %q", n)
			}
			if !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
		return out, nil
	}
	text := words(input(it))
	for _, f := range out.Facts {
		asAt, err := time.Parse(time.DateOnly, f.AsAt)
		if err != nil {
			return Reading{}, failed(it, "a Fact with no date it holds as at: %q", f.Statement)
		}
		if strings.TrimSpace(f.Statement) == "" {
			return Reading{}, failed(it, "a Fact with no statement")
		}
		// Everything traces: a Fact rests on the Item's own words, or it is
		// not drawn from the Item.
		if q := words(f.Quote); q == "  " || !strings.Contains(text, q) {
			return Reading{}, failed(it, "a Fact whose quote is not in the Item: %q", f.Quote)
		}
		ents, err := involves(f.Entities)
		if err != nil {
			return Reading{}, err
		}
		r.Facts = append(r.Facts, Fact{
			SourceID: it.SourceID, EntryID: it.EntryID,
			Statement: strings.TrimSpace(f.Statement), AsAt: asAt, Quote: f.Quote, Entities: ents,
		})
	}
	for _, c := range out.Claims {
		if strings.TrimSpace(c.MadeBy) == "" {
			return Reading{}, failed(it, "a Claim with no attribution: %q", c.Statement)
		}
		if strings.TrimSpace(c.Statement) == "" {
			return Reading{}, failed(it, "a Claim with no statement")
		}
		ents, err := involves(c.Entities)
		if err != nil {
			return Reading{}, err
		}
		r.Claims = append(r.Claims, Claim{
			SourceID: it.SourceID, EntryID: it.EntryID,
			Statement: strings.TrimSpace(c.Statement), MadeBy: strings.TrimSpace(c.MadeBy),
			Quote: c.Quote, Entities: ents,
		})
	}
	return r, nil
}

// input is the Item as the model is shown it.
func input(it Item) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\n", it.Title)
	if !it.Published.IsZero() {
		fmt.Fprintf(&b, "Published: %s\n", it.Published.UTC().Format(time.DateOnly))
	}
	if it.Summary != "" {
		fmt.Fprintf(&b, "Summary: %s\n", it.Summary)
	}
	if it.Content != "" {
		fmt.Fprintf(&b, "\n%s\n", plainText(it.Content))
	}
	return b.String()
}
