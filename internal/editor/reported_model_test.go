package editor_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// Each act's result keeps the model the provider reports answering with,
// not the model the act asked for: were a configured ID an alias the
// provider moved, the Record would show it.

// reporting is a model answering as m does, but reporting it was model.
type reporting struct {
	m     editor.Model
	model string
}

func (r reporting) Complete(ctx context.Context, req editor.Request) (editor.Response, error) {
	res, err := r.m.Complete(ctx, req)
	res.Model = r.model
	return res, err
}

// savedModels is the Record, keeping the model each placing and connecting
// it is given was made on.
type savedModels struct {
	*sqlite.Store
	placing, connecting []string
}

func (s *savedModels) SavePlacing(ctx context.Context, p editor.Placing, at time.Time) error {
	s.placing = append(s.placing, p.Model)
	return s.Store.SavePlacing(ctx, p, at)
}

func (s *savedModels) SaveConnecting(ctx context.Context, c editor.Connecting, at time.Time) error {
	s.connecting = append(s.connecting, c.Model)
	return s.Store.SaveConnecting(ctx, c, at)
}

// askingFor is an Editor over store asking for each act's model by
// modelFor, so the model asked for is never the one reported.
func askingFor(store editor.Store, d editor.Deps) *editor.Editor {
	d.Store, d.ModelFor, d.Wikidata, d.Log = store, modelFor, &fakeWikidata{}, slog.New(slog.DiscardHandler)
	return editor.New(d)
}

func TestAReadingKeepsTheModelReportedNotTheOneAskedFor(t *testing.T) {
	store := record(t)
	ed := askingFor(store, editor.Deps{Model: reporting{&fakeModel{output: goodReading}, "reported-reading"}})

	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	it, found, err := store.ReadItem(t.Context(), speech.SourceID, speech.EntryID)
	if err != nil || !found {
		t.Fatalf("ReadItem: %v %v", found, err)
	}
	if it.Model != "reported-reading" {
		t.Errorf("read by %q, want the model reported", it.Model)
	}
}

func TestAReadingFromABatchKeepsTheModelReportedNotTheOneAskedFor(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := keepingBatches(store, batcher)
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	batcher.end(1, func(editor.BatchRequest) editor.BatchResult {
		return editor.BatchResult{Response: answered(goodReading)}
	})

	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	it, found, err := store.ReadItem(t.Context(), speech.SourceID, speech.EntryID)
	if err != nil || !found {
		t.Fatalf("ReadItem: %v %v", found, err)
	}
	if it.Model != "fake-reported" {
		t.Errorf("read by %q, want the model reported", it.Model)
	}
}

func TestAPlacingKeepsTheModelReportedNotTheOneAskedFor(t *testing.T) {
	store := &savedModels{Store: record(t)}
	ed := askingFor(store, editor.Deps{
		Model:        &fakeModel{output: goodReading},
		PlacingModel: reporting{&fakeModel{output: startsAStory}, "reported-placing"},
	})
	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if len(store.placing) != 1 || store.placing[0] != "reported-placing" {
		t.Errorf("placed by %q, want the model reported", store.placing)
	}
}

func TestAConnectingKeepsTheModelReportedNotTheOneAskedFor(t *testing.T) {
	record, _ := speechAndReadout(t)
	store := &savedModels{Store: record}
	ed := askingFor(store, editor.Deps{
		ConnectingModel: reporting{&inTurn{outputs: []string{speechRoles, readoutCausedBySpeech}}, "reported-connecting"},
	})

	if err := ed.ConnectWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if len(store.connecting) != 2 || store.connecting[0] != "reported-connecting" || store.connecting[1] != "reported-connecting" {
		t.Errorf("connected by %q, want the model reported", store.connecting)
	}
}

func TestTheBriefingKeepsTheModelReportedNotTheOneAskedFor(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	ed := askingFor(store, editor.Deps{Briefings: store,
		BriefingModel: reporting{&fakeModel{output: spokeInMotherwell}, "reported-briefing"}})

	if err := ed.Brief(t.Context(), saturday.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	b, found, err := store.Briefing(t.Context(), "2026-10-24")
	if err != nil || !found {
		t.Fatalf("Briefing: %v %v", found, err)
	}
	if b.Model != "reported-briefing" {
		t.Errorf("written by %q, want the model reported", b.Model)
	}
}

func TestTheSundayReadKeepsTheModelReportedNotTheOneAskedFor(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	turn := submits(citesTheSpeech)
	turn.Model = "reported-sunday-read"
	ed := askingFor(store, editor.Deps{SundayReads: store, SundayReadModel: &fakeAgent{turns: []editor.Turn{turn}}})

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	r, found, err := store.SundayRead(t.Context(), "2026-10-25")
	if err != nil || !found {
		t.Fatalf("SundayRead: %v %v", found, err)
	}
	if r.Model != "reported-sunday-read" {
		t.Errorf("written by %q, want the model reported", r.Model)
	}
}
