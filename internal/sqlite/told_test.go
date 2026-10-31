package sqlite

import (
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

func TestTheSpansToTellAreThoseOfItemsCutInTheWindowNotRejectedAsTheirReadingReadsThem(t *testing.T) {
	s, c := cutRelease(t)
	if err := s.KeepReading(t.Context(), releaseReading(t, c)); err != nil {
		t.Fatal(err)
	}

	got, err := s.SpansToTell(t.Context(), cutAt.Add(-time.Hour), cutAt)
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.SpansToTell(t.Context(), cutAt, cutAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	if len(before) != 0 {
		t.Errorf("an Item cut before the window is told again: %+v", before)
	}
	if len(got) != 1 || !got[0].Read || len(got[0].Spans) != 2 {
		t.Fatalf("to tell %+v, want the release, read, with its two Spans not rejected", got)
	}
	s2 := got[0].Spans[0]
	if s2.Words != "This is fantastic news for Derby." || s2.Speaker != "Andy Burnham" || s2.N != 2 || s2.ReadingID == 0 {
		t.Errorf("S2 shown as %+v", s2.OtherSpan)
	}
}

func TestAStatementCitingNoSpanIsUntracedInTheRegister(t *testing.T) {
	s, c := cutRelease(t)
	cites := []editor.OtherSpan{{At: at(t, c, 2, "")}}
	if err := s.KeepBriefing(t.Context(), editor.Briefing{Day: "2026-10-24", WrittenAt: cutAt,
		Judge:      editor.Judge{Act: "briefing", Version: editor.BriefingVersion, Model: "fake"},
		Statements: []editor.Statement{{Text: "Traced.", Cites: cites}, {Text: "Untraced."}},
	}); err != nil {
		t.Fatal(err)
	}

	r, err := s.RegisterRecord(t.Context(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}

	if len(r.Untraced) != 1 || !strings.Contains(r.Untraced[0], "2026-10-24") {
		t.Errorf("untraced %v, want the statement citing nothing", r.Untraced)
	}
}
