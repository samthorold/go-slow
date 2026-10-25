package sqlite

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "go-slow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

var instructions = "Read the Item. " + string(make([]byte, 4000))

func call(input string) editor.Call {
	req := editor.Request{Act: "reading", Instructions: instructions, Input: input,
		Schema: map[string]any{"type": "object", "required": []string{"facts"}}}
	return editor.Call{
		Act: "reading", Key: "s\n" + input, At: time.Date(2026, 10, 24, 7, 0, 0, 0, time.UTC),
		Model: "m", Request: req, RequestHash: editor.RequestHash(req, "m"),
		Outcome: editor.Failed, Why: "no result for the request",
	}
}

func TestInstructionsAndSchemasSentManyTimesAreKeptOnce(t *testing.T) {
	s := openTestStore(t)
	for i := range 3 {
		if err := s.KeepCall(t.Context(), call("Title: "+strconv.Itoa(i))); err != nil {
			t.Fatal(err)
		}
	}
	// A retry sends the same input again.
	if err := s.KeepCall(t.Context(), call("Title: 0")); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := s.db.QueryRowContext(t.Context(), `SELECT count(*) FROM call_texts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Errorf("%d texts kept, want 5: one instructions, one schema, three inputs", n)
	}
	got, err := s.CallsOn(t.Context(), "reading", "s\nTitle: 0")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Request.Instructions != instructions || got[1].Request.Input != "Title: 0" {
		t.Errorf("calls on the work: %d, first with its instructions? %v", len(got), len(got) > 0 && got[0].Request.Instructions == instructions)
	}
}

func TestAKeptCallIsNeverRewrittenOrRemoved(t *testing.T) {
	s := openTestStore(t)
	if err := s.KeepCall(t.Context(), call("Title: x")); err != nil {
		t.Fatal(err)
	}

	if _, err := s.db.ExecContext(t.Context(), `UPDATE model_calls SET outcome = 'applied'`); err == nil {
		t.Error("a kept call was rewritten")
	}
	if _, err := s.db.ExecContext(t.Context(), `DELETE FROM model_calls`); err == nil {
		t.Error("a kept call was removed")
	}
}

// A request pending in a batch from before requests were kept comes back
// with no request, so its call keeps the work's request when it ends.
func TestARequestPendingFromBeforeCallsWereKeptHasNoRequest(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "go-slow.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := migrateTo(ctx, db, 16); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO batches (id, submitted_at) VALUES ('b1', '2026-10-24T07:00:00Z')`,
		`INSERT INTO batch_requests (batch_id, request_id, act, work_key) VALUES ('b1', 'r1', 'reading', 'k')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	pending, err := (&Store{db: db}).PendingBatches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || len(pending[0].Requests) != 1 || pending[0].Requests[0].Request != nil {
		t.Errorf("pending %+v, want one request with none kept", pending)
	}
}
