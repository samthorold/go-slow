package editor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

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

// connector is an Editor over store that connects with connecting.
func connector(store *sqlite.Store, connecting editor.Model) (*editor.Editor, *bytes.Buffer) {
	var logs bytes.Buffer
	return editor.New(editor.Deps{Store: store, ConnectingModel: connecting, Log: slog.New(slog.NewTextHandler(&logs, nil))}), &logs
}

// speechAndReadout is a Record holding the speech and the later readout,
// both read and placed in one Story.
func speechAndReadout(t *testing.T) (*sqlite.Store, int64) {
	t.Helper()
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	readLater(t, store)
	ed, _ := placer(store, &fakeModel{output: `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": ["S1"]}]}`})
	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	return store, 1
}

const speechRoles = `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}, {"entity": "Motherwell", "role": "place"}]}], "causes": []}`

// readoutCausedBySpeech connects the readout's Fact as caused by the speech's.
const readoutCausedBySpeech = `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}, {"entity": "Edinburgh", "role": "place"}]}],
  "causes": [{"cause": "C1", "effect": "F1"}]}`

func TestConnectingIsShownEveryEntityTheItemsReadingGaveWithItsKind(t *testing.T) {
	// The speech's Fact listed Andy Burnham and Motherwell; its Claim gave
	// Scotland.
	store, _ := speechAndReadout(t)
	model := &inTurn{outputs: []string{speechRoles, readoutCausedBySpeech}}
	ed, logs := connector(store, model)

	if err := ed.ConnectWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if len(model.sent) == 0 {
		t.Fatalf("connecting was not called:\n%s", logs)
	}
	in := model.sent[0].Input
	for _, m := range []string{"Andy Burnham (person)", "Motherwell (place)", "Scotland (place)"} {
		if !strings.Contains(in, m) {
			t.Errorf("the speech's connecting is not shown %q:\n%s", m, in)
		}
	}
}

// The Record keeps what an Item mentions as its reading gives it, apart from
// its Facts and Claims, which reading ties to no Entity: connecting is shown
// every Entity the reading gave.
func TestConnectingIsShownTheItemsMentionsAsTheRecordKeepsThem(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	if _, err := store.AddItems(t.Context(), []collect.Item{glasgowItem}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveReading(t.Context(), editor.Reading{
		SourceID: glasgowItem.SourceID, EntryID: glasgowItem.EntryID, Model: "fake",
		Entities: []editor.Entity{{Name: "Glasgow", Kind: "place"}, {Name: "Clyde", Kind: "place"}},
		Facts: []editor.Fact{{Statement: "Glasgow will get new funding.", AsAt: oct16,
			Quote: "will get new funding"}},
	}, saturday); err != nil {
		t.Fatal(err)
	}
	ed, _ := placer(store, &fakeModel{output: startsAStory})
	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	tc, err := store.FactsToConnect(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	var glasgow []editor.Mention
	for _, c := range tc {
		if c.Item.EntryID == glasgowItem.EntryID {
			glasgow = c.Mentions
		}
	}
	want := []editor.Mention{{Name: "Clyde", Kind: "place"}, {Name: "Glasgow", Kind: "place"}}
	if !slices.Equal(glasgow, want) {
		t.Errorf("connecting is shown the Glasgow Item's mentions as %+v, want %+v", glasgow, want)
	}
}

func TestACauseBetweenTwoFactsIsKeptAndTracesToTheItemsThatSupportIt(t *testing.T) {
	store, id := speechAndReadout(t)
	model := &inTurn{outputs: []string{speechRoles, readoutCausedBySpeech}}
	ed, logs := connector(store, model)

	if err := ed.ConnectWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if len(model.sent) != 2 {
		t.Fatalf("connecting called %d times, want once for each Item:\n%s", len(model.sent), logs)
	}
	// The readout's Fact is weighed against the speech's, which shares an
	// Entity and a Story with it.
	if in := model.sent[1].Input; !strings.Contains(in, "C1 (as at 2026-10-16): Andy Burnham gave a speech in Motherwell.") {
		t.Errorf("the readout's Fact is not weighed against the speech's:\n%s", in)
	}
	s, _, err := store.Story(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Causes) != 1 {
		t.Fatalf("Causes = %+v, want the speech causing the readout", s.Causes)
	}
	c := s.Causes[0]
	if c.Cause.Statement != "Andy Burnham gave a speech in Motherwell." || c.Cause.EntryID != speech.EntryID || c.Cause.ItemTitle != speech.Title {
		t.Errorf("the cause does not trace to the speech: %+v", c.Cause)
	}
	if c.Effect.EntryID != laterItem.EntryID || c.Effect.ItemTitle != laterItem.Title {
		t.Errorf("the effect does not trace to the readout: %+v", c.Effect)
	}

	// Connected once, not again.
	if err := ed.ConnectWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	if len(model.sent) != 2 {
		t.Errorf("connecting called %d times, want no more once every Fact is connected", len(model.sent))
	}
}

func TestConnectingRunsInBatchesOnceAFactIsPlaced(t *testing.T) {
	store := record(t)
	ed, _ := placer(store, &fakeModel{})
	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	batcher := &fakeBatcher{}
	log := slog.New(slog.DiscardHandler)
	b := editor.NewBatches(editor.BatchDeps{Store: store, Batcher: batcher,
		Acts: []editor.BatchAct{ed.Placing(), ed.Connecting()}, Log: log})

	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	// The speech's Fact awaits placing, and connecting waits for it.
	if len(batcher.submitted) != 1 || len(batcher.submitted[0]) != 1 || batcher.submitted[0][0].Request.Act != "placing" {
		t.Fatalf("submitted %v, want one batch placing the speech", batcher.submitted)
	}
	batcher.end(1, answering(startsAStory))
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	if len(batcher.submitted) != 2 || len(batcher.submitted[1]) != 1 || batcher.submitted[1][0].Request.Act != "connecting" {
		t.Fatalf("submitted %v, want a second batch connecting the speech", batcher.submitted)
	}
	batcher.end(2, answering(speechRoles))
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if waiting, err := store.FactsToConnect(t.Context()); err != nil || len(waiting) != 0 {
		t.Errorf("awaiting connecting: %+v %v, want none once the batch's result is applied", waiting, err)
	}
	if len(batcher.submitted) != 2 {
		t.Errorf("submitted %d batches, want nothing more once the speech is connected", len(batcher.submitted))
	}
}

func TestConnectingRunsAgainOverTheDaysFactsBeforeTheBriefing(t *testing.T) {
	// The speech was placed yesterday; the readout today, in its Story.
	store := record(t)
	yesterday := saturday.Add(-24 * time.Hour)
	ed, _ := placer(store, &fakeModel{output: startsAStory})
	if err := ed.ReadWaiting(t.Context(), yesterday); err != nil {
		t.Fatal(err)
	}
	if err := ed.PlaceWaiting(t.Context(), yesterday); err != nil {
		t.Fatal(err)
	}
	readLater(t, store)
	ed.PlacingModel = &fakeModel{output: `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": ["S1"]}]}`}
	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	// As the day's Facts arrived, the readout was connected with no cause.
	readoutRoles := `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"}, {"entity": "Edinburgh", "role": "place"}]}], "causes": []}`
	model := &inTurn{outputs: []string{speechRoles, readoutRoles}}
	ed.ConnectingModel = model
	if err := ed.ConnectWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	model.outputs = []string{readoutCausedBySpeech}
	// Just before the Briefing, over the Facts placed in the day before it:
	// the readout's, not the speech's.
	if err := ed.Reconnect(t.Context(), saturday.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if len(model.sent) != 3 || !strings.Contains(model.sent[2].Input, laterItem.Title) {
		t.Fatalf("connecting sent %d requests, the last %+v; want the readout, placed today, connected again", len(model.sent), model.sent[len(model.sent)-1])
	}
	s, _, err := store.Story(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Causes) != 1 || s.Causes[0].Effect.EntryID != laterItem.EntryID {
		t.Errorf("Causes = %+v, want the one the re-run made", s.Causes)
	}
}

// Connecting again decides afresh which Entities each Fact involves: an
// Entity the earlier connecting gave a role and the later one does not is no
// longer involved.
func TestConnectingAgainDecidesAfreshWhichEntitiesAFactInvolves(t *testing.T) {
	store, _ := speechAndReadout(t)
	model := &inTurn{outputs: []string{speechRoles, readoutCausedBySpeech}}
	ed, _ := connector(store, model)
	if err := ed.ConnectWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	// Both Items were placed in the day before; the readout's Fact now
	// involves Andy Burnham alone.
	model.outputs = []string{speechRoles, `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "undergoer"}]}], "causes": []}`}
	if err := ed.Reconnect(t.Context(), saturday.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	got, err := store.Involvements(t.Context(), laterItem.SourceID, laterItem.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Entity != "Andy Burnham" || got[0].Role != "undergoer" {
		t.Errorf("the readout's Fact involves %+v, want Andy Burnham alone, as undergoer", got)
	}
}

// speechRolesWithScotland gives Scotland, which the speech's reading gave
// only in its Claim, a role in the speech's Fact.
const speechRolesWithScotland = `{"facts": [{"fact": "F1", "roles": [{"entity": "Andy Burnham", "role": "actor"},
  {"entity": "Motherwell", "role": "place"}, {"entity": "Scotland", "role": "place"}]}], "causes": []}`

func TestARoleConnectingGivesAnEntityOfTheReadingMakesTheFactInvolveIt(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	ed, logs := connector(store, &inTurn{outputs: []string{speechRolesWithScotland}})

	if err := ed.ConnectWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if waiting, err := store.FactsToConnect(t.Context()); err != nil || len(waiting) != 0 {
		t.Fatalf("awaiting connecting: %+v %v, want the speech's connecting applied\n%s", waiting, err, logs)
	}
	s, _, err := store.Story(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Timeline) != 1 || !slices.ContainsFunc(s.Timeline[0].Involves, func(r editor.Role) bool {
		return r.Entity == "Scotland" && r.Role == "place"
	}) {
		t.Errorf("the Story page's Fact involves %+v, want Scotland too", s.Timeline)
	}
}

func TestConnectingIsRetriedAfterAFailure(t *testing.T) {
	store, _ := speechAndReadout(t)
	model := &inTurn{outputs: []string{`{"facts": [], "causes": []}`, readoutCausedBySpeech}}
	ed, logs := connector(store, model)

	if err := ed.ConnectWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "connecting failed") {
		t.Errorf("the failure was not logged:\n%s", logs)
	}
	waiting, err := store.FactsToConnect(t.Context())
	if err != nil || len(waiting) != 1 || waiting[0].Item.EntryID != speech.EntryID {
		t.Fatalf("awaiting connecting: %+v %v, want the speech, whose connecting failed", waiting, err)
	}
	model.outputs = []string{speechRoles}
	if err := ed.ConnectWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	if waiting, _ := store.FactsToConnect(t.Context()); len(model.sent) != 3 || len(waiting) != 0 {
		t.Errorf("connecting called %d times, %d Items still waiting; want the speech connected on the next run", len(model.sent), len(waiting))
	}
}
