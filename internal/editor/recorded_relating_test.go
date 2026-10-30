package editor_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// The recorded runs of relating. The fixtures' readings, from relating's own
// frozen copies (testdata/recorded/relating/inputs/reading), are replayed in
// the order the Items were published, what each names looked up in a
// Wikidata that finds nothing (so each name is the Editor's own Entity, and
// no recording makes a network call), and after each Item is read its Spans
// are related to those of the Items read before it. Each fixture's relating
// is sampled five times, over the Record its earlier fixtures' first passing
// samples left, and kept under testdata/recorded/relating/<fixture>/; a
// fixture whose Spans have no candidates (the first, with nothing read
// before it) makes no call, and its samples keep none. Recording costs five
// calls per fixture, and more for attempts the checks refuse:
//
//	go test -count=1 -timeout 90m ./internal/editor -run TestRecordedRelatings -record
//
// with GOSLOW_ANTHROPIC_API_KEY and RELATING_MODEL set in the environment.
// Re-recording reading leaves it valid; bringing it onto newer readings is a
// refresh: copy the current readings over its inputs, then re-record it.
func TestRecordedRelatings(t *testing.T) {
	t.Parallel()
	for _, err := range relatingVerdicts(t, ownDirectory(t, "relating")) {
		t.Error(err)
	}
}

// relatingCommand is how relating's recorded test is re-recorded.
var relatingCommand = rerecordCmd{"TestRecordedRelatings", "RELATING_MODEL"}

// The Holyrood press release, and the speech it announced: the speech
// follows the release, and both say Scotland is among the most centralised
// in Europe, which is the same as across the two but one publisher's, so
// no corroboration.
const (
	holyroodRelease = "prime-minister-makes-offer-to-holyrood-to-work-together-to-empower-scottish-regions"
	holyroodSpeech  = "pm-speech-in-scotland-16-october-2026"
)

// relatingVerdicts replays (or records) relating from the recordings under
// root and judges it: each failure.
func relatingVerdicts(t *testing.T, root string) []error {
	t.Helper()
	items := map[string]editor.Item{}
	for _, f := range fixtureItems(t) {
		items[f.name] = f.item
	}
	release, speech := items[holyroodRelease], items[holyroodSpeech]
	casesPresentForRelating(t, release, speech)
	return chainSampled(t, filepath.Join(root, "relating"), "relating", nil, relatingCommand,
		func(relating actModel) chain {
			return chain{reading: replayed(inputsOf(root, "relating", "reading")), relating: relating}
		},
		func(t *testing.T, store *sqlite.Store, f namedItem, p properties) {
			if f.name != holyroodSpeech {
				return
			}
			rels := relationsOfItem(t, store, speech)
			words := func(a editor.Address) string {
				if a.EntryID == release.EntryID {
					return cut.Words(release.Content, a.Span)
				}
				return cut.Words(speech.Content, a.Span)
			}
			fromSpeechToRelease := func(r editor.Relation) bool {
				return r.From.EntryID == speech.EntryID && r.To.EntryID == release.EntryID
			}
			var follows, centralised []editor.Relation
			for _, r := range rels {
				if r.Kind == editor.Follows && fromSpeechToRelease(r) && strings.Contains(words(r.To), " will ") {
					follows = append(follows, r)
				}
				if fromSpeechToRelease(r) && strings.Contains(words(r.From), "most centralised") &&
					strings.Contains(words(r.To), "most centralised") {
					centralised = append(centralised, r)
				}
			}
			p.holds("the Holyrood speech follows the press release announcing it", len(follows) > 0,
				"no Span of the speech follows a Span of the release saying what the Prime Minister will do: %d relations to the release", countTo(rels, release))
			same := p.property("the “most centralised” line is the same as across the two, and not corroboration")
			switch {
			case len(centralised) == 0:
				same("the two “most centralised” lines are not related")
			case centralised[0].Kind != editor.SameAs:
				same("the two “most centralised” lines are related as %s", centralised[0].Kind)
			case centralised[0].Corroborates():
				same("one publisher's same as is taken as corroboration")
			}
		})
}

// countTo is how many of rels are to a Span of it.
func countTo(rels []editor.Relation, it editor.Item) int {
	n := 0
	for _, r := range rels {
		if r.To.EntryID == it.EntryID || r.From.EntryID == it.EntryID {
			n++
		}
	}
	return n
}

// relationsOfItem are the relations of every Span of it's newest Cut,
// each once.
func relationsOfItem(t *testing.T, store *sqlite.Store, it editor.Item) []editor.Relation {
	t.Helper()
	c, _, _, err := store.NewestCut(t.Context(), it.SourceID, it.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[editor.Relation]bool{}
	var out []editor.Relation
	for _, sp := range c.Spans {
		got, err := store.RelationsOf(t.Context(), editor.Address{SourceID: it.SourceID, EntryID: it.EntryID, Span: sp})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range got {
			if !seen[r.Relation] {
				seen[r.Relation] = true
				out = append(out, r.Relation)
			}
		}
	}
	return out
}

// casesPresentForRelating fails unless the release and the speech carry the
// Spans the properties rest on, so a changed fixture or cutter can't leave
// them testing nothing.
func casesPresentForRelating(t *testing.T, release, speech editor.Item) {
	t.Helper()
	for _, c := range []struct {
		it    editor.Item
		words []string
	}{
		{release, []string{"will argue the next stage of devolution", "most centralised"}},
		{speech, []string{"most centralised nations in Europe"}},
	} {
		for _, w := range c.words {
			found := false
			for _, sp := range cut.Sentences(c.it.Content) {
				if strings.Contains(cut.Words(c.it.Content, sp), w) {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s has no Span with %q", c.it.Title, w)
			}
		}
	}
}
