package sqlite

import (
	"path/filepath"
	"testing"

	"github.com/samthorold/go-slow/internal/interest"
)

func TestReopeningTheStoreKeepsWhatItHolds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "go-slow.db")
	ctx := t.Context()

	first, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if _, err := first.AddTopic(ctx, interest.Topic{Name: "Derby", Terms: []string{"Derby"}}); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	topics, err := second.Topics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(topics) != 1 || topics[0].Name != "Derby" {
		t.Errorf("Topics after reopening = %+v, want the one added", topics)
	}
}
