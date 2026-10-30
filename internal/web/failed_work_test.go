package web_test

import (
	"context"
	"encoding/json"
	"html"
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

	section := leadingExceptions(t, res.body)
	if !strings.Contains(section, `id="failed"`) {
		t.Fatalf("the exceptions lack failed work:\n%s", section)
	}
	for _, want := range []string{
		"placing", `href="/items/number-10/speech"`, "PM speech in Scotland",
		"Sat 24 Oct 2026, 06:30", "F1 is unplaced",
		`<span class="status status-refuted">failed</span>`,
		"<code>docker compose run --rm app retry placing</code>",
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

func TestFailedRelatingWorkLinksToEveryAttemptAtIt(t *testing.T) {
	for _, act := range []string{"relating"} {
		t.Run(act, func(t *testing.T) {
			// The release, and a later Item naming Derby, read and awaiting
			// relating.
			store := readToRelate(t)
			log := slog.New(slog.DiscardHandler)
			ed := editor.New(editor.Deps{Store: store, Log: log})
			batchAct := ed.Relating()
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

func TestWithNoExceptionsTheItemsPageSaysSoQuietlyAndFoldsTheSources(t *testing.T) {
	store := openStore(t)
	seed(t, store, saturday.AddDate(0, -1, 0),
		item("recent", "PM statement on the Budget", saturday.Add(-time.Hour)))
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/items")

	if strings.Contains(res.body, `id="exceptions"`) {
		t.Errorf("exceptions are shown with none to show:\n%s", res.body)
	}
	sources := element(t, res.body, `id="sources"`, "</details>")
	if !strings.Contains(sources, "<details") || strings.Contains(sources, "<details open") {
		t.Errorf("the Sources list is not collapsed:\n%s", sources)
	}
	if !strings.Contains(sources, `<summary class="quiet">All Sources current · no failed work</summary>`) {
		t.Errorf("the folded Sources do not say quietly that all is well:\n%s", sources)
	}
	if !strings.Contains(sources, html.EscapeString(number10.Name)) {
		t.Errorf("the Sources list lacks the Source:\n%s", sources)
	}
}

// element is body from the start of the tag holding marker to the first end
// after it.
func element(t *testing.T, body, marker, end string) string {
	t.Helper()
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("no %q in:\n%s", marker, body)
	}
	i = strings.LastIndex(body[:i], "<")
	j := strings.Index(body[i:], end)
	if j < 0 {
		t.Fatalf("no %q after %q:\n%s", end, marker, body[i:])
	}
	return body[i : i+j+len(end)]
}
