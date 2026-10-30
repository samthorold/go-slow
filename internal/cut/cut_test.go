package cut_test

import (
	"slices"
	"testing"

	"github.com/samthorold/go-slow/internal/cut"
)

// words is the words of each Span the cutter lists, in order.
func words(text string) []string {
	var out []string
	for _, s := range cut.Sentences(text) {
		out = append(out, cut.Words(text, s))
	}
	return out
}

func TestASpanIsAnAddressIntoTheTextAsKept(t *testing.T) {
	text := "<p>Jobs &amp; <strong>growth</strong> rise. Mr Smith agrees.</p>"

	spans := cut.Sentences(text)

	want := []cut.Span{
		{Start: 3, End: len("<p>Jobs &amp; <strong>growth</strong> rise.")},
		{Start: len("<p>Jobs &amp; <strong>growth</strong> rise. "), End: len(text) - len("</p>")},
	}
	if !slices.Equal(spans, want) {
		t.Fatalf("Spans = %v, want %v", spans, want)
	}
	if got := cut.Words(text, spans[0]); got != "Jobs & growth rise." {
		t.Errorf("first Span's words = %q", got)
	}
}

func TestPlainTextIsCutAtBlankLinesAsWellAsSentences(t *testing.T) {
	got := words("Inflation rose\n\nPrices rose by 2.1%. Food led\nthe rise.")

	want := []string{"Inflation rose", "Prices rose by 2.1%.", "Food led the rise."}
	if !slices.Equal(got, want) {
		t.Errorf("Spans = %q\nwant %q", got, want)
	}
}

func TestAnItemsTextIsCutIntoItsSentences(t *testing.T) {
	text := "<p>Andy Burnham said: This is fantastic news for the city. It brings 500 jobs!</p>" +
		"<h2>Next steps</h2><p>Work starts in May. “We are ready,” he said.</p>"

	got := words(text)

	want := []string{
		"Andy Burnham said: This is fantastic news for the city.",
		"It brings 500 jobs!",
		"Next steps",
		"Work starts in May.",
		"“We are ready,” he said.",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Spans = %q\nwant %q", got, want)
	}
}
