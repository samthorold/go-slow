package sqlite

import (
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// beforeEventsOnly is the number of migrations before the Record kept only
// events: a Record at it still holds the old design's tables.
const beforeEventsOnly = 31

// oldRecord is a database at beforeEventsOnly holding what the old design
// kept beside the events already kept since the Record became events.
func oldRecord(t *testing.T) *sql.DB {
	t.Helper()
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "go-slow.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := migrateTo(ctx, db, beforeEventsOnly); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sources (id, name, feed_url, usual_gap_seconds, added_at) VALUES ('s', 'S', 'https://example.org', 86400, '2026-10-01T00:00:00.000000000Z')`,
		`INSERT INTO items (source_id, entry_id, title, link, summary, content, published, updated, raw, attribution, collected_at)
			VALUES ('s', 'old', 'Old', 'l', 'sum', '<p>Derby waits.</p>', '2026-10-16T08:00:00.000000000Z', '', '', '', '2026-10-16T09:00:00.000000000Z'),
			('s', 'newer', 'Newer', 'l', '', '<p>Derby gets jobs.</p>', '2026-10-20T08:00:00.000000000Z', '', '', '', '2026-10-20T09:00:00.000000000Z')`,
		// The old design's judgements and marks.
		`INSERT INTO topics (name, depth, sources, terms) VALUES ('Derby', 2, 's', 'Derby')`,
		`INSERT INTO item_topics (source_id, entry_id, topic_id) VALUES ('s', 'old', 1)`,
		`INSERT INTO item_checks (source_id, entry_id) VALUES ('s', 'old')`,
		`INSERT INTO item_reads (source_id, entry_id, read_at, model) VALUES ('s', 'old', '2026-10-16T10:00:00.000000000Z', 'm')`,
		`INSERT INTO entities (name, kind) VALUES ('Derby', 'place')`,
		`INSERT INTO item_mentions (source_id, entry_id, entity_id) VALUES ('s', 'old', 1)`,
		`INSERT INTO feedback (kind, level, story_id, depth, given_at) VALUES ('unwanted', 'story', 1, 2, '2026-10-17T08:00:00.000000000Z')`,
		`INSERT INTO facts (source_id, entry_id, statement, as_at, quote, withdrawn_by) VALUES ('s', 'old', 'Derby waits.', '2026-10-16', 'Derby waits.', NULL)`,
		`INSERT INTO fact_entities (fact_id, entity_id, role) VALUES (1, 1, 'place')`,
		`INSERT INTO claims (source_id, entry_id, statement, made_by, quote) VALUES ('s', 'old', 'Derby will win.', 'Mayor', 'q')`,
		`INSERT INTO stories (title, started_at, unwanted_by) VALUES ('Derby', '2026-10-16T10:00:00.000000000Z', 1)`,
		`INSERT INTO developments (story_id, fact_id) VALUES (1, 1)`,
		`INSERT INTO fact_placings (fact_id, placed_at, model) VALUES (1, '2026-10-16T10:00:00.000000000Z', 'm')`,
		`INSERT INTO briefings (day, written_at, model) VALUES ('2026-10-17', '2026-10-17T07:00:00.000000000Z', 'm')`,
		`INSERT INTO briefing_stories (day, position, story_id) VALUES ('2026-10-17', 1, 1)`,
		`INSERT INTO briefing_statements (day, story_id, position, text) VALUES ('2026-10-17', 1, 1, 'Derby waits.')`,
		`INSERT INTO statement_developments (statement_id, story_id, fact_id, position) VALUES (1, 1, 1, 1)`,
		`INSERT INTO sunday_reads (day, written_at, model, turns, input_tokens, output_tokens) VALUES ('2026-10-18', '2026-10-18T08:00:00.000000000Z', 'm', 1, 1, 1)`,
		`INSERT INTO assessments (read_id, position, statement, as_at) VALUES (1, 1, 'Derby has waited.', '2026-10-18')`,
		`INSERT INTO work_attempts (act, work_key, attempts, last_failed_at, last_error, failed)
			VALUES ('placing', 's' || char(10) || 'old', 3, '2026-10-16T10:00:00.000000000Z', 'refused', 1),
			('relating', 's' || char(10) || 'newer', 3, '2026-10-20T10:00:00.000000000Z', 'refused', 1)`,
		// What the register reads, which carries over.
		`INSERT INTO opens (output, day, opened_on) VALUES ('briefing', '2026-10-17', '2026-10-17'), ('sunday read', '2026-10-18', '2026-10-18')`,
		`INSERT INTO commits (hash, committed_at, editor) VALUES ('abc', '2026-10-17T12:00:00.000000000Z', 1)`,
		// The events kept since the Record became events, and a Briefing
		// over Spans.
		`INSERT INTO events (kind, shape, at) VALUES ('cut', 1, '2026-10-20T09:00:00.000000000Z'), ('briefing', 1, '2026-10-21T07:00:00.000000000Z')`,
		`INSERT INTO cuts (event_id, source_id, entry_id, cutter, cutter_version) VALUES (1, 's', 'newer', 'sentences', 1)`,
		`INSERT INTO cut_spans (cut_id, n, span_start, span_end) VALUES (1, 1, 3, 18)`,
		`INSERT INTO briefings (day, written_at, model) VALUES ('2026-10-21', '2026-10-21T07:00:00.000000000Z', '')`,
		`INSERT INTO told_briefings (event_id, day, act, version, model, request_hash) VALUES (2, '2026-10-21', 'briefing', 1, '', '')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return db
}

func strs(t *testing.T, db *sql.DB, q string) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), q)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTheRecordKeepsOnlyItsItemsAndEventsAndNoneOfTheOldJudgements(t *testing.T) {
	ctx := t.Context()
	db := oldRecord(t)

	if err := migrateTo(ctx, db, -1); err != nil {
		t.Fatal(err)
	}

	if got, want := strs(t, db, `SELECT entry_id || '|' || content || '|' || collected_at FROM items ORDER BY entry_id`), []string{
		"newer|<p>Derby gets jobs.</p>|2026-10-20T09:00:00.000000000Z",
		"old|<p>Derby waits.</p>|2026-10-16T09:00:00.000000000Z",
	}; !slices.Equal(got, want) {
		t.Errorf("Items = %v, want them and their text as collected, %v", got, want)
	}
	for _, table := range []string{
		"facts", "claims", "fact_entities", "fact_placings", "fact_connectings", "fact_connections",
		"stories", "developments", "briefing_stories", "briefing_statements", "statement_developments",
		"feedback", "precedents_shown", "item_topics", "item_checks", "item_reads", "item_mentions",
		"entities", "entity_names", "reference_facts", "reference_fact_entities",
		"assessments", "assessment_facts", "read_statements", "read_citations", "read_stories",
	} {
		if got := strs(t, db, `SELECT name FROM sqlite_master WHERE name = '`+table+`'`); len(got) != 0 {
			t.Errorf("the old design's %s is still kept", table)
		}
	}
	// That an output was written on a day is what the register reads; what
	// the old ones said goes.
	if got, want := strs(t, db, `SELECT day FROM briefings ORDER BY day`), []string{"2026-10-17", "2026-10-21"}; !slices.Equal(got, want) {
		t.Errorf("Briefings written on %v, want %v", got, want)
	}
	if got := strs(t, db, `SELECT day FROM sunday_reads`); !slices.Equal(got, []string{"2026-10-18"}) {
		t.Errorf("Sunday reads written on %v, want the day kept", got)
	}
	if got, want := strs(t, db, `SELECT kind FROM events WHERE kind != 'switch' ORDER BY id`), []string{"cut", "briefing"}; !slices.Equal(got, want) {
		t.Errorf("events = %v, want those kept before, %v", got, want)
	}
	if got := strs(t, db, `SELECT n || ':' || span_start || '-' || span_end FROM cut_spans`); !slices.Equal(got, []string{"1:3-18"}) {
		t.Errorf("the Cut's Spans = %v", got)
	}
	if got, want := strs(t, db, `SELECT output || ' ' || day || ' ' || opened_on FROM opens ORDER BY output`),
		[]string{"briefing 2026-10-17 2026-10-17", "sunday read 2026-10-18 2026-10-18"}; !slices.Equal(got, want) {
		t.Errorf("opens = %v, want them carried over, %v", got, want)
	}
	if got := strs(t, db, `SELECT hash FROM commits`); !slices.Equal(got, []string{"abc"}) {
		t.Errorf("commits = %v, want them carried over", got)
	}
	if got := strs(t, db, `SELECT act FROM work_attempts`); !slices.Equal(got, []string{"relating"}) {
		t.Errorf("work kept for acts = %v, want only an act the Editor still runs", got)
	}
	if got, want := strs(t, db, `SELECT name || '|' || terms FROM topics`), []string{"Derby|Derby"}; !slices.Equal(got, want) {
		t.Errorf("Topics = %v, want %v", got, want)
	}
	if got := strs(t, db, `SELECT name FROM pragma_table_info('topics') WHERE name IN ('depth', 'sources')`); len(got) != 0 {
		t.Errorf("Topics still keep %v", got)
	}
	if broken, err := brokenForeignKeys(ctx, db); err != nil || broken {
		t.Errorf("a foreign key is broken: %v %v", broken, err)
	}
}

func TestTheSwitchIsKeptAfterTheNewestItemTheOldRecordHeld(t *testing.T) {
	ctx := t.Context()
	db := oldRecord(t)

	if err := migrateTo(ctx, db, -1); err != nil {
		t.Fatal(err)
	}

	s := &Store{db: db}
	sw, found, err := s.Switch(ctx)
	if err != nil || !found {
		t.Fatalf("no switch kept: %v %v", found, err)
	}
	if want := time.Date(2026, time.October, 20, 9, 0, 0, 0, time.UTC); !sw.ItemsThrough.Equal(want) {
		t.Errorf("the switch follows the Items collected through %v, want %v", sw.ItemsThrough, want)
	}
	if time.Since(sw.At) > time.Minute || sw.At.After(time.Now()) {
		t.Errorf("the switch is kept as at %v, want when the Record changed, now", sw.At)
	}
}

func TestARecordThatHeldNoItemsHasNoSwitch(t *testing.T) {
	s := openTestStore(t)

	if _, found, err := s.Switch(t.Context()); err != nil || found {
		t.Errorf("a new Record has a switch (%v)", err)
	}
}

// Items collected before the switch are read only by re-judging: the first
// start after it would otherwise submit the whole Record for reading.
func TestOnlyItemsCollectedAfterTheSwitchAwaitReading(t *testing.T) {
	ctx := t.Context()
	db := oldRecord(t)
	if err := migrateTo(ctx, db, -1); err != nil {
		t.Fatal(err)
	}
	s := &Store{db: db}
	if _, err := s.AddItems(ctx, []collect.Item{{SourceID: "s", EntryID: "after", Title: "After",
		Content: "<p>Derby gets more jobs.</p>", CollectedAt: time.Date(2026, time.October, 31, 9, 0, 0, 0, time.UTC)}}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(s).CutNew(ctx, cutAt); err != nil {
		t.Fatal(err)
	}

	waiting, err := s.ItemsToRead(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tr := range waiting {
		got = append(got, tr.Item.EntryID)
	}
	if !slices.Equal(got, []string{"after"}) {
		t.Errorf("awaiting reading: %v, want only the Item collected after the switch", got)
	}
}

// Re-judging reads the Items collected before the switch; relating then
// relates them through its usual batch, whenever they were collected.
func TestAnItemCollectedBeforeTheSwitchAwaitsRelatingOnceReJudgingHasReadIt(t *testing.T) {
	ctx := t.Context()
	db := oldRecord(t)
	if err := migrateTo(ctx, db, -1); err != nil {
		t.Fatal(err)
	}
	s := &Store{db: db}
	if err := cut.New(s).CutNew(ctx, cutAt); err != nil {
		t.Fatal(err)
	}
	c, _, _, err := s.NewestCut(ctx, "s", "old")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.KeepReading(ctx, editor.Reading{SourceID: "s", EntryID: "old", CutID: c.ID, At: cutAt,
		Judge: editor.Judge{Act: "reading", Version: 1, Model: "claude-test", RequestHash: "h"}}); err != nil {
		t.Fatal(err)
	}

	waiting, err := s.ItemsToRelate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(waiting) != 1 || waiting[0].Item.EntryID != "old" {
		t.Errorf("awaiting relating: %+v, want the Item collected before the switch and read since", waiting)
	}
}
