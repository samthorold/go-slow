package editor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// The acts built on Facts (placing, connecting, the Briefing, the Sunday
// read) are replayed over their frozen copies of what the earlier reading
// act drew, which no act draws now. factReadingOf writes such a copy into a
// Record as that act wrote it: its mentions once by name, a role kept only
// for a person, its Facts and Claims trimmed. It runs none of that act's
// checks: a frozen copy passed them when it was recorded.
func factReadingOf(it editor.Item, output json.RawMessage, model string) (editor.FactReading, error) {
	var out struct {
		Facts []struct {
			Statement string `json:"statement"`
			AsAt      string `json:"as_at"`
			Quote     string `json:"quote"`
		} `json:"facts"`
		Claims []struct {
			Statement string `json:"statement"`
			MadeBy    string `json:"made_by"`
			Quote     string `json:"quote"`
		} `json:"claims"`
		Mentions []struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
			Role string `json:"role"`
		} `json:"mentions"`
	}
	dec := json.NewDecoder(bytes.NewReader(output))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return editor.FactReading{}, fmt.Errorf("%s: not a reading of Facts: %w", it.EntryID, err)
	}
	r := editor.FactReading{SourceID: it.SourceID, EntryID: it.EntryID, Model: model}
	named := map[string]int{}
	for _, e := range out.Mentions {
		name, role := strings.TrimSpace(e.Name), strings.TrimSpace(e.Role)
		if e.Kind != "person" {
			role = ""
		}
		i, ok := named[name]
		switch {
		case !ok:
			named[name] = len(r.Entities)
			r.Entities = append(r.Entities, editor.Entity{Name: name, Kind: e.Kind, Role: role})
		case r.Entities[i].Role == "":
			r.Entities[i].Role = role
		}
	}
	for _, f := range out.Facts {
		asAt, err := time.Parse(time.DateOnly, f.AsAt)
		if err != nil {
			return editor.FactReading{}, fmt.Errorf("%s: a Fact as at no date: %w", it.EntryID, err)
		}
		r.Facts = append(r.Facts, editor.Fact{SourceID: it.SourceID, EntryID: it.EntryID,
			Statement: strings.TrimSpace(f.Statement), AsAt: asAt, Quote: f.Quote})
	}
	for _, c := range out.Claims {
		r.Claims = append(r.Claims, editor.Claim{SourceID: it.SourceID, EntryID: it.EntryID,
			Statement: strings.TrimSpace(c.Statement), MadeBy: strings.TrimSpace(c.MadeBy), Quote: c.Quote})
	}
	return r, nil
}

// speechFacts is what the earlier reading act drew from the speech: one
// Fact, one Claim, and three Entities mentioned.
var speechFacts = editor.FactReading{
	SourceID: speech.SourceID, EntryID: speech.EntryID, Model: "fake",
	Entities: []editor.Entity{
		{Name: "Andy Burnham", Kind: "person", Role: "Prime Minister of the United Kingdom"},
		{Name: "Motherwell", Kind: "place"},
		{Name: "Scotland", Kind: "place"},
	},
	Facts: []editor.Fact{{SourceID: speech.SourceID, EntryID: speech.EntryID,
		Statement: "Andy Burnham gave a speech in Motherwell.",
		AsAt:      time.Date(2026, time.October, 16, 0, 0, 0, 0, time.UTC), Quote: "to be here in Motherwell"}},
	Claims: []editor.Claim{{SourceID: speech.SourceID, EntryID: speech.EntryID,
		Statement: "Scotland will get £2 billion.", MadeBy: "Andy Burnham", Quote: "Scotland will get £2 billion."}},
}

// drawSpeechFacts puts the speech's Facts in the Record at at, as the
// earlier reading act drew them, for the acts built on Facts.
func drawSpeechFacts(t *testing.T, store *sqlite.Store, at time.Time) {
	t.Helper()
	if err := store.SaveFactReading(t.Context(), speechFacts, at); err != nil {
		t.Fatal(err)
	}
}

// replayFactReading is the frozen reading of it that m answers with, as
// factReadingOf makes it.
func replayFactReading(ctx context.Context, m editor.Model, it editor.Item) (editor.FactReading, error) {
	res, err := m.Complete(ctx, editor.Request{Act: "reading"})
	if err != nil {
		return editor.FactReading{}, err
	}
	return factReadingOf(it, res.Output, res.Model)
}
