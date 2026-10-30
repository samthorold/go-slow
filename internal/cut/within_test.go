package cut_test

import (
	"testing"

	"github.com/samthorold/go-slow/internal/cut"
)

func TestWordsQuotedWithinASpanAreTurnedIntoTheirAddress(t *testing.T) {
	text := "<p>This is <em>fantastic</em> news for the city &amp; its   people.</p>"
	span := cut.Sentences(text)[0]

	for words, want := range map[string]string{
		"the city":                 "the city",
		"fantastic news":           "fantastic news",
		"city & its people":        "city & its people",
		"":                         "This is fantastic news for the city & its people.",
		"news for the city & its":  "news for the city & its",
		"This is fantastic news":   "This is fantastic news",
		"  for   the city ":        "for the city",
		"its people.":              "its people.",
		"is fantastic news for th": "is fantastic news for th",
	} {
		got, ok := cut.Within(text, span, words)
		if !ok {
			t.Errorf("%q not found within the Span", words)
			continue
		}
		if w := cut.Words(text, got); w != want {
			t.Errorf("%q is addressed as %v, reading %q, want %q", words, got, w, want)
		}
	}
}

func TestQuotesAreMatchedWhicheverWayTheyAreCurled(t *testing.T) {
	text := "<p>It’s “good” news.</p>"
	span := cut.Sentences(text)[0]

	got, ok := cut.Within(text, span, `It's "good"`)

	if !ok || cut.Words(text, got) != "It’s “good”" {
		t.Errorf("straight quotes found %v (%v): %q", got, ok, cut.Words(text, got))
	}
}

func TestWordsNotInTheSpanHaveNoAddress(t *testing.T) {
	text := "<p>Work starts in May. It brings 500 jobs.</p>"
	first := cut.Sentences(text)[0]

	for _, words := range []string{"500 jobs", "Work starts in June", "work starts"} {
		if got, ok := cut.Within(text, first, words); ok {
			t.Errorf("%q, not in the first Span, is addressed as %v", words, got)
		}
	}
}
