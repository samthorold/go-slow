package interest_test

import (
	"path/filepath"
	"testing"

	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
)

func openStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "go-slow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// A Topic taken up is kept with its terms, and nothing else.
func TestATopicTakenUpIsKeptWithItsTerms(t *testing.T) {
	topics := interest.New(openStore(t))

	if _, errs, err := topics.Add(t.Context(), interest.Draft{Name: "The Budget", Terms: "Budget\nOBR\n"}); err != nil || errs != nil {
		t.Fatalf("add Topic: %v %v", errs, err)
	}

	got, err := topics.Topics(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "The Budget" || len(got[0].Terms) != 2 || got[0].Terms[1] != "OBR" {
		t.Errorf("Topics = %+v", got)
	}
}

// A Span touches a Topic by its terms alone, so a Topic with none would
// touch nothing.
func TestATopicIsTakenUpOnlyWithATerm(t *testing.T) {
	topics := interest.New(openStore(t))

	_, errs, err := topics.Add(t.Context(), interest.Draft{Name: "The economy"})

	if err != nil || errs["terms"] == "" {
		t.Errorf("a Topic with no term was taken up: %v %v", errs, err)
	}
}
