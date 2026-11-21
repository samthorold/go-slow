package editor_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// weavingAt is the time of day weaving runs from in these tests; saturday
// is an hour after it.
const weavingAt = 6 * time.Hour

// weavingBatches is the batch runner over weaving alone, keeping every call.
func weavingBatches(store *sqlite.Store, b editor.Batcher) *editor.Batches {
	log := slog.New(slog.DiscardHandler)
	ed := editor.New(editor.Deps{Store: store, Weavings: store, ModelFor: modelFor, Log: log})
	return editor.NewBatches(editor.BatchDeps{
		Store: store, Batcher: b, Acts: []editor.BatchAct{ed.Weaving(weavingAt)}, ModelFor: modelFor, Log: log,
	})
}

// runAt is the batch runner's turn after a Collection run ending at now.
func runAt(t *testing.T, b *editor.Batches, now time.Time) {
	t.Helper()
	if err := b.RunWaiting(t.Context(), now); err != nil {
		t.Fatal(err)
	}
}

// oneOpinion is an answer to a weaving run shown the speech, S1 "It’s good
// to be back in Scotland, to be here in Motherwell." and S2 "Scotland will
// get £2 billion.": one Story of two Editor's Spans.
const oneOpinion = `{"stories": [{"editor_spans": [
  {"body": "Money follows the Prime Minister to Scotland.", "cites": [1, 2]},
  {"body": "Motherwell is where it lands first.", "cites": [1]}
]}]}`

// madeStories are the Stories the Editor made, newest first.
func madeStories(t *testing.T, store *sqlite.Store) []editor.Story {
	t.Helper()
	got, err := store.StoriesMade(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestAWeavingRunMakesAStoryOfEachOpinionWithItsJudgeAndItsCall(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	if len(batcher.submitted) != 1 || len(batcher.submitted[0]) != 1 {
		t.Fatalf("submitted %v, want one weaving request", batcher.submitted)
	}
	sent := batcher.submitted[0][0].Request
	if sent.Act != "weaving" || sent.Effort != editor.EffortMedium ||
		!strings.Contains(sent.Input, "S1: It’s good to be back in Scotland, to be here in Motherwell.\nS2: Scotland will get £2 billion.") {
		t.Errorf("sent %s at %q effort:\n%s", sent.Act, sent.Effort, sent.Input)
	}

	batcher.end(1, func(editor.BatchRequest) editor.BatchResult {
		return editor.BatchResult{Response: answered(oneOpinion)}
	})
	runAt(t, b, saturday.Add(time.Hour))

	stories := madeStories(t, store)
	if len(stories) != 1 {
		t.Fatalf("made %d Stories, want 1", len(stories))
	}
	s := stories[0]
	if len(s.Spans) != 2 || s.Spans[0].Body != "Money follows the Prime Minister to Scotland." ||
		len(s.Spans[0].Cites) != 2 || s.Spans[0].Cites[1].Words != "Scotland will get £2 billion." ||
		s.Spans[1].Cites[0].Words != "It’s good to be back in Scotland, to be here in Motherwell." {
		t.Errorf("Story holds %+v", s.Spans)
	}
	if c := s.Spans[0].Cites[0].Address; c.SourceID != speech.SourceID || c.EntryID != speech.EntryID {
		t.Errorf("cites %+v, want the speech", c)
	}
	if s.Judge.Act != "weaving" || s.Judge.Version != editor.WeavingVersion || s.Judge.Model != "fake-reported" ||
		s.Judge.RequestHash != editor.RequestHash(sent, "fake-weaving") || !s.At.Equal(saturday.Add(time.Hour)) {
		t.Errorf("made by %+v at %v", s.Judge, s.At)
	}
	calls, err := store.CallsOn(t.Context(), "weaving", "2026-10-24")
	if err != nil || len(calls) != 1 || calls[0].Outcome != editor.Applied || s.Call != calls[0].ID {
		t.Errorf("calls %+v (%v), Story's call %d, want the one applied call", calls, err, s.Call)
	}
}

func TestAWeavingAnswerFailingItsChecksMakesNothingAndIsAskedAgain(t *testing.T) {
	for name, tc := range map[string]struct{ answer, why string }{
		"a citation of a Span not shown": {
			`{"stories": [{"editor_spans": [{"body": "Words.", "cites": [3]}]}]}`, "cites S3, which was not shown"},
		"an Editor's Span with no words": {
			`{"stories": [{"editor_spans": [{"body": " ", "cites": [1]}]}]}`, "has no words"},
		"an Editor's Span with no citation": {
			`{"stories": [{"editor_spans": [{"body": "Words.", "cites": []}]}]}`, "cites no Span"},
		"a Story with no Editor's Spans": {
			`{"stories": [{"editor_spans": []}]}`, "has no editor_spans"},
	} {
		t.Run(name, func(t *testing.T) {
			store := record(t)
			batcher := &fakeBatcher{}
			b := weavingBatches(store, batcher)
			runAt(t, b, saturday)
			batcher.end(1, func(editor.BatchRequest) editor.BatchResult {
				return editor.BatchResult{Response: answered(tc.answer)}
			})
			runAt(t, b, saturday.Add(time.Hour))

			if got := madeStories(t, store); len(got) != 0 {
				t.Errorf("made %+v from an answer failing the checks", got)
			}
			calls, err := store.CallsOn(t.Context(), "weaving", "2026-10-24")
			if err != nil || len(calls) != 1 || calls[0].Outcome != editor.Refused || !strings.Contains(calls[0].Why, tc.why) {
				t.Errorf("calls %+v (%v), want one refused saying %q", calls, err, tc.why)
			}
			if len(batcher.submitted) != 2 || batcher.submitted[1][0].Request.Act != "weaving" {
				t.Errorf("submitted %d batches, want the run asked again in a second", len(batcher.submitted))
			}
		})
	}
}

// cutAt adds the ferries Item to the Record, collected and cut at at.
func cutAt(t *testing.T, store *sqlite.Store, at time.Time) {
	t.Helper()
	it := ferries
	it.CollectedAt = at
	if _, err := store.AddItems(t.Context(), []collect.Item{it}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(store).CutNew(t.Context(), at); err != nil {
		t.Fatal(err)
	}
}

// answerLast answers the last batch submitted with output.
func answerLast(batcher *fakeBatcher, output string) {
	batcher.end(len(batcher.submitted), func(editor.BatchRequest) editor.BatchResult {
		return editor.BatchResult{Response: answered(output)}
	})
}

func TestARunWithNothingToSaySucceedsAndTheNextIsShownOnlyWhatWasCutSince(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	answerLast(batcher, `{"stories": []}`)
	runAt(t, b, saturday.Add(time.Hour))

	if got := madeStories(t, store); len(got) != 0 {
		t.Errorf("made %+v from an answer with no stories", got)
	}
	calls, err := store.CallsOn(t.Context(), "weaving", "2026-10-24")
	if err != nil || len(calls) != 1 || calls[0].Outcome != editor.Applied {
		t.Errorf("calls %+v (%v), want one applied", calls, err)
	}
	// The next day, nothing new has been cut: no run.
	sunday := saturday.Add(24 * time.Hour)
	runAt(t, b, sunday)
	if len(batcher.submitted) != 1 {
		t.Fatalf("submitted %d batches with nothing new cut, want none after the first", len(batcher.submitted))
	}
	// Once something is cut, the next day's run is shown it alone.
	cutAt(t, store, sunday)
	runAt(t, b, sunday.Add(24*time.Hour))
	if len(batcher.submitted) != 2 {
		t.Fatalf("submitted %d batches, want a second run", len(batcher.submitted))
	}
	in := batcher.submitted[1][0].Request.Input
	if strings.Contains(in, "Motherwell") || !strings.Contains(in, "S1: Ferries to Scotland's islands will run more often.") {
		t.Errorf("the second run was shown:\n%s", in)
	}
}

func TestARunFailingAllItsAttemptsIsListedAndItsStoriesAreShownToTheNext(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	for i := range editor.MaxAttempts {
		answerLast(batcher, `{"stories": [{"editor_spans": []}]}`)
		runAt(t, b, saturday.Add(time.Duration(i+1)*time.Hour))
	}
	if len(batcher.submitted) != editor.MaxAttempts {
		t.Fatalf("submitted %d batches, want one per attempt", len(batcher.submitted))
	}

	failed, err := store.FailedWork(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 || failed[0].Act != "weaving" || failed[0].Attempts != editor.MaxAttempts ||
		!strings.Contains(failed[0].Why, "has no editor_spans") || failed[0].LastCall == 0 {
		t.Errorf("failed work %+v, want the weaving run", failed)
	}
	// No other run that day; the next day's is shown what the failed run was.
	runAt(t, b, saturday.Add(12*time.Hour))
	if len(batcher.submitted) != editor.MaxAttempts {
		t.Fatalf("submitted %d batches, want no other run the day one failed", len(batcher.submitted))
	}
	runAt(t, b, saturday.Add(24*time.Hour))
	if len(batcher.submitted) != editor.MaxAttempts+1 ||
		!strings.Contains(batcher.submitted[editor.MaxAttempts][0].Request.Input, "Motherwell") {
		t.Errorf("the next day's run was not shown the failed run's Stories")
	}
}

func TestWeavingRunsOnTheFirstRunAtOrAfterItsTimeOfDay(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	sixAM := time.Date(2026, time.October, 24, 6, 0, 0, 0, time.UTC)

	runAt(t, b, sixAM.Add(-time.Minute))
	if len(batcher.submitted) != 0 {
		t.Fatalf("submitted %d batches before the time of day", len(batcher.submitted))
	}
	runAt(t, b, sixAM)
	if len(batcher.submitted) != 1 {
		t.Fatalf("submitted %d batches at the time of day, want one", len(batcher.submitted))
	}
}

func TestWeavingRunsAtMostOnceADay(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	answerLast(batcher, oneOpinion)
	runAt(t, b, saturday.Add(time.Hour))

	cutAt(t, store, saturday.Add(2*time.Hour))
	runAt(t, b, saturday.Add(3*time.Hour))
	if len(batcher.submitted) != 1 {
		t.Errorf("submitted %d batches, want no second run the same day", len(batcher.submitted))
	}
	runAt(t, b, saturday.Add(24*time.Hour))
	if len(batcher.submitted) != 2 {
		t.Errorf("submitted %d batches, want the next day's run", len(batcher.submitted))
	}
}

func TestNoRunIsSubmittedWhileARunsBatchIsPending(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	sunday := saturday.Add(24 * time.Hour)
	cutAt(t, store, sunday)

	runAt(t, b, sunday)
	if len(batcher.submitted) != 1 {
		t.Fatalf("submitted %d batches, want none while Saturday's run is pending", len(batcher.submitted))
	}
	// Once Saturday's ends and is applied, Sunday's run is shown what was
	// cut since.
	answerLast(batcher, oneOpinion)
	runAt(t, b, sunday.Add(time.Hour))
	if len(madeStories(t, store)) != 1 {
		t.Errorf("Saturday's run was not applied")
	}
	if len(batcher.submitted) != 2 || strings.Contains(batcher.submitted[1][0].Request.Input, speechAsCut) {
		t.Errorf("submitted %d batches, want Sunday's run shown only what was cut since", len(batcher.submitted))
	}
}

// wovenThenFerries is a Record whose Saturday run made oneOpinion's Story,
// with the ferries Item cut on Sunday, and the runner whose Sunday run is
// next.
func wovenThenFerries(t *testing.T) (*sqlite.Store, *fakeBatcher, *editor.Batches, time.Time) {
	t.Helper()
	store := record(t)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	answerLast(batcher, oneOpinion)
	runAt(t, b, saturday.Add(time.Hour))
	sunday := saturday.Add(24 * time.Hour)
	cutAt(t, store, sunday)
	return store, batcher, b, sunday
}

// A run is shown only what is new: the Stories cut since the last run, and
// nothing earlier, the Editor's own Stories included, which it reaches only
// by asking (system design: A weaving run is shown what is new, and finds for
// itself what bears on it).
func TestARunIsShownOnlyTheStoriesCutSinceTheLastRunAndCanAskForEarlierOnes(t *testing.T) {
	_, batcher, b, sunday := wovenThenFerries(t)
	runAt(t, b, sunday)
	if len(batcher.submitted) != 2 {
		t.Fatalf("submitted %d batches, want Sunday's run", len(batcher.submitted))
	}
	want := `Story from "Ferries to the islands", published 2026-10-24
S1: Ferries to Scotland's islands will run more often.

`
	sent := batcher.submitted[1][0].Request
	if sent.Input != want || len(sent.Messages) != 0 {
		t.Errorf("Sunday's run was shown:\n%s\n%+v\nwant:\n%s", sent.Input, sent.Messages, want)
	}
	var tools []string
	for _, tool := range sent.Tools {
		tools = append(tools, tool.Name)
	}
	if strings.Join(tools, " ") != "find find_entity open_story" {
		t.Errorf("the run can call %v, want find, find_entity and open_story", tools)
	}
}

// asking is a turn of the model's calling tools: the name and input of
// each, in order.
func asking(calls ...string) editor.Response {
	res := answered("")
	res.StopReason = "tool_use"
	for i := 0; i+1 < len(calls); i += 2 {
		res.Calls = append(res.Calls, editor.ToolCall{ID: fmt.Sprintf("call-%d", i/2+1), Name: calls[i],
			Input: json.RawMessage(calls[i+1])})
	}
	raw, _ := json.Marshal(map[string]any{"id": "msg", "asked": calls})
	res.Raw = raw
	return res
}

// respondLast answers the last batch submitted with res.
func respondLast(batcher *fakeBatcher, res editor.Response) {
	batcher.end(len(batcher.submitted), func(editor.BatchRequest) editor.BatchResult {
		return editor.BatchResult{Response: res}
	})
}

// lastSent is the request the last batch submitted sent.
func lastSent(t *testing.T, batcher *fakeBatcher) editor.Request {
	t.Helper()
	if len(batcher.submitted) == 0 {
		t.Fatal("nothing submitted")
	}
	return batcher.submitted[len(batcher.submitted)-1][0].Request
}

// answerOf is the result the request's last message sends back for the call.
func answerOf(t *testing.T, req editor.Request, callID string) string {
	t.Helper()
	if len(req.Messages) == 0 {
		t.Fatal("the request carries no conversation")
	}
	for _, r := range req.Messages[len(req.Messages)-1].Results {
		if r.CallID == callID {
			return r.Content
		}
	}
	t.Fatalf("no result for %s in %+v", callID, req.Messages[len(req.Messages)-1])
	return ""
}

// A run reaches an earlier Story by asking for words: the next turn, in a
// batch of its own, is sent the conversation so far with a catalogue of
// the earlier Stories holding them, its own included, each matched Span
// numbered on from those shown; and its answer may cite them (system design:
// A run asks by words, Entities and citations, and is answered with a
// catalogue).
func TestARunAsksForWordsAndCitesWhatTheCatalogueShowsIt(t *testing.T) {
	store, batcher, b, sunday := wovenThenFerries(t)
	earlier := madeStories(t, store)[0]
	runAt(t, b, sunday)
	respondLast(batcher, asking("find", `{"words": "motherwell"}`))
	runAt(t, b, sunday.Add(time.Hour))

	if len(batcher.submitted) != 3 {
		t.Fatalf("submitted %d batches, want the run's second turn in a third", len(batcher.submitted))
	}
	turn2 := lastSent(t, batcher)
	if turn2.Input != batcher.submitted[1][0].Request.Input || len(turn2.Messages) != 2 ||
		turn2.Messages[0].Assistant == nil || len(turn2.Messages[0].Assistant.Calls) != 1 {
		t.Fatalf("the second turn sent %+v, want the first turn's input, then its turn and the results", turn2.Messages)
	}
	catalogue := answerOf(t, turn2, "call-1")
	for _, want := range []string{
		fmt.Sprintf("Story %d, the Editor's, made 2026-10-24: \"Money follows the Prime Minister to Scotland.\"", earlier.ID),
		"S2 (the Editor's): Motherwell is where it lands first.\n",
		`from "PM speech in Scotland: 16 October 2026", published 2026-10-16, from govuk-number-10; cited by 2 later Editor's Spans`,
		"S3: It’s good to be back in Scotland, to be here in Motherwell.\n",
	} {
		if !strings.Contains(catalogue, want) {
			t.Errorf("the catalogue:\n%s\nwant it to hold %q", catalogue, want)
		}
	}

	answerLast(batcher, `{"stories": [{"editor_spans": [
	  {"body": "The money, Motherwell and the ferries are one promise.", "cites": [1, 2, 3]}
	]}]}`)
	runAt(t, b, sunday.Add(2*time.Hour))

	stories := madeStories(t, store)
	if len(stories) != 2 {
		t.Fatalf("made %d Stories, want a second", len(stories))
	}
	cites := stories[0].Spans[0].Cites
	if len(cites) != 3 || cites[0].Address.EntryID != ferries.EntryID ||
		cites[1].Of != (editor.EditorSpanAt{Story: earlier.ID, N: 2}) || cites[1].Words != "Motherwell is where it lands first." ||
		cites[2].Address.EntryID != speech.EntryID || cites[2].Words != "It’s good to be back in Scotland, to be here in Motherwell." {
		t.Errorf("cites %+v, want the ferries, the earlier Editor's Span by address and the speech", cites)
	}
	calls, err := store.CallsOn(t.Context(), "weaving", "2026-10-25")
	if err != nil || len(calls) != 2 || calls[0].Outcome != editor.Continued || calls[1].Outcome != editor.Applied ||
		stories[0].Call != calls[1].ID || stories[0].Judge.RequestHash != editor.RequestHash(turn2, "fake-weaving") {
		t.Errorf("calls %+v (%v), Story's call %d, want the turn that asked continued and the one that answered applied",
			calls, err, stories[0].Call)
	}
}

// A run's turns go a batch at a time, over as many Collection runs as it
// takes, and while any of them is under way no other run is submitted, nor
// asked for by the Owner, however many days pass.
func TestARunThatAsksSpansCollectionRunsAndNoOtherRunGoesMeanwhile(t *testing.T) {
	store, batcher, b, sunday := wovenThenFerries(t)
	runAt(t, b, sunday)
	respondLast(batcher, asking("find", `{"words": "Scotland"}`))
	runAt(t, b, sunday.Add(time.Hour))
	respondLast(batcher, asking("find", `{"words": "billion"}`, "find", `{"words": "nothing at all"}`))
	runAt(t, b, sunday.Add(2*time.Hour))
	if len(batcher.submitted) != 4 {
		t.Fatalf("submitted %d batches, want the run's third turn in a fourth", len(batcher.submitted))
	}
	third := lastSent(t, batcher)
	if len(third.Messages) != 4 || !strings.Contains(answerOf(t, third, "call-2"), `No earlier Story has a Span with the words "nothing at all".`) {
		t.Errorf("the third turn sent %+v", third.Messages)
	}

	monday := sunday.Add(24 * time.Hour)
	it := ferries
	it.EntryID, it.CollectedAt = "ferries-again", monday
	if _, err := store.AddItems(t.Context(), []collect.Item{it}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(store).CutNew(t.Context(), monday); err != nil {
		t.Fatal(err)
	}
	runAt(t, b, monday)
	if len(batcher.submitted) != 4 {
		t.Errorf("submitted %d batches, want no run on Monday while Sunday's is under way", len(batcher.submitted))
	}
	if _, err := weavingEditor(store).WeaveToday(t.Context(), monday); !errors.Is(err, editor.ErrWeavingPending) {
		t.Errorf("today's run asked for while one is under way: %v, want ErrWeavingPending", err)
	}

	answerLast(batcher, `{"stories": []}`)
	runAt(t, b, monday.Add(time.Hour))
	if len(batcher.submitted) != 5 || strings.Contains(lastSent(t, batcher).Input, "S2") ||
		lastSent(t, batcher).Input != "Story from \"Ferries to the islands\", published 2026-10-24\nS1: Ferries to Scotland's islands will run more often.\n\n" {
		t.Errorf("submitted %d batches, the last shown:\n%s\nwant Monday's run shown what was cut since Sunday's", len(batcher.submitted),
			lastSent(t, batcher).Input)
	}
}

// A catalogue shows the first few Spans of a Story matching throughout and
// says how many more match, so a broad ask doesn't cost what it matched.
func TestACatalogueShowsTheFirstFewMatchesOfAStoryAndHowManyMore(t *testing.T) {
	store := record(t)
	it := ferries
	it.EntryID, it.Content = "many-ferries", strings.Repeat("<p>A ferry sails.</p><p>Gulls follow.</p>", 7)
	if _, err := store.AddItems(t.Context(), []collect.Item{it}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(store).CutNew(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	answerLast(batcher, `{"stories": []}`)
	runAt(t, b, saturday.Add(time.Hour))
	sunday := saturday.Add(24 * time.Hour)
	cutAt(t, store, sunday)
	runAt(t, b, sunday)
	respondLast(batcher, asking("find", `{"words": "ferry"}`))
	runAt(t, b, sunday.Add(time.Hour))

	got := answerOf(t, lastSent(t, batcher), "call-1")
	if n := strings.Count(got, ": A ferry sails."); n != 5 || !strings.Contains(got, "… and 2 more Spans of it hold the words") {
		t.Errorf("the catalogue shows %d matches:\n%s\nwant 5, and 2 more said", n, got)
	}
}

func TestACitationOfANumberNotShownIsRefusedWhateverTheEditorHasMade(t *testing.T) {
	store, batcher, b, sunday := wovenThenFerries(t)
	runAt(t, b, sunday)
	answerLast(batcher, `{"stories": [{"editor_spans": [{"body": "Words.", "cites": [2]}]}]}`)
	runAt(t, b, sunday.Add(time.Hour))

	if got := madeStories(t, store); len(got) != 1 {
		t.Errorf("made %d Stories, want none beside the first", len(got))
	}
	calls, err := store.CallsOn(t.Context(), "weaving", "2026-10-25")
	if err != nil || len(calls) != 1 || calls[0].Outcome != editor.Refused || !strings.Contains(calls[0].Why, "S1 to S1") {
		t.Errorf("calls %+v (%v), want one refused for citing a Span not shown", calls, err)
	}
}

func TestARunWithNothingNewlyCutIsSkippedWhateverTheEditorHasMade(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	answerLast(batcher, oneOpinion)
	runAt(t, b, saturday.Add(time.Hour))
	if len(madeStories(t, store)) != 1 {
		t.Fatal("Saturday's run made no Story")
	}
	runAt(t, b, saturday.Add(24*time.Hour))
	if len(batcher.submitted) != 1 {
		t.Errorf("submitted %d batches with nothing newly cut, want none after the first", len(batcher.submitted))
	}
}

func TestAResultArrivingAfterTheRecordHasMovedOnIsAppliedToWhatTheRunWasShown(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	// Cut after the request went: a run built now would be shown it as S3.
	cutAt(t, store, saturday.Add(30*time.Minute))
	answerLast(batcher, oneOpinion)
	runAt(t, b, saturday.Add(time.Hour))

	if got := madeStories(t, store); len(got) != 1 {
		t.Fatalf("made %d Stories, want the late result applied", len(got))
	}
	calls, err := store.CallsOn(t.Context(), "weaving", "2026-10-24")
	if err != nil || len(calls) != 1 || calls[0].Outcome != editor.Applied {
		t.Errorf("calls %+v (%v), want one applied", calls, err)
	}
	// What was cut after the request went is the next run's.
	runAt(t, b, saturday.Add(24*time.Hour))
	if len(batcher.submitted) != 2 {
		t.Fatalf("submitted %d batches, want the next run", len(batcher.submitted))
	}
	if in := batcher.submitted[1][0].Request.Input; !strings.Contains(in, `Story from "Ferries to the islands"`) ||
		strings.Contains(in, speechAsCut) {
		t.Errorf("the next run was not shown what was cut after the last was sent, alone:\n%s", in)
	}
}

// speechAsCut heads the speech's Story as a run shown it newly cut.
const speechAsCut = `Story from "PM speech in Scotland: 16 October 2026"`

func TestALateResultCitingASpanCutAfterItsRequestWentIsRefused(t *testing.T) {
	store := record(t)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	cutAt(t, store, saturday.Add(30*time.Minute))
	answerLast(batcher, `{"stories": [{"editor_spans": [{"body": "Ferries.", "cites": [3]}]}]}`)
	runAt(t, b, saturday.Add(time.Hour))

	calls, err := store.CallsOn(t.Context(), "weaving", "2026-10-24")
	if err != nil || len(calls) != 1 || calls[0].Outcome != editor.Refused || !strings.Contains(calls[0].Why, "S1 to S2") {
		t.Errorf("calls %+v (%v), want one refused for citing a Span not shown", calls, err)
	}
}
