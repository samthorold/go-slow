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
// Reading feeds relating. The old Briefing and the Sunday read rest on
// Facts, from their own frozen copies of the retired placing and connecting
// acts, so no current recording feeds them.
func TestEachActsCurrentRecordingsAreTakenByTheActsDownstream(t *testing.T) {
	t.Parallel()
	if err := feedDownstream(t, recordedDir, "reading"); err != nil {
		t.Error(err)
	}
}

// A reading relating can't take fails the check: one that names nothing
// gives relating no candidates to weigh the speech's Spans against.
func TestTheCompatibilityCheckFailsOnAReadingRelatingCannotTake(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, act := range []string{"reading", "relating"} {
		if err := os.CopyFS(filepath.Join(root, act), os.DirFS(filepath.Join(recordedDir, act))); err != nil {
			t.Fatal(err)
		}
	}
	name := "pm-speech-in-scotland-16-october-2026"
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

	err = feedDownstream(t, root, "reading")
	if err == nil || !strings.Contains(err.Error(), "relating was never asked about "+name) {
		t.Fatalf("feedDownstream = %v; want relating never asked about %s", err, name)
	}
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
	itemLine      = regexp.MustCompile(`(?m)^Item: (.*)$`)
	candidateLine = regexp.MustCompile(`(?m)^(S\d+): (C\d+)`)
)

func (f *fakeAct) Complete(_ context.Context, req editor.Request) (editor.Response, error) {
	if m := itemLine.FindStringSubmatch(req.Input); m != nil {
		f.items = append(f.items, m[1])
	}
	var out any
	switch req.Act {
	case "relating":
		// Each Span is the same as its first candidate.
		type relation struct {
			Span    string `json:"span"`
			To      string `json:"to"`
			Verdict string `json:"verdict"`
		}
		_, list, _ := strings.Cut(req.Input, "\nCandidates of each Span:\n")
		rels := []relation{}
		for _, m := range candidateLine.FindAllStringSubmatch(list, -1) {
			rels = append(rels, relation{m[1], m[2], editor.SameAs})
		}
		out = map[string]any{"relations": rels}
	default:
		return editor.Response{}, fmt.Errorf("no fake for the act %q", req.Act)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return editor.Response{}, err
	}
	return editor.Response{Output: b, Model: "fake-" + f.act}, nil
}
