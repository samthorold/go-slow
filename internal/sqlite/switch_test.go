package sqlite

import (
	"slices"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// november is a day in November 2026, in UTC.
func november(day int) time.Time { return time.Date(2026, time.November, day, 9, 0, 0, 0, time.UTC) }

// threeCut is a Record holding three Items, collected on 1, 3 and 5
// November, each cut, and the Cuts' events, in that order.
func threeCut(t *testing.T) (*Store, []int64) {
	t.Helper()
	ctx := t.Context()
	s := openTestStore(t)
	src := collect.Source{ID: "s", Name: "S", FeedURL: "https://example.org/feed", Attribution: collect.OGL, UsualGap: time.Hour}
	if err := s.SyncSources(ctx, []collect.Source{src}, november(1)); err != nil {
		t.Fatal(err)
	}
	for _, day := range []int{1, 3, 5} {
		entry := time.Date(2026, time.November, day, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
		if _, err := s.AddItems(ctx, []collect.Item{{SourceID: src.ID, EntryID: entry, Title: entry,
			Content: "<p>Derby gets more jobs.</p>", Published: november(day), CollectedAt: november(day)}}); err != nil {
			t.Fatal(err)
		}
		if err := cut.New(s).CutNew(ctx, november(day)); err != nil {
			t.Fatal(err)
		}
	}
	waiting, err := s.StoriesToWeave(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var cuts []int64
	for _, c := range waiting {
		cuts = append(cuts, c.CutID)
	}
	if len(cuts) != 3 {
		t.Fatalf("cut %v, want three", cuts)
	}
	return s, cuts
}

// awaiting are the Items whose Stories await weaving.
func awaiting(t *testing.T, s *Store) []string {
	t.Helper()
	waiting, err := s.StoriesToWeave(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range waiting {
		out = append(out, c.Item.EntryID)
	}
	return out
}

// The Record's switch to Stories is its newest switch event, so a switch is
// moved back, as well as forward, by keeping another, rewriting none.
func TestTheSwitchToStoriesIsTheNewestSwitchKept(t *testing.T) {
	ctx := t.Context()
	s, cuts := threeCut(t)

	if err := s.KeepStorySwitch(ctx, cuts[2], november(6)); err != nil {
		t.Fatal(err)
	}
	if got := awaiting(t, s); len(got) != 0 {
		t.Errorf("awaiting weaving %v, want nothing cut through the switch", got)
	}
	through, found, err := s.CutBefore(ctx, time.Date(2026, time.November, 3, 0, 0, 0, 0, time.UTC))
	if err != nil || !found || through != cuts[0] {
		t.Fatalf("the newest Cut before 3 November is %d (%v, %v), want %d", through, found, err, cuts[0])
	}
	if err := s.KeepStorySwitch(ctx, through, november(7)); err != nil {
		t.Fatal(err)
	}

	if got := awaiting(t, s); !slices.Equal(got, []string{"2026-11-03", "2026-11-05"}) {
		t.Errorf("awaiting weaving %v, want the Items collected on and after 3 November", got)
	}
	if got := strs(t, s.db, `SELECT kind || ' ' || shape FROM events WHERE kind = 'switch'`); !slices.Equal(got,
		[]string{"switch 2", "switch 2"}) {
		t.Errorf("switches kept = %v, want both, as events", got)
	}
}

// A Record is woven once a weaving run's result is written.
func TestARecordIsWovenOnceARunIsWritten(t *testing.T) {
	ctx := t.Context()
	s, cuts := threeCut(t)
	if woven, err := s.Woven(ctx); err != nil || woven {
		t.Errorf("woven = %v (%v) before any run", woven, err)
	}

	if err := s.KeepWeaving(ctx, editor.Weaving{Key: "2026-11-06", ThroughCut: cuts[2], At: november(6),
		Judge: editor.Judge{Act: "weaving", Version: 1, Model: "m", RequestHash: "h"}}); err != nil {
		t.Fatal(err)
	}

	if woven, err := s.Woven(ctx); err != nil || !woven {
		t.Errorf("woven = %v (%v) once a run is written", woven, err)
	}
}

func TestNoCutIsBeforeTheFirstItemCollected(t *testing.T) {
	s, _ := threeCut(t)

	if _, found, err := s.CutBefore(t.Context(), november(1).Add(-time.Hour)); err != nil || found {
		t.Errorf("found a Cut before every Item (%v)", err)
	}
}
