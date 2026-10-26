package editor_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/editor"
)

// samplesPassing is n hand-made samples, the first passed of them passing
// their checks within their attempts and holding every property they assert.
func samplesPassing(passed, n int) []sampleResult {
	out := make([]sampleResult, n)
	for i := range out {
		out[i] = sampleResult{passed: i < passed}
		if out[i].passed {
			out[i].held = map[string][]string{"every Fact traces to its Item": nil}
		}
	}
	return out
}

// A fixture's samples are judged by the daily acts' rule: "passes its checks
// within its attempts" is supported at five of five, undecided at four and
// refuted at three or fewer, each verdict but supported failing with the
// fixture, the property and the count.
func TestPassingItsChecksIsJudgedByTheRule(t *testing.T) {
	for _, tc := range []struct {
		passed int
		want   []string // in the one failure; none when supported
	}{
		{5, nil},
		{4, []string{"the-speech", "passes its checks within its attempts", "undecided", "4 of 5", "fresh samples", "TestRecordedReadings"}},
		{3, []string{"the-speech", "passes its checks within its attempts", "refuted", "3 of 5"}},
		{0, []string{"the-speech", "passes its checks within its attempts", "refuted", "0 of 5"}},
	} {
		errs := judge("the-speech", dailyRule, readingCommand, samplesPassing(tc.passed, 5))
		if tc.want == nil {
			if len(errs) != 0 {
				t.Errorf("%d of 5 passing: %v, want supported", tc.passed, errs)
			}
			continue
		}
		if len(errs) != 1 {
			t.Errorf("%d of 5 passing: %v, want one failure", tc.passed, errs)
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(errs[0].Error(), w) {
				t.Errorf("%d of 5 passing: %q does not say %q", tc.passed, errs[0], w)
			}
		}
	}
}

// A test-only property is rated over the samples that passed their checks:
// a sample refused on every attempt says nothing of it. Each sample that
// passed and fails it counts against the rule, so one failure is undecided
// and two refuted, however many samples passed.
func TestATestOnlyPropertyIsRatedOverTheSamplesThatPassed(t *testing.T) {
	const speech = "a speech's Facts come from its primary text"
	withFailures := func(passed, failing int) []sampleResult {
		out := make([]sampleResult, 5)
		for i := range passed {
			var why []string
			if i < failing {
				why = []string{"a Fact quotes the summary"}
			}
			out[i] = sampleResult{passed: true, held: map[string][]string{speech: why}}
		}
		return out
	}
	for _, tc := range []struct {
		passed, failing int
		want            []string // in the property's failure; none when supported
	}{
		{5, 0, nil},
		{5, 1, []string{"the-speech", speech, "undecided", "4 of the 5 samples that passed", "a Fact quotes the summary"}},
		{5, 2, []string{"the-speech", speech, "refuted", "3 of the 5 samples that passed"}},
		{4, 0, nil},
		{4, 1, []string{speech, "undecided", "3 of the 4 samples that passed"}},
	} {
		var got []string
		for _, err := range judge("the-speech", dailyRule, readingCommand, withFailures(tc.passed, tc.failing)) {
			if strings.Contains(err.Error(), speech) {
				got = append(got, err.Error())
			}
		}
		if tc.want == nil {
			if len(got) != 0 {
				t.Errorf("%d passed, %d failing it: %v, want it supported", tc.passed, tc.failing, got)
			}
			continue
		}
		if len(got) != 1 {
			t.Errorf("%d passed, %d failing it: %v, want one failure", tc.passed, tc.failing, got)
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(got[0], w) {
				t.Errorf("%d passed, %d failing it: %q does not say %q", tc.passed, tc.failing, got[0], w)
			}
		}
	}
}

// keepSamples writes n samples of fixture under dir, each one attempt
// answering req.
func keepSamples(t *testing.T, dir, fixture string, n int, req editor.Request) {
	t.Helper()
	for k := 1; k <= n; k++ {
		keepSample(t, samplePath(dir, fixture, k), []recording{recordedOn(t, req, "claude-haiku-5-5")})
	}
}

// A fixture with fewer samples kept than its act declares can't be judged by
// the rule: it fails, naming the command that records them.
func TestAFixtureWithTooFewSamplesFails(t *testing.T) {
	dir := t.TempDir()
	keepSamples(t, dir, "the-speech", 3, asRecorded)
	_, err := samplesOf(dir, "the-speech", dailyRule, readingCommand)
	if err == nil {
		t.Fatal("three samples were taken for five")
	}
	for _, want := range []string{"the-speech", "3 of 5 samples", "go test -count=1 ./internal/editor -run TestRecordedReadings -record"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q does not say %q", err, want)
		}
	}
	keepSamples(t, dir, "the-speech", 5, asRecorded)
	if s, err := samplesOf(dir, "the-speech", dailyRule, readingCommand); err != nil || len(s) != 5 {
		t.Errorf("five samples: %d, %v", len(s), err)
	}
}

// Every sample of a fixture answers one request: samples answering
// different ones are not samples of one rate.
func TestEverySampleOfAFixtureCarriesTheSameRequestHash(t *testing.T) {
	dir := t.TempDir()
	keepSamples(t, dir, "the-speech", 5, asRecorded)
	keepSample(t, samplePath(dir, "the-speech", 4), []recording{recordedOn(t, changed, "claude-haiku-5-5")})
	_, err := samplesOf(dir, "the-speech", dailyRule, readingCommand)
	if err == nil || !strings.Contains(err.Error(), "the-speech") || !strings.Contains(err.Error(), "sample 4") {
		t.Errorf("samples of two requests: %v; want sample 4 named", err)
	}
}

// refusedThenRead answers its first call with output the reading act's
// checks refuse, and every later one with a reading they take.
type refusedThenRead struct{ calls int }

func (m *refusedThenRead) Complete(context.Context, editor.Request) (editor.Response, error) {
	m.calls++
	if m.calls == 1 {
		return editor.Response{Output: json.RawMessage(`{"facts": [{"statement": "Made up.", "as_at": "2026-10-16", "quote": "not in the Item", "entities": []}], "claims": [], "entities": []}`), Model: "fake"}, nil
	}
	return editor.Response{Output: json.RawMessage(goodReading), Model: "fake"}, nil
}

// readSample runs one sample of reading the speech on m, as in use: up to
// its attempts against the act's checks.
func readSample(t *testing.T, m *sampleModel) bool {
	t.Helper()
	passed, err := runSample(m, func(model editor.Model) (bool, error) {
		_, err := editor.Read(t.Context(), model, speech)
		return err == nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return passed
}

// A sample is recorded as it runs in use: an attempt the act's checks refuse
// is tried again, and every attempt is kept as it arrives.
func TestASampleIsRecordedAttemptByAttempt(t *testing.T) {
	path := samplePath(t.TempDir(), "the-speech", 1)
	live := &refusedThenRead{}
	m := recordingSample(t, path, live, "claude-haiku-5-5", readingCommand)
	if !readSample(t, m) {
		t.Fatal("the sample did not pass on its second attempt")
	}
	s, err := sampleAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.attempts(); len(got) != 2 || got[0].RequestHash != got[1].RequestHash || got[1].RequestHash == "" {
		t.Errorf("kept %d attempts %+v; want the refused one and the one that passed, answering one request", len(got), got)
	}

	// Replayed, it passes as it did, on the same attempts.
	r := replayingSample(t, path, "claude-haiku-5-5", readingCommand, func(err error) { t.Error(err) })
	if !readSample(t, r) || r.next != 2 {
		t.Errorf("replayed: passed on attempt %d; want the second", r.next)
	}
}

// A sample refused on every attempt fails, after MaxAttempts.
func TestASampleRefusedOnEveryAttemptFails(t *testing.T) {
	path := samplePath(t.TempDir(), "the-speech", 1)
	live := &fakeModel{output: `{"facts": [{"statement": "Made up.", "as_at": "2026-10-16", "quote": "not in the Item", "entities": []}], "claims": [], "entities": []}`}
	m := recordingSample(t, path, live, "claude-haiku-5-5", readingCommand)
	if readSample(t, m) {
		t.Fatal("a refused sample passed")
	}
	if len(live.sent) != editor.MaxAttempts {
		t.Errorf("%d attempts made, want %d", len(live.sent), editor.MaxAttempts)
	}
	r := replayingSample(t, path, "claude-haiku-5-5", readingCommand, func(err error) { t.Error(err) })
	if readSample(t, r) {
		t.Error("replayed, the refused sample passed")
	}
}

// Recording keeps a sample already kept for the request the act sends now,
// so a recording that stopped part way resumes where it stopped; a sample
// kept for another request is recorded afresh.
func TestRecordingKeepsACurrentSampleAndRedrawsAStaleOne(t *testing.T) {
	dir := t.TempDir()
	live := &fakeModel{output: goodReading}
	readSample(t, recordingSample(t, samplePath(dir, "the-speech", 1), live, "claude-haiku-5-5", readingCommand))
	readSample(t, recordingSample(t, samplePath(dir, "the-speech", 1), live, "claude-haiku-5-5", readingCommand))
	if len(live.sent) != 1 {
		t.Errorf("%d calls made for a sample already kept, want 1", len(live.sent))
	}
	keepSample(t, samplePath(dir, "the-speech", 1), []recording{recordedOn(t, changed, "claude-haiku-5-5")})
	readSample(t, recordingSample(t, samplePath(dir, "the-speech", 1), live, "claude-haiku-5-5", readingCommand))
	s, err := sampleAt(samplePath(dir, "the-speech", 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(live.sent) != 2 || len(s.attempts()) != 1 || s.RequestHash == recordedOn(t, changed, "claude-haiku-5-5").RequestHash {
		t.Errorf("the stale sample was not drawn afresh: %d calls, %+v", len(live.sent), s.attempts())
	}
}

// Replaying a sample the act now asks something else is stale.
func TestAReplayedSampleFailsAsStale(t *testing.T) {
	path := samplePath(t.TempDir(), "the-speech", 1)
	keepSample(t, path, []recording{recordedOn(t, changed, "claude-haiku-5-5")})
	var reported []error
	r := replayingSample(t, path, "claude-haiku-5-5", readingCommand, func(err error) { reported = append(reported, err) })
	if readSample(t, r) || len(reported) != 1 || !strings.Contains(reported[0].Error(), "stale: re-record with") {
		t.Errorf("reported %v; want the sample stale", reported)
	}
}

// The recorded readings are judged on their samples: hand-made samples of
// the speech, drawn from a fake model that answers with a reading the act
// takes or one its checks refuse on every attempt, pass at five of five and
// fail at four, undecided, and three, refuted.
func TestTheRecordedReadingsAreJudgedOnTheirSamples(t *testing.T) {
	if *rerecordFlag {
		t.Skip("recording")
	}
	const name = "pm-speech-in-scotland-16-october-2026"
	path := filepath.Join("testdata", "govuk", name+".json")
	it, _ := fixture(t, path)
	good := kept(t, filepath.Join(recordedDir, "placing", "inputs", "reading", name+".json")).Output
	model := pinnedModel(t, "READING_MODEL")
	for _, tc := range []struct {
		passing int
		want    []string
	}{
		{5, nil},
		{4, []string{name, checksProperty, "undecided", "4 of 5"}},
		{3, []string{name, checksProperty, "refuted", "3 of 5"}},
	} {
		root := t.TempDir()
		for k := 1; k <= 5; k++ {
			live := &fakeModel{output: string(good)}
			if k > tc.passing {
				live.output = `{"facts": [{"statement": "Made up.", "as_at": "2026-10-16", "quote": "not in the Item", "entities": []}], "claims": []}`
			}
			m := recordingSample(t, samplePath(filepath.Join(root, "reading"), name, k), live, model, readingCommand)
			if _, err := runSample(m, func(model editor.Model) (bool, error) {
				_, err := editor.Read(t.Context(), model, it)
				return err == nil, nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		errs := readingVerdicts(t, root, path)
		if tc.want == nil {
			if len(errs) != 0 {
				t.Errorf("%d of 5 passing: %v; want every property supported", tc.passing, errs)
			}
			continue
		}
		if len(errs) != 1 {
			t.Errorf("%d of 5 passing: %v; want one failure", tc.passing, errs)
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(errs[0].Error(), w) {
				t.Errorf("%d of 5 passing: %q does not say %q", tc.passing, errs[0], w)
			}
		}
	}
}

// A chained act's fixture is carried on, for the fixtures after it, by its
// first sample to pass its checks. Hand-made samples of placing, each
// fixture's first sample five times over, with the first fixture's first
// sample refused on every attempt: that fixture is undecided, at four of
// five, and every later fixture, placed over its second sample, is judged on
// its own.
func TestAChainedFixtureIsCarriedOnByItsFirstSampleToPass(t *testing.T) {
	t.Parallel()
	chainedFixtureCarriedOn(t, recordedDir)
}

// chainedFixtureCarriedOn is the test above over the samples of placing
// under source.
func chainedFixtureCarriedOn(t *testing.T, source string) {
	t.Helper()
	if *rerecordFlag {
		t.Skip("recording")
	}
	root := t.TempDir()
	if err := os.CopyFS(filepath.Join(root, "placing", "inputs"), os.DirFS(filepath.Join(source, "placing", "inputs"))); err != nil {
		t.Fatal(err)
	}
	fixtures := fixtureItems(t)
	for i, f := range fixtures {
		first, err := sampleAt(samplePath(filepath.Join(source, "placing"), f.name, 1))
		if err != nil {
			t.Fatal(err)
		}
		for k := 1; k <= dailyRule.samples; k++ {
			attempts := first.attempts()
			if i == 0 && k == 1 {
				refusal := attempts[len(attempts)-1]
				refusal.Output = json.RawMessage(`{"not": "a placing"}`)
				attempts = []recording{refusal, refusal, refusal}
			}
			keepSample(t, samplePath(filepath.Join(root, "placing"), f.name, k), attempts)
		}
	}
	errs := placingVerdicts(t, root)
	if len(errs) != 1 {
		t.Fatalf("%d failures %v; want the first fixture's alone", len(errs), errs)
	}
	for _, want := range []string{fixtures[0].name, checksProperty, "undecided", "4 of 5"} {
		if !strings.Contains(errs[0].Error(), want) {
			t.Errorf("%q does not say %q", errs[0], want)
		}
	}
}

// The Sunday read's rule: three samples, supported at three, refuted at one
// or none.
func TestTheSundayReadsRule(t *testing.T) {
	for held, want := range []verdict{refuted, refuted, undecided, supported} {
		if got := sundayReadRule.of(held); got != want {
			t.Errorf("%d of 3: %s, want %s", held, got, want)
		}
	}
}
