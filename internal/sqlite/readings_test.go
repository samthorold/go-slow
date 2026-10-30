package sqlite

import (
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// releaseText is cut into S1 "Andy Burnham said:", S2 "This is fantastic
// news for the city." and S3 "Derby has waited long enough."
const releaseText = "<p>Andy Burnham said:</p><p>This is fantastic news for the city. Derby has waited long enough.</p>"

// cutRelease is a store holding the release, cut, and the release's Cut.
func cutRelease(t *testing.T) (*Store, cut.Cut) {
	t.Helper()
	s := withItem(t, releaseText)
	if err := cut.New(s).CutNew(t.Context(), cutAt); err != nil {
		t.Fatal(err)
	}
	c, _, _, err := s.NewestCut(t.Context(), "number-10", "jobs")
	if err != nil {
		t.Fatal(err)
	}
	return s, c
}

// at is the address of words within the release's Span n.
func at(t *testing.T, c cut.Cut, n int, words string) editor.Address {
	t.Helper()
	sp, ok := cut.Within(releaseText, c.Spans[n-1], words)
	if !ok {
		t.Fatalf("%q not in S%d", words, n)
	}
	return editor.Address{SourceID: "number-10", EntryID: "jobs", Span: sp}
}

func releaseReading(t *testing.T, c cut.Cut) editor.Reading {
	t.Helper()
	return editor.Reading{
		SourceID: "number-10", EntryID: "jobs", CutID: c.ID, At: cutAt.Add(time.Hour),
		Judge:    editor.Judge{Act: "reading", Version: 1, Model: "claude-test", RequestHash: "abc123"},
		RefersTo: []editor.RefersTo{{From: at(t, c, 2, "the city"), To: at(t, c, 3, "Derby")}},
		Names: []editor.Name{
			{At: at(t, c, 1, "Andy Burnham"), Name: "Andy Burnham", Kind: "person", Office: "Prime Minister of the United Kingdom"},
			{At: at(t, c, 3, "Derby"), Name: "Derby", Kind: "place"},
		},
		Speakers: []editor.Speaker{{Of: at(t, c, 2, ""), By: at(t, c, 1, "Andy Burnham")}},
		Rejected: []editor.Address{at(t, c, 1, "")},
	}
}

func TestEachJudgementOfAReadingIsKeptAsItsOwnEventWithItsJudge(t *testing.T) {
	s, c := cutRelease(t)
	r := releaseReading(t, c)

	if err := s.KeepReading(t.Context(), r); err != nil {
		t.Fatal(err)
	}

	for kind, want := range map[string]int{"reading": 1, "refers_to": 1, "name": 2, "speaker": 1, "rejected": 1} {
		var n int
		if err := s.db.QueryRowContext(t.Context(), `SELECT count(*) FROM events WHERE kind = ?`, kind).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Errorf("%d %s events, want %d", n, kind, want)
		}
	}
	got, found, err := s.NewestReading(t.Context(), "number-10", "jobs")
	if err != nil || !found {
		t.Fatalf("NewestReading: %v %v", found, err)
	}
	if got.ID == 0 || got.Judge != r.Judge || got.CutID != c.ID || !got.At.Equal(r.At) {
		t.Errorf("reading = %+v, judged by %+v", got, got.Judge)
	}
	if len(got.RefersTo) != 1 || got.RefersTo[0] != r.RefersTo[0] {
		t.Errorf("refers to = %+v, want %+v", got.RefersTo, r.RefersTo)
	}
	if len(got.Names) != 2 || got.Names[0] != r.Names[0] || got.Names[1] != r.Names[1] {
		t.Errorf("names = %+v", got.Names)
	}
	if len(got.Speakers) != 1 || got.Speakers[0] != r.Speakers[0] {
		t.Errorf("speakers = %+v", got.Speakers)
	}
	if len(got.Rejected) != 1 || got.Rejected[0] != r.Rejected[0] {
		t.Errorf("rejected = %+v", got.Rejected)
	}
}

func TestAJudgementAddressingAnotherItemIsRefusedAndNothingIsKept(t *testing.T) {
	s, c := cutRelease(t)
	other := editor.Address{SourceID: "number-10", EntryID: "speech", Span: cut.Span{Start: 3, End: 9}}
	for name, change := range map[string]func(*editor.Reading){
		"a refers to": func(r *editor.Reading) { r.RefersTo[0].To = other },
		"a name":      func(r *editor.Reading) { r.Names[0].At = other },
		"a speaker":   func(r *editor.Reading) { r.Speakers[0].By = other },
		"a rejection": func(r *editor.Reading) { r.Rejected[0] = other },
	} {
		t.Run(name, func(t *testing.T) {
			r := releaseReading(t, c)
			change(&r)

			err := s.KeepReading(t.Context(), r)

			if err == nil || !strings.Contains(err.Error(), "another Item") {
				t.Errorf("err = %v, want a refusal", err)
			}
			if n := count(t, s, "readings"); n != 0 {
				t.Errorf("%d readings kept", n)
			}
		})
	}
}

func TestEveryItemCutIsAwaitingReadingWhateverItIsAbout(t *testing.T) {
	s, c := cutRelease(t)
	// An Item with no text is not cut, so it is not read.
	if _, err := s.AddItems(t.Context(), []collect.Item{{SourceID: "number-10", EntryID: "empty", Title: "Empty", CollectedAt: cutAt}}); err != nil {
		t.Fatal(err)
	}

	waiting, err := s.ItemsToRead(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(waiting) != 1 || waiting[0].Item.EntryID != "jobs" || waiting[0].Item.Content != releaseText ||
		waiting[0].Cut.ID != c.ID || len(waiting[0].Cut.Spans) != 3 {
		t.Fatalf("awaiting reading: %+v", waiting)
	}

	if err := s.KeepReading(t.Context(), releaseReading(t, c)); err != nil {
		t.Fatal(err)
	}
	if waiting, err := s.ItemsToRead(t.Context()); err != nil || len(waiting) != 0 {
		t.Errorf("a read Item still awaits reading: %+v %v", waiting, err)
	}
}

func TestAnItemWhoseReadingFailedAwaitsNothing(t *testing.T) {
	s, _ := cutRelease(t)
	if _, err := s.db.ExecContext(t.Context(), `
		INSERT INTO work_attempts (act, work_key, attempts, last_failed_at, last_error, failed)
		VALUES ('reading', 'number-10' || char(10) || 'jobs', 3, ?, 'refused', 1)`, formatTime(cutAt)); err != nil {
		t.Fatal(err)
	}

	if waiting, err := s.ItemsToRead(t.Context()); err != nil || len(waiting) != 0 {
		t.Errorf("failed work awaits reading: %+v %v", waiting, err)
	}
}

func TestAnItemWithNoReadingHasNone(t *testing.T) {
	s, _ := cutRelease(t)

	if _, found, err := s.NewestReading(t.Context(), "number-10", "jobs"); err != nil || found {
		t.Errorf("an unread Item has a reading (%v)", err)
	}
}
