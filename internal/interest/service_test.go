package interest_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// The check makes no model call: the Service is built from the Record alone,
// with no model adapter constructed or passed in, and still ties every Item.
func TestTheCheckRunsWithNoModelAdapter(t *testing.T) {
	ctx := t.Context()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "go-slow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, time.October, 24, 7, 0, 0, 0, time.UTC)
	src := collect.Source{ID: "number-10", Name: "Number 10", FeedURL: "https://example.org/feed", UsualGap: time.Hour}
	if err := store.SyncSources(ctx, []collect.Source{src}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddItems(ctx, []collect.Item{
		{SourceID: src.ID, EntryID: "a", Title: "PM statement on the Budget", Published: now, CollectedAt: now},
		{SourceID: src.ID, EntryID: "b", Title: "PM call with the President", Published: now, CollectedAt: now},
	}); err != nil {
		t.Fatal(err)
	}

	topics := interest.New(store)
	if _, errs, err := topics.Add(ctx, interest.Draft{Name: "The Budget", Depth: "close", Terms: "Budget"}); err != nil || errs != nil {
		t.Fatalf("add Topic: %v %v", errs, err)
	}
	if err := topics.CheckNew(ctx); err != nil {
		t.Fatal(err)
	}

	items, err := store.ListItems(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if !it.Checked {
			t.Errorf("%q was not checked", it.Title)
		}
	}
	if len(items[0].Topics) != 1 || len(items[1].Topics) != 0 {
		t.Errorf("ties = %v and %v; want the Budget statement alone tied", items[0].Topics, items[1].Topics)
	}
}

// Deepening a Topic re-runs the check for it over the waiting Items, as
// taking it up does. Here the Topic was stored but its first check never ran
// (as when it fails after the Topic is saved).
func TestDeepeningATopicRerunsTheCheckOverTheWaitingItems(t *testing.T) {
	ctx := t.Context()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "go-slow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, time.October, 24, 7, 0, 0, 0, time.UTC)
	src := collect.Source{ID: "number-10", Name: "Number 10", FeedURL: "https://example.org/feed", UsualGap: time.Hour}
	if err := store.SyncSources(ctx, []collect.Source{src}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddItems(ctx, []collect.Item{
		{SourceID: src.ID, EntryID: "a", Title: "PM statement on the Budget", Published: now, CollectedAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	topics := interest.New(store)
	if err := topics.CheckNew(ctx); err != nil {
		t.Fatal(err)
	}
	topic, err := store.AddTopic(ctx, interest.Topic{Name: "The Budget", Depth: interest.Passing, Terms: []string{"Budget"}})
	if err != nil {
		t.Fatal(err)
	}

	if errs, err := topics.SetDepth(ctx, topic.ID, "close"); err != nil || errs != nil {
		t.Fatalf("SetDepth: %v %v", errs, err)
	}

	items, err := store.ListItems(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items[0].Topics) != 1 {
		t.Errorf("after deepening, the Budget statement is tied to %v", items[0].Topics)
	}
}
