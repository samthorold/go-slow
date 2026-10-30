package editor_test

import (
	"testing"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// read is the release's reading.
func read(t *testing.T, output string) editor.Reading {
	t.Helper()
	r, err := editor.Read(t.Context(), &fakeModel{output: output}, "", toRead(release))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// hydrated is a Span's hydrated reading as text, each stretch read in
// another's words marked [as those words].
func hydrated(r editor.Reading, text string, s cut.Span) string {
	out := ""
	for _, p := range r.Hydrated(text, s) {
		if p.Was != "" {
			out += "[" + p.Words + "]"
			continue
		}
		out += p.Words
	}
	return out
}

func TestASpansHydratedReadingFollowsWhatItsStretchesReferTo(t *testing.T) {
	r := read(t, releaseReading)
	spans := cut.Sentences(release.Content)

	for i, want := range []string{
		"Andy Burnham said:",
		"“This is fantastic news for [Derby].",
		"Derby has waited long enough.”",
		"[Andy Burnham] visited the site on Monday.",
	} {
		if got := hydrated(r, release.Content, spans[i]); got != want {
			t.Errorf("S%d reads %q, want %q", i+1, got, want)
		}
	}
	pieces := r.Hydrated(release.Content, spans[3])
	if pieces[0].Was != "He" {
		t.Errorf("the hydrated stretch does not say what it was: %+v", pieces[0])
	}
}

func TestAReferenceIsFollowedThroughWhatItRefersTo(t *testing.T) {
	// S4's "He" refers to S2's "the city", which refers to S3's "Derby",
	// which (wrongly) refers back to "He".
	r := read(t, `{"names": [], "refers_to": [
		{"from": {"span": 4, "words": "He"}, "to": {"span": 2, "words": "the city"}},
		{"from": {"span": 2, "words": "the city"}, "to": {"span": 3, "words": "Derby"}},
		{"from": {"span": 3, "words": "Derby"}, "to": {"span": 4, "words": "He"}}
	], "rejected": [], "speakers": []}`)

	got := hydrated(r, release.Content, cut.Sentences(release.Content)[3])

	// A cycle is followed until it would come back on itself.
	if got != "[Derby] visited the site on Monday." {
		t.Errorf("S4 reads %q", got)
	}
}

func TestASpanWithNothingReferredToReadsAsItsWords(t *testing.T) {
	r := read(t, goodReadingOfRelease)
	text := "<p>Jobs &amp; <em>growth</em>   rise.</p>"

	if got := hydrated(r, text, cut.Sentences(text)[0]); got != "Jobs & growth rise." {
		t.Errorf("reads %q", got)
	}
}

const goodReadingOfRelease = `{"names": [], "refers_to": [], "rejected": [], "speakers": []}`

func TestASpeakerIsNamedAsTheSpanNamingThemIsHydrated(t *testing.T) {
	r := read(t, `{"names": [], "refers_to": [{"from": {"span": 4, "words": "He"}, "to": {"span": 1, "words": "Andy Burnham"}}],
		"rejected": [], "speakers": [{"span": {"span": 4, "words": ""}, "speaker": {"span": 4, "words": "He"}}]}`)

	if got := r.Words(release.Content, r.Speakers[0].By); got != "Andy Burnham" {
		t.Errorf("the speaker reads %q", got)
	}
}
