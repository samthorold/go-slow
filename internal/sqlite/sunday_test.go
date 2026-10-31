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

func TestASundayReadOfTheEarlierShapeIsReadAsWritten(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.db.ExecContext(t.Context(), `
		INSERT INTO sunday_reads (id, day, written_at, model, turns, input_tokens, output_tokens) VALUES (1, '2026-10-18', ?, 'm', 1, 1, 1);
		INSERT INTO read_statements (read_id, position, text) VALUES (1, 0, 'Said before.');
		INSERT INTO assessments (read_id, position, statement, as_at) VALUES (1, 0, 'Judged before.', '2026-10-18')`,
		formatTime(cutAt)); err != nil {
		t.Fatal(err)
	}

	statements, assessments, found, err := s.EarlierSundayRead(t.Context(), "2026-10-18")
	if err != nil || !found || !slices.Equal(statements, []string{"Said before."}) || !slices.Equal(assessments, []string{"Judged before."}) {
		t.Errorf("earlier read = %v %v %v %v", statements, assessments, found, err)
	}
	if _, found, err := s.SundayRead(t.Context(), "2026-10-18"); err != nil || found {
		t.Errorf("an earlier read is read as one over Spans: %v %v", found, err)
	}
	if _, _, found, _ := s.EarlierSundayRead(t.Context(), "2026-10-25"); found {
		t.Error("a Sunday with no read has an earlier one")
	}
}
