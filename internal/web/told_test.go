package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// teller writes the Briefing telling, in one statement saying text, the
// Span shown with the words tell, its Story gathered of the Spans shown
// with the words in story, finding each by the words the Briefing is shown;
// the statement also cites the Span shown with the words also, if any.
type teller struct {
	text, tell string
	story      []string
	also       string
}

var shownSpan = regexp.MustCompile(`(?m)^([SE]\d+)(?: \(spoken by [^)]*\))?: (.*)$`)

func (f teller) Complete(_ context.Context, req editor.Request) (editor.Response, error) {
	ref := func(words string) (string, error) {
		for _, m := range shownSpan.FindAllStringSubmatch(req.Input, -1) {
			if strings.Contains(m[2], words) {
				return m[1], nil
			}
		}
		return "", fmt.Errorf("no Span shown with %q:\n%s", words, req.Input)
	}
	told, err := ref(f.tell)
	if err != nil {
		return editor.Response{}, err
	}
	story := []string{}
	for _, w := range f.story {
		r, err := ref(w)
		if err != nil {
			return editor.Response{}, err
		}
		story = append(story, r)
	}
	spans := []string{told}
	if f.also != "" {
		also, err := ref(f.also)
		if err != nil {
			return editor.Response{}, err
		}
		spans = append(spans, also)
	}
	out, err := json.Marshal(map[string]any{
		"statements": []any{map[string]any{"spans": spans,
			"stories": []any{map[string]any{"span": told, "story": story}}, "text": f.text}},
	})
	return editor.Response{Output: out, Model: "fake"}, err
}

// tell writes the Briefing at at with m.
func tell(t *testing.T, store *sqlite.Store, at time.Time, m editor.Model) {
	t.Helper()
	ed := editor.New(editor.Deps{Store: store, Briefings: store, BriefingModel: m, Log: slog.New(slog.DiscardHandler)})
	if err := ed.Brief(t.Context(), at); err != nil {
		t.Fatal(err)
	}
}

// relate relates what awaits relating with output.
func relate(t *testing.T, store *sqlite.Store, output string) {
	t.Helper()
	ed := editor.New(editor.Deps{Store: store, RelatingModel: fakeModel{output}, Log: slog.New(slog.DiscardHandler)})
	if err := ed.RelateWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
}

func TestASpansPageShowsItsLatestGatheringThenTheRelationsJudgedSinceAsNotYetGathered(t *testing.T) {
	store := readToRelate(t)
	// The Briefing tells the release's “fantastic news” before the later
	// Item is related to it: its Story is gathered with nothing in it.
	tell(t, store, saturday, teller{text: "Andy Burnham called it fantastic news.", tell: "This is fantastic news"})
	relate(t, store, `{"relations": [{"span": "S1", "to": "C1", "verdict": "follows"}]}`)
	app, _ := newAppWithLog(t, saturday, store)

	page := get(t, app, spanPath(t, app, "release", 2)).body

	story := strings.Index(page, `id="story"`)
	since := strings.Index(page, "Derby will host the headquarters.")
	if story < 0 || since < story {
		t.Fatalf("the page does not show the gathering, then the relation since:\n%s", page)
	}
	for _, want := range []string{`<a href="/briefings/2026-10-24">the Briefing for Saturday 24 October 2026</a>`, "not yet gathered"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q:\n%s", want, page)
		}
	}
}

func TestTheTrailOfABriefingOverSpansLinksItsCallAndTheReadingAndRelatingCallsBehindEachSpanItCites(t *testing.T) {
	store := relatedRecord(t)
	tell(t, store, saturday, teller{text: "Derby will host the headquarters.", tell: "Derby will host",
		story: []string{"This is fantastic news"}})
	app, _ := newAppWithLog(t, saturday, store)

	if page := get(t, app, "/").body; !strings.Contains(page, `<a href="/briefings/2026-10-24/trail">How this was written</a>`) {
		t.Fatalf("the Briefing does not link how it was written:\n%s", page)
	}
	res := get(t, app, "/briefings/2026-10-24/trail")

	if res.status != http.StatusOK {
		t.Fatalf("status %d:\n%s", res.status, res.body)
	}
	var acts []string
	for _, m := range regexp.MustCompile(`<a class="machine" href="/calls/(\d+)">(\w+) · call \d+`).FindAllStringSubmatch(res.body, -1) {
		acts = append(acts, m[2])
		if call := get(t, app, "/calls/"+m[1]); call.status != http.StatusOK {
			t.Errorf("/calls/%s: status %d", m[1], call.status)
		}
	}
	if strings.Join(acts, " ") != "written read related" {
		t.Errorf("the trail labels calls %v, want the Briefing's, then the cited Span's reading and relating:\n%s", acts, res.body)
	}
	if !strings.Contains(res.body, "Derby will host the headquarters.") || !strings.Contains(res.body, "over 3 calls") {
		t.Errorf("the trail does not show the Span cited, over its three calls:\n%s", res.body)
	}
}

func TestASpansPageShowsTheSpansItsLatestGatheringHoldsAndNothingSinceAsNotYetGathered(t *testing.T) {
	store := relatedRecord(t)
	tell(t, store, saturday, teller{text: "Andy Burnham called it fantastic news.", tell: "This is fantastic news",
		story: []string{"Derby will host the headquarters."}})
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, spanPath(t, app, "release", 2))
	page := res.body

	if res.status != http.StatusOK || strings.Contains(page, "not yet gathered") {
		t.Fatalf("status %d, or a gathered relation shown as not yet gathered:\n%s", res.status, page)
	}
	gathered := page[strings.Index(page, `id="story"`):]
	if !strings.Contains(gathered, "Derby will host the headquarters.") ||
		!regexp.MustCompile(`<a href="/items/`+number10.ID+`/hosting/spans/\d+-\d+">`).MatchString(gathered) {
		t.Errorf("the Story does not hold the Span gathered, linked:\n%s", page)
	}
}
