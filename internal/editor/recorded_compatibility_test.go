package editor_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

// The compatibility check (system design: Each act is recorded over its own
// frozen inputs). Until a refresh, the acts downstream of one re-recorded
// are replayed over what it used to write. So each act's current
// recordings are fed through every act downstream of it, each on a fake
// model that answers from its request alone, as any model could: their
// requests must build, the fake's answers must pass their checks, and every
// downstream act must be asked about every fixture its own recordings were
// asked about. A change to an earlier act's output that a later act can't
// take fails here at once, with no recording made.
//
// Reading feeds relating, and both feed the Briefing. The Sunday read rests
// on Facts, from its own frozen copies of the retired placing and
// connecting acts, so no current recording feeds it.
func TestEachActsCurrentRecordingsAreTakenByTheActsDownstream(t *testing.T) {
	t.Parallel()
	if err := feedDownstream(t, recordedDir, "reading"); err != nil {
		t.Error(err)
	}
	if err := feedBriefing(t, recordedDir); err != nil {
		t.Error(err)
	}
}

// feedBriefing replays the current readings under root (each fixture's
// first sample), relates them on a fake model and writes the Briefing over
// them on another. Relating's own recordings are not replayed: their
// answers name candidates by number, as the one reading they were recorded
// over offered them, and a fresh reading may offer others. It is an error
// if relating's or the Briefing's request fails to build, or a fake's
// answer fails its checks.
func feedBriefing(t *testing.T, root string) error {
	t.Helper()
	store, topics := newRecord(t, nil)
	relating := &fakeAct{act: "relating"}
	c := chain{
		reading:  firstSamples(filepath.Join(root, "reading")),
		relating: func(string) (editor.Model, error) { return relating, nil },
	}
	if err := c.run(t.Context(), store, topics, fixtureItems(t)); err != nil {
		return err
	}
	ed, logs := briefer(store, &fakeAct{act: "briefing"})
	if err := ed.Brief(t.Context(), briefingAt); err != nil {
		return err
	}
	b, found, err := store.Briefing(t.Context(), briefingAt.Format(time.DateOnly))
	if err != nil || !found || len(b.Statements) != 1 {
		return fmt.Errorf("the Briefing over the current readings and relatings = %+v, %v, %w:\n%s", b, found, err, logs)
	}
	return nil
}

// The Briefing takes reading's current recordings whatever candidates they
// offer relating: a reading of the Holyrood press release that names
// nothing offers the speech's Spans none of the release's, so relating's
// recorded answer, which relates the speech to candidates by their numbers
// as one reading offered them, no longer fits what it would be shown.
func TestTheCompatibilityCheckTakesReadingsOfferingOtherCandidates(t *testing.T) {
	t.Parallel()
	root := copyRecordings(t)
	namingNothing(t, root, "prime-minister-makes-offer-to-holyrood-to-work-together-to-empower-scottish-regions")
	if err := feedBriefing(t, root); err != nil {
		t.Fatal(err)
	}
}

// A reading relating can't take fails the check: one that names nothing
// gives relating no candidates to weigh the speech's Spans against.
func TestTheCompatibilityCheckFailsOnAReadingRelatingCannotTake(t *testing.T) {
	t.Parallel()
	root := copyRecordings(t)
	name := "pm-speech-in-scotland-16-october-2026"
	namingNothing(t, root, name)
	err := feedDownstream(t, root, "reading")
	if err == nil || !strings.Contains(err.Error(), "relating was never asked about "+name) {
		t.Fatalf("feedDownstream = %v; want relating never asked about %s", err, name)
	}
}

// copyRecordings is a root holding a copy of reading's and relating's
// current recordings, to change in a test.
func copyRecordings(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, act := range []string{"reading", "relating"} {
		if err := os.CopyFS(filepath.Join(root, act), os.DirFS(filepath.Join(recordedDir, act))); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// namingNothing rewrites the first sample of the fixture name's reading
// under root so that it names nothing.
func namingNothing(t *testing.T, root, name string) {
	t.Helper()
	path := samplePath(filepath.Join(root, "reading"), name, 1)
	sample, err := sampleAt(path)
	if err != nil {
		t.Fatal(err)
	}
	attempts := sample.attempts()
	rec := attempts[len(attempts)-1]
	var out map[string]json.RawMessage
	if err := json.Unmarshal(rec.Output, &out); err != nil {
		t.Fatal(err)
	}
	out["names"] = json.RawMessage(`[]`)
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	rec.Output = b
	attempts[len(attempts)-1] = rec
	keepSample(t, path, attempts)
}

// downstream are the acts that rest on each act's recordings.
var downstream = map[string][]string{"reading": {"relating"}}

// feedDownstream replays act's current recordings under root (each
// fixture's first sample, as a refresh would copy it) and runs every act
// downstream of it on a fake model. It is an error if a downstream act's
// answer fails its checks, or it is never asked about a fixture its own
// recordings under root were asked about.
func feedDownstream(t *testing.T, root, act string) error {
	t.Helper()
	store, topics := newRecord(t, nil)
	fixtures := fixtureItems(t)
	fakes := map[string]*fakeAct{}
	models := map[string]actModel{act: firstSamples(filepath.Join(root, act))}
	for _, down := range downstream[act] {
		f := &fakeAct{act: down}
		fakes[down] = f
		models[down] = func(string) (editor.Model, error) { return f, nil }
	}
	c := chain{reading: models["reading"], relating: models["relating"]}
	if err := c.run(t.Context(), store, topics, fixtures); err != nil {
		return err
	}
	for down, f := range fakes {
		for _, fx := range fixtures {
			s, err := sampleAt(samplePath(filepath.Join(root, down), fx.name, 1))
			if err != nil {
				return err
			}
			if len(s.attempts()) > 0 && !slices.Contains(f.items, fx.item.Title) {
				return fmt.Errorf("%s was never asked about %s", down, fx.name)
			}
		}
	}
	return nil
}

// fakeAct stands in for the model of an act downstream: it answers from
// the request alone, as the act's input shows it, with an answer the act's
// checks take, and keeps the Items it was asked about.
type fakeAct struct {
	act   string
	items []string
}

var (
	itemLine = regexp.MustCompile(`(?m)^Item: (.*)$`)
	spanLine = regexp.MustCompile(`(?m)^(S\d+)(?: \(spoken by [^)]*\))?: `)
)

func (f *fakeAct) Complete(_ context.Context, req editor.Request) (editor.Response, error) {
	if m := itemLine.FindStringSubmatch(req.Input); m != nil {
		f.items = append(f.items, m[1])
	}
	var out any
	switch req.Act {
	case "relating":
		// The first Span is the same as the first candidate.
		type relation struct {
			Span    string `json:"span"`
			To      string `json:"to"`
			Verdict string `json:"verdict"`
		}
		rels := []relation{}
		if m := spanLine.FindStringSubmatch(req.Input); m != nil {
			rels = append(rels, relation{m[1], "C1", editor.SameAs})
		}
		out = map[string]any{"relations": rels}
	case "briefing":
		// The first Span shown is told, with an empty Story.
		out = map[string]any{
			"statements": []any{map[string]any{"spans": []string{"S1"},
				"stories": []any{map[string]any{"span": "S1", "story": []string{}}}, "text": "Told."}},
		}
	default:
		return editor.Response{}, fmt.Errorf("no fake for the act %q", req.Act)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return editor.Response{}, err
	}
	return editor.Response{Output: b, Model: "fake-" + f.act}, nil
}
