package sqlite

import (
	"slices"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

func TestASundayReadStatementCitingNothingOrAnAssessmentRestingOnNoSpanIsUntracedInTheRegister(t *testing.T) {
	s, c := cutRelease(t)
	span := editor.OtherSpan{At: at(t, c, 2, "")}
	if err := s.KeepSundayRead(t.Context(), editor.SundayRead{Day: "2026-10-25", WrittenAt: cutAt,
		Judge: editor.Judge{Act: "sunday_read", Version: editor.SundayReadVersion, Model: "fake"},
		Assessments: []editor.Assessment{
			{Statement: "Rests on a Span.", AsAt: cutAt, Spans: []editor.OtherSpan{span}},
			{Statement: "Rests on nothing.", AsAt: cutAt},
		},
		Statements: []editor.ReadStatement{
			{Text: "Traced.", Cites: []editor.Citation{{Kind: editor.CitesSpan, Span: span}}},
			{Text: "Untraced."},
		},
	}); err != nil {
		t.Fatal(err)
	}

	r, err := s.RegisterRecord(t.Context(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"Statement 2 of the Sunday read of 2026-10-25 cites nothing.",
		"Assessment 2 of the Sunday read of 2026-10-25 rests on no Span."}
	if len(r.Untraced) != 2 || !slices.Equal(r.Untraced, want) {
		t.Errorf("untraced %v, want %v", r.Untraced, want)
	}
	if !slices.Contains(r.SundayReads, "2026-10-25") {
		t.Errorf("Sunday reads written = %v", r.SundayReads)
	}
}
