package sqlitetest_test

import (
	"testing"

	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
	"github.com/samthorold/go-slow/internal/sqlite/sqlitetest"
)

func TestEachPathIsARecordOfItsOwnWithEveryMigrationApplied(t *testing.T) {
	first, err := sqlite.Open(t.Context(), sqlitetest.Path(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	if _, err := first.AddTopic(t.Context(), interest.Topic{Name: "Rail", Terms: []string{"rail"}}); err != nil {
		t.Fatal(err)
	}

	second, err := sqlite.Open(t.Context(), sqlitetest.Path(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()

	if got, err := second.Topics(t.Context()); err != nil || len(got) != 0 {
		t.Errorf("a second Record holds the first's Topics: %+v %v", got, err)
	}
	if got, err := first.Topics(t.Context()); err != nil || len(got) != 1 {
		t.Errorf("the first Record's Topics: %+v %v", got, err)
	}
}
