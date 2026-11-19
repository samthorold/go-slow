package main

import (
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

var saturday = time.Date(2026, time.October, 24, 7, 0, 0, 0, time.UTC)

// env is the app's environment, from .env.example, with the Record at db.
func env(db string) func(string) (string, bool) {
	vars := map[string]string{
		"PORT": "8080", "DATABASE_PATH": db, "COLLECTION_CONTACT": "owner@example.org",
		"COLLECTION_INTERVAL": "15m", "HYDRATION_MODEL": "claude-haiku-5-5", "GOSLOW_ANTHROPIC_API_KEY": "set-your-own",
		"WEAVING_MODEL": "claude-opus-5-5", "WEAVING_TIME": "06:00", "NAMING_MODEL": "claude-haiku-5-5",
	}
	return func(k string) (string, bool) { v, ok := vars[k]; return v, ok }
}

// failedRecord is a Record at db holding an Item cut and awaiting hydration,
// whose hydration is marked failed, and the day's weaving run, marked failed
// too.
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
		Link: "https://example.org/speech", Content: "<p>It is good to be in Scotland.</p>", Published: saturday, Attribution: collect.OGL, CollectedAt: saturday}}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(store).CutNew(ctx, saturday); err != nil {
		t.Fatal(err)
	}
	var failures []editor.WorkFailure
	for act, key := range map[string]string{"hydration": src.ID + "\nspeech", "weaving": "2026-10-24"} {
		failures = append(failures, editor.WorkFailure{Act: act, Key: key, At: saturday,
			Why: "the result fails the act's checks", Failed: true})
	}
	if err := store.EndBatch(ctx, "batch-1", failures, nil); err != nil {
		t.Fatal(err)
	}
}

// inRecord is what the Record at db holds of failed work and work awaiting
// hydration.
func inRecord(t *testing.T, db string) (failed []editor.FailedWork, toHydrate []editor.ToHydrate) {
	t.Helper()
	store, err := sqlite.Open(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if failed, err = store.FailedWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	if toHydrate, err = store.ItemsToHydrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return failed, toHydrate
}

func TestTheRetryCommandTakesAnActsFailedWorkUpAgain(t *testing.T) {
	db := filepath.Join(t.TempDir(), "go-slow.db")
	failedRecord(t, db)
	log := slog.New(slog.DiscardHandler)

	if err := admin(t.Context(), log, []string{"retry", "hydration"}, adminDeps{Lookup: env(db)}); err != nil {
		t.Fatal(err)
	}

	failed, toHydrate := inRecord(t, db)
	if len(toHydrate) != 1 {
		t.Errorf("awaiting hydration %+v, want the speech taken up again", toHydrate)
	}
	if len(failed) != 1 || failed[0].Act != "weaving" {
		t.Errorf("failed work %+v, want only weaving's left", failed)
	}

	if err := admin(t.Context(), log, []string{"retry"}, adminDeps{Lookup: env(db)}); err != nil {
		t.Fatal(err)
	}
	if failed, _ := inRecord(t, db); len(failed) != 0 {
		t.Errorf("failed work %+v, want none once every act's is retried", failed)
	}
}

func TestTheRetryCommandKnowsNaming(t *testing.T) {
	db := filepath.Join(t.TempDir(), "go-slow.db")
	failedRecord(t, db)

	if err := admin(t.Context(), slog.New(slog.DiscardHandler), []string{"retry", "naming"}, adminDeps{Lookup: env(db)}); err != nil {
		t.Errorf("retrying naming failed: %v", err)
	}
	if failed, _ := inRecord(t, db); len(failed) != 2 {
		t.Errorf("failed work %+v, want the other acts' left", failed)
	}
}

func TestTheRetryCommandRefusesAnActItDoesNotKnow(t *testing.T) {
	db := filepath.Join(t.TempDir(), "go-slow.db")
	failedRecord(t, db)

	// The retired acts are no longer run, so their work is not retried.
	for _, act := range []string{"briefing", "relating", "sunday_read"} {
		if err := admin(t.Context(), slog.New(slog.DiscardHandler), []string{"retry", act}, adminDeps{Lookup: env(db)}); err == nil {
			t.Errorf("retrying %s, which is not run, did not fail", act)
		}
	}
	if failed, _ := inRecord(t, db); len(failed) != 2 {
		t.Errorf("failed work %+v, want both left", failed)
	}
}

// The register, which read the commits, is retired with the old design.
func TestThereIsNoCommitsCommand(t *testing.T) {
	db := filepath.Join(t.TempDir(), "go-slow.db")

	if err := admin(t.Context(), slog.New(slog.DiscardHandler), []string{"commits"}, adminDeps{Lookup: env(db)}); err == nil {
		t.Error("the commits command ran")
	}
}
