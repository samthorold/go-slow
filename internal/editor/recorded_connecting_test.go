package editor_test

import (
	"path/filepath"
	"testing"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// connectingCommand is how connecting's recorded test is re-recorded.
var connectingCommand = rerecordCmd{"TestRecordedConnectings", "CONNECTING_MODEL"}

// The recorded runs of connecting. The fixtures' readings and placings,
// from connecting's own frozen copies (testdata/recorded/connecting/inputs),
// are replayed in the order the Items were published, and after each Item
// is placed its Facts are connected, so later Facts are weighed against
// earlier ones. Each fixture's connecting is sampled five times, over the
// Record its earlier fixtures' first passing samples left, and kept under
// testdata/recorded/connecting/<fixture>/. Recording costs five calls per
// fixture, and more for attempts the checks refuse:
//
//	go test -count=1 ./internal/editor -run TestRecordedConnectings -record
//
// with GOSLOW_ANTHROPIC_API_KEY and CONNECTING_MODEL set in the environment.
// Re-recording reading or placing leaves it valid; bringing it onto newer
// ones is a refresh: copy the current readings and placings together over
// its inputs, then re-record it.
func TestRecordedConnectings(t *testing.T) {
	t.Parallel()
	recordedConnectings(t, ownDirectory(t, "connecting"))
}

// recordedConnectings replays (or records) connecting from the recordings
// under root.
func recordedConnectings(t *testing.T, root string) {
	for _, err := range connectingVerdicts(t, root) {
		t.Error(err)
	}
}

// connectingVerdicts replays (or records) connecting from the recordings
// under root and judges it: each failure.
func connectingVerdicts(t *testing.T, root string) []error {
	return chainSampled(t, filepath.Join(root, "connecting"), "connecting", nil, connectingCommand,
		func(connecting actModel) chain {
			return chain{
				reading:    replayed(inputsOf(root, "connecting", "reading")),
				placing:    replayed(inputsOf(root, "connecting", "placing")),
				connecting: connecting,
			}
		},
		func(t *testing.T, store *sqlite.Store, _ namedItem, p properties) {
			ctx := t.Context()
			// Every cause Connection traces to Items through its Facts.
			traces := p.property("every cause traces to Items through its Facts")
			causes, err := store.Causes(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range causes {
				for _, d := range []editor.Development{c.Cause, c.Effect} {
					if d.ID == 0 || d.SourceID != number10.ID || d.EntryID == "" || d.ItemTitle == "" {
						traces("a cause that does not trace to an Item through its Fact: %+v", c)
					}
				}
			}
			// No Connection is between Stories: two Stories are connected
			// only through the Facts they share, which each connection points
			// at.
			shared := p.property("Stories are connected only through the Facts they share")
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
						shared("Story %q is connected to %q with no shared Fact behind it", s.Title, c.Title)
					}
					for _, e := range c.Entities {
						if e.Here.ID == 0 || e.There.ID == 0 || e.Here.ID == e.There.ID {
							shared("Story %q is connected to %q by %s as %s without a Fact on each side: %+v", s.Title, c.Title, e.Entity, e.Role, e)
						}
					}
				}
			}
		})
}
