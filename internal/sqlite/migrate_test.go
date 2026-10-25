package sqlite

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

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
	rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Error("a mark on the Record names Feedback that is gone")
	}
	_ = rows.Close()
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
