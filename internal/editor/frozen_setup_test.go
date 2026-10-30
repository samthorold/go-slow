package editor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/editor/frozentest"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// The retired placing and connecting acts' outputs, written by frozentest:
// the Briefing and the Sunday read still read the Stories they built, and
// their tests build them so.

var oct16 = time.Date(2026, time.October, 16, 0, 0, 0, 0, time.UTC)

// frozenActs writes placing's and connecting's outputs over store, as the
// models given answer.
type frozenActs struct {
	store               *sqlite.Store
	placing, connecting editor.Model
	log                 *slog.Logger
}

func (f frozenActs) PlaceWaiting(ctx context.Context, now time.Time) error {
	return frozentest.PlaceWaiting(ctx, frozentest.Deps{Store: f.store, Model: f.placing, Log: f.log}, now)
}

func (f frozenActs) ConnectWaiting(ctx context.Context, now time.Time) error {
	return frozentest.ConnectWaiting(ctx, frozentest.Deps{Store: f.store, Model: f.connecting, Log: f.log}, now)
}

// placer places over store with placing's outputs.
func placer(store *sqlite.Store, placing editor.Model) (frozenActs, *bytes.Buffer) {
	var logs bytes.Buffer
	return frozenActs{store: store, placing: placing, log: slog.New(slog.NewTextHandler(&logs, nil))}, &logs
}

// connector connects over store with connecting's outputs.
func connector(store *sqlite.Store, connecting editor.Model) (frozenActs, *bytes.Buffer) {
	var logs bytes.Buffer
	return frozenActs{store: store, connecting: connecting, log: slog.New(slog.NewTextHandler(&logs, nil))}, &logs
}

const startsAStory = `{"new_stories": [{"ref": "N1", "title": "Andy Burnham in Scotland"}],
  "facts": [{"fact": "F1", "developments_in": ["N1"]}]}`

// readAndPlace draws the speech's Fact and places it with placing.
func readAndPlace(t *testing.T, store *sqlite.Store, placing editor.Model) frozenActs {
	t.Helper()
	ed, _ := placer(store, placing)
	drawSpeechFacts(t, store, saturday)
	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	return ed
}

// inTurn answers each request with the next of its outputs, and keeps the
// requests it was sent.
type inTurn struct {
	outputs []string
	sent    []editor.Request
}

func (m *inTurn) Complete(_ context.Context, r editor.Request) (editor.Response, error) {
	m.sent = append(m.sent, r)
	out := m.outputs[0]
	m.outputs = m.outputs[1:]
	return editor.Response{Output: json.RawMessage(out), Model: "fake"}, nil
}

const speechRoles = `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}, {"entity": "Motherwell", "role": "place"}]}], "causes": []}`

// readoutCausedBySpeech connects the readout's Fact as caused by the speech's.
const readoutCausedBySpeech = `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}, {"entity": "Edinburgh", "role": "place"}]}],
  "causes": [{"cause": "C1", "effect": "F1"}]}`

// speechRolesWithScotland gives Scotland, which the speech's reading gave
// only in its Claim, a role in the speech's Fact.
const speechRolesWithScotland = `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"},
  {"entity": "Motherwell", "role": "place"}, {"entity": "Scotland", "role": "place"}]}], "causes": []}`

// laterItem is a readout, published after the speech, that mentions Andy
// Burnham too.
var laterItem = collect.Item{
	SourceID: number10.ID, EntryID: "readout", Title: "PM meeting with the First Minister",
	Link: "https://www.gov.uk/readout", Content: "<p>The Prime Minister met the First Minister in Edinburgh.</p>",
	Published: speech.Published.Add(24 * time.Hour), Attribution: collect.OGL, CollectedAt: saturday,
}

// readLater reads the readout, which mentions Andy Burnham and Edinburgh,
// into one Fact.
func readLater(t *testing.T, store *sqlite.Store) {
	t.Helper()
	if _, err := store.AddItems(t.Context(), []collect.Item{laterItem}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveFactReading(t.Context(), editor.FactReading{
		SourceID: laterItem.SourceID, EntryID: laterItem.EntryID, Model: "fake",
		Entities: []editor.Entity{{Name: "Andy Burnham", Kind: "person"}, {Name: "Edinburgh", Kind: "place"}},
		Facts: []editor.Fact{{Statement: "Andy Burnham met the First Minister in Edinburgh.",
			AsAt: time.Date(2026, time.October, 17, 0, 0, 0, 0, time.UTC), Quote: "met the First Minister in Edinburgh"}},
	}, saturday); err != nil {
		t.Fatal(err)
	}
}
