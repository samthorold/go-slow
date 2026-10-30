package editor_test

import (
	"slices"
	"testing"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// mentioned are the names of what the Span s mentions, by r.
func mentioned(r editor.Reading, s cut.Span) []string {
	var out []string
	for _, n := range r.Mentions(s) {
		out = append(out, n.Name)
	}
	return out
}

func TestASpanMentionsWhatASpanInsideItNamesOrRefersTo(t *testing.T) {
	r := read(t, releaseReading)
	spans := cut.Sentences(release.Content)

	for i, want := range [][]string{
		{"Andy Burnham"},
		// "the city" refers to "Derby".
		{"Derby"},
		{"Derby"},
		// "He" refers to "Andy Burnham".
		{"Andy Burnham"},
	} {
		if got := mentioned(r, spans[i]); !slices.Equal(got, want) {
			t.Errorf("S%d mentions %v, want %v", i+1, got, want)
		}
	}
}

func TestASpanMentionsWhatAChainOfReferencesReachesEachOnce(t *testing.T) {
	// S4's "He" refers to S2's "the city", which refers to S3's "Derby",
	// which (wrongly) refers back to "He"; S3 names Derby twice over.
	r := read(t, `{"names": [
		{"at": {"span": 3, "words": "Derby"}, "kind": "place", "name": "Derby", "office": ""},
		{"at": {"span": 3, "words": ""}, "kind": "place", "name": "Derby", "office": ""}
	], "refers_to": [
		{"from": {"span": 4, "words": "He"}, "to": {"span": 2, "words": "the city"}},
		{"from": {"span": 2, "words": "the city"}, "to": {"span": 3, "words": "Derby"}},
		{"from": {"span": 3, "words": "Derby"}, "to": {"span": 4, "words": "He"}}
	], "rejected": [], "speakers": []}`)

	if got := mentioned(r, cut.Sentences(release.Content)[3]); !slices.Equal(got, []string{"Derby"}) {
		t.Errorf("S4 mentions %v, want Derby once", got)
	}
}
