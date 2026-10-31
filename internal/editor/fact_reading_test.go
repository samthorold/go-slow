package editor_test

import (
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// The acts built on Facts (placing, connecting, the Briefing over Stories)
// are tested over what the earlier reading act drew, which no act draws
// now, written into the Record as that act wrote it.

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
