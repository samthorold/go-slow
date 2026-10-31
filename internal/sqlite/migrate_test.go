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

	if err := migrateTo(ctx, db, 20); err != nil {
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

// What earlier readings tied to each Fact and Claim is kept as what their
// Item mentions, each Entity once, and the ties stay as they were.
func TestEarlierReadingsListsAreKeptAsTheirItemsMentions(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "go-slow.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := migrateTo(ctx, db, 20); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sources (id, name, feed_url, usual_gap_seconds, added_at) VALUES ('s', 'S', 'https://example.org', 86400, '2026-10-16T00:00:00Z')`,
		`INSERT INTO items (source_id, entry_id, title, link, summary, content, published, updated, raw, attribution, collected_at)
			VALUES ('s', 'e', 't', 'l', '', '', '', '', '', '', '2026-10-16T00:00:00Z'),
			('s', 'other', 't', 'l', '', '', '', '', '', '', '2026-10-16T00:00:00Z')`,
		`INSERT INTO entities (name, kind) VALUES ('Andy Burnham', 'person'), ('Motherwell', 'place'), ('Scotland', 'place'), ('Glasgow', 'place')`,
		`INSERT INTO facts (source_id, entry_id, statement, as_at, quote) VALUES
			('s', 'e', 'Andy Burnham spoke in Motherwell.', '2026-10-16', 'q'),
			('s', 'e', 'Andy Burnham spoke again.', '2026-10-16', 'q'),
			('s', 'other', 'Glasgow got funding.', '2026-10-16', 'q')`,
		`INSERT INTO fact_entities (fact_id, entity_id, role) VALUES (1, 1, 'actor'), (1, 2, NULL), (2, 1, NULL), (3, 4, NULL)`,
		`INSERT INTO claims (source_id, entry_id, statement, made_by, quote) VALUES ('s', 'e', 'Scotland will get money.', 'Andy Burnham', 'q')`,
		`INSERT INTO claim_entities (claim_id, entity_id) VALUES (1, 1), (1, 3)`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}

	if err := migrateTo(ctx, db, 21); err != nil {
		t.Fatal(err)
	}

	rows, err := db.QueryContext(ctx, `SELECT entry_id || ':' || entity_id FROM item_mentions ORDER BY entry_id, entity_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			t.Fatal(err)
		}
		got = append(got, m)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"e:1", "e:2", "e:3", "other:4"}; !slices.Equal(got, want) {
		t.Errorf("mentions kept = %v, want %v", got, want)
	}
	var ties int
	if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM fact_entities) + (SELECT count(*) FROM claim_entities)`).Scan(&ties); err != nil || ties != 6 {
		t.Errorf("ties kept = %d, want the 6 there were: %v", ties, err)
	}
	if broken, err := brokenForeignKeys(ctx, db); err != nil || broken {
		t.Errorf("a mention names an Item or Entity that is gone: %v %v", broken, err)
	}
}

// A Fact is tied to an Entity only by a Role connecting gives it: the ties
// earlier readings made with no Role go, the roles connecting gave stay, and
// what each Item mentions is unchanged.
func TestTheTiesReadingMadeWithNoRoleAreRemovedAndTheMentionsKept(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "go-slow.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := migrateTo(ctx, db, 21); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sources (id, name, feed_url, usual_gap_seconds, added_at) VALUES ('s', 'S', 'https://example.org', 86400, '2026-10-16T00:00:00Z')`,
		`INSERT INTO items (source_id, entry_id, title, link, summary, content, published, updated, raw, attribution, collected_at)
			VALUES ('s', 'e', 't', 'l', '', '', '', '', '', '', '2026-10-16T00:00:00Z')`,
		`INSERT INTO entities (name, kind) VALUES ('Andy Burnham', 'person'), ('Motherwell', 'place'), ('Scotland', 'place')`,
		`INSERT INTO item_mentions (source_id, entry_id, entity_id) VALUES ('s', 'e', 1), ('s', 'e', 2), ('s', 'e', 3)`,
		`INSERT INTO facts (source_id, entry_id, statement, as_at, quote) VALUES
			('s', 'e', 'Andy Burnham spoke in Motherwell.', '2026-10-16', 'q'),
			('s', 'e', 'Andy Burnham spoke again.', '2026-10-16', 'q')`,
		`INSERT INTO fact_entities (fact_id, entity_id, role) VALUES
			(1, 1, 'actor'), (1, 2, 'place'), (1, 3, NULL), (2, 1, NULL), (2, 2, NULL), (2, 3, NULL)`,
		`INSERT INTO claims (source_id, entry_id, statement, made_by, quote) VALUES ('s', 'e', 'Scotland will get money.', 'Andy Burnham', 'q')`,
		`INSERT INTO claim_entities (claim_id, entity_id) VALUES (1, 1), (1, 3)`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}

	if err := migrateTo(ctx, db, 22); err != nil {
		t.Fatal(err)
	}

	strs := func(q string) []string {
		t.Helper()
		rows, err := db.QueryContext(ctx, q)
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
	if got, want := strs(`SELECT fact_id || ':' || entity_id || ':' || role FROM fact_entities ORDER BY fact_id, entity_id`),
		[]string{"1:1:actor", "1:2:place"}; !slices.Equal(got, want) {
		t.Errorf("ties kept = %v, want only the roles %v", got, want)
	}
	if got, want := strs(`SELECT entry_id || ':' || entity_id FROM item_mentions ORDER BY entry_id, entity_id`),
		[]string{"e:1", "e:2", "e:3"}; !slices.Equal(got, want) {
		t.Errorf("mentions = %v, want them unchanged, %v", got, want)
	}
	if got := strs(`SELECT name FROM sqlite_master WHERE name = 'claim_entities'`); len(got) != 0 {
		t.Errorf("Claims are still tied to Entities: %v", got)
	}
	if broken, err := brokenForeignKeys(ctx, db); err != nil || broken {
		t.Errorf("a role names a Fact or Entity that is gone: %v %v", broken, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO fact_entities (fact_id, entity_id, role) VALUES (2, 1, NULL)`); err == nil {
		t.Error("the Record still keeps a tie with no role")
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

	if err := migrateTo(ctx, db, 22); err != nil {
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
