package editor_test

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
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

// refusedReading is a reading of the speech the act's checks refuse: it
// cites words the speech's Span does not have.
const refusedReading = `{"names": [{"at": {"span": 1, "words": "not in the Item"}, "kind": "place", "name": "Nowhere", "office": ""}],
	"refers_to": [], "rejected": [], "speakers": []}`

// refusedThenRead answers its first call with output the reading act's
// checks refuse, and every later one with a reading they take.
type refusedThenRead struct{ calls int }

func (m *refusedThenRead) Complete(context.Context, editor.Request) (editor.Response, error) {
	m.calls++
	if m.calls == 1 {
		return editor.Response{Output: json.RawMessage(refusedReading), Model: "fake"}, nil
	}
	return editor.Response{Output: json.RawMessage(goodReading), Model: "fake"}, nil
}

// readSample runs one sample of reading the speech on m, as in use: up to
// its attempts against the act's checks.
func readSample(t *testing.T, m *sampleModel) bool {
	t.Helper()
	passed, err := runSample(m, func(model editor.Model) (bool, error) {
		_, err := editor.Read(t.Context(), model, "", toRead(speech))
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
	live := &fakeModel{output: refusedReading}
	m := recordingSample(t, path, live, "claude-haiku-5-5", readingCommand)
	if readSample(t, m) {
		t.Fatal("a refused sample passed")
	}
	if len(live.sent) != editor.MaxAttempts {
		t.Errorf("%d attempts made, want %d", len(live.sent), editor.MaxAttempts)
	}
	if s, err := sampleAt(path); err != nil || len(s.attempts()) != editor.MaxAttempts {
		t.Errorf("kept %+v, %v; want the sample with its %d refused attempts", s, err, editor.MaxAttempts)
	}
	r := replayingSample(t, path, "claude-haiku-5-5", readingCommand, func(err error) { t.Error(err) })
	if readSample(t, r) {
		t.Error("replayed, the refused sample passed")
	}
}

// erring answers its first errs calls with an error, as a provider that
// answers nothing does, and every later one as answer does.
type erring struct {
	errs   int
	calls  int
	answer editor.Model
}

func (m *erring) Complete(ctx context.Context, r editor.Request) (editor.Response, error) {
	m.calls++
	if m.calls <= m.errs {
		return editor.Response{}, errors.New("overloaded")
	}
	return m.answer.Complete(ctx, r)
}

// A live call that errors, answering nothing, is not an attempt at the
// sample: it is made again, and the sample is kept with the attempt that
// answered.
func TestALiveErrorWhileRecordingIsTriedAgain(t *testing.T) {
	path := samplePath(t.TempDir(), "the-speech", 1)
	live := &erring{errs: 1, answer: &fakeModel{output: goodReading}}
	m := recordingSample(t, path, live, "claude-haiku-5-5", readingCommand)
	m.pause = 0
	if !readSample(t, m) {
		t.Fatal("the sample did not pass once the live call answered")
	}
	s, err := sampleAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.attempts(); len(got) != 1 || got[0].RequestHash == "" {
		t.Errorf("kept %d attempts %+v; want the one that answered", len(got), got)
	}
}

// A live call that answers nothing on every try stops the sample with the
// error, and nothing of it is kept: no empty sample counting as failing.
func TestALiveCallAnsweringNothingStopsTheRecordingUnkept(t *testing.T) {
	path := samplePath(t.TempDir(), "the-speech", 1)
	live := &erring{errs: liveTries, answer: &fakeModel{output: goodReading}}
	m := recordingSample(t, path, live, "claude-haiku-5-5", readingCommand)
	m.pause = 0
	passed, err := runSample(m, func(model editor.Model) (bool, error) {
		_, err := editor.Read(t.Context(), model, "", toRead(speech))
		return err == nil, nil
	})
	if !errors.Is(err, errLost) || passed {
		t.Errorf("the sample ran to %v, %v; want it stopped, the live call lost", passed, err)
	}
	if live.calls != liveTries {
		t.Errorf("%d live calls, want %d", live.calls, liveTries)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a sample is kept: %v", err)
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
	// A reading judging nothing passes the act's checks, and the speech has
	// no case a property of its own rests on.
	good := `{"names": [], "refers_to": [], "rejected": [], "speakers": []}`
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
			live := &fakeModel{output: good}
			if k > tc.passing {
				live.output = refusedReading
			}
			m := recordingSample(t, samplePath(filepath.Join(root, "reading"), name, k), live, model, readingCommand)
			if _, err := runSample(m, func(model editor.Model) (bool, error) {
				_, err := editor.Read(t.Context(), model, "", fixtureToRead(it))
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
// first sample to pass its checks. Hand-made samples of relating, each
// fixture's first sample five times over, with the first fixture to make a
// call refused on every attempt of its first sample: that fixture is
// undecided, at four of five, and every later fixture, related over its
// second sample, is judged on its own.
func TestAChainedFixtureIsCarriedOnByItsFirstSampleToPass(t *testing.T) {
	t.Parallel()
	chainedFixtureCarriedOn(t, recordedDir)
}

// chainedFixtureCarriedOn is the test above over the samples of relating
// under source.
func chainedFixtureCarriedOn(t *testing.T, source string) {
	t.Helper()
	if *rerecordFlag {
		t.Skip("recording")
	}
	root := t.TempDir()
	if err := os.CopyFS(filepath.Join(root, "relating", "inputs"), os.DirFS(filepath.Join(source, "relating", "inputs"))); err != nil {
		t.Fatal(err)
	}
	refused := ""
	for _, f := range fixtureItems(t) {
		first, err := sampleAt(samplePath(filepath.Join(source, "relating"), f.name, 1))
		if err != nil {
			t.Fatal(err)
		}
		for k := 1; k <= dailyRule.samples; k++ {
			attempts := first.attempts()
			if len(attempts) > 0 && (refused == "" || refused == f.name) && k == 1 {
				refused = f.name
				refusal := attempts[len(attempts)-1]
				refusal.Output = json.RawMessage(`{"not": "a relating"}`)
				attempts = []recording{refusal, refusal, refusal}
			}
			keepSample(t, samplePath(filepath.Join(root, "relating"), f.name, k), attempts)
		}
	}
	errs := relatingVerdicts(t, root)
	if len(errs) != 1 {
		t.Fatalf("%d failures %v; want %s's alone", len(errs), errs, refused)
	}
	for _, want := range []string{refused, checksProperty, "undecided", "4 of 5"} {
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
