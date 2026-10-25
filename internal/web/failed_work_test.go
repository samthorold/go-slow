package web_test

import (
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

func TestTheItemsPageSaysWhenNoWorkHasFailed(t *testing.T) {
	store := openStore(t)
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/items")

	if !strings.Contains(res.body, "No work has failed.") {
		t.Errorf("the Items page does not say no work has failed:\n%s", res.body)
	}
}
