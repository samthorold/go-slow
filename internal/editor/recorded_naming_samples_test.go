package editor_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/editor"
)

// namingEachStory answers every naming with a Name of its own, drawn from the
// Story it is shown, keeping each request.
type namingEachStory struct{ sent []editor.Request }

func (m *namingEachStory) Complete(_ context.Context, req editor.Request) (editor.Response, error) {
	m.sent = append(m.sent, req)
	return editor.Response{Output: json.RawMessage(fmt.Sprintf(`{"name": "The Story %x"}`, sha256.Sum256([]byte(req.Input)))),
		Model: "fake"}, nil
}

// namingFrozenRoot is a recordings root holding naming's frozen copies alone.
func namingFrozenRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	in := filepath.Join("naming", "inputs")
	if err := os.CopyFS(filepath.Join(root, in), os.DirFS(filepath.Join(recordedDir, in))); err != nil {
		t.Fatal(err)
	}
	return root
}

// Naming is recorded as samples of each Story its frozen copies of weaving
// make, and the suite replays them: recorded on a fake model taking the act's
// checks, every property of every Story is supported, with no model called.
func TestNamingIsRecordedAndReplayedAsSamples(t *testing.T) {
	if *rerecordFlag {
		t.Skip("recording")
	}
	root := namingFrozenRoot(t)
	model := pinnedModel(t, namingCommand.modelVar)
	live := &namingEachStory{}
	if errs := namingVerdicts(t, root, live, model, reportTo(t)); len(errs) != 0 {
		t.Fatalf("recording: %v", errs)
	}
	stories := storiesToName(t, filepath.Join(root, "naming", "inputs"))
	if len(stories) < 2 {
		t.Fatalf("naming's frozen copies make %d Stories; want at least two, to tell their Names apart", len(stories))
	}
	if want := len(stories) * dailyRule.samples; len(live.sent) != want {
		t.Errorf("%d calls recording, want %d: one per sample", len(live.sent), want)
	}
	for _, s := range stories {
		if _, err := samplesOf(filepath.Join(root, "naming"), s.name, dailyRule, namingCommand); err != nil {
			t.Error(err)
		}
	}
	if errs := namingVerdicts(t, root, nil, model, reportTo(t)); len(errs) != 0 {
		t.Errorf("replaying: %v", errs)
	}
	if want := len(stories) * dailyRule.samples; len(live.sent) != want {
		t.Errorf("%d calls after replaying, want %d: replaying calls no model", len(live.sent), want)
	}
}

// A Name another Story is also given fails the Names differing: every Story
// given one Name refutes it on each, its checks passed.
func TestStoriesGivenOneNameRefuteTheirNamesDiffering(t *testing.T) {
	if *rerecordFlag {
		t.Skip("recording")
	}
	root := namingFrozenRoot(t)
	errs := namingVerdicts(t, root, &fakeModel{output: `{"name": "Britain this week"}`},
		pinnedModel(t, namingCommand.modelVar), reportTo(t))
	stories := storiesToName(t, filepath.Join(root, "naming", "inputs"))
	if len(errs) != len(stories) {
		t.Fatalf("%d failures %v; want the Names differing refuted on each Story", len(errs), errs)
	}
	for i, err := range errs {
		for _, want := range []string{stories[i].name, namesDiffer, "refuted", "0 of the 5", `"Britain this week" is also a Name of`} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%q does not say %q", err, want)
			}
		}
	}
}

// Names that differ only in case or spacing are one Name to a reader.
func TestNamesDifferingOnlyInCaseOrSpacingAreOneName(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{"Britain this week", "britain  This week", true},
		{"Britain this week", "Britain next week", false},
	} {
		if got := sameName(c.a, c.b); got != c.same {
			t.Errorf("sameName(%q, %q) = %v, want %v", c.a, c.b, got, c.same)
		}
	}
}

// longName is a Name of more words than naming's checks take.
const longName = `{"name": "One two three four five six seven eight nine ten eleven twelve thirteen"}`

// refusedThenNamed answers each sample's first attempt with a Name its
// checks refuse, and its second with one of its own.
type refusedThenNamed struct{ calls int }

func (m *refusedThenNamed) Complete(ctx context.Context, req editor.Request) (editor.Response, error) {
	m.calls++
	if m.calls%2 == 1 {
		return editor.Response{Output: json.RawMessage(longName), Model: "fake"}, nil
	}
	return (&namingEachStory{}).Complete(ctx, req)
}

// A sample runs as naming runs in use, through the batch runner: a Name its
// checks refuse is asked again, and the sample passes on the attempt that
// passes, each attempt kept.
func TestANamingSampleRefusedOnceIsAskedAgainAndPasses(t *testing.T) {
	if *rerecordFlag {
		t.Skip("recording")
	}
	root := namingFrozenRoot(t)
	model := pinnedModel(t, namingCommand.modelVar)
	if errs := namingVerdicts(t, root, &refusedThenNamed{}, model, reportTo(t)); len(errs) != 0 {
		t.Fatalf("recording: %v", errs)
	}
	first := storiesToName(t, filepath.Join(root, "naming", "inputs"))[0].name
	s, err := sampleAt(samplePath(filepath.Join(root, "naming"), first, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.attempts(); len(got) != 2 || got[0].RequestHash != got[1].RequestHash {
		t.Errorf("kept %d attempts %+v; want the refused one and the one that passed, answering one request", len(got), got)
	}
	if errs := namingVerdicts(t, root, nil, model, reportTo(t)); len(errs) != 0 {
		t.Errorf("replaying: %v", errs)
	}
}

// A Story whose samples fail the act's checks on every attempt is refuted on
// passing them.
func TestAStoryRefusedOnEveryAttemptIsRefuted(t *testing.T) {
	if *rerecordFlag {
		t.Skip("recording")
	}
	root := namingFrozenRoot(t)
	live := &fakeModel{output: longName}
	errs := namingVerdicts(t, root, live, pinnedModel(t, namingCommand.modelVar), reportTo(t))
	stories := storiesToName(t, filepath.Join(root, "naming", "inputs"))
	if want := len(stories) * dailyRule.samples * editor.MaxAttempts; len(live.sent) != want {
		t.Errorf("%d calls, want %d: every attempt of every sample", len(live.sent), want)
	}
	if len(errs) != len(stories) {
		t.Fatalf("%d failures %v; want each Story refuted on its checks", len(errs), errs)
	}
	for _, want := range []string{stories[0].name, checksProperty, "refuted", "0 of 5"} {
		if !strings.Contains(errs[0].Error(), want) {
			t.Errorf("%q does not say %q", errs[0], want)
		}
	}
}

// A replay of naming whose act now sends another request fails as stale,
// naming the Story and the command that re-records it: when the model it is
// pinned to changed, or a frozen copy of weaving it rests on (the Story's
// words are its input).
func TestANamingReplayFailsAsStaleWhenItsRequestHasChanged(t *testing.T) {
	if *rerecordFlag {
		t.Skip("recording")
	}
	root := namingFrozenRoot(t)
	model := pinnedModel(t, namingCommand.modelVar)
	if errs := namingVerdicts(t, root, &namingEachStory{}, model, reportTo(t)); len(errs) != 0 {
		t.Fatalf("recording: %v", errs)
	}
	first := storiesToName(t, filepath.Join(root, "naming", "inputs"))[0].name

	// The same recordings over a frozen copy of Saturday's weaving whose
	// first Editor's Span says other words.
	changedCopy := t.TempDir()
	if err := os.CopyFS(changedCopy, os.DirFS(root)); err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(changedCopy, "naming", "inputs", "weaving", "to-15-october.json")
	b, err := os.ReadFile(copied) //nolint:gosec // a test's own temporary file
	if err != nil {
		t.Fatal(err)
	}
	var s sampleFile
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	s.Output = json.RawMessage(strings.Replace(string(s.Output), `"body": "`, `"body": "Other words. `, 1))
	keepSample(t, copied, s.attempts())

	for name, tc := range map[string]struct {
		root, model string
	}{
		"the model changed":         {root, "claude-opus-5-5"},
		"a frozen copy it rests on": {changedCopy, model},
	} {
		t.Run(name, func(t *testing.T) {
			var reported []error
			namingVerdicts(t, tc.root, nil, tc.model, func(err error) { reported = append(reported, err) })
			if len(reported) == 0 {
				t.Fatal("a stale recording replayed")
			}
			for _, want := range []string{first, "stale: re-record with", "-run TestRecordedNaming -record", "NAMING_MODEL"} {
				if !strings.Contains(reported[0].Error(), want) {
					t.Errorf("%q does not say %q", reported[0], want)
				}
			}
		})
	}
}
