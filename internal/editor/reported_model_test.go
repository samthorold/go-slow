package editor_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/samthorold/go-slow/internal/editor"
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

// askingFor is an Editor over store asking for each act's model by
// modelFor, so the model asked for is never the one reported.
func askingFor(store editor.Store, d editor.Deps) *editor.Editor {
	d.Store, d.ModelFor, d.Log = store, modelFor, slog.New(slog.DiscardHandler)
	return editor.New(d)
}

func TestAHydrationKeepsTheModelReportedNotTheOneAskedFor(t *testing.T) {
	store := record(t)
	ed := askingFor(store, editor.Deps{Model: reporting{&fakeModel{output: goodHydration}, "reported-hydration"}})

	if err := ed.RunWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	r, found, err := store.NewestHydration(t.Context(), speech.SourceID, speech.EntryID)
	if err != nil || !found {
		t.Fatalf("NewestHydration: %v %v", found, err)
	}
	if r.Judge.Model != "reported-hydration" {
		t.Errorf("hydrated by %q, want the model reported", r.Judge.Model)
	}
}

func TestAHydrationFromABatchKeepsTheModelReportedNotTheOneAskedFor(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := keepingBatches(store, batcher)
	if err := b.RunWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	batcher.end(1, func(editor.BatchRequest) editor.BatchResult {
		return editor.BatchResult{Response: answered(goodHydration)}
	})

	if err := b.RunWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	r, found, err := store.NewestHydration(t.Context(), speech.SourceID, speech.EntryID)
	if err != nil || !found {
		t.Fatalf("NewestHydration: %v %v", found, err)
	}
	if r.Judge.Model != "fake-reported" {
		t.Errorf("hydrated by %q, want the model reported", r.Judge.Model)
	}
}
