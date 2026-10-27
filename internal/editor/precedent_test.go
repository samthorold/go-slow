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

// speechStory reads and places the speech, starting a Story with its Fact,
// and returns the Story's and the Fact's ids.
func speechStory(t *testing.T, store *sqlite.Store) (story, fact int64) {
	t.Helper()
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	stories, err := store.ListStories(t.Context())
	if err != nil || len(stories) != 1 {
		t.Fatalf("Stories: %v %v", stories, err)
	}
	s, _, err := store.Story(t.Context(), stories[0].ID)
	if err != nil || len(s.Timeline) != 1 {
		t.Fatalf("Story: %+v %v", s, err)
	}
	return s.ID, s.Timeline[0].ID
}

// give gives the Owner's Feedback on a Development, at saturday.
func give(t *testing.T, store *sqlite.Store, kind string, story, fact int64) feedback.Feedback {
	t.Helper()
	return giveAt(t, store, kind, story, fact, saturday)
}

func giveAt(t *testing.T, store *sqlite.Store, kind string, story, fact int64, at time.Time) feedback.Feedback {
	t.Helper()
	f, errs, err := feedback.New(store).Give(t.Context(), feedback.Draft{
		Kind: kind, Level: "development",
		Story: strconv.FormatInt(story, 10), Fact: strconv.FormatInt(fact, 10),
	}, at)
	if err != nil || errs != nil {
		t.Fatalf("give Feedback: %v %v", errs, err)
	}
	return f
}

func TestPlacingIsShownPastFeedbackOnDevelopmentsSharingItsEntitiesWithTheirDepth(t *testing.T) {
	store := record(t)
	story, fact := speechStory(t, store)
	give(t, store, "unwanted", story, fact)
	readLater(t, store)
	placing := &fakeModel{output: `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": []}]}`}
	ed, _ := placer(store, placing)

	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if len(placing.sent) != 1 {
		t.Fatalf("placing called %d times, want once", len(placing.sent))
	}
	in := placing.sent[0].Input
	// The readout shares Andy Burnham with the speech's Development, which
	// the Owner said was unwanted at the Scotland Topic's Depth.
	want := `{"ref":"P1","feedback":"unwanted","depth":"following","story":"Andy Burnham in Scotland","development":"Andy Burnham gave a speech in Motherwell."`
	if !strings.Contains(in, want) {
		t.Errorf("the readout's placing is not shown the precedent %s:\n%s", want, in)
	}
}

func TestThePrecedentsAPlacingWasShownAreKeptWithItsResult(t *testing.T) {
	for name, place := range map[string]func(t *testing.T, store *sqlite.Store, output string){
		"in real time": func(t *testing.T, store *sqlite.Store, output string) {
			ed, _ := placer(store, &fakeModel{output: output})
			if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}
		},
		"in a batch": func(t *testing.T, store *sqlite.Store, output string) {
			batcher := &fakeBatcher{}
			b := placingBatches(store, batcher)
			if err := b.ReadWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}
			batcher.end(1, answering(output))
			if err := b.ReadWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			store := record(t)
			story, fact := speechStory(t, store)
			newToMe := give(t, store, "new to me", story, fact)
			unwanted := give(t, store, "unwanted", story, fact)
			readLater(t, store)

			place(t, store, `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": []}]}`)

			shown, err := store.PrecedentsShown(t.Context(), "placing", laterItem.SourceID, laterItem.EntryID)
			if err != nil {
				t.Fatal(err)
			}
			// The newest first, as shown.
			if want := []int64{unwanted.ID, newToMe.ID}; !slices.Equal(shown, want) {
				t.Errorf("precedents kept with the placing = %v, want %v", shown, want)
			}
		})
	}
}

func TestPlacingIsShownAtMostEightPrecedentsTheMostRelevantThenTheNewest(t *testing.T) {
	store := record(t)
	speechStoryID, speechFact := speechStory(t, store)
	readTyroneNotice(t, store)
	ed, _ := placer(store, &fakeModel{output: `{"new_stories": [{"ref": "N1", "title": "Lieutenancy of County Tyrone"}],
		"facts": [{"fact": "F1", "developments_in": ["N1"]}]}`})
	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	tyroneStory, tyroneFact := storyTitled(t, store, "Lieutenancy of County Tyrone")
	// The oldest is on the speech's Development, which shares two Entities
	// with the Item placed next; the eight newer are on the Tyrone notice's,
	// which shares one.
	oldest := giveAt(t, store, "unwanted", speechStoryID, speechFact, saturday)
	var tyrone []int64
	for i := range 8 {
		tyrone = append(tyrone, giveAt(t, store, "new to me", tyroneStory, tyroneFact, saturday.Add(time.Duration(i+1)*time.Minute)).ID)
	}
	visit := collect.Item{
		SourceID: number10.ID, EntryID: "visit", Title: "PM visits Motherwell and County Tyrone",
		Link: "https://www.gov.uk/visit", Published: speech.Published.Add(72 * time.Hour),
		Attribution: collect.OGL, CollectedAt: saturday,
	}
	if _, err := store.AddItems(t.Context(), []collect.Item{visit}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveReading(t.Context(), editor.Reading{
		SourceID: visit.SourceID, EntryID: visit.EntryID, Model: "fake",
		Entities: []editor.Entity{{Name: "Andy Burnham", Kind: "person"}, {Name: "Motherwell", Kind: "place"},
			{Name: "County Tyrone", Kind: "place"}},
		Facts: []editor.Fact{{Statement: "Andy Burnham visited Motherwell and County Tyrone.", AsAt: oct16,
			Quote: "visits"}},
	}, saturday); err != nil {
		t.Fatal(err)
	}

	ed, _ = placer(store, &fakeModel{output: `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": []}]}`})
	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	shown, err := store.PrecedentsShown(t.Context(), "placing", visit.SourceID, visit.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{oldest.ID}
	for i := len(tyrone) - 1; i >= 1; i-- {
		want = append(want, tyrone[i])
	}
	if !slices.Equal(shown, want) {
		t.Errorf("precedents shown = %v, want the most relevant (%d), then the seven newest of the rest: %v", shown, oldest.ID, want)
	}
}

func TestFeedbackOnAWithdrawnFactIsNoPrecedent(t *testing.T) {
	store := record(t)
	story, fact := speechStory(t, store)
	give(t, store, "unwanted", story, fact)
	if _, errs, err := feedback.New(store).Give(t.Context(), feedback.Draft{
		Kind: "wrong", Level: "fact", Fact: strconv.FormatInt(fact, 10),
	}, saturday); err != nil || errs != nil {
		t.Fatalf("give Feedback: %v %v", errs, err)
	}
	readLater(t, store)
	placing := &fakeModel{output: `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": []}]}`}
	ed, _ := placer(store, placing)

	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if in := placing.sent[0].Input; !strings.Contains(in, "Precedents: none") {
		t.Errorf("the placing is shown Feedback on a withdrawn Fact:\n%s", in)
	}
}

func TestUndoneFeedbackIsNoPrecedent(t *testing.T) {
	store := record(t)
	story, fact := speechStory(t, store)
	f := give(t, store, "unwanted", story, fact)
	if _, err := feedback.New(store).Undo(t.Context(), f.ID, saturday); err != nil {
		t.Fatal(err)
	}
	readLater(t, store)
	placing := &fakeModel{output: `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": []}]}`}
	ed, _ := placer(store, placing)

	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if in := placing.sent[0].Input; !strings.Contains(in, "Precedents: none") {
		t.Errorf("the placing is shown undone Feedback:\n%s", in)
	}
}

// storyTitled is the Story with the title, and its first Development's Fact.
func storyTitled(t *testing.T, store *sqlite.Store, title string) (story, fact int64) {
	t.Helper()
	stories, err := store.ListStories(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range stories {
		if l.Title == title {
			s, _, err := store.Story(t.Context(), l.ID)
			if err != nil || len(s.Timeline) == 0 {
				t.Fatalf("Story %q: %+v %v", title, s, err)
			}
			return s.ID, s.Timeline[0].ID
		}
	}
	t.Fatalf("no Story %q in %+v", title, stories)
	return 0, 0
}

// glasgowItem names Scotland, so the check ties it to the Scotland Topic,
// but its Fact shares no Entity with the speech's.
var glasgowItem = collect.Item{
	SourceID: number10.ID, EntryID: "glasgow", Title: "Funding for Glasgow",
	Link: "https://www.gov.uk/glasgow", Content: "<p>Glasgow, Scotland, will get new funding.</p>",
	Published: speech.Published.Add(48 * time.Hour), Attribution: collect.OGL, CollectedAt: saturday,
}

func TestPlacingIsShownPastFeedbackOnDevelopmentsSharingItsTopicsAndTheDepthItIsFollowedAtNow(t *testing.T) {
	store := record(t)
	story, fact := speechStory(t, store)
	give(t, store, "unwanted", story, fact)
	if _, err := store.AddItems(t.Context(), []collect.Item{glasgowItem}); err != nil {
		t.Fatal(err)
	}
	if err := interest.New(store).CheckNew(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveReading(t.Context(), editor.Reading{
		SourceID: glasgowItem.SourceID, EntryID: glasgowItem.EntryID, Model: "fake",
		Entities: []editor.Entity{{Name: "Glasgow", Kind: "place"}},
		Facts: []editor.Fact{{Statement: "Glasgow will get new funding.", AsAt: oct16,
			Quote: "will get new funding"}},
	}, saturday); err != nil {
		t.Fatal(err)
	}
	readTyroneNotice(t, store)
	placing := &fakeModel{output: `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": []}]}`}
	ed, _ := placer(store, placing)

	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	inputs := map[string]string{}
	for _, r := range placing.sent {
		inputs[strings.SplitN(r.Input, "\n", 2)[0]] = r.Input
	}
	glasgow, tyrone := inputs["Item: "+glasgowItem.Title], inputs["Item: "+tyroneNotice.Title]
	if glasgow == "" || tyrone == "" {
		t.Fatalf("placing was not called on both Items: %v", inputs)
	}
	// The Glasgow Item is tied to the Topic the speech is tied to.
	if !strings.Contains(glasgow, `"feedback":"unwanted","depth":"following"`) ||
		!strings.Contains(glasgow, "Depth: following") {
		t.Errorf("the Glasgow Item's placing is not shown the precedent and its Depth now:\n%s", glasgow)
	}
	// The Tyrone notice shares neither an Entity nor a Topic with it.
	if strings.Contains(tyrone, `"feedback"`) || !strings.Contains(tyrone, "Precedents: none") {
		t.Errorf("the Tyrone notice's placing is shown a precedent it shares nothing with:\n%s", tyrone)
	}
}
