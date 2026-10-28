package editor_test

import (
	"path/filepath"
	"slices"
	"strings"
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

// zelenskyyMeeting is the fixture whose Fact welcomes "President Volodymyr
// Zelenskyy of Ukraine": Ukraine there only says who Zelenskyy is.
const zelenskyyMeeting = "pm-meeting-with-president-zelenskyy-of-ukraine-14-october-2026"

// notOfUkraine is the property declared before connecting first decided
// involvement (#101): the Fact welcoming President Zelenskyy of Ukraine does
// not involve Ukraine. The Fact must be there, or the property tests nothing.
func notOfUkraine(t *testing.T, store *sqlite.Store, f namedItem, p properties) {
	t.Helper()
	ctx := t.Context()
	it, found, err := store.ReadItem(ctx, f.item.SourceID, f.item.EntryID)
	if err != nil || !found {
		t.Fatalf("%s is not read: %v %v", f.name, found, err)
	}
	var welcome int64
	for _, fact := range it.Facts {
		if strings.Contains(fact.Statement, "welcomed President Volodymyr Zelenskyy of Ukraine") {
			welcome = fact.ID
		}
	}
	if welcome == 0 {
		t.Fatalf("%s: no Fact welcomes President Volodymyr Zelenskyy of Ukraine, so the property tests nothing: %+v", f.name, it.Facts)
	}
	involved, err := store.Involvements(ctx, f.item.SourceID, f.item.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	ofUkraine := p.property(`the Fact welcoming "President Volodymyr Zelenskyy of Ukraine" does not involve Ukraine`)
	for _, r := range involved {
		if r.FactID == welcome && r.Entity == "Ukraine" {
			ofUkraine("the welcome involves Ukraine, as %s", r.Role)
		}
	}
}

// The two Facts of the Zelenskyy meeting the reciprocal rule is read on
// (#115): a mutual act (the two spoke) and a directed one (a welcome). The
// meeting's other mutual Fact, "The leaders discussed…", names neither party
// and is left out.
const (
	theyHadACall = "The Prime Minister and President Zelenskyy had a call on Saturday and reflected on it at the meeting."
	theyWelcomed = "The Prime Minister welcomed President Volodymyr Zelenskyy of Ukraine to Downing Street."
)

// reciprocalActors are the properties declared before connecting gave each
// party of a mutual act the role actor (#115): the call has both the Prime
// Minister and Zelenskyy as actor, and the welcome has the Prime Minister as
// its one actor. Each Fact must be there, with the two as separate mentions,
// or the property tests nothing.
func reciprocalActors(t *testing.T, store *sqlite.Store, f namedItem, p properties) {
	t.Helper()
	ctx := t.Context()
	it, found, err := store.ReadItem(ctx, f.item.SourceID, f.item.EntryID)
	if err != nil || !found {
		t.Fatalf("%s is not read: %v %v", f.name, found, err)
	}
	ids := map[string]int64{}
	for _, fact := range it.Facts {
		if fact.Statement != theyHadACall && fact.Statement != theyWelcomed {
			continue
		}
		ids[fact.Statement] = fact.ID
	}
	// The two must be mentioned apart, or the property tests nothing.
	for _, name := range []string{primeMinister, zelenskyy} {
		if !slices.ContainsFunc(it.Mentions, func(e editor.Entity) bool { return e.Name == name }) {
			t.Fatalf("%s: %q is not mentioned on its own, so the property tests nothing: %+v", f.name, name, it.Mentions)
		}
	}
	for _, s := range []string{theyHadACall, theyWelcomed} {
		if ids[s] == 0 {
			t.Fatalf("%s: no Fact %q, so the property tests nothing: %+v", f.name, s, it.Facts)
		}
	}
	involved, err := store.Involvements(ctx, f.item.SourceID, f.item.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	actors := func(id int64) []string {
		var names []string
		for _, r := range involved {
			if r.FactID == id && r.Role == "actor" {
				names = append(names, r.Entity)
			}
		}
		slices.Sort(names)
		return names
	}
	both := p.property("the call has both the Prime Minister and Zelenskyy as actor")
	if got := actors(ids[theyHadACall]); !slices.Contains(got, primeMinister) || !slices.Contains(got, zelenskyy) {
		both("the call's actors are %q", got)
	}
	one := p.property("the welcome has the Prime Minister as its one actor")
	if got := actors(ids[theyWelcomed]); !slices.Equal(got, []string{primeMinister}) {
		one("the welcome's actors are %q", got)
	}
}

// The meeting's two parties, as its reading names them.
const (
	primeMinister = "the Prime Minister"
	zelenskyy     = "Volodymyr Zelenskyy"
)

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
		func(t *testing.T, store *sqlite.Store, f namedItem, p properties) {
			ctx := t.Context()
			if f.name == zelenskyyMeeting {
				notOfUkraine(t, store, f, p)
				reciprocalActors(t, store, f, p)
			}
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
					if len(c.Shared)+len(c.Items)+len(c.Entities) == 0 {
						shared("Story %q is connected to %q with no shared Fact or Item behind it", s.Title, c.Title)
					}
					for _, e := range c.Entities {
						if e.Here.ID == 0 || e.There.ID == 0 || e.Here.ID == e.There.ID {
							shared("Story %q is connected to %q by %s as %s without a Fact on each side: %+v", s.Title, c.Title, e.Entity, e.Role, e)
						}
						if e.Here.SourceID == e.There.SourceID && e.Here.EntryID == e.There.EntryID {
							shared("Story %q is connected to %q by %s as %s between Facts of one Item: %+v", s.Title, c.Title, e.Entity, e.Role, e)
						}
					}
				}
			}
		})
}
