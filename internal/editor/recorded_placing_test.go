package editor_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/samthorold/go-slow/internal/editor"
)

// The recorded runs of placing. The fixtures' readings, from placing's own
// frozen copies (testdata/recorded/placing/inputs/reading), are placed in
// the order the Items were published, each Item's Facts in one call, so
// later Facts meet the Stories earlier ones started. Each call is kept under
// testdata/recorded/placing and replayed; the Stories it builds are the
// same on every replay because each call is answered as it was when kept.
// Re-recording costs one call per fixture:
//
//	go test -count=1 ./internal/editor -run TestRecordedPlacings -record
//
// with GOSLOW_ANTHROPIC_API_KEY and PLACING_MODEL set in the environment.
// Re-recording reading leaves it valid; bringing it onto newer readings is a
// refresh: copy the current readings over its inputs, then re-record it.
func TestRecordedPlacings(t *testing.T) { recordedPlacings(t, recordedDir) }

// recordedPlacings replays (or records) placing from the recordings under
// root.
func recordedPlacings(t *testing.T, root string) {
	ctx := t.Context()
	store, topics := newRecord(t, nil)
	var total editor.Usage
	c := chain{
		reading:       replayed(inputsOf(root, "placing", "reading")),
		placing:       recordedOrReplayed(t, filepath.Join(root, "placing"), "PLACING_MODEL", "TestRecordedPlacings", &total),
		readingFrozen: true,
	}
	fixtures := fixtureItems(t)
	if err := c.run(ctx, store, topics, fixtures); err != nil {
		t.Fatal(err)
	}
	t.Logf("recorded placing usage over %d fixtures: %d input, %d output tokens", len(fixtures), total.InputTokens, total.OutputTokens)

	stories, err := store.ListStories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stories) == 0 {
		t.Fatal("placing started no Story")
	}
	developments := map[string][]string{} // Fact statement -> the Stories it is a Development in
	threaded := false
	for _, l := range stories {
		s, _, err := store.Story(ctx, l.ID)
		if err != nil {
			t.Fatal(err)
		}
		items := map[string]bool{}
		for _, d := range s.Timeline {
			// Every Development traces to its Fact and Item.
			if d.ID == 0 || d.SourceID != number10.ID || d.EntryID == "" || d.ItemTitle == "" {
				t.Errorf("Story %q: a Development that does not trace: %+v", s.Title, d)
			}
			items[d.EntryID] = true
			developments[d.Statement] = append(developments[d.Statement], s.Title)
		}
		if len(items) > 1 {
			threaded = true
		}
		t.Logf("Story %q: %d Developments from %d Items", s.Title, len(s.Timeline), len(items))
	}
	// Later Facts meet the Stories earlier ones started.
	if !threaded {
		t.Error("no Story has Developments from more than one Item")
	}
	// A reaction is not a Development: the government's response to the
	// US-Russia agreement is a reaction as a whole, so none of its Facts
	// moves a Story on.
	reaction := "tag:www.gov.uk,2005:/government/news/uk-government-response-to-agreement-between-us-and-russia-9-october"
	got, _, err := store.ReadItem(ctx, number10.ID, reaction)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Facts) == 0 {
		t.Fatal("reading drew no Facts from the reaction, so placing is not tested on one")
	}
	for _, f := range got.Facts {
		if in := developments[f.Statement]; len(in) > 0 {
			t.Errorf("the reaction's Fact %q is a Development in %v", f.Statement, in)
		}
	}
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

// keep writes a response to be replayed.
func keep(t *testing.T, path string, rec recording) {
	t.Helper()
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
