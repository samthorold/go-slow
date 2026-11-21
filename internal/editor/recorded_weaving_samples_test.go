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

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// shownLine is a line of a weaving run's input, or of an ask's answer,
// showing a numbered Span.
var shownLine = regexp.MustCompile(`(?m)^\s*S(\d+)[ :]`)

// opinionOnShown is an answer to a weaving run shown input, then answered
// asked: one Story of one Editor's Span, citing the first and third Spans
// shown as new, which every run is shown, and the first an ask's answer
// numbered, if any did, an earlier Story's.
func opinionOnShown(input string, asked []editor.ToolResult) string {
	shown := shownLine.FindAllStringSubmatch(input, 3)
	cites := []string{shown[0][1], shown[2][1]}
	for _, r := range asked {
		if m := shownLine.FindStringSubmatch(r.Content); m != nil {
			cites = append(cites, m[1])
			break
		}
	}
	return fmt.Sprintf(`{"stories": [{"editor_spans": [{"body": "What none of them says.", "cites": [%s]}]}]}`,
		strings.Join(cites, ", "))
}

// fakeAsks are what weavingOnShown asks for on a run's first turn: a name
// Saturday's Items and Sunday's speech share, and the words of the Story
// Saturday's run made on it.
var fakeAsks = []string{"Burnham", "none of them says"}

// weavingOnShown answers a weaving run's first turn by asking for each of
// fakeAsks, and its next with opinionOnShown, keeping each request.
type weavingOnShown struct{ sent []editor.Request }

func (m *weavingOnShown) Complete(_ context.Context, req editor.Request) (editor.Response, error) {
	m.sent = append(m.sent, req)
	if len(req.Messages) == 0 {
		res := editor.Response{Output: json.RawMessage(""), Raw: json.RawMessage(`{"id":"fake"}`), Model: "fake",
			StopReason: "tool_use"}
		for i, w := range fakeAsks {
			in, _ := json.Marshal(map[string]string{"words": w})
			res.Calls = append(res.Calls, editor.ToolCall{ID: fmt.Sprintf("call-%d", i+1), Name: "find", Input: in})
		}
		return res, nil
	}
	asked := req.Messages[len(req.Messages)-1].Results
	return editor.Response{Output: json.RawMessage(opinionOnShown(req.Input, asked)), Model: "fake"}, nil
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
	if want := 2 * len(runs) * dailyRule.samples; len(live.sent) != want {
		t.Errorf("%d calls recording, want %d: two turns per sample", len(live.sent), want)
	}
	last := live.sent[len(live.sent)-1]
	if found := last.Messages[len(last.Messages)-1].Results[1].Content; !strings.Contains(found, "(the Editor's): What none of them says.") {
		t.Errorf("Sunday's run did not find Saturday's Story:\n%.500s", found)
	}
	for _, r := range runs {
		if _, err := samplesOf(filepath.Join(root, "weaving"), r.name, dailyRule, weavingCommand); err != nil {
			t.Error(err)
		}
	}
	if errs := weavingVerdicts(t, root, runs, nil, model, reportTo(t)); len(errs) != 0 {
		t.Errorf("replaying: %v", errs)
	}
	if want := 2 * len(runs) * dailyRule.samples; len(live.sent) != want {
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
// act's checks refuse, and goes on as weavingOnShown does: its second
// attempt asks, and its next turn answers.
type refusedThenWoven struct {
	weavingOnShown
	refused bool
}

func (m *refusedThenWoven) Complete(ctx context.Context, req editor.Request) (editor.Response, error) {
	if len(req.Messages) == 0 && !m.refused {
		m.refused = true
		return editor.Response{Output: json.RawMessage(citingUnshown), Model: "fake"}, nil
	}
	m.refused = len(req.Messages) == 0
	return m.weavingOnShown.Complete(ctx, req)
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
	if got := s.attempts(); len(got) != 3 || got[0].RequestHash != got[1].RequestHash || got[1].RequestHash == got[2].RequestHash ||
		len(got[1].Calls) != 2 {
		t.Errorf("kept %d attempts %+v; want the refused one, the one that asked, answering one request, then the next turn's", len(got), got)
	}
	if errs := weavingVerdicts(t, root, runs, nil, model, reportTo(t)); len(errs) != 0 {
		t.Errorf("replaying: %v", errs)
	}
}

// A sample keeps, beside each turn that called tools, what each tool
// returned, as the next turn sent it back: the replay doesn't need it, but
// whoever counts what the tools found does (the yield report).
func TestAWeavingSampleKeepsWhatItsToolsReturned(t *testing.T) {
	if *rerecordFlag {
		t.Skip("recording")
	}
	root := frozenRoot(t)
	runs := weavingRuns(t)
	live := &weavingOnShown{}
	if errs := weavingVerdicts(t, root, runs, live, pinnedModel(t, weavingCommand.modelVar), reportTo(t)); len(errs) != 0 {
		t.Fatalf("recording: %v", errs)
	}
	s, err := sampleAt(samplePath(filepath.Join(root, "weaving"), runs[1].name, 1))
	if err != nil {
		t.Fatal(err)
	}
	got := s.attempts()
	if len(got) != 2 {
		t.Fatalf("kept %d attempts, want the turn that asked and the answer", len(got))
	}
	// The last request recorded is Sunday's fifth sample's second turn; every
	// sample's asks are answered from the same Record, so alike.
	last := live.sent[len(live.sent)-1]
	want := last.Messages[len(last.Messages)-1].Results
	if !slices.Equal(got[0].Results, want) {
		t.Errorf("the turn that asked keeps results %+v, want %+v", got[0].Results, want)
	}
	if got[1].Results != nil {
		t.Errorf("the answer, which called no tool, keeps results %+v", got[1].Results)
	}
}

// A replay whose tools now return other than a sample kept (the Record they
// answer from, or how they answer, changed) fails as stale, naming the run,
// the tool and the command that re-records it; and so does a sample that
// keeps no results for a turn that called tools.
func TestAWeavingReplayFailsAsStaleWhenItsToolsReturnOtherwise(t *testing.T) {
	if *rerecordFlag {
		t.Skip("recording")
	}
	model := pinnedModel(t, weavingCommand.modelVar)
	for name, change := range map[string]func(*recording){
		"a result changed": func(r *recording) { r.Results[0].Content += " And more." },
		"no results kept":  func(r *recording) { r.Results = nil },
	} {
		t.Run(name, func(t *testing.T) {
			root := frozenRoot(t)
			runs := weavingRuns(t)
			if errs := weavingVerdicts(t, root, runs, &weavingOnShown{}, model, reportTo(t)); len(errs) != 0 {
				t.Fatalf("recording: %v", errs)
			}
			path := samplePath(filepath.Join(root, "weaving"), runs[0].name, 2)
			s, err := sampleAt(path)
			if err != nil {
				t.Fatal(err)
			}
			change(&s.EarlierAttempts[0])
			keepSample(t, path, s.attempts())
			var reported []error
			weavingVerdicts(t, root, runs, nil, model, func(err error) { reported = append(reported, err) })
			if len(reported) == 0 {
				t.Fatal("a sample whose tools returned otherwise replayed")
			}
			for _, want := range []string{runs[0].name + " sample 2", "stale: re-record with", "-run TestRecordedWeaving -record", "find"} {
				if !strings.Contains(reported[0].Error(), want) {
					t.Errorf("%q does not say %q", reported[0], want)
				}
			}
		})
	}
}

// Weaving is recorded over its frozen copies of hydration's recordings
// (weaving/inputs/hydration/): each run is shown its Items hydrated, a Span
// with a Speaker as spoken, a Span read in other words in brackets, and a
// Span Rejected left out.
func TestWeavingIsRecordedOverItsFrozenCopiesOfHydration(t *testing.T) {
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
	// The US–Russia response's first Span is Rejected by its frozen hydration.
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
