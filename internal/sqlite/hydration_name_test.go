package sqlite

import (
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
)

// beforeHydrationName is the number of migrations before the act that
// judges a Cut's Spans was called hydration: a Record at it keeps the act
// as reading.
const beforeHydrationName = 36

// readingRecord is a database at beforeHydrationName holding an Item cut and
// judged by the act under its old name, reading: a live judgement with a
// refers to, a candidate's, the kept call that made the live one, a failed
// mark on another Item's work and a pending batch.
func readingRecord(t *testing.T) *sql.DB {
	t.Helper()
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "go-slow.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := migrateTo(ctx, db, beforeHydrationName); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sources (id, name, feed_url, usual_gap_seconds, added_at) VALUES ('s', 'S', 'https://example.org', 86400, '2026-11-01T00:00:00.000000000Z')`,
		`INSERT INTO items (source_id, entry_id, title, link, summary, content, published, updated, raw, attribution, collected_at)
			VALUES ('s', 'a', 'A', 'l', '', '<p>Derby waits. The city waits.</p>', '2026-11-10T08:00:00.000000000Z', '', '', '', '2026-11-10T09:00:00.000000000Z'),
			('s', 'b', 'B', 'l', '', '<p>Derby gets jobs.</p>', '2026-11-11T08:00:00.000000000Z', '', '', '', '2026-11-11T09:00:00.000000000Z')`,
		`INSERT INTO events (id, kind, shape, at) VALUES
			(1, 'cut', 1, '2026-11-10T09:00:00.000000000Z'), (2, 'reading', 1, '2026-11-10T10:00:00.000000000Z'),
			(3, 'refers_to', 1, '2026-11-10T10:00:00.000000000Z'), (4, 'candidate', 1, '2026-11-11T10:00:00.000000000Z'),
			(5, 'reading', 1, '2026-11-11T10:00:00.000000000Z')`,
		`INSERT INTO cuts (event_id, source_id, entry_id, cutter, cutter_version) VALUES (1, 's', 'a', 'sentences', 1)`,
		`INSERT INTO cut_spans (cut_id, n, span_start, span_end) VALUES (1, 1, 3, 16), (1, 2, 17, 32)`,
		`INSERT INTO readings (event_id, source_id, entry_id, cut_id, act, version, model, request_hash)
			VALUES (2, 's', 'a', 1, 'reading', 3, 'claude-test', 'ra'), (5, 's', 'a', 1, 'reading', 3, 'claude-candidate', 'rc')`,
		`INSERT INTO refers_to (event_id, reading_id, from_start, from_end, to_start, to_end) VALUES (3, 2, 17, 25, 3, 8)`,
		`INSERT INTO candidates (event_id, act, version, model, range_from, range_to)
			VALUES (4, 'reading', 3, 'claude-candidate', '2026-11-01T00:00:00.000000000Z', '2026-11-30T00:00:00.000000000Z')`,
		`INSERT INTO candidate_readings (reading_id, candidate_id) VALUES (5, 4)`,
		`INSERT INTO call_texts (hash, text) VALUES ('i', 'Judge the Spans.'), ('sc', '{"type":"object"}'), ('in', 'S1: Derby waits.')`,
		`INSERT INTO model_calls (id, act, work_key, at, batch_id, model, request_hash, instructions, schema, input, precedents,
				output, reported_model, outcome, why)
			VALUES (1, 'reading', 's' || char(10) || 'a', '2026-11-10T10:00:00.000000000Z', 'batch-0', 'claude-test', 'ra',
				'i', 'sc', 'in', '[]', '{}', 'claude-test', 'applied', '')`,
		`INSERT INTO work_attempts (act, work_key, attempts, last_failed_at, last_error, failed)
			VALUES ('reading', 's' || char(10) || 'b', 3, '2026-11-11T10:00:00.000000000Z', 'refused', 1)`,
		`INSERT INTO batches (id, submitted_at) VALUES ('batch-1', '2026-11-12T09:00:00.000000000Z')`,
		`INSERT INTO batch_requests (batch_id, request_id, act, work_key, model, instructions, schema, input, precedents)
			VALUES ('batch-1', 'r1', 'reading', 's' || char(10) || 'b', 'claude-test', 'i', 'sc', 'in', '[]')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return db
}

func TestARecordKeepingTheActAsReadingIsReadAsHydrationOnceMigrated(t *testing.T) {
	ctx := t.Context()
	db := readingRecord(t)

	if err := migrateTo(ctx, db, -1); err != nil {
		t.Fatal(err)
	}
	s := &Store{db: db}

	r, found, err := s.NewestHydration(ctx, "s", "a")
	if err != nil {
		t.Fatal(err)
	}
	if !found || r.ID != 2 || r.Judge.Act != "hydration" || len(r.RefersTo) != 1 || r.RefersTo[0].ID != 3 || r.Call != 1 {
		t.Errorf("newest = %+v (found %v), want the live judgement, as hydration's, with its refers to and its call", r, found)
	}
	call, found, err := s.Call(ctx, 1)
	if err != nil || !found {
		t.Fatalf("call 1: found %v, %v", found, err)
	}
	if call.Act != "hydration" {
		t.Errorf("the kept call's act = %q, want hydration", call.Act)
	}
	cands, err := s.Candidates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].Act != "hydration" || cands[0].Judgements != 1 {
		t.Errorf("candidates = %+v, want the one, hydration's, with its judgement", cands)
	}
	failed, err := s.FailedWork(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 || failed[0].Act != "hydration" || failed[0].EntryID != "b" {
		t.Errorf("failed work = %+v, want B's, hydration's", failed)
	}
	pending, err := s.PendingBatches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || len(pending[0].Requests) != 1 || pending[0].Requests[0].Act != "hydration" ||
		pending[0].Requests[0].Request == nil || pending[0].Requests[0].Request.Act != "hydration" {
		t.Errorf("pending batches = %+v, want the one request, hydration's", pending)
	}
	if n, err := s.RetryFailed(ctx, "hydration"); err != nil || n != 1 {
		t.Errorf("retrying hydration cleared %d, %v; want B's failed mark", n, err)
	}

	for _, q := range []string{
		`SELECT kind FROM events WHERE kind = 'reading'`,
		`SELECT act FROM hydrations WHERE act = 'reading'`,
		`SELECT act FROM candidates WHERE act = 'reading'`,
		`SELECT act FROM model_calls WHERE act = 'reading'`,
		`SELECT act FROM conversations WHERE act = 'reading'`,
		`SELECT act FROM work_attempts WHERE act = 'reading'`,
		`SELECT act FROM batch_requests WHERE act = 'reading'`,
	} {
		if got := strs(t, db, q); len(got) != 0 {
			t.Errorf("%s: %v, want the act called hydration everywhere", q, got)
		}
	}
	if got := strs(t, db, `SELECT kind FROM events ORDER BY id`); !slices.Equal(got, []string{"cut", "hydration", "refers_to", "candidate", "hydration"}) {
		t.Errorf("events = %v, want the judgements' events of kind hydration", got)
	}
	if _, err := db.ExecContext(ctx, `UPDATE events SET kind = kind`); err == nil {
		t.Error("an event was rewritten after the migration, want events append-only still")
	}
	if _, err := db.ExecContext(ctx, `UPDATE model_calls SET act = act`); err == nil {
		t.Error("a kept call was rewritten after the migration, want it never rewritten still")
	}
	if broken, err := brokenForeignKeys(ctx, db); err != nil || broken {
		t.Errorf("a foreign key is broken: %v %v", broken, err)
	}
}
