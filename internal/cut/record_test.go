package cut_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/sqlite"
)

var now = time.Date(2026, time.October, 30, 7, 0, 0, 0, time.UTC)

func openStore(t *testing.T) *sqlite.Store {
	t.Helper()
	s, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "go-slow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func collectItems(t *testing.T, store *sqlite.Store, items ...collect.Item) {
	t.Helper()
	src := collect.Source{ID: "number-10", Name: "No 10", FeedURL: "https://example.org/feed", UsualGap: time.Hour}
	if err := store.SyncSources(t.Context(), []collect.Source{src}, now); err != nil {
		t.Fatal(err)
	}
	for i := range items {
		items[i].SourceID = src.ID
		items[i].CollectedAt = now
	}
	if _, err := store.AddItems(t.Context(), items); err != nil {
		t.Fatal(err)
	}
}

func TestACollectedItemIsCutAndItsCutNamesItsCutter(t *testing.T) {
	store := openStore(t)
	text := "<p>Work starts in May. It brings 500 jobs.</p>"
	collectItems(t, store, collect.Item{EntryID: "jobs", Title: "Jobs", Content: text})

	if err := cut.New(store).CutNew(t.Context(), now); err != nil {
		t.Fatal(err)
	}

	c, kept, found, err := store.NewestCut(t.Context(), "number-10", "jobs")
	if err != nil || !found {
		t.Fatalf("NewestCut: found %v, %v", found, err)
	}
	if c.Judge != cut.SentenceCutter || c.Judge.Cutter != "sentences" || c.Judge.Version != 1 {
		t.Errorf("Judge = %+v, want the sentence cutter, version 1", c.Judge)
	}
	if c.ID == 0 || !c.At.Equal(now) {
		t.Errorf("Cut is event %d at %v, want an identifier, at %v", c.ID, c.At, now)
	}
	if kept != text {
		t.Errorf("text = %q, want the Item's text as kept", kept)
	}
	var got []string
	for _, s := range c.Spans {
		got = append(got, cut.Words(kept, s))
	}
	if len(got) != 2 || got[0] != "Work starts in May." || got[1] != "It brings 500 jobs." {
		t.Errorf("Spans' words = %q", got)
	}
}
