package editor_test

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
	"github.com/samthorold/go-slow/internal/sqlite/sqlitetest"
)

var saturday = time.Date(2026, time.October, 24, 7, 0, 0, 0, time.UTC)

var number10 = collect.Source{
	ID: "govuk-number-10", Name: "Number 10", FeedURL: "https://example.org/feed",
	Keeps: collect.KeepFullText, Attribution: collect.OGL, UsualGap: time.Hour,
}

// record is a Record holding the speech, cut and awaiting hydration, and a
// call readout with no text, which is not cut and so not hydrated; with a Topic
// on Scotland, which the speech touches.
func record(t *testing.T) *sqlite.Store {
	t.Helper()
	ctx := t.Context()
	store, err := sqlite.Open(ctx, sqlitetest.Path(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SyncSources(ctx, []collect.Source{number10}, saturday); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddItems(ctx, []collect.Item{
		{
			SourceID: speech.SourceID, EntryID: speech.EntryID, Title: speech.Title, Link: speech.Link,
			Summary: speech.Summary, Content: speech.Content, Published: speech.Published,
			Attribution: collect.OGL, CollectedAt: saturday,
		},
		{
			SourceID: number10.ID, EntryID: "call", Title: "PM call with the President of Finland",
			Link: "https://www.gov.uk/call", Published: saturday, Attribution: collect.OGL, CollectedAt: saturday,
		},
	}); err != nil {
		t.Fatal(err)
	}
	topics := interest.New(store)
	if _, errs, err := topics.Add(ctx, interest.Draft{Name: "Scotland", Terms: "Scotland"}); err != nil || errs != nil {
		t.Fatalf("add Topic: %v %v", errs, err)
	}
	if err := cut.New(store).CutNew(ctx, saturday); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestTheEditorHydratesEachItemCutIntoTheRecordEachJudgementWithItsJudge(t *testing.T) {
	store := record(t)
	model := &fakeModel{output: goodHydration}
	ed, _ := newEditor(store, model)

	if err := ed.RunWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if len(model.sent) != 1 {
		t.Fatalf("model called %d times, want once: only the speech has text to cut", len(model.sent))
	}
	r, found, err := store.NewestHydration(t.Context(), speech.SourceID, speech.EntryID)
	if err != nil || !found {
		t.Fatalf("NewestHydration: %v %v", found, err)
	}
	if r.Judge.Act != "hydration" || r.Judge.Version != editor.HydrationVersion || r.Judge.Model != "fake" ||
		r.Judge.RequestHash != editor.RequestHash(model.sent[0], "") || !r.At.Equal(saturday) {
		t.Errorf("hydration judged by %+v at %v", r.Judge, r.At)
	}
	if len(r.Names) != 2 || spanWords(speech, r.Names[1].At) != "Motherwell" {
		t.Errorf("names = %+v", r.Names)
	}
}

func TestAnItemOnceHydratedIsNotHydratedAgain(t *testing.T) {
	store := record(t)
	model := &fakeModel{output: goodHydration}
	ed, _ := newEditor(store, model)

	for range 2 {
		if err := ed.RunWaiting(t.Context(), saturday); err != nil {
			t.Fatal(err)
		}
	}

	if len(model.sent) != 1 {
		t.Errorf("model called %d times, want once", len(model.sent))
	}
}

func TestAnItemTouchingNoTopicIsHydratedAllTheSame(t *testing.T) {
	store := record(t)
	if _, err := store.AddItems(t.Context(), []collect.Item{{
		SourceID: number10.ID, EntryID: "budget", Title: "The Budget", Content: "<p>The Chancellor set out the Budget.</p>",
		Published: saturday, Attribution: collect.OGL, CollectedAt: saturday,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(store).CutNew(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	model := &fakeModel{output: `{"names": [], "refers_to": [], "rejected": [], "speakers": []}`}
	ed, _ := newEditor(store, model)

	if err := ed.RunWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if _, found, err := store.NewestHydration(t.Context(), number10.ID, "budget"); err != nil || !found {
		t.Errorf("the Budget, touching no Topic, was not hydrated (%v)", err)
	}
}

func TestAHydrationThatFailsWritesNothingAndTheItemStaysAwaitingHydration(t *testing.T) {
	for name, model := range map[string]*fakeModel{
		"failing the checks": {output: refusedHydration},
		"a failed call":      {err: errors.New("overloaded")},
	} {
		t.Run(name, func(t *testing.T) {
			store := record(t)
			ed, logs := newEditor(store, model)

			if err := ed.RunWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}

			if _, found, err := store.NewestHydration(t.Context(), speech.SourceID, speech.EntryID); err != nil || found {
				t.Errorf("a failed hydration was kept (%v)", err)
			}
			if logs.Len() == 0 {
				t.Error("the failure was not logged")
			}
			// It is hydrated on the next run.
			model.err, model.output = nil, goodHydration
			if err := ed.RunWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}
			if _, found, _ := store.NewestHydration(t.Context(), speech.SourceID, speech.EntryID); !found {
				t.Error("the Item was not hydrated on the next run")
			}
		})
	}
}

func newEditor(store *sqlite.Store, model editor.Model) (*editor.Editor, *bytes.Buffer) {
	var logs bytes.Buffer
	return editor.New(editor.Deps{
		Store: store,
		Model: model,
		Log:   slog.New(slog.NewTextHandler(&logs, nil)),
	}), &logs
}
