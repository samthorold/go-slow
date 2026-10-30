package sqlite

import (
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
)

var cutAt = time.Date(2026, time.October, 30, 7, 0, 0, 0, time.UTC)

// withItem is a store holding one Item with text.
func withItem(t *testing.T, text string) *Store {
	t.Helper()
	s := openTestStore(t)
	src := collect.Source{ID: "number-10", Name: "No 10", FeedURL: "https://example.org/feed", UsualGap: time.Hour}
	if err := s.SyncSources(t.Context(), []collect.Source{src}, cutAt); err != nil {
		t.Fatal(err)
	}
	it := collect.Item{SourceID: src.ID, EntryID: "jobs", Title: "Jobs", Content: text, CollectedAt: cutAt}
	if _, err := s.AddItems(t.Context(), []collect.Item{it}); err != nil {
		t.Fatal(err)
	}
	return s
}

func count(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestItemsAreCutOnceByPlainCode(t *testing.T) {
	s := withItem(t, "<p>Work starts in May.</p>")
	cutter := cut.New(s)

	for range 2 {
		if err := cutter.CutNew(t.Context(), cutAt); err != nil {
			t.Fatal(err)
		}
	}

	if n := count(t, s, "cuts"); n != 1 {
		t.Errorf("%d Cuts kept, want 1", n)
	}
}

func TestAnItemWithNoTextIsNotCut(t *testing.T) {
	s := withItem(t, "")

	if err := cut.New(s).CutNew(t.Context(), cutAt); err != nil {
		t.Fatal(err)
	}

	if _, _, found, err := s.NewestCut(t.Context(), "number-10", "jobs"); err != nil || found {
		t.Errorf("an Item with no text was cut (%v)", err)
	}
}

func TestASecondCutterVersionAddsACutAndLeavesTheFirst(t *testing.T) {
	text := "<p>Work starts in May. It brings 500 jobs.</p>"
	s := withItem(t, text)
	if err := cut.New(s).CutNew(t.Context(), cutAt); err != nil {
		t.Fatal(err)
	}
	first, _, _, err := s.NewestCut(t.Context(), "number-10", "jobs")
	if err != nil {
		t.Fatal(err)
	}

	// A second version of the cutter, keeping the whole paragraph as one
	// Span beside the first sentence.
	v2 := cut.Judge{Cutter: "sentences", Version: 2}
	whole := cut.Span{Start: 3, End: len(text) - len("</p>")}
	second := cut.Cut{SourceID: "number-10", EntryID: "jobs", Judge: v2, At: cutAt.Add(time.Hour),
		Spans: []cut.Span{first.Spans[0], whole}}
	if err := s.KeepCut(t.Context(), second); err != nil {
		t.Fatal(err)
	}

	newest, _, _, err := s.NewestCut(t.Context(), "number-10", "jobs")
	if err != nil {
		t.Fatal(err)
	}
	if newest.Judge != v2 || newest.ID == first.ID {
		t.Errorf("newest Cut is %+v (event %d), want the second version's", newest.Judge, newest.ID)
	}
	if n := count(t, s, "cuts"); n != 2 {
		t.Errorf("%d Cuts kept, want both", n)
	}
	var judge cut.Judge
	if err := s.db.QueryRowContext(t.Context(),
		`SELECT cutter, cutter_version FROM cuts WHERE event_id = ?`, first.ID,
	).Scan(&judge.Cutter, &judge.Version); err != nil || judge != cut.SentenceCutter {
		t.Errorf("the first Cut is now %+v (%v), want it left as it was", judge, err)
	}
	// Two Judgements over the same words name the same Span.
	if newest.Spans[0] != first.Spans[0] {
		t.Errorf("the same sentence is %v in one Cut and %v in the other", first.Spans[0], newest.Spans[0])
	}
}

func TestEventsAreNeverChangedOrRemoved(t *testing.T) {
	s := withItem(t, "<p>Work starts in May.</p>")
	if err := cut.New(s).CutNew(t.Context(), cutAt); err != nil {
		t.Fatal(err)
	}

	for _, stmt := range []string{
		`UPDATE events SET at = ''`,
		`DELETE FROM events`,
		`UPDATE cuts SET cutter_version = 9`,
		`DELETE FROM cuts`,
		`UPDATE cut_spans SET span_end = span_end + 1`,
		`DELETE FROM cut_spans`,
	} {
		if _, err := s.db.ExecContext(t.Context(), stmt); err == nil {
			t.Errorf("%s succeeded, want it refused", stmt)
		}
	}
}
