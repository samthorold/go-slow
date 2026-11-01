package web_test

import (
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// budget is cut one Span per paragraph: S1 "Read today's Budget in full.",
// S2 "This Budget means the West wins.", S3 "The Prime Minister spoke.",
// S4 "It was set out by him today.", S5 "It is in his 10-year plan.", S6
// "The Northern Ireland Executive met.", S7 "It will set up its own
// version.", S8 "Scottish Ministers agreed.", S9 "It is their largest
// spending.", S10 "Port Talbot was named." and S11 "It is part of the
// town's regeneration."
func budget() string {
	return "<p>Read today's Budget in full.</p>" +
		"<p>This Budget means the West wins.</p>" +
		"<p>The Prime Minister spoke.</p>" +
		"<p>It was set out by him today.</p>" +
		"<p>It is in his 10-year plan.</p>" +
		"<p>The Northern Ireland Executive met.</p>" +
		"<p>It will set up its own version.</p>" +
		"<p>Scottish Ministers agreed.</p>" +
		"<p>It is their largest spending.</p>" +
		"<p>Port Talbot was named.</p>" +
		"<p>It is part of the town's regeneration.</p>"
}

const budgetReading = `{
  "names": [],
  "refers_to": [
    {"from": {"span": 2, "words": "This Budget"}, "to": {"span": 1, "words": "today's Budget"}},
    {"from": {"span": 4, "words": "him"}, "to": {"span": 3, "words": "The Prime Minister"}},
    {"from": {"span": 5, "words": "his"}, "to": {"span": 3, "words": "The Prime Minister"}},
    {"from": {"span": 7, "words": "its"}, "to": {"span": 6, "words": "Northern Ireland Executive"}},
    {"from": {"span": 9, "words": "their"}, "to": {"span": 8, "words": "Scottish Ministers"}},
    {"from": {"span": 11, "words": "the town's"}, "to": {"span": 10, "words": "Port Talbot"}}
  ],
  "rejected": [],
  "speakers": []
}`

// readBudget seeds the budget Item, cuts it and reads it.
func readBudget(t *testing.T, store *sqlite.Store) {
	t.Helper()
	it := item("budget", "The Budget", saturday.Add(-24*time.Hour))
	it.Content = budget()
	seed(t, store, saturday.AddDate(0, 0, -7), it)
	if err := cut.New(store).CutNew(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Deps{Store: store, Model: fakeModel{budgetReading}, Log: slog.New(slog.DiscardHandler)})
	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
}

// bodyOf is the block body of Span n on the page.
func bodyOf(t *testing.T, page string, n int) string {
	t.Helper()
	at := strings.Index(page, `id="span-`+strconv.Itoa(n)+`"`)
	if at < 0 {
		t.Fatalf("the page has no S%d:\n%s", n, page)
	}
	rest := page[at:]
	return rest[strings.Index(rest, `class="block-body`):strings.Index(rest, `class="block-margin`)]
}

func TestHydratedWordsAreCapitalisedAtASentencesStart(t *testing.T) {
	store := openStore(t)
	readBudget(t, store)
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, "/items/"+number10.ID+"/budget").body

	if b := bodyOf(t, page, 2); !strings.Contains(b, `<ins title="This Budget">Today&#39;s Budget</ins> means the West wins.`) {
		t.Errorf("S2 does not read its insert capitalised at its start:\n%s", b)
	}
}

func TestALeadingTheOfHydratedWordsIsLoweredMidSentence(t *testing.T) {
	store := openStore(t)
	readBudget(t, store)
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, "/items/"+number10.ID+"/budget").body

	if b := bodyOf(t, page, 4); !strings.Contains(b, `It was set out by <ins title="him">the Prime Minister</ins> today.`) {
		t.Errorf("S4 does not read its insert with \"the\" mid-sentence:\n%s", b)
	}
}

func TestASpansPageReadsItsHydratedWordsInPlace(t *testing.T) {
	store := openStore(t)
	readBudget(t, store)
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, spanPath(t, app, "budget", 5)).body

	if !strings.Contains(page, `It is in <ins title="his">the Prime Minister’s</ins> 10-year plan.`) {
		t.Errorf("the Span's page does not read its insert in place:\n%s", page)
	}
}

func TestHydratedWordsForAPossessiveArePossessive(t *testing.T) {
	store := openStore(t)
	readBudget(t, store)
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, "/items/"+number10.ID+"/budget").body

	for n, want := range map[int]string{
		5:  `It is in <ins title="his">the Prime Minister’s</ins> 10-year plan.`,
		7:  `It will set up <ins title="its">Northern Ireland Executive’s</ins> own version.`,
		9:  `It is <ins title="their">Scottish Ministers’</ins> largest spending.`,
		11: `It is part of <ins title="the town&#39;s">Port Talbot’s</ins> regeneration.`,
	} {
		if b := bodyOf(t, page, n); !strings.Contains(b, want) {
			t.Errorf("S%d does not read its insert possessive, want %s:\n%s", n, want, b)
		}
	}
}
