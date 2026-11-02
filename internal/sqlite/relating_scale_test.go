package sqlite

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
)

// scaleItems is how many Items the Record holds in the scale test: the
// Record a year or two of use makes.
const scaleItems = 10_000

// scaleBudget is how long relating's waiting check may take over it. The
// race detector slows SQLite's translated C some twentyfold, so under it
// the budget is scaled to match; the check is of the query's shape, which
// grows with the Record if it reads every name once per Entity.
func scaleBudget() time.Duration {
	if raceDetector {
		return 20 * time.Second
	}
	return time.Second
}

// scaleRecord is a Record of scaleItems Items, each cut into one Span, read,
// naming a place of its own, its look-up kept, and related, but the last,
// which awaits relating. Every hundredth Item, the last among them, also
// names Shared, so the last Item's one Span has 99 candidates. It is written
// in bulk, a statement per table, since writing it an event at a time
// takes minutes; and it is written once per test binary, each test given a
// copy.
func scaleRecord(t *testing.T) *Store {
	t.Helper()
	scaleOnce.Do(func() {
		path := migratedPath(t)
		s, err := Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		writeScaleRecord(t, s)
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(path) //nolint:gosec // the test's own temporary file
		if err != nil {
			t.Fatal(err)
		}
		scaleBytes = b
	})
	if scaleBytes == nil {
		t.Fatal("writing the scale Record failed in another test")
	}
	path := filepath.Join(t.TempDir(), "scale.db")
	if err := os.WriteFile(path, scaleBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

var (
	scaleOnce  sync.Once
	scaleBytes []byte
)

// writeScaleRecord writes scaleRecord's Items and their events into s.
func writeScaleRecord(t *testing.T, s *Store) {
	t.Helper()
	src := collect.Source{ID: "number-10", Name: "No 10", FeedURL: "https://example.org/feed", UsualGap: time.Hour}
	if err := s.SyncSources(t.Context(), []collect.Source{src}, cutAt); err != nil {
		t.Fatal(err)
	}
	// Item i's events are numbered from i*10; the one look-up of Shared is 5.
	const seq = `WITH RECURSIVE seq(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM seq WHERE i < ?) `
	shares := `i % 100 = 0`
	tx, err := s.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		seq + `INSERT INTO items (source_id, entry_id, title, link, summary, content, published, updated, raw, attribution, collected_at)
			SELECT 'number-10', 'e' || i, 'Item ' || i, '', '', printf('<p>Place %05d and Shared.</p>', i),
				strftime('%Y-%m-%dT%H:%M:%SZ', '2025-01-01', '+' || i || ' minutes'), '', '', '', '2026-10-30T07:00:00Z'
			FROM seq`,
		seq + `INSERT INTO events (id, kind, shape, at)
			SELECT i * 10 + k.n, k.kind, 1, '2026-10-30T07:00:00Z'
			FROM seq, (SELECT 1 AS n, 'cut' AS kind UNION ALL SELECT 2, 'reading' UNION ALL SELECT 3, 'name'
				UNION ALL SELECT 4, 'name' UNION ALL SELECT 5, 'lookup' UNION ALL SELECT 7, 'relating') k
			WHERE (k.n != 4 OR ` + shares + `) AND (k.n != 7 OR i < ?1)`,
		`INSERT INTO events (id, kind, shape, at) VALUES (5, 'lookup', 1, '2026-10-30T07:00:00Z')`,
		seq + `INSERT INTO cuts (event_id, source_id, entry_id, cutter, cutter_version)
			SELECT i * 10 + 1, 'number-10', 'e' || i, 'sentences', 1 FROM seq`,
		seq + `INSERT INTO cut_spans (cut_id, n, span_start, span_end) SELECT i * 10 + 1, 1, 3, 26 FROM seq`,
		seq + `INSERT INTO readings (event_id, source_id, entry_id, cut_id, act, version, model, request_hash)
			SELECT i * 10 + 2, 'number-10', 'e' || i, i * 10 + 1, 'reading', 3, 'm', 'h' || i FROM seq`,
		seq + `INSERT INTO span_names (event_id, reading_id, span_start, span_end, name, kind, office)
			SELECT i * 10 + 3, i * 10 + 2, 3, 14, printf('Place %05d', i), 'place', '' FROM seq`,
		seq + `INSERT INTO span_names (event_id, reading_id, span_start, span_end, name, kind, office)
			SELECT i * 10 + 4, i * 10 + 2, 19, 25, 'Shared', 'place', '' FROM seq WHERE ` + shares,
		seq + `INSERT INTO lookups (event_id, name, kind, office, on_date, looker, looker_version, wikidata_id, label, revision)
			SELECT i * 10 + 5, printf('Place %05d', i), 'place', '', '', 'wikidata', 1, 'Q' || i, printf('Place %05d', i), 1 FROM seq`,
		`INSERT INTO lookups (event_id, name, kind, office, on_date, looker, looker_version, wikidata_id, label, revision)
			VALUES (5, 'Shared', 'place', '', '', 'wikidata', 1, 'Q0', 'Shared', 1)`,
		seq + `INSERT INTO relatings (event_id, source_id, entry_id, reading_id, act, version, model, request_hash)
			SELECT i * 10 + 7, 'number-10', 'e' || i, i * 10 + 2, 'relating', 1, '', '' FROM seq WHERE i < ?1`,
	} {
		var args []any
		if strings.HasPrefix(stmt, seq) {
			args = []any{scaleItems}
		}
		if _, err := tx.ExecContext(t.Context(), stmt, args...); err != nil {
			t.Fatalf("%v\n%s", err, stmt)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestEveryKeyAskedAboutIsNamedHoweverManyAreAsked(t *testing.T) {
	s := scaleRecord(t)
	// More keys than one query asks about.
	const asked = 2500
	var keys []editor.LookupKey
	for i := 1; i <= asked; i++ {
		keys = append(keys, editor.LookupKey{Name: fmt.Sprintf("Place %05d", i), Kind: "place"})
	}

	is, err := s.EntitiesNamed(t.Context(), keys)

	if err != nil {
		t.Fatal(err)
	}
	if len(is) != asked {
		t.Fatalf("%d of %d keys named", len(is), asked)
	}
	for i, k := range keys {
		if want := fmt.Sprintf("Q%d", i+1); is[k].WikidataID != want {
			t.Fatalf("%v names %+v, want %s", k, is[k], want)
		}
	}
}

func TestRelatingsWaitingCheckOverTenThousandItemsTakesUnderASecond(t *testing.T) {
	s := scaleRecord(t)
	relating := editor.New(editor.Deps{Store: s}).Relating()

	start := time.Now()
	jobs, err := relating.Waiting(t.Context())
	took := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Key != "number-10\ne10000" {
		t.Fatalf("awaiting relating: %+v", jobs)
	}
	if in := jobs[0].Request.Input; !strings.Contains(in, "C10") || strings.Contains(in, "C11") {
		t.Errorf("the last Item's Span is not weighed against its 10 best candidates:\n%s", in)
	}
	t.Logf("relating's waiting check took %v over %d Items", took, scaleItems)
	if took > scaleBudget() {
		t.Errorf("relating's waiting check took %v over %d Items, over its %v", took, scaleItems, scaleBudget())
	}
}
