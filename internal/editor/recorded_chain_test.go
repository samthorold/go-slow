package editor_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/claude"
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
	ctx := t.Context()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "go-slow.db"))
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

// recordedOrReplayed is how a recorded test runs its own act on each
// fixture under dir: on the real model named by modelVar when recording,
// keeping each response as it arrives, otherwise replayed. Replaying, the
// first fixture not yet recorded skips the rest of the run, which rests on
// it, naming the command that records it. Each call's usage adds to total.
func recordedOrReplayed(t *testing.T, dir, modelVar, test string, total *editor.Usage) actModel {
	t.Helper()
	return func(name string) (editor.Model, error) {
		path := filepath.Join(dir, name+".json")
		if *rerecordFlag {
			key, model := os.Getenv("GOSLOW_ANTHROPIC_API_KEY"), os.Getenv(modelVar)
			if key == "" || model == "" {
				t.Fatalf("recording needs GOSLOW_ANTHROPIC_API_KEY and %s", modelVar)
			}
			return keeping{t, claude.New(claude.Options{APIKey: key, Model: model}), path, total}, nil
		}
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			t.Skipf("%s has not been recorded yet; record it with: "+
				"go test -count=1 ./internal/editor -run %s -record "+
				"(GOSLOW_ANTHROPIC_API_KEY and %s set)", name, test, modelVar)
		}
		rec, err := keptAt(path)
		if err != nil {
			return nil, err
		}
		total.InputTokens += rec.Usage.InputTokens
		total.OutputTokens += rec.Usage.OutputTokens
		return replay{rec}, nil
	}
}

// keeping passes a call to the real model and keeps the response at once,
// whether or not it passes the act's checks: the replay shows if it doesn't.
type keeping struct {
	t     *testing.T
	model editor.Model
	path  string
	total *editor.Usage
}

func (k keeping) Complete(ctx context.Context, req editor.Request) (editor.Response, error) {
	var rec recording
	res, err := recorder{k.model, &rec}.Complete(ctx, req)
	if err != nil {
		return res, err
	}
	keep(k.t, k.path, rec)
	k.total.InputTokens += res.Usage.InputTokens
	k.total.OutputTokens += res.Usage.OutputTokens
	return res, nil
}

// run runs the chain's acts over the fixtures in order into store, as the
// Editor does: each Item is collected and tied to the Topics, read, and its
// Facts placed and connected, so later Facts meet what earlier ones made.
// The first act that fails stops the run: every later call rests on it.
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
		m, err := c.reading(f.name)
		if err != nil {
			return err
		}
		reading, err := editor.Read(ctx, m, it)
		if err != nil {
			return fmt.Errorf("%s: the reading fails its checks: %w", f.name, err)
		}
		if err := store.SaveReading(ctx, reading, saturday); err != nil {
			return err
		}
		if c.placing == nil {
			continue
		}
		var logs bytes.Buffer
		log := slog.New(slog.NewTextHandler(&logs, nil))
		if m, err = c.placing(f.name); err != nil {
			return err
		}
		if err := editor.New(editor.Deps{Store: store, PlacingModel: m, Log: log}).PlaceWaiting(ctx, saturday); err != nil {
			return err
		}
		// A failed placing leaves its Facts waiting, to be placed with the
		// next fixture's in a call no recording holds.
		if strings.Contains(logs.String(), "placing failed") {
			return fmt.Errorf("%s: the placing fails the act's checks:\n%s", f.name, logs.String())
		}
		if c.connecting == nil {
			continue
		}
		if m, err = c.connecting(f.name); err != nil {
			return err
		}
		if err := editor.New(editor.Deps{Store: store, ConnectingModel: m, Log: log}).ConnectWaiting(ctx, saturday); err != nil {
			return err
		}
		if strings.Contains(logs.String(), "connecting failed") {
			return fmt.Errorf("%s: the connecting fails the act's checks:\n%s", f.name, logs.String())
		}
	}
	return nil
}

// frozenRecord is a Record holding the fixture Items, tied to the
// briefingTopics, with the recordings of reading, placing and connecting
// under dir replayed into it: an act's frozen copies, for the acts that rest
// on all three.
func frozenRecord(t *testing.T, dir string) *sqlite.Store {
	t.Helper()
	store, topics := newRecord(t, briefingTopics)
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

// Each recorded act downstream of another replays over its own directory
// alone: copied away from every other act's recordings, it replays all the
// way through. So re-recording any act, reading included, changes nothing
// another act replays.
func TestEachRecordedActReplaysOverItsOwnInputsAlone(t *testing.T) {
	if *rerecordFlag {
		t.Skip("recording writes the acts' own directories")
	}
	for _, tc := range []struct {
		act     string
		replays []func(*testing.T, string)
	}{
		{"placing", []func(*testing.T, string){recordedPlacings}},
		{"connecting", []func(*testing.T, string){recordedConnectings}},
		{"briefing", []func(*testing.T, string){recordedBriefing}},
		{"precedent", []func(*testing.T, string){recordedPlacingPrecedent, recordedReadingPrecedent}},
		{"sunday", []func(*testing.T, string){recordedSundayRead}},
	} {
		for i, replay := range tc.replays {
			done := false
			t.Run(fmt.Sprintf("%s/%d", tc.act, i), func(t *testing.T) {
				root := t.TempDir()
				if err := os.CopyFS(filepath.Join(root, tc.act), os.DirFS(filepath.Join(recordedDir, tc.act))); err != nil {
					t.Fatal(err)
				}
				replay(t, root)
				done = true
			})
			// A replay that skipped tested nothing.
			if !done && !t.Failed() {
				t.Errorf("%s did not replay all the way over its own directory", tc.act)
			}
		}
	}
}

// The chain replay: the Briefing's frozen copies and its recording are one
// consistent run of the Editor, and over it every statement of the Briefing
// traces through a Development and its Fact to a quote in the Item the Fact
// was drawn from. It asserts only what spans the acts; each act's own
// properties are its recorded test's.
func TestRecordedChainTracesEveryBriefingStatementToAQuote(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(recordedDir, "briefing", "saturday.json")
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		t.Skip("the Briefing has not been recorded yet")
	}
	texts := map[string]string{} // EntryID -> the Item's text
	for _, f := range fixtureItems(t) {
		texts[f.item.EntryID] = normal(stripTags(f.item.Title + " " + f.item.Summary + " " + f.item.Content))
	}
	store := frozenRecord(t, filepath.Join(recordedDir, "briefing", "inputs"))
	at := saturday.Add(time.Hour)
	var logs bytes.Buffer
	ed := editor.New(editor.Deps{Store: store, Briefings: store, BriefingModel: replay{kept(t, path)},
		Log: slog.New(slog.NewTextHandler(&logs, nil))})
	if err := ed.Brief(ctx, at); err != nil {
		t.Fatal(err)
	}
	b, found, err := store.Briefing(ctx, at.Format(time.DateOnly))
	if err != nil || !found {
		t.Fatalf("no Briefing written: %v %v\n%s", found, err, logs.String())
	}
	n := 0
	for _, bs := range b.Stories {
		for _, st := range bs.Statements {
			n++
			if len(st.Developments) == 0 {
				t.Errorf("%q: a statement citing no Development: %q", bs.Story.Title, st.Text)
			}
			for _, d := range st.Developments {
				text, ok := texts[d.EntryID]
				switch {
				case !ok:
					t.Errorf("%q: %q cites a Development whose Fact traces to no Item: %+v", bs.Story.Title, st.Text, d)
				case strings.TrimSpace(d.Quote) == "" || !strings.Contains(text, normal(d.Quote)):
					t.Errorf("%q: %q cites %q, whose quote is not in its Item: %q", bs.Story.Title, st.Text, d.Statement, d.Quote)
				}
			}
		}
	}
	if n == 0 {
		t.Fatal("the Briefing makes no statement, so nothing is traced")
	}
}
