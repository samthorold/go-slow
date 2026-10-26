package sqlite

import (
	"database/sql"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The role affected is renamed undergoer: the roles kept are renamed with
// it, every Entity a Fact involves stays involved, and affected is a role no
// more.
func TestTheRolesKeptAsAffectedAreRenamedUndergoer(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "go-slow.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := migrateTo(ctx, db, 19); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sources (id, name, feed_url, usual_gap_seconds, added_at) VALUES ('s', 'S', 'https://example.org', 86400, '2026-10-16T00:00:00Z')`,
		`INSERT INTO items (source_id, entry_id, title, link, summary, content, published, updated, raw, attribution, collected_at)
			VALUES ('s', 'e', 't', 'l', '', '', '', '', '', '', '2026-10-16T00:00:00Z')`,
		`INSERT INTO entities (name, kind) VALUES ('Andy Burnham', 'person'), ('Motherwell', 'place'), ('Scotland', 'place')`,
		`INSERT INTO facts (source_id, entry_id, statement, as_at, quote) VALUES ('s', 'e', 'Andy Burnham funded Scotland in Motherwell.', '2026-10-16', 'q')`,
		`INSERT INTO fact_entities (fact_id, entity_id, role) VALUES (1, 1, 'actor'), (1, 2, NULL), (1, 3, 'affected')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}

	if err := migrateTo(ctx, db, -1); err != nil {
		t.Fatal(err)
	}

	rows, err := db.QueryContext(ctx, `SELECT entity_id, coalesce(role, '') FROM fact_entities WHERE fact_id = 1 ORDER BY entity_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var id int
		var role string
		if err := rows.Scan(&id, &role); err != nil {
			t.Fatal(err)
		}
		got = append(got, strconv.Itoa(id)+":"+role)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"1:actor", "2:", "3:undergoer"}; !slices.Equal(got, want) {
		t.Errorf("roles kept = %v, want %v", got, want)
	}
	if broken, err := brokenForeignKeys(ctx, db); err != nil || broken {
		t.Errorf("a role names a Fact or Entity that is gone: %v %v", broken, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fact_entities SET role = 'affected' WHERE entity_id = 2`); err == nil {
		t.Error("affected is still a role the Record keeps")
	}
}

// Rebuilding the feedback table, to give Feedback levels for the Sunday read,
// keeps the Feedback already given and every mark on the Record that names
// it.
func TestTheFeedbackKeptBeforeTheSundayReadSurvivesItsMigration(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "go-slow.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := migrateTo(ctx, db, 11); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO feedback (kind, level, title, depth, given_at) VALUES ('missed', 'story', 'Travels', 2, '2026-10-24T08:00:00Z')`,
		`INSERT INTO stories (title, started_at, missed_by) VALUES ('Travels', '2026-10-24T08:00:00Z', 1)`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}

	if err := migrateTo(ctx, db, -1); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM feedback WHERE id = 1 AND title = 'Travels' AND depth = 2`).Scan(&n); err != nil || n != 1 {
		t.Errorf("the Feedback given is not kept: %d, %v", n, err)
	}
	if broken, err := brokenForeignKeys(ctx, db); err != nil || broken {
		t.Errorf("a mark on the Record names Feedback that is gone: %v %v", broken, err)
	}
	var schema string
	if err := db.QueryRowContext(ctx, `SELECT group_concat(sql, ';') FROM sqlite_master`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(schema, "feedback_0011") {
		t.Errorf("the schema still names the old table:\n%s", schema)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO feedback (kind, level, assessment_id, depth, given_at)
		VALUES ('wrong', 'assessment', 1, 0, '2026-10-25T08:00:00Z')`); err != nil {
		t.Errorf("Feedback on an Assessment can't be kept: %v", err)
	}
}
