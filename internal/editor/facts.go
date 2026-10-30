package editor

import (
	"errors"
	"slices"
	"time"
)

// Item is what the acts look at of a collected Item.
type Item struct {
	SourceID  string
	EntryID   string
	Title     string
	Link      string
	Summary   string
	Content   string
	Published time.Time
}

// Entity is a thing in the world an Item mentions, as the Item names it.
// One Wikidata matched is that item; one with no match is the Editor's own.
type Entity struct {
	// Name is how the Item names it.
	Name string
	Kind string
	// Role is the office or position the Item gives a person, if any, in
	// full ("Prime Minister of the United Kingdom"). Only a person has one.
	Role string
	// WikidataID and Label are the Wikidata item it matched, if any.
	WikidataID string
	Label      string
}

// Fact is a proposition drawn from an Item or a Reference source, holding as
// at a date. It carries the Item it was drawn from, or the Lookup.
//
// Facts are what the earlier reading act drew from an Item, before the
// Record's unit became the Span: placing, connecting, the Briefing and the
// Sunday read are still built on them, and read those the Record holds, but
// no act draws one now (system design: Record construction).
type Fact struct {
	// ID is the Fact's in the Record, once it is kept there.
	ID int64
	// Lookup is the Reference source lookup a Fact not drawn from an Item
	// came from; nil for one drawn from an Item.
	Lookup    *Lookup
	SourceID  string
	EntryID   string
	Statement string
	AsAt      time.Time
	// Quote is the Item's own words the Fact rests on.
	Quote string
	// Entities are, for a Fact from a Reference source, the name of the
	// Entity it was looked up for. A Fact drawn from an Item carries none:
	// the Entities its Item mentions are the Item's (ToPlace.Mentions,
	// ToConnect.Mentions, ReadItem.Mentions), and those it involves are in
	// Involves.
	Entities []string
	// Involves are the Entities it involves, each in the Role connecting
	// gave it, by name: set where a page or the Sunday read shows them, and
	// shown no daily act.
	Involves []Role
}

// Claim is a proposition attributed to whoever made it, not corroborated.
// It carries the Item it was found in. Like a Fact, no act draws one now.
type Claim struct {
	SourceID  string
	EntryID   string
	Statement string
	// MadeBy is who made the Claim.
	MadeBy string
	Quote  string
}

// FactReading is what the earlier reading act drew from an Item: its Facts
// and Claims, and the Entities the Item mentions. No act makes one now; the
// Record keeps those it made, which placing and connecting still read, and
// tests and the frozen recordings of the acts built on Facts write them,
// until those acts move onto Spans.
type FactReading struct {
	SourceID string
	EntryID  string
	// Entities are the Entities the Item mentions, each once.
	Entities []Entity
	Facts    []Fact
	Claims   []Claim
	// ReferenceFacts are Facts about its Entities that looking them up
	// brought in, each traced to its Lookup.
	ReferenceFacts []Fact
	// Model is the model that read the Item.
	Model string
}

// ErrFailedChecks is the error for a model result that fails the act's
// checks. Nothing of it is written to the Record.
var ErrFailedChecks = errors.New("the result fails the act's checks")

func str() map[string]any { return map[string]any{"type": "string"} }

func integer() map[string]any { return map[string]any{"type": "integer"} }

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
