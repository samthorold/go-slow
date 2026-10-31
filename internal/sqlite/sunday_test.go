package sqlite

import (
	"slices"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

func TestASundayReadStatementOrAnAssessmentCitingNothingIsUntracedInTheRegister(t *testing.T) {
	s, c := cutRelease(t)
	span := editor.OtherSpan{At: at(t, c, 2, "")}
	judge := editor.Judge{Act: "sunday_read", Version: editor.SundayReadVersion, Model: "fake"}
	if err := s.KeepSundayRead(t.Context(), editor.SundayRead{Day: "2026-10-18", WrittenAt: cutAt.Add(-7 * 24 * time.Hour),
		Judge:       judge,
		Assessments: []editor.Assessment{{Statement: "An earlier one.", AsAt: cutAt, Spans: []editor.OtherSpan{span}}},
		Statements:  []editor.ReadStatement{{Text: "Traced.", Cites: []editor.Citation{{Kind: editor.CitesSpan, Span: span}}}},
	}); err != nil {
		t.Fatal(err)
	}
	var earlier int64
	if err := s.db.QueryRowContext(t.Context(), `SELECT event_id FROM told_assessments`).Scan(&earlier); err != nil {
		t.Fatal(err)
	}
	if err := s.KeepSundayRead(t.Context(), editor.SundayRead{Day: "2026-10-25", WrittenAt: cutAt,
		Judge: judge,
		Assessments: []editor.Assessment{
			{Statement: "Rests on a Span.", AsAt: cutAt, Spans: []editor.OtherSpan{span}},
			{Statement: "Rests on nothing.", AsAt: cutAt},
			// An Assessment cites a Span or an earlier Assessment.
			{Statement: "Rests on an earlier Assessment.", AsAt: cutAt, Earlier: []editor.Assessment{{ID: earlier}}},
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
		"Assessment 2 of the Sunday read of 2026-10-25 cites nothing."}
	if len(r.Untraced) != 2 || !slices.Equal(r.Untraced, want) {
		t.Errorf("untraced %v, want %v", r.Untraced, want)
	}
	if !slices.Contains(r.SundayReads, "2026-10-25") {
		t.Errorf("Sunday reads written = %v", r.SundayReads)
	}
}
