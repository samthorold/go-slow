package editor_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/claude"
	"github.com/samthorold/go-slow/internal/editor"
)

// A recording is one sample, and a property is judged by its pass rate
// (system design: The Editor is tested in three tiers). Each fixture of an
// act is sampled rule.samples times when it is recorded; every sample is
// kept, and the suite replays them all and judges each property per fixture
// by the act's rule, declared before the samples were drawn.

// A sample is kept at <act's directory>/<fixture>/<k>.json, k from 1: the
// sample's last attempt as a recording, with the attempts the act's checks
// refused before it. So a frozen copy of a fixture's recording for an act
// downstream is its first sample's file, copied as it is
// (<downstream>/inputs/<act>/<fixture>.json): the first sample, chosen
// before its results are seen, and copied only from samples whose every
// property is supported, so its last attempt is the one that passed.
type sampleFile struct {
	recording
	EarlierAttempts []recording `json:"earlier_attempts,omitempty"`
}

// attempts are the sample's attempts in the order they were made.
func (s sampleFile) attempts() []recording {
	if s.Output == nil {
		return s.EarlierAttempts
	}
	return append(slices.Clone(s.EarlierAttempts), s.recording)
}

// samplePath is where sample k of fixture is kept under dir.
func samplePath(dir, fixture string, k int) string {
	return filepath.Join(dir, fixture, fmt.Sprintf("%d.json", k))
}

// keepSample writes a sample's attempts, made so far, to path.
func keepSample(t *testing.T, path string, attempts []recording) {
	t.Helper()
	var s sampleFile
	if n := len(attempts); n > 0 {
		s = sampleFile{recording: attempts[n-1], EarlierAttempts: attempts[:n-1]}
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// sampleAt is the sample kept at path.
func sampleAt(path string) (sampleFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return sampleFile{}, err
	}
	var s sampleFile
	if err := json.Unmarshal(b, &s); err != nil {
		return sampleFile{}, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// samplesOf is fixture's samples under dir, each its attempts: as many as
// the rule takes, every attempt answering one request, or an error naming
// the fixture and the command that records them.
func samplesOf(dir, fixture string, r rule, cmd rerecordCmd) ([][]recording, error) {
	var out [][]recording
	hash := ""
	for k := 1; ; k++ {
		s, err := sampleAt(samplePath(dir, fixture, k))
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return nil, err
		}
		for _, a := range s.attempts() {
			if hash == "" {
				hash = a.RequestHash
			}
			if a.RequestHash != hash {
				return nil, fmt.Errorf("%s: sample %d answers a request the samples before it did not (request hash %s, not %s); "+
					"its samples are not of one request: delete them and re-record with %s", fixture, k, a.RequestHash, hash, cmd)
			}
		}
		out = append(out, s.attempts())
	}
	if len(out) != r.samples {
		return nil, fmt.Errorf("%s: %d of %d samples recorded; its properties are judged on %d: record them with %s",
			fixture, len(out), r.samples, r.samples, cmd)
	}
	return out, nil
}

// rule is how an act's recorded properties are judged on each fixture: over
// samples samples, a property is supported when it holds in supportedAt,
// refuted when in refutedAtMost or fewer, and undecided between.
type rule struct{ samples, supportedAt, refutedAtMost int }

var (
	// dailyRule is reading's (with its precedent), placing's (with its
	// precedent), connecting's and the Briefing's.
	dailyRule = rule{samples: 5, supportedAt: 5, refutedAtMost: 3}
	// sundayReadRule is the Sunday read's, at dollars a sample: each sample
	// a whole conversation, with its tries (recorded_sunday_test.go).
	sundayReadRule = rule{samples: 3, supportedAt: 3, refutedAtMost: 1}
)

// verdict is what a rule makes of a count.
type verdict string

const (
	supported verdict = "supported"
	undecided verdict = "undecided"
	refuted   verdict = "refuted"
)

func (r rule) of(held int) verdict {
	switch {
	case held >= r.supportedAt:
		return supported
	case held <= r.refutedAtMost:
		return refuted
	}
	return undecided
}

// checksProperty is the property every act has: the work passes the act's
// own checks within its attempts, as it must in use.
const checksProperty = "passes its checks within its attempts"

// sampleResult is one sample of a fixture, replayed: whether it passed its
// checks within its attempts, and, when it did, each test-only property it
// asserted with what failed it (none when it held).
type sampleResult struct {
	passed bool
	held   map[string][]string
}

// judge is every property of fixture that its samples do not support, by
// the rule, as failures naming the fixture, the property and the count.
// Passing its checks is rated over all the samples; a test-only property
// over those that passed them, each one that fails it counting against the
// rule's samples.
func judge(fixture string, r rule, cmd rerecordCmd, samples []sampleResult) []error {
	var errs []error
	fail := func(property string, v verdict, count, detail string) {
		next := "the act is changed before its samples are drawn again"
		if v == undecided {
			next = fmt.Sprintf("read it again on %d fresh samples, judged alone: delete the fixture's samples and re-record with %s", r.samples, cmd)
		}
		errs = append(errs, fmt.Errorf("%s: %q is %s: %s; %s%s", fixture, property, v, count, next, detail))
	}
	passed := 0
	properties := map[string]bool{}
	for _, s := range samples {
		if s.passed {
			passed++
			for p := range s.held {
				properties[p] = true
			}
		}
	}
	if v := r.of(passed); v != supported {
		fail(checksProperty, v, fmt.Sprintf("%d of %d samples pass", passed, len(samples)), "")
	}
	names := make([]string, 0, len(properties))
	for p := range properties {
		names = append(names, p)
	}
	slices.Sort(names)
	for _, p := range names {
		held, first := 0, ""
		for _, s := range samples {
			if !s.passed {
				continue
			}
			why, asserted := s.held[p]
			switch {
			case asserted && len(why) == 0:
				held++
			case first != "":
			case !asserted:
				first = "a sample that passed its checks did not assert it"
			default:
				first = strings.Join(why, "; ")
			}
		}
		failed := passed - held
		if v := r.of(r.samples - failed); v != supported {
			fail(p, v, fmt.Sprintf("%d of the %d samples that passed their checks hold it", held, passed), "\n\tfirst failure: "+first)
		}
	}
	return errs
}

// sampleModel answers an act's attempts at one sample, kept at path:
// replaying, with the attempts kept, each checked against the request the
// act sends now; recording, with them while they are current and then with
// the live model, keeping each attempt as it arrives.
type sampleModel struct {
	t        *testing.T
	path     string
	attempts []recording
	next     int // the attempt to answer next
	live     editor.Model
	model    string // the model a request is hashed with
	cmd      rerecordCmd
	report   func(error)
	// unchecked answers the attempts kept in order, whatever is asked: an
	// act's current recordings given to an act downstream.
	unchecked bool
}

// replayingSample replays the sample kept at path, hashing each request
// with model, the one the act is pinned to; a stale attempt is reported.
func replayingSample(t *testing.T, path, model string, cmd rerecordCmd, report func(error)) *sampleModel {
	t.Helper()
	s, err := sampleAt(path)
	if err != nil {
		t.Fatal(err)
	}
	return &sampleModel{t: t, path: path, attempts: s.attempts(), model: model, cmd: cmd, report: report}
}

// recordingSample records the sample at path on live, asked for model,
// keeping what is already kept there while it answers the request the act
// sends now.
func recordingSample(t *testing.T, path string, live editor.Model, model string, cmd rerecordCmd) *sampleModel {
	t.Helper()
	s, err := sampleAt(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return &sampleModel{t: t, path: path, attempts: s.attempts(), live: live, model: model, cmd: cmd,
		report: func(err error) { t.Error(err) }}
}

func (s *sampleModel) Complete(ctx context.Context, req editor.Request) (editor.Response, error) {
	hash := editor.RequestHash(req, s.model)
	if s.next < len(s.attempts) {
		a := s.attempts[s.next]
		if a.RequestHash == hash || s.unchecked {
			s.next++
			return replay{a}.Complete(ctx, req)
		}
		if s.live == nil {
			err := stale(filepath.Base(filepath.Dir(s.path))+" sample "+strings.TrimSuffix(filepath.Base(s.path), ".json"), a.RequestHash, hash, s.cmd)
			s.report(err)
			return editor.Response{}, err
		}
		// Kept for another request: drawn afresh.
		s.attempts, s.next = nil, 0
	}
	if s.live == nil {
		return editor.Response{}, fmt.Errorf("%s: no attempt kept past the %d made", s.path, len(s.attempts))
	}
	var rec recording
	res, err := recorder{s.live, &rec, s.model}.Complete(ctx, req)
	if err != nil {
		return res, err
	}
	s.attempts = append(s.attempts, rec)
	s.next++
	keepSample(s.t, s.path, s.attempts)
	return res, nil
}

// more is whether another attempt can be made at the sample: in use, up to
// MaxAttempts; replaying, as many as were kept.
func (s *sampleModel) more() bool {
	if s.next >= editor.MaxAttempts {
		return false
	}
	return s.live != nil || s.next < len(s.attempts)
}

// runSample runs one sample on m as the work runs in use: attempt is tried
// on m until it passes the act's checks or no attempt is left. An error is
// the Record's or the harness's, not the act's.
func runSample(m *sampleModel, attempt func(editor.Model) (bool, error)) (bool, error) {
	for {
		before := m.next
		passed, err := attempt(m)
		if err != nil || passed {
			return passed, err
		}
		if m.next == before || !m.more() {
			return false, nil
		}
	}
}

// properties are one sample's test-only properties as they are asserted,
// each with what failed it.
type properties map[string][]string

// property asserts the property named in this sample: it holds unless the
// function it gives is called, with what failed it.
func (p properties) property(name string) func(format string, args ...any) {
	if _, ok := p[name]; !ok {
		p[name] = nil
	}
	return func(format string, args ...any) { p[name] = append(p[name], fmt.Sprintf(format, args...)) }
}

// sampled replays (or, with -record, records) every sample of fixture under
// dir, running each with run, and judges its properties by the rule. run
// runs one sample on its model with runSample, and gives its result: the
// test-only properties it asserted, if it passed its checks.
func sampled(t *testing.T, dir, fixture string, r rule, cmd rerecordCmd, run func(*sampleModel) sampleResult) []error {
	t.Helper()
	var models []*sampleModel
	if *rerecordFlag {
		live, model := liveModel(t, cmd.modelVar)
		for k := 1; k <= r.samples; k++ {
			models = append(models, recordingSample(t, samplePath(dir, fixture, k), live, model, cmd))
		}
	} else {
		if _, err := samplesOf(dir, fixture, r, cmd); err != nil {
			return []error{err}
		}
		model := pinnedModel(t, cmd.modelVar)
		for k := 1; k <= r.samples; k++ {
			models = append(models, replayingSample(t, samplePath(dir, fixture, k), model, cmd, func(err error) { t.Error(err) }))
		}
	}
	results := make([]sampleResult, 0, len(models))
	var total editor.Usage
	for _, m := range models {
		results = append(results, run(m))
		for _, a := range m.attempts {
			total.InputTokens += a.Usage.InputTokens
			total.OutputTokens += a.Usage.OutputTokens
		}
	}
	t.Logf("%s: %d samples, %d input, %d output tokens", fixture, len(models), total.InputTokens, total.OutputTokens)
	return judge(fixture, r, cmd, results)
}

// liveModel is the real model an act is recorded on, named by modelVar.
func liveModel(t *testing.T, modelVar string) (editor.Model, string) {
	t.Helper()
	key, model := os.Getenv("GOSLOW_ANTHROPIC_API_KEY"), os.Getenv(modelVar)
	if key == "" || model == "" {
		t.Fatalf("recording needs GOSLOW_ANTHROPIC_API_KEY and %s", modelVar)
	}
	return claude.New(claude.Options{APIKey: key, Model: model}), model
}

// holds asserts the property named in this sample, failed unless ok.
func (p properties) holds(name string, ok bool, format string, args ...any) {
	fail := p.property(name)
	if !ok {
		fail(format, args...)
	}
}
