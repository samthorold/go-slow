package editor_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/feedback"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// readGlasgow adds the Glasgow Item, tied to the Scotland Topic the speech
// is tied to, reads it with one Fact, and returns that Fact's id.
func readGlasgow(t *testing.T, store *sqlite.Store) int64 {
	t.Helper()
	return readTied(t, store, glasgowItem, editor.Fact{Statement: "Glasgow will get new funding.", AsAt: oct16,
		Quote: "will get new funding", Entities: []string{"Glasgow"}})
}

// readTied adds it, runs the lazy-reading check over it, reads it with the
// one Fact f, and returns f's id.
func readTied(t *testing.T, store *sqlite.Store, it collect.Item, f editor.Fact) int64 {
	t.Helper()
	if _, err := store.AddItems(t.Context(), []collect.Item{it}); err != nil {
		t.Fatal(err)
	}
	if err := interest.New(store).CheckNew(t.Context()); err != nil {
		t.Fatal(err)
	}
	var ents []editor.Entity
	for _, n := range f.Entities {
		ents = append(ents, editor.Entity{Name: n, Kind: "place"})
	}
	if err := store.SaveReading(t.Context(), editor.Reading{
		SourceID: it.SourceID, EntryID: it.EntryID, Model: "fake", Entities: ents, Facts: []editor.Fact{f},
	}, saturday); err != nil {
		t.Fatal(err)
	}
	read, _, err := store.ReadItem(t.Context(), it.SourceID, it.EntryID)
	if err != nil || len(read.Facts) != 1 {
		t.Fatalf("read %s: %+v %v", it.EntryID, read.Facts, err)
	}
	return read.Facts[0].ID
}

// wrongAt gives the Owner's Feedback that a Fact is wrong, at at.
func wrongAt(t *testing.T, store *sqlite.Store, fact int64, at time.Time) feedback.Feedback {
	t.Helper()
	f, errs, err := feedback.New(store).Give(t.Context(), feedback.Draft{
		Kind: "wrong", Level: "fact", Fact: strconv.FormatInt(fact, 10),
	}, at)
	if err != nil || errs != nil {
		t.Fatalf("give Feedback: %v %v", errs, err)
	}
	return f
}

func TestReadingIsShownPastWrongFeedbackOnFactsFromItemsSharingItsTopicsWithTheirDepth(t *testing.T) {
	store := record(t)
	wrongAt(t, store, readGlasgow(t, store), saturday)
	model := &fakeModel{output: goodReading}
	ed, _ := newEditor(store, model, &fakeWikidata{})

	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if len(model.sent) != 1 {
		t.Fatalf("reading called %d times, want once: only the speech awaits reading", len(model.sent))
	}
	// The Glasgow Item is tied to Scotland, as the speech is, and its Fact
	// was said to be wrong while the Owner followed Scotland.
	want := `{"ref":"P1","feedback":"wrong","depth":"following","fact":"Glasgow will get new funding.","quote":"will get new funding","given":"2026-10-24"}`
	if in := model.sent[0].Input; !strings.Contains(in, want) {
		t.Errorf("the speech's reading is not shown the precedent %s:\n%s", want, in)
	}
}

func TestReadingIsShownNoFeedbackOnFactsFromItemsSharingNoTopicNorFeedbackOtherThanWrong(t *testing.T) {
	store := record(t)
	if _, errs, err := interest.New(store).Add(t.Context(),
		interest.Draft{Name: "Tyrone", Depth: "close", Terms: "Tyrone"}); err != nil || errs != nil {
		t.Fatalf("add Topic: %v %v", errs, err)
	}
	// The Tyrone notice is tied to Tyrone alone, which the speech is not.
	tyrone := readTied(t, store, collect.Item{
		SourceID: number10.ID, EntryID: "tyrone", Title: "Lord-Lieutenant of County Tyrone",
		Link: "https://www.gov.uk/tyrone", Content: "<p>A Lord-Lieutenant of County Tyrone was appointed.</p>",
		Published: saturday, Attribution: collect.OGL, CollectedAt: saturday,
	}, editor.Fact{Statement: "A Lord-Lieutenant of County Tyrone was appointed.", AsAt: oct16,
		Quote: "was appointed", Entities: []string{"County Tyrone"}})
	wrongAt(t, store, tyrone, saturday)
	// The Glasgow Fact shares the speech's Topic, but was new to the Owner,
	// not wrong.
	if _, errs, err := feedback.New(store).Give(t.Context(), feedback.Draft{
		Kind: "new to me", Level: "fact", Fact: strconv.FormatInt(readGlasgow(t, store), 10),
	}, saturday); err != nil || errs != nil {
		t.Fatalf("give Feedback: %v %v", errs, err)
	}
	model := &fakeModel{output: goodReading}
	ed, _ := newEditor(store, model, &fakeWikidata{})

	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if len(model.sent) != 1 {
		t.Fatalf("reading called %d times, want once", len(model.sent))
	}
	if in := model.sent[0].Input; strings.Contains(in, `"feedback"`) || !strings.Contains(in, "Precedents: none") {
		t.Errorf("the speech's reading is shown a precedent it should not be:\n%s", in)
	}
}

func TestReadingIsShownAtMostEightPrecedentsThoseSharingTheMostTopicsThenTheNewest(t *testing.T) {
	store := record(t)
	if _, errs, err := interest.New(store).Add(t.Context(),
		interest.Draft{Name: "Motherwell", Depth: "close", Terms: "Motherwell"}); err != nil || errs != nil {
		t.Fatalf("add Topic: %v %v", errs, err)
	}
	// The oldest is on an Item tied to both the speech's Topics; the eight
	// newer are on Items tied to Scotland alone.
	both := readTied(t, store, collect.Item{
		SourceID: number10.ID, EntryID: "steel", Title: "Steel in Motherwell, Scotland",
		Link: "https://www.gov.uk/steel", Content: "<p>The Motherwell steelworks will reopen.</p>",
		Published: saturday, Attribution: collect.OGL, CollectedAt: saturday,
	}, editor.Fact{Statement: "The Motherwell steelworks will reopen.", AsAt: oct16,
		Quote: "will reopen", Entities: []string{"Motherwell"}})
	oldest := wrongAt(t, store, both, saturday)
	var scotland []int64
	for i := range 8 {
		id := "scotland-" + strconv.Itoa(i)
		fact := readTied(t, store, collect.Item{
			SourceID: number10.ID, EntryID: id, Title: "Scotland " + strconv.Itoa(i),
			Link: "https://www.gov.uk/" + id, Content: "<p>Scotland will get funding.</p>",
			Published: saturday, Attribution: collect.OGL, CollectedAt: saturday,
		}, editor.Fact{Statement: "Scotland will get funding " + strconv.Itoa(i) + ".", AsAt: oct16,
			Quote: "will get funding", Entities: []string{"Scotland"}})
		scotland = append(scotland, wrongAt(t, store, fact, saturday.Add(time.Duration(i+1)*time.Minute)).ID)
	}
	ed, _ := newEditor(store, &fakeModel{output: goodReading}, &fakeWikidata{})

	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	shown, err := store.PrecedentsShown(t.Context(), "reading", speech.SourceID, speech.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{oldest.ID}
	for i := len(scotland) - 1; i >= 1; i-- {
		want = append(want, scotland[i])
	}
	if !slices.Equal(shown, want) {
		t.Errorf("precedents shown = %v, want the most relevant (%d), then the seven newest of the rest: %v", shown, oldest.ID, want)
	}
}

func TestAFactQuotingOnlyAPrecedentDoesNotTraceToTheItem(t *testing.T) {
	store := record(t)
	wrongAt(t, store, readGlasgow(t, store), saturday)
	// The quote is in the precedent the reading is shown, not in the speech.
	model := &fakeModel{output: `{"entities": [{"name": "Glasgow", "kind": "place"}], "claims": [],
		"facts": [{"statement": "Glasgow will get new funding.", "as_at": "2026-10-16",
			"quote": "will get new funding", "entities": ["Glasgow"]}]}`}
	ed, logs := newEditor(store, model, &fakeWikidata{})

	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if got, _, _ := store.ReadItem(t.Context(), speech.SourceID, speech.EntryID); got.Read {
		t.Errorf("a reading drawing a Fact from a precedent was written: %+v", got.Facts)
	}
	if !strings.Contains(logs.String(), "quote is not in the Item") {
		t.Errorf("the reading was not refused for its quote:\n%s", logs.String())
	}
}

func TestThePrecedentsAReadingWasShownAreKeptWithItsResult(t *testing.T) {
	for name, read := range map[string]func(t *testing.T, store *sqlite.Store){
		"in real time": func(t *testing.T, store *sqlite.Store) {
			ed, _ := newEditor(store, &fakeModel{output: goodReading}, &fakeWikidata{})
			if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}
		},
		"in a batch": func(t *testing.T, store *sqlite.Store) {
			batcher := &fakeBatcher{}
			b, _ := newBatches(store, batcher)
			if err := b.ReadWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}
			batcher.end(1, answering(goodReading))
			if err := b.ReadWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			store := record(t)
			wrong := wrongAt(t, store, readGlasgow(t, store), saturday)

			read(t, store)

			if got, _, _ := store.ReadItem(t.Context(), speech.SourceID, speech.EntryID); !got.Read {
				t.Fatal("the speech was not read")
			}
			shown, err := store.PrecedentsShown(t.Context(), "reading", speech.SourceID, speech.EntryID)
			if err != nil {
				t.Fatal(err)
			}
			if want := []int64{wrong.ID}; !slices.Equal(shown, want) {
				t.Errorf("precedents kept with the reading = %v, want %v", shown, want)
			}
		})
	}
}
