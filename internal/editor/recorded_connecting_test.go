package editor_test

import (
	"path/filepath"
	"testing"

	"github.com/samthorold/go-slow/internal/editor"
)

// The recorded runs of connecting. The fixtures' readings and placings,
// from connecting's own frozen copies (testdata/recorded/connecting/inputs),
// are replayed in the order the Items were published, and after each Item
// is placed its Facts are connected, so later Facts are weighed against
// earlier ones. Each connecting call is kept under
// testdata/recorded/connecting and replayed. Re-recording costs one call per
// fixture:
//
//	go test -count=1 ./internal/editor -run TestRecordedConnectings -record
//
// with GOSLOW_ANTHROPIC_API_KEY and CONNECTING_MODEL set in the environment.
// Re-recording reading or placing leaves it valid; bringing it onto newer
// ones is a refresh: copy the current readings and placings together over
// its inputs, then re-record it.
func TestRecordedConnectings(t *testing.T) { recordedConnectings(t, recordedDir) }

// recordedConnectings replays (or records) connecting from the recordings
// under root.
func recordedConnectings(t *testing.T, root string) {
	ctx := t.Context()
	store, topics := newRecord(t, nil)
	var total editor.Usage
	c := chain{
		reading:       replayed(inputsOf(root, "connecting", "reading")),
		placing:       replayed(inputsOf(root, "connecting", "placing")),
		connecting:    recordedOrReplayed(t, filepath.Join(root, "connecting"), "CONNECTING_MODEL", "TestRecordedConnectings", &total),
		readingFrozen: true,
	}
	fixtures := fixtureItems(t)
	if err := c.run(ctx, store, topics, fixtures); err != nil {
		t.Fatal(err)
	}
	t.Logf("recorded connecting usage over %d fixtures: %d input, %d output tokens", len(fixtures), total.InputTokens, total.OutputTokens)

	// Every cause Connection traces to Items through its Facts.
	causes, err := store.Causes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range causes {
		for _, d := range []editor.Development{c.Cause, c.Effect} {
			if d.ID == 0 || d.SourceID != number10.ID || d.EntryID == "" || d.ItemTitle == "" {
				t.Errorf("a cause that does not trace to an Item through its Fact: %+v", c)
			}
		}
		t.Logf("cause: %q (%s) led to %q (%s)", c.Cause.Statement, c.Cause.ItemTitle, c.Effect.Statement, c.Effect.ItemTitle)
	}
	// No Connection is between Stories: two Stories are connected only
	// through the Facts they share, which each connection points at.
	stories, err := store.ListStories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stories {
		connected, err := store.ConnectedStories(ctx, s.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range connected {
			if len(c.Shared)+len(c.Entities) == 0 {
				t.Errorf("Story %q is connected to %q with no shared Fact behind it", s.Title, c.Title)
			}
			for _, e := range c.Entities {
				if e.Here.ID == 0 || e.There.ID == 0 || e.Here.ID == e.There.ID {
					t.Errorf("Story %q is connected to %q by %s as %s without a Fact on each side: %+v", s.Title, c.Title, e.Entity, e.Role, e)
				}
			}
			t.Logf("Story %q is connected to %q: %d shared Developments, %d shared Entities", s.Title, c.Title, len(c.Shared), len(c.Entities))
		}
	}
}
