package editor_test

import (
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// editorialBatches is the batch runner over weaving and naming, in the order
// main runs them, keeping every call.
func editorialBatches(store *sqlite.Store, b editor.Batcher) *editor.Batches {
	log := slog.New(slog.DiscardHandler)
	ed := editor.New(editor.Deps{Store: store, Weavings: store, Namings: store, ModelFor: modelFor, Log: log})
	return editor.NewBatches(editor.BatchDeps{
		Store: store, Batcher: b, Acts: []editor.BatchAct{ed.Weaving(weavingAt), ed.Naming()},
		ModelFor: modelFor, Log: log,
	})
}

// woven is a Record whose Editor has made the Story of oneOpinion, its
// naming submitted in the batcher's second batch.
func woven(t *testing.T) (*sqlite.Store, *fakeBatcher, *editor.Batches) {
	t.Helper()
	store := record(t)
	batcher := &fakeBatcher{}
	b := editorialBatches(store, batcher)
	runAt(t, b, saturday)
	answerLast(batcher, oneOpinion)
	runAt(t, b, saturday.Add(time.Hour))
	return store, batcher, b
}

func TestEachStoryTheEditorMadeIsNamedWithItsJudgeAndItsCall(t *testing.T) {
	store, batcher, b := woven(t)
	story := madeStories(t, store)[0]
	if story.Name.Text != "" {
		t.Fatalf("the Story has a Name before naming: %+v", story.Name)
	}
	if len(batcher.submitted) != 2 || len(batcher.submitted[1]) != 1 || batcher.submitted[1][0].Request.Act != "naming" {
		t.Fatalf("submitted %v, want the Story's naming in a second batch", batcher.submitted)
	}
	sent := batcher.submitted[1][0].Request
	for _, want := range []string{
		"Money follows the Prime Minister to Scotland.", "Motherwell is where it lands first.",
		"Scotland will get £2 billion.",
	} {
		if !strings.Contains(sent.Input, want) {
			t.Errorf("naming was not shown %q:\n%s", want, sent.Input)
		}
	}

	answerLast(batcher, `{"name": "Scotland's £2 billion"}`)
	runAt(t, b, saturday.Add(2*time.Hour))

	story = madeStories(t, store)[0]
	n := story.Name
	if n.Text != "Scotland's £2 billion" {
		t.Fatalf("the Story is named %q", n.Text)
	}
	if n.Judge.Act != "naming" || n.Judge.Version != editor.NamingVersion || n.Judge.Model != "fake-reported" ||
		n.Judge.RequestHash != editor.RequestHash(sent, "fake-naming") || !n.At.Equal(saturday.Add(2*time.Hour)) {
		t.Errorf("named by %+v at %v", n.Judge, n.At)
	}
	calls, err := store.CallsOn(t.Context(), "naming", strconv.FormatInt(story.ID, 10))
	if err != nil || len(calls) != 1 || calls[0].Outcome != editor.Applied || n.Call != calls[0].ID {
		t.Errorf("calls %+v (%v), Name's call %d, want the one applied call", calls, err, n.Call)
	}
	if !strings.Contains(batcher.submitted[1][0].Request.Instructions, "at most eight words") ||
		editor.MaxNameWords != 8 {
		t.Errorf("the instructions do not state the check on a Name's length")
	}
	// Named once: nothing more is asked.
	runAt(t, b, saturday.Add(3*time.Hour))
	if len(batcher.submitted) != 2 {
		t.Errorf("submitted %d batches, want no naming of a named Story", len(batcher.submitted))
	}
}

func TestAStoryACutMadeGetsNoName(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := editorialBatches(store, batcher)
	// Before weaving's time of day: the speech's Cut has made a Story, and
	// the Editor none.
	runAt(t, b, saturday.Add(-2*time.Hour))
	if len(batcher.submitted) != 0 {
		t.Errorf("submitted %v, want no naming of a Story a Cut made", batcher.submitted)
	}
}

func TestNoCommandIsShownAName(t *testing.T) {
	store, batcher, b := woven(t)
	answerLast(batcher, `{"name": "Scotland's £2 billion"}`)
	runAt(t, b, saturday.Add(2*time.Hour))
	if madeStories(t, store)[0].Name.Text == "" {
		t.Fatal("the Story was not named")
	}
	// The next day's run, and the naming of what it makes, are shown no Name.
	sunday := saturday.Add(24 * time.Hour)
	cutAt(t, store, sunday)
	runAt(t, b, sunday)
	answerLast(batcher, `{"stories": [{"editor_spans": [{"body": "Ferries follow the money.", "cites": [1]}]}]}`)
	runAt(t, b, sunday.Add(time.Hour))
	if len(batcher.submitted) != 4 {
		t.Fatalf("submitted %d batches, want a second weaving and its naming", len(batcher.submitted))
	}
	for _, batch := range batcher.submitted {
		for _, r := range batch {
			if strings.Contains(r.Request.Input, "Scotland's £2 billion") ||
				strings.Contains(r.Request.Instructions, "Scotland's £2 billion") {
				t.Errorf("%s was shown a Name:\n%s", r.Request.Act, r.Request.Input)
			}
		}
	}
}

func TestANamingFailingAllItsAttemptsIsListedAndNotAskedAgain(t *testing.T) {
	store, batcher, b := woven(t)
	story := madeStories(t, store)[0]
	for i := range editor.MaxAttempts {
		answerLast(batcher, `{"name": ""}`)
		runAt(t, b, saturday.Add(time.Duration(i+2)*time.Hour))
	}
	failed, err := store.FailedWork(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 || failed[0].Act != "naming" || failed[0].Story != story.ID ||
		failed[0].Attempts != editor.MaxAttempts || !strings.Contains(failed[0].Why, "the name is empty") ||
		failed[0].LastCall == 0 {
		t.Errorf("failed work %+v, want the Story's naming", failed)
	}
	runAt(t, b, saturday.Add(24*time.Hour))
	if n := len(batcher.submitted); n != 1+editor.MaxAttempts {
		t.Errorf("submitted %d batches, want no naming after its last attempt", n)
	}
	// The Story is still listed, unnamed.
	if got := madeStories(t, store); len(got) != 1 || got[0].Name.Text != "" {
		t.Errorf("Stories made %+v", got)
	}
}

func TestANamingAnswerFailingItsChecksNamesNothingAndIsAskedAgain(t *testing.T) {
	for name, tc := range map[string]struct{ answer, why string }{
		"no name":             {`{}`, "no name"},
		"an empty name":       {`{"name": "  "}`, "the name is empty"},
		"a name on two lines": {`{"name": "Scotland\nmoney"}`, "more than one line"},
		"a name too long": {
			`{"name": "Money follows the Prime Minister to Scotland and Motherwell"}`, "9 words, more than 8"},
	} {
		t.Run(name, func(t *testing.T) {
			store, batcher, b := woven(t)
			story := madeStories(t, store)[0]
			answerLast(batcher, tc.answer)
			runAt(t, b, saturday.Add(2*time.Hour))

			if got := madeStories(t, store)[0].Name; got.Text != "" {
				t.Errorf("named %+v from an answer failing the checks", got)
			}
			calls, err := store.CallsOn(t.Context(), "naming", strconv.FormatInt(story.ID, 10))
			if err != nil || len(calls) != 1 || calls[0].Outcome != editor.Refused || !strings.Contains(calls[0].Why, tc.why) {
				t.Errorf("calls %+v (%v), want one refused saying %q", calls, err, tc.why)
			}
			if len(batcher.submitted) != 3 || batcher.submitted[2][0].Request.Act != "naming" {
				t.Errorf("submitted %d batches, want the naming asked again in a third", len(batcher.submitted))
			}
		})
	}
}
