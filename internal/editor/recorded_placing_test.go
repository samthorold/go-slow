package editor_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/samthorold/go-slow/internal/sqlite"
)

// The recorded runs of placing. The fixtures' readings, from placing's own
// frozen copies (testdata/recorded/placing/inputs/reading), are placed in
// the order the Items were published, each Item's Facts in one call, so
// later Facts meet the Stories earlier ones started. Each fixture's placing
// is sampled five times, over the Record its earlier fixtures' first passing
// samples left, so every sample of it answers one request, and kept under
// testdata/recorded/placing/<fixture>/. Recording costs five calls per
// fixture, and more for attempts the checks refuse:
//
//	go test -count=1 ./internal/editor -run TestRecordedPlacings -record
//
// with GOSLOW_ANTHROPIC_API_KEY and PLACING_MODEL set in the environment.
// Re-recording reading leaves it valid; bringing it onto newer readings is a
// refresh: copy the current readings over its inputs, then re-record it.
func TestRecordedPlacings(t *testing.T) {
	t.Parallel()
	recordedPlacings(t, ownDirectory(t, "placing"))
}

// placingCommand is how placing's recorded test is re-recorded.
var placingCommand = rerecordCmd{"TestRecordedPlacings", "PLACING_MODEL"}

// The reaction placing is tested on, and the fixture last published: after
// it, the whole run's Stories are judged.
const reactionFixture = "uk-government-response-to-agreement-between-us-and-russia-9-october"

// recordedPlacings replays (or records) placing from the recordings under
// root.
func recordedPlacings(t *testing.T, root string) {
	for _, err := range placingVerdicts(t, root) {
		t.Error(err)
	}
}

// placingVerdicts replays (or records) placing from the recordings under
// root and judges it: each failure.
func placingVerdicts(t *testing.T, root string) []error {
	fixtures := fixtureItems(t)
	last := fixtures[len(fixtures)-1].name
	return chainSampled(t, filepath.Join(root, "placing"), "placing", nil, placingCommand,
		func(placing actModel) chain {
			return chain{reading: replayed(inputsOf(root, "placing", "reading")), placing: placing}
		},
		func(t *testing.T, store *sqlite.Store, f namedItem, p properties) {
			ctx := t.Context()
			stories, err := store.ListStories(ctx)
			if err != nil {
				t.Fatal(err)
			}
			traces := p.property("every Development traces to its Fact and Item")
			developments := map[string][]string{} // Fact statement -> the Stories it is a Development in
			threaded := false
			for _, l := range stories {
				s, _, err := store.Story(ctx, l.ID)
				if err != nil {
					t.Fatal(err)
				}
				items := map[string]bool{}
				for _, d := range s.Timeline {
					if d.ID == 0 || d.SourceID != number10.ID || d.EntryID == "" || d.ItemTitle == "" {
						traces("Story %q: a Development that does not trace: %+v", s.Title, d)
					}
					items[d.EntryID] = true
					developments[d.Statement] = append(developments[d.Statement], s.Title)
				}
				threaded = threaded || len(items) > 1
			}
			if f.name == last {
				p.holds("placing starts Stories", len(stories) > 0, "placing started no Story")
				// Later Facts meet the Stories earlier ones started.
				p.holds("later Facts meet the Stories earlier ones started", threaded,
					"no Story has Developments from more than one Item")
			}
			if f.name != reactionFixture {
				return
			}
			// A reaction is not a Development: the government's response to
			// the US-Russia agreement is a reaction as a whole, so none of its
			// Facts moves a Story on.
			got, _, err := store.ReadItem(ctx, number10.ID, f.item.EntryID)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Facts) == 0 {
				t.Fatal("reading drew no Facts from the reaction, so placing is not tested on one")
			}
			reaction := p.property("a reaction is not a Development")
			for _, fact := range got.Facts {
				if in := developments[fact.Statement]; len(in) > 0 {
					reaction("the reaction's Fact %q is a Development in %v", fact.Statement, in)
				}
			}
		})
}

// kept is a kept response.
func kept(t *testing.T, path string) recording {
	t.Helper()
	rec, err := keptAt(path)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// keptAt is the response kept at path.
func keptAt(path string) (recording, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return recording{}, err
	}
	var rec recording
	if err := json.Unmarshal(b, &rec); err != nil {
		return recording{}, fmt.Errorf("%s: %w", path, err)
	}
	return rec, nil
}
