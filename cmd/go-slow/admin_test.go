package main

import (
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

// env is the app's environment, from .env.example, with the Record at db.
func env(db string) func(string) (string, bool) {
	vars := map[string]string{
		"PORT": "8080", "DATABASE_PATH": db, "COLLECTION_CONTACT": "owner@example.org",
		"COLLECTION_INTERVAL": "15m", "READING_MODEL": "claude-haiku-5-5", "PLACING_MODEL": "claude-haiku-5-5",
		"CONNECTING_MODEL": "claude-haiku-5-5", "BRIEFING_MODEL": "claude-haiku-5-5",
		"SUNDAY_READ_MODEL": "claude-opus-5-5", "BRIEFING_TIME": "07:00", "GOSLOW_ANTHROPIC_API_KEY": "set-your-own",
	}
	return func(k string) (string, bool) { v, ok := vars[k]; return v, ok }
}

// failedRecord is a Record at db holding an Item awaiting reading for a
// Topic, whose reading and placing are both marked failed.
func failedRecord(t *testing.T, db string) {
	t.Helper()
	ctx := t.Context()
	store, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	src := collect.Source{ID: "number-10", Name: "Number 10", FeedURL: "https://example.org/feed",
		Attribution: collect.OGL, UsualGap: time.Hour}
	if err := store.SyncSources(ctx, []collect.Source{src}, saturday); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddItems(ctx, []collect.Item{{SourceID: src.ID, EntryID: "speech", Title: "PM speech in Scotland",
		Link: "https://example.org/speech", Published: saturday, Attribution: collect.OGL, CollectedAt: saturday}}); err != nil {
		t.Fatal(err)
	}
	topics := interest.New(store)
	if _, errs, err := topics.Add(ctx, interest.Draft{Name: "Scotland", Depth: "following", Terms: "Scotland"}); err != nil || errs != nil {
		t.Fatalf("add Topic: %v %v", errs, err)
	}
	if err := topics.CheckNew(ctx); err != nil {
		t.Fatal(err)
	}
	var failures []editor.WorkFailure
	for _, act := range []string{"reading", "placing"} {
		failures = append(failures, editor.WorkFailure{Act: act, Key: src.ID + "\nspeech", At: saturday,
			Why: "the result fails the act's checks", Failed: true})
	}
	if err := store.EndBatch(ctx, "batch-1", failures, nil); err != nil {
		t.Fatal(err)
	}
}

// inRecord is what the Record at db holds of failed work and work awaiting
// reading.
func inRecord(t *testing.T, db string) (failed []editor.FailedWork, toRead []editor.Item) {
	t.Helper()
	store, err := sqlite.Open(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if failed, err = store.FailedWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	if toRead, err = store.ItemsToRead(t.Context()); err != nil {
		t.Fatal(err)
	}
	return failed, toRead
}

func TestTheRetryCommandTakesAnActsFailedWorkUpAgain(t *testing.T) {
	db := filepath.Join(t.TempDir(), "go-slow.db")
	failedRecord(t, db)
	log := slog.New(slog.DiscardHandler)

	if err := admin(t.Context(), log, []string{"retry", "reading"}, env(db), nil); err != nil {
		t.Fatal(err)
	}

	failed, toRead := inRecord(t, db)
	if len(toRead) != 1 {
		t.Errorf("awaiting reading %+v, want the speech taken up again", toRead)
	}
	if len(failed) != 1 || failed[0].Act != "placing" {
		t.Errorf("failed work %+v, want only placing's left", failed)
	}

	if err := admin(t.Context(), log, []string{"retry"}, env(db), nil); err != nil {
		t.Fatal(err)
	}
	if failed, _ := inRecord(t, db); len(failed) != 0 {
		t.Errorf("failed work %+v, want none once every act's is retried", failed)
	}
}

func TestTheRetryCommandRefusesAnActItDoesNotKnow(t *testing.T) {
	db := filepath.Join(t.TempDir(), "go-slow.db")
	failedRecord(t, db)

	if err := admin(t.Context(), slog.New(slog.DiscardHandler), []string{"retry", "briefing"}, env(db), nil); err == nil {
		t.Error("retrying an act that is not run in batches did not fail")
	}
	if failed, _ := inRecord(t, db); len(failed) != 2 {
		t.Errorf("failed work %+v, want both left", failed)
	}
}
