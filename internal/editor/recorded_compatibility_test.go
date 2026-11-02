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
	"github.com/samthorold/go-slow/internal/sqlite"
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
// Reading feeds relating, and both feed the Briefing: reading's current
// recordings through relating and the Briefing, and relating's through the
// Briefing over the readings they were recorded over, relating's own frozen
// copies. The Sunday read rests on its own frozen copies of reading,
// relating and the Briefing (sunday/inputs/), and is not fed through the
// check: its recordings cost dollars to refresh.
func TestEachActsCurrentRecordingsAreTakenByTheActsDownstream(t *testing.T) {
	t.Parallel()
	if err := feedDownstream(t, recordedDir, "reading"); err != nil {
		t.Error(err)
	}
	if err := feedBriefing(t, recordedDir); err != nil {
		t.Error(err)
	}
	if err := feedRelatingToBriefing(t, recordedDir); err != nil {
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
	return briefOnFake(t, store, "the current readings and relatings")
}

// feedRelatingToBriefing replays relating's current recordings under root
// (each fixture's first sample) over the readings they were recorded over,
// relating's own frozen copies, so their candidate numbers line up, and
// writes the Briefing over them on a fake model. It is an error if a
// relating fails relating's checks or can't be kept, or the Briefing's
// request fails to build, or the fake's answer fails its checks.
func feedRelatingToBriefing(t *testing.T, root string) error {
	t.Helper()
	store, topics := newRecord(t, nil)
	c := chain{
		reading:  replayed(inputsOf(root, "relating", "reading")),
		relating: firstSamples(filepath.Join(root, "relating")),
	}
	if err := c.run(t.Context(), store, topics, fixtureItems(t)); err != nil {
		return err
	}
	return briefOnFake(t, store, "relating's current recordings")
}

// briefOnFake writes the Briefing over the Record in store on a fake model
// telling every Span shown with relations, with its Story (over, what the
// Record was built from). It is an error if the Briefing is not written
// with a statement for each Span the fake told, or it is shown no
// relation, which would leave its checks on Stories testing nothing.
func briefOnFake(t *testing.T, store *sqlite.Store, over string) error {
	t.Helper()
	f := &fakeAct{act: "briefing"}
	ed, logs := briefer(store, f)
	if err := ed.Brief(t.Context(), briefingAt); err != nil {
		return err
	}
	b, found, err := store.Briefing(t.Context(), briefingAt.Format(time.DateOnly))
	if err != nil || !found || f.told == 0 || len(b.Statements) != f.told {
		return fmt.Errorf("the Briefing over %s, telling %d Spans = %+v, %v, %w:\n%s", over, f.told, b, found, err, logs)
	}
	if f.relations == 0 {
		return fmt.Errorf("the Briefing over %s was shown no relation", over)
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

// A relating answer the Briefing can't be written over fails the check:
// one relating the speech to a candidate it was not shown never reaches
// the Record the Briefing is written over.
func TestTheCompatibilityCheckFailsOnARelatingTheBriefingCannotTake(t *testing.T) {
	t.Parallel()
	root := copyRecordings(t)
	name := "pm-speech-in-scotland-16-october-2026"
	reanswered(t, filepath.Join(root, "relating"), name, func(out map[string]json.RawMessage) {
		out["relations"] = json.RawMessage(`[{"span": "S1", "to": "C999", "verdict": "follows"}]`)
	})
	err := feedRelatingToBriefing(t, root)
	if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "the relating fails the act's checks") {
		t.Fatalf("feedRelatingToBriefing = %v; want the relating of %s refused", err, name)
	}
}

// Relating's recordings feeding the Briefing no relation fail the check:
// the Briefing's checks on Stories would be tested on nothing.
func TestTheCompatibilityCheckFailsOnRelatingsRelatingNothing(t *testing.T) {
	t.Parallel()
	root := copyRecordings(t)
	for _, fx := range fixtureItems(t) {
		s, err := sampleAt(samplePath(filepath.Join(root, "relating"), fx.name, 1))
		if err != nil {
			t.Fatal(err)
		}
		if len(s.attempts()) == 0 {
			continue // related with no call
		}
		reanswered(t, filepath.Join(root, "relating"), fx.name, func(out map[string]json.RawMessage) {
			out["relations"] = json.RawMessage(`[]`)
		})
	}
	err := feedRelatingToBriefing(t, root)
	if err == nil || !strings.Contains(err.Error(), "shown no relation") {
		t.Fatalf("feedRelatingToBriefing = %v; want the Briefing shown no relation", err)
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
	reanswered(t, filepath.Join(root, "reading"), name, func(out map[string]json.RawMessage) {
		out["names"] = json.RawMessage(`[]`)
	})
}

// reanswered rewrites the answer of the first sample of the fixture name
// under dir, an act's recordings, by change.
func reanswered(t *testing.T, dir, name string, change func(map[string]json.RawMessage)) {
	t.Helper()
	path := samplePath(dir, name, 1)
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
	change(out)
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
// checks take, and keeps the Items it was asked about and, writing the
// Briefing, how many Spans it told and relations it was shown.
type fakeAct struct {
	act       string
	items     []string
	told      int
	relations int
}

var (
	itemLine     = regexp.MustCompile(`(?m)^Item: (.*)$`)
	spanLine     = regexp.MustCompile(`(?m)^(S\d+)(?: \(spoken by [^)]*\))?: `)
	relationLine = regexp.MustCompile(`^  (?:follows|followed by|same as|contradicts) ([SE]\d+)`)
)

// shownSpan is one of today's Spans as the Briefing's input shows it: its
// ref, and the refs of the Spans its relations reach.
type shownSpan struct {
	ref   string
	story []string
}

// toldAsShown is today's Spans in the Briefing's input, each with the refs
// shown beneath it.
func toldAsShown(input string) []shownSpan {
	var out []shownSpan
	for line := range strings.Lines(input) {
		if m := spanLine.FindStringSubmatch(line); m != nil {
			out = append(out, shownSpan{ref: m[1], story: []string{}})
			continue
		}
		if m := relationLine.FindStringSubmatch(line); m != nil && len(out) > 0 {
			out[len(out)-1].story = append(out[len(out)-1].story, m[1])
		}
	}
	return out
}

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
		// Each of today's Spans with relations is told in a statement of its
		// own, its Story gathered beside it from every Span its relations
		// reach, as shown beneath it: so the act's checks see every
		// relation. With none, the first Span is told, with an empty Story.
		shown := toldAsShown(req.Input)
		told := slices.DeleteFunc(slices.Clone(shown), func(s shownSpan) bool { return len(s.story) == 0 })
		if len(told) == 0 && len(shown) > 0 {
			told = shown[:1]
		}
		statements := []any{}
		for _, s := range told {
			f.told++
			f.relations += len(s.story)
			statements = append(statements, map[string]any{"spans": []string{s.ref},
				"stories": []any{map[string]any{"span": s.ref, "story": s.story}}, "text": "Told."})
		}
		out = map[string]any{"statements": statements}
	default:
		return editor.Response{}, fmt.Errorf("no fake for the act %q", req.Act)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return editor.Response{}, err
	}
	return editor.Response{Output: b, Model: "fake-" + f.act}, nil
}
