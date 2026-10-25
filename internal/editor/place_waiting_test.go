package editor_test

import (
	"bytes"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// placer is an Editor over store that reads with goodReading and places
// with placing.
func placer(store *sqlite.Store, placing *fakeModel) (*editor.Editor, *bytes.Buffer) {
	var logs bytes.Buffer
	return editor.New(editor.Deps{
		Store:   store,
		Model:   &fakeModel{output: goodReading},
		Placing: placing,
		Log:     slog.New(slog.NewTextHandler(&logs, nil)),
	}), &logs
}

const startsAStory = `{"new_stories": [{"ref": "N1", "title": "Andy Burnham in Scotland"}],
  "facts": [{"fact": "F1", "developments_in": ["N1"]}]}`

// readAndPlace reads the speech and places its Fact with placing.
func readAndPlace(t *testing.T, store *sqlite.Store, placing *fakeModel) *editor.Editor {
	t.Helper()
	ed, _ := placer(store, placing)
	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	return ed
}

func TestPlacingAFactThatStartsAStoryPutsItOnTheStorysTimeline(t *testing.T) {
	store := record(t)

	readAndPlace(t, store, &fakeModel{output: startsAStory})

	stories, err := store.ListStories(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(stories) != 1 || stories[0].Title != "Andy Burnham in Scotland" {
		t.Fatalf("Stories = %+v, want the one placing started", stories)
	}
	s, found, err := store.Story(t.Context(), stories[0].ID)
	if err != nil || !found {
		t.Fatalf("Story: %v %v", found, err)
	}
	if len(s.Timeline) != 1 {
		t.Fatalf("Timeline = %+v, want the speech's Fact", s.Timeline)
	}
	d := s.Timeline[0]
	if d.Statement != "Andy Burnham gave a speech in Motherwell." || d.SourceID != speech.SourceID ||
		d.EntryID != speech.EntryID || d.ItemTitle != speech.Title {
		t.Errorf("the Development does not trace to its Fact and Item: %+v", d)
	}
}

// laterItem is a readout, published after the speech, that involves Andy
// Burnham too.
var laterItem = collect.Item{
	SourceID: number10.ID, EntryID: "readout", Title: "PM meeting with the First Minister",
	Link: "https://www.gov.uk/readout", Content: "<p>The Prime Minister met the First Minister in Edinburgh.</p>",
	Published: speech.Published.Add(24 * time.Hour), Attribution: collect.OGL, CollectedAt: saturday,
}

func readLater(t *testing.T, store *sqlite.Store) {
	t.Helper()
	if _, err := store.AddItems(t.Context(), []collect.Item{laterItem}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveReading(t.Context(), editor.Reading{
		SourceID: laterItem.SourceID, EntryID: laterItem.EntryID, Model: "fake",
		Entities: []editor.Entity{{Name: "Andy Burnham", Kind: "person"}, {Name: "Edinburgh", Kind: "place"}},
		Facts: []editor.Fact{{Statement: "Andy Burnham met the First Minister in Edinburgh.",
			AsAt: time.Date(2026, time.October, 17, 0, 0, 0, 0, time.UTC), Quote: "met the First Minister in Edinburgh",
			Entities: []string{"Andy Burnham", "Edinburgh"}}},
	}, saturday); err != nil {
		t.Fatal(err)
	}
}

func TestALaterFactMeetsTheStoryAnEarlierOneStarted(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	stories, err := store.ListStories(t.Context())
	if err != nil || len(stories) != 1 {
		t.Fatalf("Stories: %v %v", stories, err)
	}
	id := stories[0].ID
	readLater(t, store)
	ref := "S" + strconv.FormatInt(id, 10)
	placing := &fakeModel{output: `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": ["` + ref + `"]}]}`}
	ed, _ := placer(store, placing)

	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if len(placing.sent) != 1 {
		t.Fatalf("placing called %d times, want once: only the readout's Fact awaits placing", len(placing.sent))
	}
	if in := placing.sent[0].Input; !strings.Contains(in, "Andy Burnham in Scotland") || !strings.Contains(in, "Candidate Stories: "+ref) {
		t.Errorf("the readout's Fact is not weighed against the Story the speech started:\n%s", in)
	}
	s, _, err := store.Story(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Timeline) != 2 || s.Timeline[0].EntryID != speech.EntryID || s.Timeline[1].EntryID != laterItem.EntryID {
		t.Errorf("Timeline = %+v, want the speech then the readout", s.Timeline)
	}
}

func TestAFactOncePlacedIsNotPlacedAgain(t *testing.T) {
	store := record(t)
	placing := &fakeModel{output: `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": []}]}`}
	ed := readAndPlace(t, store, placing)

	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	if len(placing.sent) != 1 {
		t.Errorf("placing called %d times, want once", len(placing.sent))
	}
}

func TestAPlacingThatFailsWritesNothingAndIsRetried(t *testing.T) {
	for name, placing := range map[string]*fakeModel{
		"failing the checks": {output: `{"new_stories": [], "facts": []}`},
		"a failed call":      {err: errors.New("overloaded")},
	} {
		t.Run(name, func(t *testing.T) {
			store := record(t)
			ed, logs := placer(store, placing)
			if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}

			if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}

			if stories, _ := store.ListStories(t.Context()); len(stories) != 0 {
				t.Errorf("a failed placing was written: %+v", stories)
			}
			if !strings.Contains(logs.String(), "placing failed") {
				t.Errorf("the failure was not logged:\n%s", logs)
			}
			placing.err, placing.output = nil, startsAStory
			if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
				t.Fatal(err)
			}
			if stories, _ := store.ListStories(t.Context()); len(stories) != 1 {
				t.Errorf("the Fact was not placed on the next run: %+v", stories)
			}
		})
	}
}
