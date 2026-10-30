package web_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
)

func TestAnItemsPageShowsItsTextAsItsNumberedSpans(t *testing.T) {
	store := openStore(t)
	speech := item("speech", "PM speech in Scotland", saturday.Add(-48*time.Hour))
	speech.Content = "<p>It’s good to be here in Motherwell. Scotland is the true north.</p>"
	seed(t, store, saturday.AddDate(0, 0, -7), speech)
	if err := cut.New(store).CutNew(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	app, _ := newAppWithLog(t, saturday, store)

	res := itemPage(t, app, "PM speech in Scotland")

	if res.status != 200 {
		t.Fatalf("status = %d", res.status)
	}
	spans := regexp.MustCompile(`(?s)<li id="span-(\d+)"[^>]*>(.*?)</li>`).FindAllStringSubmatch(res.body, -1)
	if len(spans) != 2 {
		t.Fatalf("page shows %d Spans, want 2:\n%s", len(spans), res.body)
	}
	for i, want := range []string{"It’s good to be here in Motherwell.", "Scotland is the true north."} {
		if spans[i][1] != string(rune('1'+i)) || !strings.Contains(spans[i][2], want) {
			t.Errorf("Span %d is %q (%q), want %d: %q", i+1, spans[i][1], spans[i][2], i+1, want)
		}
	}
	if !strings.Contains(res.body, "cut · sentences v1") {
		t.Errorf("page does not say which cutter cut it:\n%s", res.body)
	}
}
