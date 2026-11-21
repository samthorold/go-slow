package editor_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// The recorded runs of weaving. A run is a conversation: its first turn is
// shown the Stories the fixture Items' Cuts made, cut by the sentence cutter,
// which is plain code, each Span hydrated by its Item's hydration and shown
// with the Entities it mentions (each name looked up in a Wikidata that
// matches nothing, so an Editor's Entity), and it reaches earlier Stories by
// asking (find, find_entity, open_story), each turn a request of its own. Weaving rests on hydration's recordings, and is recorded over
// its own frozen copies of them,
// testdata/recorded/weaving/inputs/hydration/<fixture>.json, each a fixture's
// first sample (collectAndCut). The compatibility check feeds hydration's
// current recordings to weaving (recorded_compatibility_test.go). The fixtures
// are two runs, a day apart, over the fixture Items in the order they were
// published (weavingRuns): the first shown the Items published up to 15
// October, with nothing earlier in the Record; the second the speech of 16
// October, its asks answered from the earlier Record the first run left:
// the first run's Cut Stories (some bearing on the speech, Burnham's
// devolution and his Commons statement; some not, a Lord-Lieutenant's
// appointment) and the Stories the first run made, carried on by its first
// sample to pass its checks, all of it made from committed recordings, so
// the asks' answers, and each turn's request, are the same on every replay.
// Each run is sampled five times (dailyRule), each sample as in use,
// through the batch runner, every turn and every attempt against the act's
// checks, and kept under testdata/recorded/weaving/<run>/, its calls in the
// order made. Recording costs a call per turn of each sample on Claude Opus
// 5.5, and more for attempts the checks refuse:
//
//	go test -count=1 -timeout 90m ./internal/editor -run '^TestRecordedWeaving$' -record
//
// with GOSLOW_ANTHROPIC_API_KEY and WEAVING_MODEL set in the environment;
// -max-samples 1 draws one sample of each run, to probe before paying for
// five. A fresh recording of the first run makes the second's stale.
//
// What it asserts is limited to what code can say: each run passes its
// checks within its attempts, and every citation of every Story it makes is
// a Span the run was shown, with the words it was shown; and at least one
// sample of the second run reaches an earlier Story through an ask. Whether
// an opinion is any good, or that a run makes any Story, is read in use.
func TestRecordedWeaving(t *testing.T) {
	t.Parallel()
	root := ownDirectory(t, "weaving")
	var live editor.Model
	model := pinnedModel(t, weavingCommand.modelVar)
	if *rerecordFlag {
		live, model = liveModel(t, weavingCommand.modelVar)
	}
	for _, err := range weavingVerdicts(t, root, weavingRuns(t), live, model, func(err error) { t.Error(err) }) {
		t.Error(err)
	}
}

// weavingCommand is how weaving's recorded test is re-recorded.
var weavingCommand = rerecordCmd{test: "TestRecordedWeaving", modelVar: "WEAVING_MODEL", conversation: true}

// weavingRun is one of weaving's fixtures: the Items collected and cut at
// at, and woven at at.
type weavingRun struct {
	name  string
	items []namedItem
	at    time.Time
}

// weavingRuns are weaving's fixtures: Saturday's run over the fixture Items
// published up to 15 October, and Sunday's over those published since.
func weavingRuns(t *testing.T) []weavingRun {
	t.Helper()
	split := time.Date(2026, time.October, 16, 0, 0, 0, 0, time.UTC)
	runs := []weavingRun{{name: "to-15-october", at: saturday}, {name: "16-october", at: saturday.AddDate(0, 0, 1)}}
	for _, f := range fixtureItems(t) {
		i := 0
		if !f.item.Published.Before(split) {
			i = 1
		}
		runs[i].items = append(runs[i].items, f)
	}
	return runs
}

// weavingVerdicts replays (or, with live set, records on live) the samples
// of each run under root, and judges each run's properties by the rule:
// each failure, naming its run. Each sample is hashed with model, and a
// stale one reported to report. Each sample of a run runs over a copy of the
// Record the runs before it left, each carried on by its first sample to
// pass its checks.
func weavingVerdicts(t *testing.T, root string, runs []weavingRun, live editor.Model, model string, report func(error)) []error {
	t.Helper()
	ctx := t.Context()
	dir := filepath.Join(root, "weaving")
	frozen := filepath.Join(dir, "inputs", "hydration")
	// The Record carried on so far, kept closed between runs so each sample
	// starts from a copy of its file.
	carried := filepath.Join(t.TempDir(), "carried.db")
	store, _ := newRecordAt(t, carried, nil)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	var errs []error
	for i, r := range runs {
		thread := 0
		reached := 0
		errs = append(errs, sampledOn(t, dir, r.name, dailyRule, weavingCommand, live, model, report, func(m *sampleModel) sampleResult {
			store := copyOfRecord(t, carried)
			collectAndCut(t, store, r, frozen)
			shown := shownAsNew(t, store)
			before := madeIDs(t, store)
			if i > 0 && len(before) == 0 {
				t.Fatalf("%s: the earlier Record holds no Story the Editor made: the run before it, carried on by its first sample to pass, made none", r.name)
			}
			passed, err := weave(t, store, m, r.at)
			if err != nil {
				t.Fatal(err)
			}
			if !passed {
				return sampleResult{}
			}
			if thread == 0 {
				thread = sampleNumber(m.path)
			}
			if shownByAsks(t, store, r.at.Format(time.DateOnly), shown) {
				reached++
			}
			p := properties{}
			citedShown := p.property("every citation is a Span the run was shown")
			made, err := store.StoriesMade(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, st := range made {
				if slices.Contains(before, st.ID) {
					continue
				}
				for _, es := range st.Spans {
					for _, c := range es.Cites {
						words, ok := shown[citedAt{c.Address, c.Of}]
						switch {
						case !ok:
							citedShown("%q cites %+v, which the run was not shown", es.Body, c)
						case words != c.Words:
							citedShown("%q cites %q, which the run was shown as %q", es.Body, c.Words, words)
						}
					}
				}
			}
			return sampleResult{passed: true, held: p}
		})...)
		if thread == 0 {
			return append(errs, fmt.Errorf("%s: no sample passes its checks, and the runs after it rest on one: they are not judged", r.name))
		}
		if i > 0 && reached == 0 {
			errs = append(errs, fmt.Errorf("%s: no sample reaches an earlier Story through an ask", r.name))
		}
		if i == len(runs)-1 {
			break
		}
		// Carry the Record on by the run's first sample to pass.
		store, err := sqlite.Open(ctx, carried)
		if err != nil {
			t.Fatal(err)
		}
		collectAndCut(t, store, r, frozen)
		threaded := replayingSample(t, samplePath(dir, r.name, thread), model, weavingCommand, report)
		if passed, err := weave(t, store, threaded, r.at); err != nil || !passed {
			t.Fatalf("%s: carrying on by sample %d: passed %v, %v", r.name, thread, passed, err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return errs
}

// collectAndCut collects the run's Items into store at its time, cuts them,
// and hydrates them by weaving's frozen copies of hydration's recordings in
// frozen (system design: Each Command is recorded over its own frozen
// inputs), replayed unchecked. Each Item must have its copy, and be
// hydrated by it: weaving's recordings answer the requests those copies
// make. Then every name hydrated is looked up, in a Wikidata that matches
// nothing (noWikidata), so each is an Editor's Entity, one per name and
// kind, and a run is shown which a Span mentions and can ask by them.
func collectAndCut(t *testing.T, store *sqlite.Store, r weavingRun, frozen string) {
	t.Helper()
	if err := collectCutAndHydrate(t.Context(), store, r, frozen); err != nil {
		t.Fatal(err)
	}
	lu := editor.NewLookups(editor.LookupDeps{Store: store, Wikidata: noWikidata{}, Log: slog.New(slog.DiscardHandler)})
	if err := lu.LookUpWaiting(t.Context(), r.at); err != nil {
		t.Fatal(err)
	}
}

// noWikidata is a Wikidata that holds nothing: the recorded runs make no
// network call, and Wikidata's answers for the fixtures' names are not
// recorded, so a name the fixtures share is one Entity only when hydration
// gives it the same name and kind.
type noWikidata struct{}

func (noWikidata) Search(context.Context, string) ([]string, error) { return nil, nil }

func (noWikidata) Items(context.Context, []string) ([]editor.WikidataItem, error) { return nil, nil }

func (noWikidata) Labels(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

// collectCutAndHydrate is collectAndCut, reporting what fails: an error
// naming the Item whose copy is missing or not taken.
func collectCutAndHydrate(ctx context.Context, store *sqlite.Store, r weavingRun, frozen string) error {
	hydrations := frozenHydrations{}
	for _, f := range r.items {
		it := f.item
		if _, err := store.AddItems(ctx, []collect.Item{{
			SourceID: it.SourceID, EntryID: it.EntryID, Title: it.Title, Link: it.Link, Summary: it.Summary,
			Content: it.Content, Published: it.Published, Attribution: collect.OGL, CollectedAt: r.at,
		}}); err != nil {
			return err
		}
		rec, err := keptAt(filepath.Join(frozen, f.name+".json"))
		if err != nil {
			return fmt.Errorf("%s: no frozen copy of its hydration: %w", f.name, err)
		}
		hydrations["Title: "+it.Title+"\n"] = rec
	}
	if err := cut.New(store).CutNew(ctx, r.at); err != nil {
		return err
	}
	ed := editor.New(editor.Deps{Store: store, Model: hydrations, Log: slog.New(slog.DiscardHandler)})
	if err := ed.RunWaiting(ctx, r.at); err != nil {
		return err
	}
	for _, f := range r.items {
		_, hydrated, err := store.NewestHydration(ctx, f.item.SourceID, f.item.EntryID)
		if err != nil {
			return err
		}
		if !hydrated {
			return fmt.Errorf("%s: not hydrated by its copy of hydration's recording: hydration's checks refuse it", f.name)
		}
	}
	return nil
}

// frozenHydrations answers each hydration request with the frozen copy kept for
// the Item it shows, by the title line it opens with, unchecked.
type frozenHydrations map[string]recording

func (f frozenHydrations) Complete(ctx context.Context, req editor.Request) (editor.Response, error) {
	for title, rec := range f {
		if strings.HasPrefix(req.Input, title) {
			return replay{rec}.Complete(ctx, req)
		}
	}
	return editor.Response{}, fmt.Errorf("no frozen copy of a hydration for %.80q", req.Input)
}

// keptAt is the recording kept at path: for a sample's file, its last
// attempt, the one that passed when the sample did.
func keptAt(path string) (recording, error) {
	s, err := sampleAt(path)
	return s.recording, err
}

// citedAt is a Span a run may be shown: a Source's, by its address, or an
// Editor's, by its.
type citedAt struct {
	at editor.Address
	of editor.EditorSpanAt
}

// shownAsNew is every Span a weaving run over store would be shown as new
// now, with its words as written: those of the Stories cut since the last
// run that their hydration does not Reject. It reads the Record, not the
// request, so a Story holding a citation the run was not shown fails,
// however the act numbered it.
func shownAsNew(t *testing.T, store *sqlite.Store) map[citedAt]string {
	t.Helper()
	ctx := t.Context()
	shown := map[citedAt]string{}
	stories, err := store.StoriesToWeave(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stories {
		r, _, err := store.NewestHydration(ctx, s.Item.SourceID, s.Item.EntryID)
		if err != nil {
			t.Fatal(err)
		}
		for _, sp := range s.Spans {
			at := editor.Address{SourceID: s.Item.SourceID, EntryID: s.Item.EntryID, Span: sp}
			if !slices.Contains(r.Rejected, at) {
				shown[citedAt{at: at}] = cut.Words(s.Item.Content, sp)
			}
		}
	}
	return shown
}

// answeredStory is a Story an ask's answer names, a catalogue's entry or the
// Story opened, at the start of a line.
var answeredStory = regexp.MustCompile(`(?m)^\s*Story (\d+),`)

// shownByAsks adds to shown every Span of each earlier Story the answers to
// the run keyed key's asks named, as its kept calls hold them, with its
// words as written: a Story a Cut made, its Spans; one the Editor made, its
// Editor's Spans and what they cite. It reports whether any answer named
// one: whether the run reached an earlier Story through an ask. A catalogue
// numbers only the Spans that match, so this is looser than what the run was
// shown: it catches a citation of a Story no ask reached.
func shownByAsks(t *testing.T, store *sqlite.Store, key string, shown map[citedAt]string) bool {
	t.Helper()
	ctx := t.Context()
	calls, err := store.CallsOn(ctx, "weaving", key)
	if err != nil {
		t.Fatal(err)
	}
	named := map[int64]bool{}
	for _, c := range calls {
		for _, m := range c.Request.Messages {
			for _, r := range m.Results {
				for _, id := range answeredStory.FindAllStringSubmatch(r.Content, -1) {
					n, _ := strconv.ParseInt(id[1], 10, 64)
					named[n] = true
				}
			}
		}
	}
	cuts, err := store.StoriesCutThrough(ctx, math.MaxInt64)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range cuts {
		if named[s.CutID] {
			for _, sp := range s.Spans {
				shown[citedAt{at: editor.Address{SourceID: s.Item.SourceID, EntryID: s.Item.EntryID, Span: sp}}] = cut.Words(s.Item.Content, sp)
			}
		}
	}
	made, err := store.StoriesMade(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range made {
		if !named[st.ID] {
			continue
		}
		for n, es := range st.Spans {
			shown[citedAt{of: editor.EditorSpanAt{Story: st.ID, N: n + 1}}] = es.Body
			for _, c := range es.Cites {
				shown[citedAt{c.Address, c.Of}] = c.Words
			}
		}
	}
	return len(named) > 0
}

// madeIDs are the ids of the Stories the Editor has made in store.
func madeIDs(t *testing.T, store *sqlite.Store) []int64 {
	t.Helper()
	made, err := store.StoriesMade(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, 0, len(made))
	for _, st := range made {
		ids = append(ids, st.ID)
	}
	return ids
}

// maxTurns bounds the batch runner's turns a sample of weaving is given,
// every turn of its conversation and every attempt at one: a sample still
// under way after them has not passed.
const maxTurns = 40

// weave runs one sample of weaving on m over store, as in use: through the
// batch runner from at, its batches answered at once by m, turn after turn
// until no batch is pending (each turn of the conversation, and up to
// MaxAttempts of each against the act's checks), at most maxTurns. It
// reports whether the run's result was written. An error is the Record's, or
// a live call lost (errLost), which stops the sample unkept.
func weave(t *testing.T, store *sqlite.Store, m *sampleModel, at time.Time) (bool, error) {
	t.Helper()
	ctx := t.Context()
	log := slog.New(slog.DiscardHandler)
	modelFor := func(string) string { return m.model }
	ed := editor.New(editor.Deps{Store: store, Weavings: store, ModelFor: modelFor, Log: log})
	b := editor.NewBatches(editor.BatchDeps{Store: store, Batcher: &modelBatcher{model: m, prefix: at.Format(time.DateOnly)},
		Acts: []editor.BatchAct{ed.Weaving(weavingAt)}, ModelFor: modelFor, Log: log})
	for turn := range maxTurns {
		if err := b.RunWaiting(ctx, at.Add(time.Duration(turn)*time.Minute)); err != nil {
			return false, err
		}
		if m.lost != nil {
			return false, m.lost
		}
		pending, err := store.PendingBatches(ctx)
		if err != nil {
			return false, err
		}
		if len(pending) == 0 {
			break
		}
	}
	key := at.Format(time.DateOnly)
	ran, err := store.WeavingRan(ctx, key)
	if err != nil {
		return false, err
	}
	failed, err := store.FailedWork(ctx)
	if err != nil {
		return false, err
	}
	return ran && !slices.ContainsFunc(failed, func(f editor.FailedWork) bool { return f.Act == "weaving" && f.Title == key }), nil
}

// modelBatcher is a batch interface answering each request of a batch on
// model as it is submitted, so the batch has ended by the next turn. An
// answer that came back as an error (a refusal, an answer cut off) is that
// request's result; any other error (a stale replay, a live call lost)
// fails the submission, as a batch interface that is down does.
type modelBatcher struct {
	model   editor.Model
	prefix  string
	batches [][]editor.BatchResult
}

func (b *modelBatcher) Submit(ctx context.Context, rs []editor.BatchRequest) (string, error) {
	results := make([]editor.BatchResult, 0, len(rs))
	for _, r := range rs {
		res, err := b.model.Complete(ctx, r.Request)
		if err != nil && !errors.Is(err, editor.ErrRefused) && !errors.Is(err, editor.ErrTruncated) {
			return "", err
		}
		results = append(results, editor.BatchResult{ID: r.ID, Response: res, Err: err})
	}
	b.batches = append(b.batches, results)
	return fmt.Sprintf("%s-%d", b.prefix, len(b.batches)), nil
}

func (b *modelBatcher) Poll(_ context.Context, id string) (editor.BatchPoll, error) {
	var n int
	if _, err := fmt.Sscanf(id, b.prefix+"-%d", &n); err != nil || n < 1 || n > len(b.batches) {
		return editor.BatchPoll{}, fmt.Errorf("no batch %s", id)
	}
	return editor.BatchPoll{Ended: true, Results: b.batches[n-1]}, nil
}
