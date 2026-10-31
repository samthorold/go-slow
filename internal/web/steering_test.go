package web_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/interest"
)

// sourceLine is the Items page's line for the Source named name, among the
// Sources.
func sourceLine(t *testing.T, page, name string) string {
	t.Helper()
	i := strings.Index(page, `id="sources"`)
	if i < 0 {
		t.Fatalf("the page has no Sources:\n%s", page)
	}
	m := regexp.MustCompile(`(?s)<li>` + regexp.QuoteMeta(name) + `.*?</li>`).FindString(page[i:])
	if m == "" {
		t.Fatalf("the Sources do not list %q:\n%s", name, page)
	}
	return m
}

func TestTheItemsPageShowsPerSourceTheStoriesTouchingATopicAndTheSpansRejectedOverItsWindow(t *testing.T) {
	store := relatedRecord(t)
	tell(t, store, saturday, teller{text: "Derby will host the headquarters.", tell: "This is fantastic news",
		story: []string{"Derby will host the headquarters."}})
	app, _ := newAppWithLog(t, saturday, store)
	name := "Prime Minister&#39;s Office, 10 Downing Street"

	before := get(t, app, "/items").body
	if _, errs, err := interest.New(store).Add(t.Context(), interest.Draft{Name: "Derby", Terms: "Derby"}); err != nil || errs != nil {
		t.Fatalf("take up a Topic: %v %v", errs, err)
	}
	after := get(t, app, "/items").body

	if line := sourceLine(t, before, name); !strings.Contains(line, "Stories: 0 touching a Topic, 1 not") {
		t.Errorf("with no Topic, the Story gathered is not counted as touching none:\n%s", line)
	}
	line := sourceLine(t, after, name)
	if !strings.Contains(line, "Stories: 1 touching a Topic, 0 not") {
		t.Errorf("taking up a Topic the Story names does not count it as touching:\n%s", line)
	}
	if !regexp.MustCompile(`Spans: \d+ of \d+ rejected`).MatchString(line) {
		t.Errorf("the Source's line lacks its Spans rejected:\n%s", line)
	}
	if !strings.Contains(after, "over the last 28 days") {
		t.Errorf("the page does not state the counts' window:\n%s", after)
	}
}
