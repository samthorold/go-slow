package editor_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// relatingEditor is an Editor relating on model over store.
func relatingEditor(store *sqlite.Store, model editor.Model) (*editor.Editor, *bytes.Buffer) {
	var logs bytes.Buffer
	return editor.New(editor.Deps{Store: store, RelatingModel: model, Wikidata: &fakeWikidata{},
		Log: slog.New(slog.NewTextHandler(&logs, nil))}), &logs
}

// readNaming collects it into store, cuts it, and keeps a reading of it
// naming each place in names at its first words, each looked up.
func readNaming(t *testing.T, store *sqlite.Store, it editor.Item, names ...string) {
	t.Helper()
	ctx := t.Context()
	if _, err := store.AddItems(ctx, []collect.Item{{SourceID: it.SourceID, EntryID: it.EntryID, Title: it.Title,
		Content: it.Content, Published: it.Published, Attribution: collect.OGL, CollectedAt: saturday}}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(store).CutNew(ctx, saturday); err != nil {
		t.Fatal(err)
	}
	c, _, _, err := store.NewestCut(ctx, it.SourceID, it.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	r := editor.Reading{SourceID: it.SourceID, EntryID: it.EntryID, CutID: c.ID, At: saturday,
		Judge: editor.Judge{Act: "reading", Version: 1, Model: "fake", RequestHash: "r-" + it.EntryID}}
	for _, name := range names {
		for _, sp := range c.Spans {
			if w, ok := cut.Within(it.Content, sp, name); ok {
				r.Names = append(r.Names, editor.Name{At: editor.Address{SourceID: it.SourceID, EntryID: it.EntryID, Span: w},
					Name: name, Kind: "place"})
			}
		}
	}
	if err := store.KeepReading(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := editor.NewLookups(editor.LookupDeps{Store: store, Wikidata: &fakeWikidata{},
		Log: slog.New(slog.DiscardHandler)}).LookUpWaiting(ctx, saturday); err != nil {
		t.Fatal(err)
	}
}

// relationsOf are the relations of the Span n (from 1) of it.
func relationsOf(t *testing.T, store *sqlite.Store, it editor.Item, n int) []editor.SpanRelation {
	t.Helper()
	got, err := store.RelationsOf(t.Context(), editor.Address{SourceID: it.SourceID, EntryID: it.EntryID, Span: spansOf(it)[n-1]})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestAnItemPublishedMondayButReadWednesdayIsRelatedToTuesdaysAndFollowsRunsTuesdayToMonday(t *testing.T) {
	store := record(t)
	monday := pressRelease
	monday.EntryID, monday.Published = "monday", time.Date(2026, time.October, 12, 9, 0, 0, 0, time.UTC)
	tuesday := holyrood
	tuesday.EntryID, tuesday.Published = "tuesday", monday.Published.AddDate(0, 0, 1)

	// Tuesday's is read and related on Tuesday, with nothing before it.
	readNaming(t, store, tuesday, "Scotland")
	model := &fakeModel{output: `{"relations": []}`}
	ed, _ := relatingEditor(store, model)
	if err := ed.RelateWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	if len(model.sent) != 0 {
		t.Fatalf("relating Tuesday's sent %d calls, want none: nothing was read before it", len(model.sent))
	}

	// Monday's is read on Wednesday, and related to Tuesday's.
	readNaming(t, store, monday, "Scotland")
	model.output = `{"relations": [{"span": "S2", "to": "C1", "verdict": "follows"}]}`
	if err := ed.RelateWaiting(t.Context(), saturday.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if len(model.sent) != 1 || !strings.Contains(model.sent[0].Input, "Item: "+monday.Title) ||
		!strings.Contains(model.sent[0].Input, "C1: Scotland is one of the most centralised nations in Europe.") {
		t.Fatalf("sent %+v, want Monday's related to Tuesday's", model.sent)
	}
	got := relationsOf(t, store, tuesday, 2)
	if len(got) != 1 || got[0].Relation.Kind != editor.Follows || got[0].Relation.From.EntryID != "tuesday" ||
		got[0].Relation.To.EntryID != "monday" || got[0].Call == 0 {
		t.Errorf("Tuesday's S2: %+v, want it following Monday's S2, with its call", got)
	}
}

func TestAPairIsJudgedOnceByTheItemReadSecondAndNotAgainFromTheOtherSide(t *testing.T) {
	store := record(t)
	readNaming(t, store, pressRelease, "Scotland")
	readNaming(t, store, holyrood, "Scotland")
	model := &fakeModel{output: `{"relations": [{"span": "S2", "to": "C1", "verdict": "same_as"}]}`}
	ed, _ := relatingEditor(store, model)

	if err := ed.RelateWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if len(model.sent) != 1 || !strings.Contains(model.sent[0].Input, "Item: "+holyrood.Title+"\n") {
		t.Fatalf("sent %d calls, want one, relating the speech read second", len(model.sent))
	}
	if strings.Contains(model.sent[0].Input, "C1: Today I argue") {
		t.Errorf("the speech's own Spans are its candidates:\n%s", model.sent[0].Input)
	}
	if got := relationsOf(t, store, pressRelease, 2); len(got) != 1 || got[0].Relation.Kind != editor.SameAs {
		t.Errorf("the release's S2: %+v", got)
	}
	if waiting, err := store.ItemsToRelate(t.Context()); err != nil || len(waiting) != 0 {
		t.Errorf("still awaiting relating: %+v %v", waiting, err)
	}
}

func TestCandidatesAreRankedByTheRarityOfWhatTheyShareAndCappedNotByRecency(t *testing.T) {
	store := record(t)
	day := time.Date(2026, time.September, 1, 9, 0, 0, 0, time.UTC)
	// Months ago, the one Item naming Derby; since then, many naming Scotland.
	readNaming(t, store, editor.Item{SourceID: number10.ID, EntryID: "rail", Title: "Rail", Content: "<p>Derby gets rail money.</p>", Published: day}, "Derby")
	for i := range 12 {
		readNaming(t, store, editor.Item{SourceID: number10.ID, EntryID: "vote-" + string(rune('a'+i)), Title: "Vote",
			Content: "<p>Scotland votes again.</p>", Published: day.AddDate(0, 1, i)}, "Scotland")
	}
	ed, _ := relatingEditor(store, &fakeModel{output: `{"relations": []}`})
	if err := ed.RelateWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	model := &fakeModel{output: `{"relations": []}`}
	ed, _ = relatingEditor(store, model)
	readNaming(t, store, editor.Item{SourceID: number10.ID, EntryID: "welcome", Title: "Welcome",
		Content: "<p>Derby and Scotland welcome it.</p>", Published: saturday}, "Derby", "Scotland")

	if err := ed.RelateWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if len(model.sent) != 1 {
		t.Fatalf("sent %d calls", len(model.sent))
	}
	in := model.sent[0].Input
	if !strings.Contains(in, "C1: Derby gets rail money.\n") {
		t.Errorf("the Span sharing the rarer Entity is not first:\n%s", in)
	}
	_, list, _ := strings.Cut(in, "Candidates of each Span:\nS1: ")
	if refs := strings.Split(strings.TrimSpace(list), ", "); len(refs) != 10 {
		t.Errorf("S1 is weighed against %d candidates, want 10: %v", len(refs), refs)
	}
}

func TestRelatingThatKeepsFailingIsMarkedFailedAndNoLongerSubmitted(t *testing.T) {
	store := record(t)
	readNaming(t, store, pressRelease, "Scotland")
	readNaming(t, store, holyrood, "Scotland")
	batcher := &fakeBatcher{}
	log := slog.New(slog.DiscardHandler)
	ed := editor.New(editor.Deps{Store: store, Log: log})
	b := editor.NewBatches(editor.BatchDeps{Store: store, Batcher: batcher, Acts: []editor.BatchAct{ed.Relating()}, Log: log})
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= editor.MaxAttempts; n++ {
		batcher.end(n, failingChecks)
		if err := b.ReadWaiting(t.Context(), saturday); err != nil {
			t.Fatal(err)
		}
	}

	if len(batcher.submitted) != editor.MaxAttempts {
		t.Errorf("%d batches submitted, want %d", len(batcher.submitted), editor.MaxAttempts)
	}
	failed, err := store.FailedWork(t.Context())
	if err != nil || len(failed) != 1 || failed[0].Act != "relating" || failed[0].EntryID != holyrood.EntryID {
		t.Errorf("failed work %+v %v, want the speech's relating", failed, err)
	}
	if waiting, err := store.ItemsToRelate(t.Context()); err != nil || len(waiting) != 0 {
		t.Errorf("awaiting relating: %+v %v", waiting, err)
	}
}

func TestTwoItemsInOneBatchAreRelatedOnceByTheLaterPublished(t *testing.T) {
	store := record(t)
	withFerries(t, store)
	batcher := &fakeBatcher{}
	log := slog.New(slog.DiscardHandler)
	ed := editor.New(editor.Deps{Store: store, Wikidata: &fakeWikidata{}, Log: log})
	b := editor.NewBatches(editor.BatchDeps{Store: store, Batcher: batcher, Acts: []editor.BatchAct{ed.Reading(), ed.Relating()}, Log: log})
	lookups := editor.NewLookups(editor.LookupDeps{Store: store, Wikidata: &fakeWikidata{}, Log: log})
	if err := b.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	batcher.end(1, byItem) // both read in one batch, answered in reverse
	for _, step := range []func() error{
		func() error { return b.ReadWaiting(t.Context(), saturday.Add(15*time.Minute)) },
		func() error { return lookups.LookUpWaiting(t.Context(), saturday.Add(15*time.Minute)) },
		func() error { return b.ReadWaiting(t.Context(), saturday.Add(30*time.Minute)) },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}

	if len(batcher.submitted) != 2 || len(batcher.submitted[1]) != 1 {
		t.Fatalf("submitted %+v, want one request relating", batcher.submitted)
	}
	in := batcher.submitted[1][0].Request.Input
	if !strings.HasPrefix(in, "Item: "+ferries.Title+"\n") || !strings.Contains(in, "C1: It’s good to be back in Scotland") {
		t.Errorf("relating asks:\n%s\nwant the ferries, published later, shown the speech's Spans", in)
	}
	if waiting, err := store.ItemsToRelate(t.Context()); err != nil || len(waiting) != 1 || waiting[0].Item.EntryID != ferries.EntryID {
		t.Errorf("awaiting relating: %+v %v, want only the ferries, in its batch: the speech is related, with no call", waiting, err)
	}
}
