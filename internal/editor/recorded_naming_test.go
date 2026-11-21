package editor_test

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// The recorded runs of naming. Its input is a Story the Editor made, so
// naming rests on weaving's recordings, and those on hydration's: it is
// recorded over its own frozen copies of both,
// testdata/recorded/naming/inputs/weaving/<run>.json, each weaving run's
// first sample, and testdata/recorded/naming/inputs/hydration/<fixture>.json,
// the hydrations those samples were recorded over (weaving's Stories cite the
// Spans they were shown by number, which only the hydrations they were shown
// make). Weaving's two runs are replayed from the copies in order (wovenRecord),
// and each Story they make is a fixture of naming's, <run>-story-<n>. The
// compatibility check feeds weaving's current recordings to naming
// (recorded_compatibility_test.go). Each Story is sampled five times
// (dailyRule), each sample as in use, through the batch runner, up to
// MaxAttempts against the act's checks, and kept under
// testdata/recorded/naming/<fixture>/. Recording costs five calls per Story
// on Claude Haiku 5.5, and more for attempts the checks refuse:
//
//	go test -count=1 -timeout 90m ./internal/editor -run '^TestRecordedNaming$' -record
//
// with GOSLOW_ANTHROPIC_API_KEY and NAMING_MODEL set in the environment;
// -max-samples 1 draws one sample of each Story, to probe before paying for
// five.
//
// Its properties, beside passing its checks within its attempts: no Name a
// Story is given is a Name another Story is given in any of its samples
// (namesDiffer). Nothing asserts that a Name uses only its Story's words: a
// Name is only for the Owner to recognise a Story by in a list, so any plain
// subject will do.
func TestRecordedNaming(t *testing.T) {
	t.Parallel()
	root := ownDirectory(t, "naming")
	var live editor.Model
	model := pinnedModel(t, namingCommand.modelVar)
	if *rerecordFlag {
		live, model = liveModel(t, namingCommand.modelVar)
	}
	for _, err := range namingVerdicts(t, root, live, model, func(err error) { t.Error(err) }) {
		t.Error(err)
	}
}

// namingCommand is how naming's recorded test is re-recorded.
var namingCommand = rerecordCmd{test: "TestRecordedNaming", modelVar: "NAMING_MODEL"}

// namesDiffer is naming's property beside its checks.
const namesDiffer = "its Name is no other Story's"

// storyToName is one of naming's fixtures: a Story weaving's frozen copies
// make, by the name its samples are kept under and its id in the Record.
type storyToName struct {
	name string
	id   int64
}

// namedAt is when naming's samples run: after both of weaving's runs.
var namedAt = saturday.AddDate(0, 0, 2)

// namingVerdicts replays (or, with live set, records on live) the samples
// of naming each Story under root, and judges each Story's properties by the
// rule: each failure, naming its Story. Each sample is hashed with model,
// and a stale one reported to report. Each sample runs over a copy of the
// Record naming's frozen copies of weaving make.
func namingVerdicts(t *testing.T, root string, live editor.Model, model string, report func(error)) []error {
	t.Helper()
	dir := filepath.Join(root, "naming")
	record, stories, err := wovenRecord(t, filepath.Join(dir, "inputs"))
	if err != nil {
		return []error{err}
	}
	var errs []error
	// The Names each Story's samples that passed their checks gave it, in
	// order, and each Story's results to judge.
	names := map[string][]string{}
	results := map[string][]sampleResult{}
	for _, s := range stories {
		rs, err := drawSamples(t, dir, s.name, dailyRule, namingCommand, live, model, report, func(m *sampleModel) sampleResult {
			named, err := nameStory(t, copyOfRecord(t, record), m, s.id)
			if err != nil {
				t.Fatal(err)
			}
			if named == "" {
				return sampleResult{}
			}
			names[s.name] = append(names[s.name], named)
			return sampleResult{passed: true}
		})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		results[s.name] = rs
	}
	for _, s := range stories {
		rs, ok := results[s.name]
		if !ok {
			continue
		}
		i := 0
		for k := range rs {
			if !rs[k].passed {
				continue
			}
			p := properties{}
			differs := p.property(namesDiffer)
			n := names[s.name][i]
			i++
			for _, other := range stories {
				if other.name == s.name {
					continue
				}
				if slices.ContainsFunc(names[other.name], func(o string) bool { return sameName(n, o) }) {
					differs("%q is also a Name of %s", n, other.name)
				}
			}
			rs[k].held = p
		}
		errs = append(errs, judge(s.name, dailyRule, namingCommand, rs)...)
	}
	return errs
}

// sameName is whether two Names are one to a reader scanning a list: the
// same words, whatever their case or spacing.
func sameName(a, b string) bool {
	return strings.EqualFold(strings.Join(strings.Fields(a), " "), strings.Join(strings.Fields(b), " "))
}

// storiesToName are the Stories the frozen copies under frozen make, naming's
// fixtures.
func storiesToName(t *testing.T, frozen string) []storyToName {
	t.Helper()
	_, stories, err := wovenRecord(t, frozen)
	if err != nil {
		t.Fatal(err)
	}
	return stories
}

// wovenRecord is the Record weaving's two runs make over the frozen copies
// under frozen, closed at a file of its own: each run's Items collected, cut
// and hydrated by hydration's copies (frozen/hydration/), then woven by weaving's
// copy of that run (frozen/weaving/<run>.json), replayed unchecked, as the
// act that made it checked it. With it, each Story the runs make, in the
// order made. A missing copy, or one weaving's checks refuse, is an error
// naming its run.
func wovenRecord(t *testing.T, frozen string) (string, []storyToName, error) {
	t.Helper()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "woven.db")
	store, _ := newRecordAt(t, path, nil)
	var stories []storyToName
	for _, r := range weavingRuns(t) {
		if err := collectCutAndHydrate(ctx, store, r, filepath.Join(frozen, "hydration")); err != nil {
			return "", nil, err
		}
		before := madeIDs(t, store)
		copied := filepath.Join(frozen, "weaving", r.name+".json")
		s, err := sampleAt(copied)
		if err != nil {
			return "", nil, fmt.Errorf("%s: no frozen copy of its weaving: %w", r.name, err)
		}
		if err := weaveFrozen(t, store, r, copied, s); err != nil {
			return "", nil, err
		}
		made := madeIDs(t, store)
		slices.Sort(made) // in the order the run wrote them
		n := 0
		for _, id := range made {
			if slices.Contains(before, id) {
				continue
			}
			n++
			stories = append(stories, storyToName{fmt.Sprintf("%s-story-%d", r.name, n), id})
		}
	}
	if err := store.Close(); err != nil {
		return "", nil, err
	}
	return path, stories, nil
}

// weaveFrozen weaves the run r over store by s, a frozen copy of one of
// weaving's samples kept at copied, as the act that made it checked it. A
// copy of a conversation (its turns kept as they came back) is replayed
// unchecked, every attempt in order, through the batch runner. A copy
// recorded before a run was a conversation, weaving's version 3, keeps no
// turn as it came back: it is kept as version 3 numbered what it was shown
// (editor.KeepAsWeavingVersion3), at the time a run's result was applied.
func weaveFrozen(t *testing.T, store *sqlite.Store, r weavingRun, copied string, s sampleFile) error {
	t.Helper()
	attempts := s.attempts()
	if !slices.ContainsFunc(attempts, func(a recording) bool { return a.Raw != nil }) {
		ed := editor.New(editor.Deps{Weavings: store, Log: slog.New(slog.DiscardHandler)})
		if err := editor.KeepAsWeavingVersion3(t.Context(), ed, r.at.Format(time.DateOnly), r.at.Add(time.Minute),
			editor.Response{Output: s.Output, Model: s.Model, Usage: s.Usage}); err != nil {
			return fmt.Errorf("%s: its weaving is not taken: %w", r.name, err)
		}
		return nil
	}
	m := &sampleModel{t: t, path: copied, attempts: attempts, model: "frozen", unchecked: true,
		cmd: weavingCommand, report: func(error) {}}
	passed, err := weave(t, store, m, r.at)
	if err != nil {
		return fmt.Errorf("%s: its weaving is not taken: %w", r.name, err)
	}
	if !passed {
		return fmt.Errorf("%s: its weaving is not taken: weaving's checks refuse it", r.name)
	}
	return nil
}

// oneStory is the Record as naming one Story sees it: that Story alone is
// waiting to be named.
type oneStory struct {
	editor.NamingStore
	id int64
}

func (o oneStory) StoriesToName(ctx context.Context) ([]editor.Story, error) {
	all, err := o.NamingStore.StoriesToName(ctx)
	return slices.DeleteFunc(all, func(s editor.Story) bool { return s.ID != o.id }), err
}

// nameStory runs one sample of naming the Story id on m over store, as in
// use: through the batch runner, its batches answered at once by m, turn
// after turn until no batch is pending, up to MaxAttempts against the act's
// checks. It gives the Name kept, empty if none was. An error is the
// Record's, or a live call lost (errLost), which stops the sample unkept.
func nameStory(t *testing.T, store *sqlite.Store, m *sampleModel, id int64) (string, error) {
	t.Helper()
	ctx := t.Context()
	log := slog.New(slog.DiscardHandler)
	modelFor := func(string) string { return m.model }
	ed := editor.New(editor.Deps{Store: store, Namings: oneStory{store, id}, ModelFor: modelFor, Log: log})
	b := editor.NewBatches(editor.BatchDeps{Store: store, Batcher: &modelBatcher{model: m, prefix: "naming"},
		Acts: []editor.BatchAct{ed.Naming()}, ModelFor: modelFor, Log: log})
	for turn := range editor.MaxAttempts + 1 {
		if err := b.RunWaiting(ctx, namedAt.Add(time.Duration(turn)*time.Minute)); err != nil {
			return "", err
		}
		if m.lost != nil {
			return "", m.lost
		}
		pending, err := store.PendingBatches(ctx)
		if err != nil {
			return "", err
		}
		if len(pending) == 0 {
			break
		}
	}
	made, err := store.StoriesMade(ctx)
	if err != nil {
		return "", err
	}
	for _, s := range made {
		if s.ID == id {
			return s.Name.Text, nil
		}
	}
	return "", fmt.Errorf("no Story %d in the Record", id)
}
