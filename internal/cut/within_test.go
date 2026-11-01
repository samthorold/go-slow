package cut_test

import (
	"testing"

	"github.com/samthorold/go-slow/internal/cut"
)

func TestWordsQuotedWithinASpanAreTurnedIntoTheirAddress(t *testing.T) {
	text := "<p>This is <em>fantastic</em> news for the city &amp; its   people.</p>"
	span := cut.Sentences(text)[0]

	for words, want := range map[string]string{
		"the city":                "the city",
		"fantastic news":          "fantastic news",
		"city & its people":       "city & its people",
		"":                        "This is fantastic news for the city & its people.",
		"news for the city & its": "news for the city & its",
		"This is fantastic news":  "This is fantastic news",
		"  for   the city ":       "for the city",
		"its people.":             "its people.",
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

func TestQuotedWordsAddressAWholeWordNotOneInsideALongerWord(t *testing.T) {
	text := "<p>In addition, it will open.</p>"
	span := cut.Sentences(text)[0]

	got, ok := cut.Within(text, span, "it")

	if !ok || text[got.Start-1:got.End+1] != " it " {
		t.Errorf(`"it" is addressed as %v (%v), reading %q`, got, ok, text[got.Start-1:got.End+1])
	}
}

func TestQuotedWordsAddressTheStandaloneWordAfterALongerOneHoldingIt(t *testing.T) {
	text := "<p>The Prime Minister said he would go.</p>"
	span := cut.Sentences(text)[0]

	got, ok := cut.Within(text, span, "he")

	if !ok || text[got.Start-1:got.End+1] != " he " {
		t.Errorf(`"he" is addressed as %v (%v), reading %q`, got, ok, text[got.Start-1:got.End+1])
	}
}

func TestWordsOnlyInsideLongerWordsHaveNoAddress(t *testing.T) {
	text := "<p>This is <em>fantastic</em> news for the city &amp; its   people, 2026 itself.</p>"
	span := cut.Sentences(text)[0]

	for _, words := range []string{"is fantastic news for th", "he", "antastic", "it", "202", "ity &"} {
		if got, ok := cut.Within(text, span, words); ok {
			t.Errorf("%q, only inside longer words, is addressed as %v, reading %q", words, got, cut.Words(text, got))
		}
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
