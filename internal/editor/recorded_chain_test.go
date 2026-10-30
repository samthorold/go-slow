package editor_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// Each recorded act is replayed over its own frozen copies of the
// recordings it rests on (system design: Each act is recorded over its own
// frozen inputs), kept beside its own recordings under <act>/inputs/<act
// upstream>: placing over readings, connecting over readings and placings,
// the Briefing and the Sunday read over all three. Re-recording one act
// leaves every other act's recordings valid; refreshing an act's copies is a
// change of its own, copying every upstream act's recordings together.

// recordedDir holds the recordings of each act, one directory per act.
var recordedDir = filepath.Join("testdata", "recorded")

// inputsOf is the directory holding act's frozen copies of the recordings
// of upstream, under root.
func inputsOf(root, act, upstream string) string {
	return filepath.Join(root, act, "inputs", upstream)
}

// namedItem is a fixture Item with the name its recordings are kept under.
type namedItem struct {
	name string
	item editor.Item
}

// fixtureItems are the fixture Items in the order they were published, the
// order the recorded acts run over them.
func fixtureItems(t *testing.T) []namedItem {
	t.Helper()
	paths, err := filepath.Glob("testdata/govuk/*.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	var out []namedItem
	for _, path := range paths {
		it, _ := fixture(t, path)
		out = append(out, namedItem{strings.TrimSuffix(filepath.Base(path), ".json"), it})
	}
	slices.SortFunc(out, func(a, b namedItem) int { return a.item.Published.Compare(b.item.Published) })
	return out
}

// newRecord is an empty Record with the fixtures' Source and the Topics
// given.
func newRecord(t *testing.T, topics []interest.Draft) (*sqlite.Store, *interest.Service) {
	t.Helper()
	return newRecordAt(t, filepath.Join(t.TempDir(), "go-slow.db"), topics)
}

// newRecordAt is newRecord, kept at path.
func newRecordAt(t *testing.T, path string, topics []interest.Draft) (*sqlite.Store, *interest.Service) {
	t.Helper()
	ctx := t.Context()
	store, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SyncSources(ctx, []collect.Source{number10}, saturday); err != nil {
		t.Fatal(err)
	}
	svc := interest.New(store)
	for _, d := range topics {
		if _, errs, err := svc.Add(ctx, d); err != nil || errs != nil {
			t.Fatalf("add Topic %s: %v %v", d.Name, errs, err)
		}
	}
	return store, svc
}

// actModel is the model an act runs on for one fixture, by its name.
type actModel func(name string) (editor.Model, error)

// chain is the models the daily acts run on: reading always, placing and
// connecting only when set.
type chain struct {
	reading, placing, connecting actModel
}

// replayed answers each fixture's call from the recording kept for it in
// dir. A missing recording is an error: a frozen copy is complete.
func replayed(dir string) actModel {
	return func(name string) (editor.Model, error) {
		rec, err := keptAt(filepath.Join(dir, name+".json"))
		if err != nil {
			return nil, err
		}
		return replay{rec}, nil
	}
}

// firstSamples answers each fixture's call from the first sample kept for it
// in dir, attempt by attempt: an act's current recordings, as an act
// downstream would be given them.
func firstSamples(dir string) actModel {
	return func(name string) (editor.Model, error) {
		s, err := sampleAt(samplePath(dir, name, 1))
		if err != nil {
			return nil, err
		}
		return &sampleModel{path: samplePath(dir, name, 1), attempts: s.attempts(), unchecked: true}, nil
	}
}

// refused is an act's work on a fixture that failed the act's checks on
// every attempt it had.
type refused struct{ act, fixture, why string }

func (e refused) Error() string {
	return fmt.Sprintf("%s: the %s fails the act's checks:\n%s", e.fixture, e.act, e.why)
}

// attempted runs attempt on m: as a sample, up to its attempts, when m is
// one, otherwise once.
func attempted(m editor.Model, attempt func(editor.Model) (bool, error)) (bool, error) {
	if s, ok := m.(*sampleModel); ok {
		return runSample(s, attempt)
	}
	return attempt(m)
}

// run runs the chain's acts over the fixtures in order into store, as the
// Editor does: each Item is collected and tied to the Topics, read, and its
// Facts placed and connected, so later Facts meet what earlier ones made.
// The first act that fails stops the run, as refused: every later call rests
// on it. (A failed placing leaves its Facts waiting, to be placed with the
// next fixture's in a call no recording holds.)
func (c chain) run(ctx context.Context, store *sqlite.Store, topics *interest.Service, fixtures []namedItem) error {
	for _, f := range fixtures {
		it := f.item
		if _, err := store.AddItems(ctx, []collect.Item{{
			SourceID: it.SourceID, EntryID: it.EntryID, Title: it.Title, Link: it.Link, Summary: it.Summary,
			Content: it.Content, Published: it.Published, Attribution: collect.OGL, CollectedAt: saturday,
		}}); err != nil {
			return err
		}
		if err := topics.CheckNew(ctx); err != nil {
			return err
		}
		var why string
		steps := []struct {
			act   string
			model actModel
			run   func(editor.Model, *slog.Logger) error
		}{
			{"reading", c.reading, func(m editor.Model, _ *slog.Logger) error {
				// What the earlier reading act drew, frozen: the acts built on
				// Facts rest on it.
				reading, err := replayFactReading(ctx, m, it)
				if err != nil {
					return err
				}
				return store.SaveFactReading(ctx, reading, saturday)
			}},
			{"placing", c.placing, func(m editor.Model, log *slog.Logger) error {
				return editor.New(editor.Deps{Store: store, PlacingModel: m, Log: log}).PlaceWaiting(ctx, saturday)
			}},
			{"connecting", c.connecting, func(m editor.Model, log *slog.Logger) error {
				return editor.New(editor.Deps{Store: store, ConnectingModel: m, Log: log}).ConnectWaiting(ctx, saturday)
			}},
		}
		for _, s := range steps {
			if s.model == nil {
				break
			}
			m, err := s.model(f.name)
			if err != nil {
				return err
			}
			passed, err := attempted(m, func(m editor.Model) (bool, error) {
				var logs bytes.Buffer
				why = ""
				if err := s.run(m, slog.New(slog.NewTextHandler(&logs, nil))); err != nil {
					return false, err
				}
				if strings.Contains(logs.String(), s.act+" failed") {
					why = logs.String()
				}
				return why == "", nil
			})
			if err != nil {
				return err
			}
			if !passed {
				return refused{s.act, f.name, why}
			}
		}
	}
	return nil
}

// chainSampled replays (or records) act's samples of each fixture, in the
// order they were published, under dir, and judges each one's properties by
// the rule: each failure, naming its fixture. Each sample of a fixture runs
// over a copy of the Record of the fixtures before it, each carried on by
// its first sample to pass its checks, so every sample of a fixture answers
// one request. c is the chain with act's model the one given; assert asserts
// a sample's test-only properties, for the fixture f, on the Record it left.
// A fixture with no sample passing ends the run: the fixtures after it rest
// on one.
func chainSampled(t *testing.T, dir, act string, topics []interest.Draft, cmd rerecordCmd,
	c func(actModel) chain, assert func(*testing.T, *sqlite.Store, namedItem, properties),
) []error {
	t.Helper()
	ctx := t.Context()
	fixtures := fixtureItems(t)
	model := pinnedModel(t, cmd.modelVar)
	threads := map[string]int{} // a fixture's first sample to pass its checks
	threaded := func(name string) (editor.Model, error) {
		return replayingSample(t, samplePath(dir, name, threads[name]), model, cmd, func(err error) { t.Error(err) }), nil
	}
	// The Record of the fixtures carried on so far, kept closed between
	// fixtures so each sample starts from a copy of its file.
	carried := filepath.Join(t.TempDir(), "carried.db")
	store, _ := newRecordAt(t, carried, topics)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	var errs []error
	for i, f := range fixtures {
		errs = append(errs, sampled(t, dir, f.name, dailyRule, cmd, func(m *sampleModel) sampleResult {
			store := copyOfRecord(t, carried)
			err := c(func(name string) (editor.Model, error) {
				if name == f.name {
					return m, nil
				}
				return threaded(name)
			}).run(ctx, store, interest.New(store), fixtures[i:i+1])
			var r refused
			if errors.As(err, &r) && r.act == act && r.fixture == f.name {
				t.Logf("%s sample %d: refused on its last attempt: %s", f.name, sampleNumber(m.path), r.why)
				return sampleResult{}
			}
			if err != nil {
				t.Fatal(err)
			}
			if threads[f.name] == 0 {
				threads[f.name] = sampleNumber(m.path)
			}
			p := properties{}
			assert(t, store, f, p)
			return sampleResult{passed: true, held: p}
		})...)
		if threads[f.name] == 0 {
			return append(errs, fmt.Errorf("%s: no sample passes its checks, and the fixtures after it rest on one: they are not judged", f.name))
		}
		// Carry the Record on by the fixture's first sample to pass.
		store, err := sqlite.Open(ctx, carried)
		if err != nil {
			t.Fatal(err)
		}
		if err := c(threaded).run(ctx, store, interest.New(store), fixtures[i:i+1]); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return errs
}

// ownDirectory is the recordings root an act's own recorded test replays
// over: a copy holding the act's own directory alone, so a test that reads
// another act's recordings fails; recording, the recordings themselves.
func ownDirectory(t *testing.T, act string) string {
	t.Helper()
	if *rerecordFlag {
		return recordedDir
	}
	root := t.TempDir()
	if err := os.CopyFS(filepath.Join(root, act), os.DirFS(filepath.Join(recordedDir, act))); err != nil {
		t.Fatal(err)
	}
	return root
}

// copyOfRecord opens a copy of the closed Record at path, in a directory of
// its own.
func copyOfRecord(t *testing.T, path string) *sqlite.Store {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // a test's own temporary file
	if err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(t.TempDir(), "go-slow.db")
	if err := os.WriteFile(copied, b, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(t.Context(), copied)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// sampleNumber is the k of the sample kept at path.
func sampleNumber(path string) int {
	var k int
	_, _ = fmt.Sscanf(filepath.Base(path), "%d.json", &k)
	return k
}

// frozenRecordAt is a Record kept at path holding the fixture Items, tied to
// the briefingTopics, with the recordings of reading, placing and connecting
// under dir replayed into it: an act's frozen copies, for the acts that rest
// on all three.
func frozenRecordAt(t *testing.T, path, dir string) *sqlite.Store {
	t.Helper()
	store, topics := newRecordAt(t, path, briefingTopics)
	c := chain{
		reading:    replayed(filepath.Join(dir, "reading")),
		placing:    replayed(filepath.Join(dir, "placing")),
		connecting: replayed(filepath.Join(dir, "connecting")),
	}
	if err := c.run(t.Context(), store, topics, fixtureItems(t)); err != nil {
		t.Fatal(err)
	}
	return store
}

// frozenRecordFile is the file of a closed frozenRecord, to be copied by
// copyOfRecord: one for each sample of an act that rests on it.
func frozenRecordFile(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "frozen.db")
	if err := frozenRecordAt(t, path, dir).Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// Each recorded act downstream of another replays over its own directory
// alone: copied away from every other act's recordings, it replays all the
// way through. So re-recording any act, reading included, changes nothing
// another act replays.
func TestEachRecordedActReplaysOverItsOwnInputsAlone(t *testing.T) {
	t.Parallel()
	if *rerecordFlag {
		t.Skip("recording writes the acts' own directories")
	}
	// Placing, connecting, the Briefing and the precedents' own recorded tests
	// replay over their own directories copied alone (ownDirectory), so they
	// are not replayed twice here.
	for _, tc := range []struct {
		act     string
		replays []func(*testing.T, string)
	}{
		{"sunday", []func(*testing.T, string){recordedSundayRead}},
	} {
		for i, replay := range tc.replays {
			t.Run(fmt.Sprintf("%s/%d", tc.act, i), func(t *testing.T) {
				t.Parallel()
				done := false
				// A replay that skipped tested nothing.
				t.Cleanup(func() {
					if !done && !t.Failed() {
						t.Errorf("%s did not replay all the way over its own directory", tc.act)
					}
				})
				root := t.TempDir()
				if err := os.CopyFS(filepath.Join(root, tc.act), os.DirFS(filepath.Join(recordedDir, tc.act))); err != nil {
					t.Fatal(err)
				}
				replay(t, root)
				done = true
			})
		}
	}
}

// The chain replay: the Briefing's frozen copies and each of its samples are
// one consistent run of the Editor, and over it every statement of the Briefing
// traces through a Development and its Fact to a quote in the Item the Fact
// was drawn from. It asserts only what spans the acts; each act's own
// properties, whether its recording is stale and how many of its samples
// pass its checks, are its recorded test's, so it replays unchecked, over
// every sample of the Briefing that passes.
func TestRecordedChainTracesEveryBriefingStatementToAQuote(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	dir := filepath.Join(recordedDir, "briefing")
	texts := map[string]string{} // EntryID -> the Item's text
	for _, f := range fixtureItems(t) {
		texts[f.item.EntryID] = normal(stripTags(f.item.Title + " " + f.item.Summary + " " + f.item.Content))
	}
	traced := 0
	frozen := frozenRecordFile(t, filepath.Join(dir, "inputs"))
	for k := 1; k <= dailyRule.samples; k++ {
		s, err := sampleAt(samplePath(dir, "saturday", k))
		if err != nil {
			t.Fatal(err)
		}
		store := copyOfRecord(t, frozen)
		if !briefingSample(t, store, &sampleModel{attempts: s.attempts(), unchecked: true}) {
			continue
		}
		b, found, err := store.Briefing(ctx, briefingAt.Format(time.DateOnly))
		if err != nil || !found {
			t.Fatalf("sample %d: no Briefing written: %v %v", k, found, err)
		}
		n := 0
		for _, bs := range b.Stories {
			for _, st := range bs.Statements {
				n++
				if len(st.Developments) == 0 {
					t.Errorf("sample %d: %q: a statement citing no Development: %q", k, bs.Story.Title, st.Text)
				}
				for _, d := range st.Developments {
					text, ok := texts[d.EntryID]
					switch {
					case !ok:
						t.Errorf("sample %d: %q: %q cites a Development whose Fact traces to no Item: %+v", k, bs.Story.Title, st.Text, d)
					case strings.TrimSpace(d.Quote) == "" || !strings.Contains(text, normal(d.Quote)):
						t.Errorf("sample %d: %q: %q cites %q, whose quote is not in its Item: %q", k, bs.Story.Title, st.Text, d.Statement, d.Quote)
					}
				}
			}
		}
		if n == 0 {
			t.Errorf("sample %d: the Briefing makes no statement, so nothing is traced", k)
		}
		traced++
	}
	if traced == 0 {
		t.Fatal("no sample of the Briefing passes its checks, so nothing is traced")
	}
}
