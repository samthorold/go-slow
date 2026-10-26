package editor_test

import (
	"bytes"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
)

var saturday = time.Date(2026, time.October, 24, 7, 0, 0, 0, time.UTC)

var number10 = collect.Source{
	ID: "govuk-number-10", Name: "Number 10", FeedURL: "https://example.org/feed",
	Keeps: collect.KeepFullText, Attribution: collect.OGL, UsualGap: time.Hour,
}

// record is a Record holding the speech and a call readout, with a Topic
// on Scotland: the speech is awaiting reading for it, the readout touches
// no Topic.
func record(t *testing.T) *sqlite.Store {
	t.Helper()
	ctx := t.Context()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "go-slow.db"))
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
	if _, errs, err := topics.Add(ctx, interest.Draft{Name: "Scotland", Depth: "following", Terms: "Scotland"}); err != nil || errs != nil {
		t.Fatalf("add Topic: %v %v", errs, err)
	}
	if err := topics.CheckNew(ctx); err != nil {
		t.Fatal(err)
	}
	return store
}

func newEditor(store *sqlite.Store, model editor.Model, wd editor.Wikidata) (*editor.Editor, *bytes.Buffer) {
	var logs bytes.Buffer
	return editor.New(editor.Deps{
		Store:    store,
		Model:    model,
		Wikidata: wd,
		Log:      slog.New(slog.NewTextHandler(&logs, nil)),
	}), &logs
}

func TestTheEditorReadsTheItemsAwaitingReadingIntoTheRecord(t *testing.T) {
	store := record(t)
	model := &fakeModel{output: goodReading}
	ed, _ := newEditor(store, model, &fakeWikidata{})

	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if len(model.sent) != 1 {
		t.Fatalf("model called %d times, want once: only the speech touches a Topic", len(model.sent))
	}
	got, found, err := store.ReadItem(t.Context(), speech.SourceID, speech.EntryID)
	if err != nil || !found {
		t.Fatalf("ReadItem: %v %v", found, err)
	}
	if !got.Read {
		t.Error("the speech is not marked read")
	}
	if len(got.Facts) != 1 || got.Facts[0].Statement != "Andy Burnham gave a speech in Motherwell." {
		t.Errorf("Facts = %+v", got.Facts)
	}
	if f := got.Facts[0]; f.SourceID != speech.SourceID || f.EntryID != speech.EntryID ||
		!f.AsAt.Equal(time.Date(2026, time.October, 16, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("the Fact does not carry its Item and date: %+v", f)
	}
	// The Fact is tied to every Entity the Item mentions.
	if len(got.Facts[0].Entities) != 3 {
		t.Errorf("the Fact's Entities = %v, want the Item's three mentions", got.Facts[0].Entities)
	}
	if len(got.Claims) != 1 || got.Claims[0].MadeBy != "Andy Burnham" {
		t.Errorf("Claims = %+v", got.Claims)
	}
}

func TestAnItemOnceReadIsNotReadAgain(t *testing.T) {
	store := record(t)
	model := &fakeModel{output: goodReading}
	ed, _ := newEditor(store, model, &fakeWikidata{})

	for range 2 {
		if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
			t.Fatal(err)
		}
	}

	if len(model.sent) != 1 {
		t.Errorf("model called %d times, want once", len(model.sent))
	}
}

func TestTakingUpATopicTiesOnlyItemsNotYetRead(t *testing.T) {
	store := record(t)
	ed, _ := newEditor(store, &fakeModel{output: goodReading}, &fakeWikidata{})
	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if _, errs, err := interest.New(store).Add(t.Context(),
		interest.Draft{Name: "Motherwell", Depth: "close", Terms: "Motherwell"}); err != nil || errs != nil {
		t.Fatalf("add Topic: %v %v", errs, err)
	}

	got, _, err := store.ReadItem(t.Context(), speech.SourceID, speech.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Topics) != 1 || got.Topics[0] != "Scotland" {
		t.Errorf("the read speech is tied to %v; a Topic taken up after reading waits on unread Items only", got.Topics)
	}
}

func TestAReadingThatFailsWritesNothingAndTheItemStaysAwaitingReading(t *testing.T) {
	for name, model := range map[string]*fakeModel{
		"failing the checks": {output: `{"facts": [], "claims": [{"statement": "x", "made_by": "", "quote": "x"}], "mentions": []}`},
		"a failed call":      {err: errors.New("overloaded")},
	} {
		t.Run(name, func(t *testing.T) {
			store := record(t)
			ed, logs := newEditor(store, model, &fakeWikidata{})

			if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}

			got, _, err := store.ReadItem(t.Context(), speech.SourceID, speech.EntryID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Read || len(got.Facts)+len(got.Claims) > 0 {
				t.Errorf("a failed reading was written: %+v", got)
			}
			if logs.Len() == 0 {
				t.Error("the failure was not logged")
			}
			// It is read on the next run.
			model.err, model.output = nil, goodReading
			if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}
			if got, _, _ := store.ReadItem(t.Context(), speech.SourceID, speech.EntryID); !got.Read {
				t.Error("the Item was not read on the next run")
			}
		})
	}
}
