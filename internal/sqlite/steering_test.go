package sqlite

import (
	"slices"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
)

// steeredRelease is the release (collected at cutAt), read (S1 Rejected),
// with a weaving run at cutAt that made two Stories: one whose Editor's Span
// cites S2 and S3, which names Derby, and one whose Editor's Span cites S3
// alone; and a Topic following Derby.
func steeredRelease(t *testing.T) *Store {
	t.Helper()
	return steeredReleaseWovenAt(t, cutAt)
}

// steeredReleaseWovenAt is steeredRelease with its Stories made at made.
func steeredReleaseWovenAt(t *testing.T, made time.Time) *Store {
	t.Helper()
	s, c := cutRelease(t)
	if err := s.KeepReading(t.Context(), releaseReading(t, c)); err != nil {
		t.Fatal(err)
	}
	s2, s3 := editor.Cited{Address: at(t, c, 2, "")}, editor.Cited{Address: at(t, c, 3, "")}
	if err := s.KeepWeaving(t.Context(), editor.Weaving{Key: "2026-10-30", ThroughCut: c.ID, At: made,
		Judge: editor.Judge{Act: "weaving", Version: 1, Model: "fake"},
		Stories: []editor.Story{
			{Spans: []editor.EditorSpan{{Body: "A city has waited, and is glad.", Cites: []editor.Cited{s2, s3}}}},
			{Spans: []editor.EditorSpan{{Body: "Long enough.", Cites: []editor.Cited{s3}}}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddTopic(t.Context(), interest.Topic{Name: "Derby", Terms: []string{"Derby"}}); err != nil {
		t.Fatal(err)
	}
	return s
}

func steering(t *testing.T, s *Store, from, to time.Time) interest.SourceCounts {
	t.Helper()
	counts, err := s.SourceCounts(t.Context(), from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 1 || counts[0].SourceID != "number-10" {
		t.Fatalf("counts %+v, want one for each Source", counts)
	}
	return counts[0]
}

func TestASourceCountsTheStoriesItContributesToThatTouchATopicAndThoseThatDont(t *testing.T) {
	s := steeredRelease(t)

	got := steering(t, s, cutAt.AddDate(0, 0, -28), cutAt)

	// Both Stories hold S3, which names Derby.
	if got.Touching != 2 || got.Untouched != 0 {
		t.Errorf("touching %d, untouched %d; want 2 and 0", got.Touching, got.Untouched)
	}
}

func TestAStoryTouchingNoTopicCountsAsUntouched(t *testing.T) {
	s := steeredRelease(t)
	topics, err := s.Topics(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveTopic(t.Context(), topics[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddTopic(t.Context(), interest.Topic{Name: "Fantastic", Terms: []string{"fantastic"}}); err != nil {
		t.Fatal(err)
	}

	got := steering(t, s, cutAt.AddDate(0, 0, -28), cutAt)

	// "fantastic" is in S2: the Story citing S2 touches it, the one citing
	// S3 alone does not.
	if got.Touching != 1 || got.Untouched != 1 {
		t.Errorf("touching %d, untouched %d; want 1 and 1", got.Touching, got.Untouched)
	}
}

func TestASourceCountsItsSpansRejectedByTheirNewestReadings(t *testing.T) {
	s := steeredRelease(t)

	got := steering(t, s, cutAt.AddDate(0, 0, -28), cutAt)

	if got.Spans != 3 || got.Rejected != 1 {
		t.Errorf("%d of %d Spans rejected, want 1 of 3", got.Rejected, got.Spans)
	}
}

func TestASourceCountsOnlyTheStoriesItContributesTo(t *testing.T) {
	s := steeredRelease(t)
	treasury := collect.Source{ID: "hm-treasury", Name: "HM Treasury", FeedURL: "https://example.org/treasury", UsualGap: time.Hour}
	if err := s.SyncSources(t.Context(), []collect.Source{
		{ID: "number-10", Name: "No 10", FeedURL: "https://example.org/feed", UsualGap: time.Hour}, treasury,
	}, cutAt); err != nil {
		t.Fatal(err)
	}

	counts, err := s.SourceCounts(t.Context(), cutAt.AddDate(0, 0, -28), cutAt)
	if err != nil {
		t.Fatal(err)
	}

	want := []interest.SourceCounts{
		{SourceID: "hm-treasury"},
		{SourceID: "number-10", Touching: 2, Spans: 3, Rejected: 1},
	}
	if !slices.Equal(counts, want) {
		t.Errorf("counts %+v, want %+v", counts, want)
	}
}

// The window is on when the Items were collected, not when the Stories
// holding their Spans were made.
func TestTheStoriesCountedAreThoseHoldingASpanOfAnItemCollectedInTheWindow(t *testing.T) {
	s := steeredReleaseWovenAt(t, cutAt.AddDate(0, 0, 60))

	got := steering(t, s, cutAt.AddDate(0, 0, -28), cutAt)

	if got.Touching != 2 || got.Untouched != 0 {
		t.Errorf("touching %d, untouched %d; want 2 and 0", got.Touching, got.Untouched)
	}
}

// An Editor's Span's own words touch a Topic, as the Spans it cites do.
func TestAStoryTouchesATopicItsEditorsSpanNames(t *testing.T) {
	s := steeredRelease(t)
	if _, err := s.AddTopic(t.Context(), interest.Topic{Name: "Gladness", Terms: []string{"glad"}}); err != nil {
		t.Fatal(err)
	}
	topics, err := s.Topics(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, tp := range topics {
		if tp.Name == "Derby" {
			if _, err := s.RemoveTopic(t.Context(), tp.ID); err != nil {
				t.Fatal(err)
			}
		}
	}

	got := steering(t, s, cutAt.AddDate(0, 0, -28), cutAt)

	if got.Touching != 1 || got.Untouched != 1 {
		t.Errorf("touching %d, untouched %d; want 1 and 1", got.Touching, got.Untouched)
	}
}

func TestTheCountsAreOverTheirWindow(t *testing.T) {
	s := steeredRelease(t)

	got := steering(t, s, cutAt, cutAt.AddDate(0, 0, 28))

	if got != (interest.SourceCounts{SourceID: "number-10"}) {
		t.Errorf("counts after the window holding everything: %+v, want none", got)
	}
}
