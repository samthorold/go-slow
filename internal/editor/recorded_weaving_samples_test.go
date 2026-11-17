package editor_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// shownLine is a line of a weaving run's input showing a numbered Span.
var shownLine = regexp.MustCompile(`(?m)^S(\d+)[ :]`)

// opinionOnShown is an answer to a weaving run shown input: one Story of one
// Editor's Span, citing the first and third Spans shown, which every run is
// shown. Sunday's run, shown Saturday's Story first, is shown the Spans it
// cites first and second and its Editor's Span third, so Sunday's answer
// cites an earlier Editor's Span.
func opinionOnShown(input string) string {
	shown := shownLine.FindAllStringSubmatch(input, 3)
	return fmt.Sprintf(`{"stories": [{"editor_spans": [{"body": "What none of them says.", "cites": [%s, %s]}]}]}`,
		shown[0][1], shown[2][1])
}

// weavingOnShown answers every weaving run with opinionOnShown, keeping each
// request.
type weavingOnShown struct{ sent []editor.Request }

func (m *weavingOnShown) Complete(_ context.Context, req editor.Request) (editor.Response, error) {
	m.sent = append(m.sent, req)
	return editor.Response{Output: json.RawMessage(opinionOnShown(req.Input)), Model: "fake"}, nil
}

// frozenRoot is a recordings root holding weaving's frozen copies alone.
func frozenRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	in := filepath.Join("weaving", "inputs")
	if err := os.CopyFS(filepath.Join(root, in), os.DirFS(filepath.Join(recordedDir, in))); err != nil {
		t.Fatal(err)
	}
	return root
}

// Weaving is recorded as samples of each run, and the suite replays them:
// recorded on a fake model taking the act's checks, every property of every
// run is supported, with no model called.
func TestWeavingIsRecordedAndReplayedAsSamples(t *testing.T) {
	if *rerecordFlag {
		t.Skip("recording")
	}
	root := frozenRoot(t)
	runs := weavingRuns(t)
	model := pinnedModel(t, weavingCommand.modelVar)
	live := &weavingOnShown{}
	if errs := weavingVerdicts(t, root, runs, live, model, reportTo(t)); len(errs) != 0 {
		t.Fatalf("recording: %v", errs)
	}
	if want := len(runs) * dailyRule.samples; len(live.sent) != want {
		t.Errorf("%d calls recording, want %d: one per sample", len(live.sent), want)
	}
	if in := live.sent[len(live.sent)-1].Input; !strings.Contains(in, "S3 (the Editor's, citing S1, S2): What none of them says.") {
		t.Errorf("Sunday's run was not shown Saturday's Story:\n%.500s", in)
	}
	for _, r := range runs {
		if _, err := samplesOf(filepath.Join(root, "weaving"), r.name, dailyRule, weavingCommand); err != nil {
			t.Error(err)
		}
	}
	if errs := weavingVerdicts(t, root, runs, nil, model, reportTo(t)); len(errs) != 0 {
		t.Errorf("replaying: %v", errs)
	}
	if want := len(runs) * dailyRule.samples; len(live.sent) != want {
		t.Errorf("%d calls after replaying, want %d: replaying calls no model", len(live.sent), want)
	}
}

// A replay of weaving whose act now sends another request (the model it is
// pinned to changed, or a fixture Item's text) fails as stale, naming the
// run and the command that re-records it. The instructions and schema are in
// the request hashed as the input and model are (TestAReplayFailsAsStale-
// WhenTheActsRequestHasChanged).
func TestAWeavingReplayFailsAsStaleWhenItsRequestHasChanged(t *testing.T) {
	if *rerecordFlag {
		t.Skip("recording")
	}
	root := frozenRoot(t)
	runs := weavingRuns(t)
	model := pinnedModel(t, weavingCommand.modelVar)
	if errs := weavingVerdicts(t, root, runs, &weavingOnShown{}, model, reportTo(t)); len(errs) != 0 {
		t.Fatalf("recording: %v", errs)
	}
	changedItem := weavingRuns(t)
	changedItem[0].items[0].item.Content += "<p>One more sentence.</p>"
	for name, tc := range map[string]struct {
		runs  []weavingRun
		model string
	}{
		"the model changed": {runs, "claude-haiku-5-5"},
		"a fixture changed": {changedItem, model},
	} {
		t.Run(name, func(t *testing.T) {
			var reported []error
			weavingVerdicts(t, root, tc.runs, nil, tc.model, func(err error) { reported = append(reported, err) })
			if len(reported) == 0 {
				t.Fatal("a stale recording replayed")
			}
			for _, want := range []string{runs[0].name, "stale: re-record with", "-run TestRecordedWeaving -record", "WEAVING_MODEL"} {
				if !strings.Contains(reported[0].Error(), want) {
					t.Errorf("%q does not say %q", reported[0], want)
				}
			}
		})
	}
}

// citingUnshown cites a Span no run is shown: the act's checks refuse it.
const citingUnshown = `{"stories": [{"editor_spans": [{"body": "Words.", "cites": [100000]}]}]}`

// refusedThenWoven answers each sample's first attempt with an answer the
// act's checks refuse, and its second with one they take.
type refusedThenWoven struct{ calls int }

func (m *refusedThenWoven) Complete(_ context.Context, req editor.Request) (editor.Response, error) {
	m.calls++
	if m.calls%2 == 1 {
		return editor.Response{Output: json.RawMessage(citingUnshown), Model: "fake"}, nil
	}
	return editor.Response{Output: json.RawMessage(opinionOnShown(req.Input)), Model: "fake"}, nil
}

// A sample runs as weaving runs in use, through the batch runner: an answer
// its checks refuse is asked again, and the sample passes on the attempt
// that passes, each attempt kept.
func TestAWeavingSampleRefusedOnceIsAskedAgainAndPasses(t *testing.T) {
	if *rerecordFlag {
		t.Skip("recording")
	}
	root := frozenRoot(t)
	runs := weavingRuns(t)
	model := pinnedModel(t, weavingCommand.modelVar)
	live := &refusedThenWoven{}
	if errs := weavingVerdicts(t, root, runs, live, model, reportTo(t)); len(errs) != 0 {
		t.Fatalf("recording: %v", errs)
	}
	s, err := sampleAt(samplePath(filepath.Join(root, "weaving"), runs[0].name, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.attempts(); len(got) != 2 || got[0].RequestHash != got[1].RequestHash {
		t.Errorf("kept %d attempts %+v; want the refused one and the one that passed, answering one request", len(got), got)
	}
	if errs := weavingVerdicts(t, root, runs, nil, model, reportTo(t)); len(errs) != 0 {
		t.Errorf("replaying: %v", errs)
	}
}

// Weaving is recorded over its frozen copies of reading's recordings
// (weaving/inputs/reading/): each run is shown its Items hydrated, a Span
// with a Speaker as spoken, a Span read in other words in brackets, and a
// Span Rejected left out.
func TestWeavingIsRecordedOverItsFrozenCopiesOfReading(t *testing.T) {
	if *rerecordFlag {
		t.Skip("recording")
	}
	root := frozenRoot(t)
	runs := weavingRuns(t)
	live := &weavingOnShown{}
	if errs := weavingVerdicts(t, root, runs, live, pinnedModel(t, weavingCommand.modelVar), reportTo(t)); len(errs) != 0 {
		t.Fatalf("recording: %v", errs)
	}
	in := live.sent[0].Input
	// The US–Russia response's first Span is Rejected by its frozen reading.
	first := runs[0].items[0].item
	rejected := cut.Words(first.Content, cut.Sentences(first.Content)[0])
	for _, c := range []struct {
		what string
		ok   bool
	}{
		{"a Span spoken", strings.Contains(in, " (spoken by ")},
		{"a stretch read in other words", strings.Contains(in, "[")},
		{"no Rejected Span: " + rejected, !strings.Contains(in, rejected)},
	} {
		if !c.ok {
			t.Errorf("Saturday's run is not shown %s:\n%.1500s", c.what, in)
		}
	}
}

// reportTo reports a stale sample as a failure of t.
func reportTo(t *testing.T) func(error) { return func(err error) { t.Error(err) } }

// A run whose samples fail the act's checks on every attempt is refuted on
// passing them, and the runs after it, resting on one that passed, are not
// judged.
func TestAWeavingRunRefusedOnEveryAttemptIsRefuted(t *testing.T) {
	if *rerecordFlag {
		t.Skip("recording")
	}
	root := frozenRoot(t)
	runs := weavingRuns(t)
	model := pinnedModel(t, weavingCommand.modelVar)
	live := &fakeModel{output: citingUnshown}
	errs := weavingVerdicts(t, root, runs, live, model, reportTo(t))
	if want := dailyRule.samples * editor.MaxAttempts; len(live.sent) != want {
		t.Errorf("%d calls, want %d: every attempt of every sample of the first run", len(live.sent), want)
	}
	if len(errs) != 2 {
		t.Fatalf("%d failures %v; want the first run refuted, and the next not judged", len(errs), errs)
	}
	for _, want := range []string{runs[0].name, checksProperty, "refuted", "0 of 5"} {
		if !strings.Contains(errs[0].Error(), want) {
			t.Errorf("%q does not say %q", errs[0], want)
		}
	}
	if !strings.Contains(errs[1].Error(), "not judged") {
		t.Errorf("%q does not say the runs after it are not judged", errs[1])
	}
}
