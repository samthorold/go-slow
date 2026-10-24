package sqlite

import (
	"path/filepath"
	"testing"
	"time"
)

func TestReopeningTheStoreKeepsWhatItHolds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "go-slow.db")
	ctx := t.Context()
	day := time.Date(2026, time.October, 24, 7, 0, 0, 0, time.UTC)

	first, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	// There is no command that writes a Briefing yet, so the test writes the
	// row itself.
	if _, err := first.db.ExecContext(ctx, `INSERT INTO briefings (day) VALUES ('2026-10-24')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	has, err := second.HasBriefing(ctx, day)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Error("HasBriefing(24 October) = false after reopening, want true")
	}
	has, err = second.HasBriefing(ctx, day.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Error("HasBriefing(25 October) = true, want false")
	}
}
