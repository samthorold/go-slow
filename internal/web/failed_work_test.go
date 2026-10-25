package web_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

func TestTheItemsPageShowsFailedWorkWithWhenAndWhyItLastFailed(t *testing.T) {
	store := openStore(t)
	seed(t, store, saturday.AddDate(0, 0, -7), item("speech", "PM speech in Scotland", saturday.Add(-2*time.Hour)))
	if err := store.EndBatch(t.Context(), "batch-1", []editor.WorkFailure{{
		Act: "placing", Key: number10.ID + "\nspeech", At: saturday.Add(-time.Hour),
		Why: "the result fails the act's checks: F1 is unplaced", Failed: true,
	}}, nil); err != nil {
		t.Fatal(err)
	}
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/items")

	i := strings.Index(res.body, `id="failed"`)
	if i < 0 {
		t.Fatalf("the Items page has no failed work:\n%s", res.body)
	}
	section := res.body[i:]
	section = section[:strings.Index(section, "</section>")]
	for _, want := range []string{
		"placing", `href="/items/number-10/speech"`, "PM speech in Scotland",
		"Sat 24 Oct 2026, 06:30", "F1 is unplaced", "retry placing",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("failed work lacks %q:\n%s", want, section)
		}
	}
}

// refusingBatcher ends every batch at once, answering each request with
// output.
type refusingBatcher struct {
	output    string
	submitted map[string][]editor.BatchRequest
}

func (b *refusingBatcher) Submit(_ context.Context, rs []editor.BatchRequest) (string, error) {
	if b.submitted == nil {
		b.submitted = map[string][]editor.BatchRequest{}
	}
	id := "batch-" + strconv.Itoa(len(b.submitted)+1)
	b.submitted[id] = rs
	return id, nil
}

func (b *refusingBatcher) Poll(_ context.Context, id string) (editor.BatchPoll, error) {
	var out []editor.BatchResult
	for _, r := range b.submitted[id] {
		out = append(out, editor.BatchResult{ID: r.ID, Response: editor.Response{Output: json.RawMessage(b.output), Model: "fake"}})
	}
	return editor.BatchPoll{Ended: true, Results: out}, nil
}

func TestFailedPlacingAndConnectingWorkLinksToEveryAttemptAtIt(t *testing.T) {
	for _, act := range []string{"placing", "connecting"} {
		t.Run(act, func(t *testing.T) {
			store := openStore(t)
			readSpeech(t, store, reading)
			log := slog.New(slog.DiscardHandler)
			ed := editor.New(editor.Deps{Store: store, Log: log})
			batchAct := ed.Placing()
			if act == "connecting" {
				placeWaiting(t, store, `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": []}]}`)
				batchAct = ed.Connecting()
			}
			b := editor.NewBatches(editor.BatchDeps{Store: store, Batcher: &refusingBatcher{output: `{}`},
				Acts: []editor.BatchAct{batchAct}, Log: log})
			// Submitted, then refused on each of its attempts.
			for range editor.MaxAttempts + 1 {
				if err := b.ReadWaiting(t.Context(), saturday); err != nil {
					t.Fatal(err)
				}
			}
			app, _ := newAppWithLog(t, saturday, store)

			res := get(t, app, callLink(t, get(t, app, "/items").body, `id="failed"`))

			if res.status != 200 || !strings.Contains(res.body, "A "+act+" call") {
				t.Fatalf("status %d, or not a %s call:\n%s", res.status, act, res.body)
			}
			attempts := res.body[strings.Index(res.body, `id="attempts"`):]
			if n := strings.Count(attempts, "refused"); n != editor.MaxAttempts {
				t.Errorf("%d refused attempts listed, want %d:\n%s", n, editor.MaxAttempts, attempts)
			}
		})
	}
}

func TestTheItemsPageSaysWhenNoWorkHasFailed(t *testing.T) {
	store := openStore(t)
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/items")

	if !strings.Contains(res.body, "No work has failed.") {
		t.Errorf("the Items page does not say no work has failed:\n%s", res.body)
	}
}
