package sqlite

import (
	"database/sql"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// beforeStoriesOnly is the number of migrations before the old design's acts
// (relating, the Briefing, the Sunday read, gatherings and Feedback) were
// retired: a Record at it may hold what they wrote.
const beforeStoriesOnly = 35

// retiringRecord is a database at beforeStoriesOnly holding an Item cut and
// hydrated, and what the retired acts wrote of it, as events, with their work
// failed; woven says whether a weaving run has been written too.
func retiringRecord(t *testing.T, woven bool) *sql.DB {
	t.Helper()
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "go-slow.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := migrateTo(ctx, db, beforeStoriesOnly); err != nil {
		t.Fatal(err)
	}
	qs := []string{
		`INSERT INTO sources (id, name, feed_url, usual_gap_seconds, added_at) VALUES ('s', 'S', 'https://example.org', 86400, '2026-11-01T00:00:00.000000000Z')`,
		`INSERT INTO items (source_id, entry_id, title, link, summary, content, published, updated, raw, attribution, collected_at)
			VALUES ('s', 'a', 'A', 'l', '', '<p>Derby waits.</p>', '2026-11-10T08:00:00.000000000Z', '', '', '', '2026-11-10T09:00:00.000000000Z'),
			('s', 'b', 'B', 'l', '', '<p>Derby gets jobs.</p>', '2026-11-11T08:00:00.000000000Z', '', '', '', '2026-11-11T09:00:00.000000000Z')`,
		`INSERT INTO events (id, kind, shape, at) VALUES
			(1, 'cut', 1, '2026-11-10T09:00:00.000000000Z'), (2, 'cut', 1, '2026-11-11T09:00:00.000000000Z'),
			(3, 'reading', 1, '2026-11-11T10:00:00.000000000Z'), (4, 'reading', 1, '2026-11-11T10:00:00.000000000Z'),
			(5, 'relating', 1, '2026-11-11T11:00:00.000000000Z'), (6, 'relation', 1, '2026-11-11T11:00:00.000000000Z'),
			(7, 'briefing', 1, '2026-11-12T07:00:00.000000000Z'), (8, 'statement', 1, '2026-11-12T07:00:00.000000000Z'),
			(9, 'gathering', 1, '2026-11-12T07:00:00.000000000Z'),
			(10, 'sunday_read', 1, '2026-11-15T07:00:00.000000000Z'), (11, 'assessment', 1, '2026-11-15T07:00:00.000000000Z'),
			(12, 'feedback', 1, '2026-11-12T08:00:00.000000000Z')`,
		`INSERT INTO cuts (event_id, source_id, entry_id, cutter, cutter_version) VALUES (1, 's', 'a', 'sentences', 1), (2, 's', 'b', 'sentences', 1)`,
		`INSERT INTO cut_spans (cut_id, n, span_start, span_end) VALUES (1, 1, 3, 15), (2, 1, 3, 19)`,
		`INSERT INTO readings (event_id, source_id, entry_id, cut_id, act, version, model, request_hash)
			VALUES (3, 's', 'a', 1, 'reading', 3, 'claude-test', 'ra'), (4, 's', 'b', 2, 'reading', 3, 'claude-test', 'rb')`,
		`INSERT INTO relatings (event_id, source_id, entry_id, reading_id, act, version, model, request_hash)
			VALUES (5, 's', 'b', 4, 'relating', 1, 'claude-test', 'rel')`,
		`INSERT INTO relations (event_id, relating_id, kind, from_source_id, from_entry_id, from_start, from_end,
			to_source_id, to_entry_id, to_start, to_end) VALUES (6, 5, 'follows', 's', 'b', 3, 19, 's', 'a', 3, 15)`,
		`INSERT INTO briefings (day, written_at, model) VALUES ('2026-11-12', '2026-11-12T07:00:00.000000000Z', 'claude-test')`,
		`INSERT INTO told_briefings (event_id, day, act, version, model, request_hash) VALUES (7, '2026-11-12', 'briefing', 1, 'claude-test', 'br')`,
		`INSERT INTO told_statements (event_id, briefing_id, position, text) VALUES (8, 7, 1, 'Derby gets jobs at last.')`,
		`INSERT INTO told_citations (statement_id, position, source_id, entry_id, span_start, span_end, reading_id) VALUES (8, 1, 's', 'b', 3, 19, 4)`,
		`INSERT INTO gatherings (event_id, briefing_id, source_id, entry_id, span_start, span_end) VALUES (9, 7, 's', 'b', 3, 19)`,
		`INSERT INTO gathered_spans (gathering_id, position, source_id, entry_id, span_start, span_end) VALUES (9, 1, 's', 'a', 3, 15)`,
		`INSERT INTO sunday_reads (day, written_at, model, turns, input_tokens, output_tokens) VALUES ('2026-11-15', '2026-11-15T07:00:00.000000000Z', 'claude-test', 1, 1, 1)`,
		`INSERT INTO told_reads (event_id, day, act, version, model) VALUES (10, '2026-11-15', 'sunday_read', 1, 'claude-test')`,
		`INSERT INTO told_assessments (event_id, read_id, position, statement, as_at) VALUES (11, 10, 1, 'Derby has waited.', '2026-11-15')`,
		`INSERT INTO assessment_grounds (assessment_id, position, source_id, entry_id, span_start, span_end, reading_id) VALUES (11, 1, 's', 'a', 3, 15, 3)`,
		`INSERT INTO feedback_given (event_id, kind, judgement_id) VALUES (12, 'wrong', 6)`,
		// A refers to given wrong, in the app, before Feedback was retired.
		`INSERT INTO events (id, kind, shape, at) VALUES (20, 'refers_to', 1, '2026-11-11T10:00:00.000000000Z'),
			(21, 'feedback', 1, '2026-11-12T08:00:00.000000000Z')`,
		`INSERT INTO refers_to (event_id, reading_id, from_start, from_end, to_start, to_end) VALUES (20, 4, 14, 18, 3, 8)`,
		`INSERT INTO feedback_given (event_id, kind, judgement_id) VALUES (21, 'wrong', 20)`,
		`INSERT INTO work_attempts (act, work_key, attempts, last_failed_at, last_error, failed)
			VALUES ('relating', 's' || char(10) || 'a', 3, '2026-11-11T10:00:00.000000000Z', 'refused', 1),
			('briefing', '2026-11-13', 3, '2026-11-13T10:00:00.000000000Z', 'refused', 1),
			('sunday_read', '2026-11-15', 3, '2026-11-15T10:00:00.000000000Z', 'refused', 1),
			('reading', 's' || char(10) || 'b', 3, '2026-11-11T10:00:00.000000000Z', 'refused', 1)`,
	}
	if woven {
		qs = append(qs,
			`INSERT INTO events (id, kind, shape, at) VALUES (13, 'weaving', 1, '2026-11-11T12:00:00.000000000Z'), (14, 'story', 1, '2026-11-11T12:00:00.000000000Z')`,
			`INSERT INTO weavings (event_id, work_key, through_cut, act, version, model, request_hash) VALUES (13, '2026-11-11', 1, 'weaving', 1, 'claude-test', 'w')`,
			`INSERT INTO woven_stories (event_id, weaving_id) VALUES (14, 13)`,
			`INSERT INTO editor_spans (story_id, n, body) VALUES (14, 1, 'Derby has waited long.')`,
			`INSERT INTO editor_span_cites (story_id, n, m, source_id, entry_id, span_start, span_end) VALUES (14, 1, 1, 's', 'a', 3, 15)`,
		)
	}
	for _, q := range qs {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return db
}

func TestWhatTheRetiredActsWroteStaysInTheRecord(t *testing.T) {
	ctx := t.Context()
	db := retiringRecord(t, false)

	if err := migrateTo(ctx, db, -1); err != nil {
		t.Fatal(err)
	}

	for table, want := range map[string]int{
		"relatings": 1, "relations": 1, "told_briefings": 1, "told_statements": 1, "told_citations": 1,
		"gatherings": 1, "gathered_spans": 1, "briefings": 1, "told_reads": 1, "told_assessments": 1,
		"assessment_grounds": 1, "sunday_reads": 1, "feedback_given": 2, "hydrations": 2, "cuts": 2,
	} {
		if got := strs(t, db, `SELECT count(*) FROM `+table); !slices.Equal(got, []string{strconv.Itoa(want)}) {
			t.Errorf("%s holds %v rows, want the %d kept", table, got, want)
		}
	}
	if got, want := strs(t, db, `SELECT kind FROM events WHERE kind != 'switch' ORDER BY id`), []string{
		"cut", "cut", "hydration", "hydration", "relating", "relation", "briefing", "statement", "gathering",
		"sunday_read", "assessment", "feedback", "refers_to", "feedback",
	}; !slices.Equal(got, want) {
		t.Errorf("events = %v, want every one kept, %v", got, want)
	}
	if broken, err := brokenForeignKeys(ctx, db); err != nil || broken {
		t.Errorf("a foreign key is broken: %v %v", broken, err)
	}
}

func TestNoProjectionReadsWhatTheRetiredActsWrote(t *testing.T) {
	ctx := t.Context()
	db := retiringRecord(t, true)
	if err := migrateTo(ctx, db, -1); err != nil {
		t.Fatal(err)
	}
	s := &Store{db: db}

	made, err := s.StoriesMade(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(made) != 1 || made[0].ID != 14 {
		t.Errorf("Stories made = %+v, want only the woven one, never a gathering", made)
	}
	// The Span the Briefing told, gathered and related is held by its Cut's
	// Story alone; the gathered Span by its Cut's and the woven Story.
	told, err := s.HoldingSpan(ctx, editor.Address{SourceID: "s", EntryID: "b", Span: cut.Span{Start: 3, End: 19}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(told.Cuts, []int64{2}) || len(told.Citing) != 0 {
		t.Errorf("held by %+v, want its Cut's Story alone", told)
	}
	gathered, err := s.HoldingSpan(ctx, editor.Address{SourceID: "s", EntryID: "a", Span: cut.Span{Start: 3, End: 15}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(gathered.Cuts, []int64{1}) || len(gathered.Citing) != 1 || gathered.Citing[0].Story != 14 {
		t.Errorf("held by %+v, want its Cut's Story and the woven one", gathered)
	}
	failed, err := s.FailedWork(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var acts []string
	for _, f := range failed {
		acts = append(acts, f.Act)
	}
	if !slices.Equal(acts, []string{"hydration"}) {
		t.Errorf("failed work listed for %v, want only the acts the Editor runs", acts)
	}
	counts, err := s.SourceCounts(ctx, time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC), cutAt.AddDate(0, 1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 1 || counts[0].Touching+counts[0].Untouched != 1 {
		t.Errorf("Source counts = %+v, want the one woven Story counted", counts)
	}
	// Feedback given in the app is read by no Projection: what it said was
	// wrong is read as judged.
	r, _, err := s.NewestHydration(ctx, "s", "b")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.RefersTo) != 1 || r.RefersTo[0].ID != 20 {
		t.Errorf("refers to = %+v, want the one given wrong, read all the same", r.RefersTo)
	}
}

// Items collected before the switch are woven only on the Owner's say: the
// first weaving run after the next start is not shown the whole Record.
func TestWeavingIsShownOnlyWhatWasCutAfterTheSwitch(t *testing.T) {
	ctx := t.Context()
	db := retiringRecord(t, false)
	if err := migrateTo(ctx, db, -1); err != nil {
		t.Fatal(err)
	}
	s := &Store{db: db}

	if got := strs(t, db, `SELECT kind || ' ' || shape FROM events WHERE kind = 'switch'`); !slices.Equal(got, []string{"switch 2"}) {
		t.Errorf("switches kept = %v, want this one, as an event", got)
	}
	waiting, err := s.StoriesToWeave(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(waiting) != 0 {
		t.Errorf("awaiting weaving: %+v, want nothing cut before the switch", waiting)
	}
	if _, err := s.AddItems(ctx, []collect.Item{{SourceID: "s", EntryID: "after", Title: "After",
		Content: "<p>Derby gets more jobs.</p>", CollectedAt: time.Date(2026, time.November, 18, 9, 0, 0, 0, time.UTC)}}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(s).CutNew(ctx, cutAt); err != nil {
		t.Fatal(err)
	}
	waiting, err = s.StoriesToWeave(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(waiting) != 1 || waiting[0].Item.EntryID != "after" {
		t.Errorf("awaiting weaving: %+v, want the Item collected after the switch", waiting)
	}
}

// A Record already woven carries on from its last run: the switch takes
// nothing from it.
func TestARecordAlreadyWovenCarriesOnFromItsLastRun(t *testing.T) {
	ctx := t.Context()
	db := retiringRecord(t, true)
	if err := migrateTo(ctx, db, -1); err != nil {
		t.Fatal(err)
	}
	s := &Store{db: db}

	waiting, err := s.StoriesToWeave(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(waiting) != 1 || waiting[0].Item.EntryID != "b" {
		t.Errorf("awaiting weaving: %+v, want the Item cut after the last run", waiting)
	}
}

func TestARecordWithNothingCutHasNoSwitchToStories(t *testing.T) {
	s := openTestStore(t)

	if got := strs(t, s.db, `SELECT kind FROM events WHERE kind = 'switch'`); len(got) != 0 {
		t.Errorf("a new Record has switches %v", got)
	}
}
