package editor_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// lookUp runs the look-up step over store on wd at at.
func lookUp(t *testing.T, store *sqlite.Store, wd editor.Wikidata, at time.Time) {
	t.Helper()
	lu, logs := lookups(store, wd)
	if err := lu.LookUpWaiting(t.Context(), at); err != nil {
		t.Fatalf("%v\n%s", err, logs)
	}
}

// burnhamThenAgain is a Record whose Saturday run was shown the release
// "earlier", hydrated as naming Andy Burnham by earlierNaming (S1 "Andy
// Burnham said:", with S4 "He visited the site on Monday." referring to
// him), and wrote nothing; with, cut since, the release "later", naming him
// "Burnham" as Prime Minister, every name looked up on wd after each was
// hydrated; and the runner whose Sunday run is next.
func burnhamThenAgain(t *testing.T, earlierNaming string, wd editor.Wikidata) (*sqlite.Store, *fakeBatcher, *editor.Batches, time.Time) {
	t.Helper()
	store := hydratedRecord(t, earlierNaming, releaseOn("earlier", speech.Published))
	lookUp(t, store, wd, saturday)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	answerLast(batcher, `{"stories": []}`)
	runAt(t, b, saturday.Add(time.Hour))
	sunday := saturday.Add(24 * time.Hour)
	hydrateMore(t, store, naming("Burnham", pm), releaseOn("later", speech.Published.AddDate(0, 0, 1)))
	lookUp(t, store, primeMinisters(), sunday)
	return store, batcher, b, sunday
}

// A run is shown which Entities each Span mentions, by an id it can ask by,
// and asking by one is answered with a catalogue of the earlier Stories whose
// Spans mention it, whatever they call it: by its name, or by words that
// refer to it (system design: A run asks by words, Entities and citations,
// and is answered with a catalogue).
func TestARunAsksByAnEntityItWasShownAndFindsSpansMentioningItInOtherWords(t *testing.T) {
	store, batcher, b, sunday := burnhamThenAgain(t, naming("Andy Burnham", pm), primeMinisters())
	runAt(t, b, sunday)
	first := lastSent(t, batcher)
	if !strings.Contains(first.Input, "S1 (mentioning Andy Burnham {Q269909}): Andy Burnham said:\n") ||
		!strings.Contains(first.Input, "S4 (mentioning Andy Burnham {Q269909}): ⟦Andy Burnham⟧ visited the site on Monday.\n") {
		t.Errorf("Sunday's run was shown:\n%s\nwant each Span saying it mentions Andy Burnham", first.Input)
	}
	var tools []string
	for _, tool := range first.Tools {
		tools = append(tools, tool.Name)
	}
	if strings.Join(tools, " ") != "find find_entity open_story" {
		t.Errorf("the run can call %v, want find, find_entity and open_story", tools)
	}

	respondLast(batcher, asking("find_entity", `{"entity": "Q269909"}`))
	runAt(t, b, sunday.Add(time.Hour))
	earlier, _, _, err := store.NewestCut(t.Context(), number10.ID, "earlier")
	if err != nil {
		t.Fatal(err)
	}
	catalogue := answerOf(t, lastSent(t, batcher), "call-1")
	for _, want := range []string{
		"1 earlier Story has Spans mentioning Andy Burnham {Q269909}.\n",
		fmt.Sprintf("Story %d, from \"GBR headquarters in Derby\", published 2026-10-16", earlier.ID),
		"  S5 (mentioning Andy Burnham {Q269909}): Andy Burnham said:\n",
		"  S6 (mentioning Andy Burnham {Q269909}): ⟦Andy Burnham⟧ visited the site on Monday.\n",
	} {
		if !strings.Contains(catalogue, want) {
			t.Errorf("the catalogue:\n%s\nwant it to hold %q", catalogue, want)
		}
	}

	answerLast(batcher, `{"stories": [{"editor_spans": [{"body": "He keeps coming back.", "cites": [1, 6]}]}]}`)
	runAt(t, b, sunday.Add(2*time.Hour))
	made := madeStories(t, store)
	if len(made) != 1 || made[0].Spans[0].Cites[1].Address.EntryID != "earlier" ||
		made[0].Spans[0].Cites[1].Words != "He visited the site on Monday." {
		t.Errorf("made %+v, want a Story citing the earlier release's S4", made)
	}
}

// An Editor's Entity merged into a Wikidata item since is asked for as the
// item, and found where the Spans that mention it were looked up before the
// merge (system design: Entities come from Wikidata first, otherwise from
// the Editor).
func TestAnEditorsEntityMergedIntoAWikidataItemIsFoundByAskingForTheItem(t *testing.T) {
	_, batcher, b, sunday := burnhamThenAgain(t, naming("Andy Burnham", ""), &fakeWikidata{})
	runAt(t, b, sunday)
	respondLast(batcher, asking("find_entity", `{"entity": "Q269909"}`))
	runAt(t, b, sunday.Add(time.Hour))

	catalogue := answerOf(t, lastSent(t, batcher), "call-1")
	for _, want := range []string{
		"1 earlier Story has Spans mentioning Andy Burnham {Q269909}.\n",
		"  S5 (mentioning Andy Burnham {Q269909}): Andy Burnham said:\n",
		"  S6 (mentioning Andy Burnham {Q269909}): ⟦Andy Burnham⟧ visited the site on Monday.\n",
	} {
		if !strings.Contains(catalogue, want) {
			t.Errorf("the catalogue:\n%s\nwant it to hold %q", catalogue, want)
		}
	}
}

// resultOf is the result the request's last message sends back for the
// call.
func resultOf(t *testing.T, req editor.Request, callID string) editor.ToolResult {
	t.Helper()
	if len(req.Messages) == 0 {
		t.Fatal("the request carries no conversation")
	}
	for _, r := range req.Messages[len(req.Messages)-1].Results {
		if r.CallID == callID {
			return r
		}
	}
	t.Fatalf("no result for %s in %+v", callID, req.Messages[len(req.Messages)-1])
	return editor.ToolResult{}
}

// Asking for an Entity no Span the run was shown mentions is answered as an
// error the model reads, and the run goes on to its next turn.
func TestAskingForAnEntityTheRunWasNeverShownIsAnErrorItReads(t *testing.T) {
	_, batcher, b, sunday := burnhamThenAgain(t, naming("Andy Burnham", pm), primeMinisters())
	runAt(t, b, sunday)
	respondLast(batcher, asking("find_entity", `{"entity": "Q6555365"}`, "find_entity", `{"entity": "{q269909}"}`))
	runAt(t, b, sunday.Add(time.Hour))

	if len(batcher.submitted) != 3 {
		t.Fatalf("submitted %d batches, want the run's next turn in a third", len(batcher.submitted))
	}
	next := lastSent(t, batcher)
	if got := resultOf(t, next, "call-1"); !got.IsError ||
		!strings.Contains(got.Content, `No Span shown to you mentions an Entity with the id "Q6555365"`) {
		t.Errorf("asking for Keir Starmer, never shown, is answered %+v, want an error", got)
	}
	if got := resultOf(t, next, "call-2"); got.IsError || !strings.Contains(got.Content, "mentioning Andy Burnham {Q269909}") {
		t.Errorf("asking for {q269909} is answered %+v, want Andy Burnham's catalogue", got)
	}

	// An error naming the id asked for never counts as showing it.
	respondLast(batcher, asking("find_entity", `{"entity": "{Q6555365}"}`))
	runAt(t, b, sunday.Add(2*time.Hour))
	respondLast(batcher, asking("find_entity", `{"entity": "Q6555365"}`))
	runAt(t, b, sunday.Add(3*time.Hour))
	if got := resultOf(t, lastSent(t, batcher), "call-1"); !got.IsError {
		t.Errorf("asking again for Keir Starmer, after an error named him, is answered %+v, want an error", got)
	}
}
