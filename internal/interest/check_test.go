package interest_test

import (
	"testing"

	"github.com/samthorold/go-slow/internal/interest"
)

func TestATermOnlyInTheMarkupTouchesNothing(t *testing.T) {
	content := `<div class="govspeak"><h3 id="background">Context</h3><p>See <a href="https://www.gov.uk/government/news/omagh-inquiry">the inquiry</a>.</p></div>`

	cases := []struct {
		name string
		term string
	}{
		{"a tag name", "div"},
		{"an attribute name", "class"},
		{"an attribute value", "govspeak"},
		{"an id", "background"},
		{"a word in a link URL", "Omagh"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			topic := interest.Topic{Name: c.term, Terms: []string{c.term}}
			if topic.Touches(content) {
				t.Errorf("the term %q, only in the markup, touches the Span", c.term)
			}
		})
	}
}

func TestATermInTheVisibleTextTouches(t *testing.T) {
	cases := []struct {
		name    string
		term    string
		content string
	}{
		{"inside a tag", "Omagh", `<p>Omagh</p>`},
		{"across tags", "Omagh bombing", `<h3>Omagh</h3><p>bombing inquiry</p>`},
		{"with a named entity decoded", "Marks & Spencer", `<p>Marks &amp; Spencer</p>`},
		{"with a numeric entity decoded", "O'Neill", `<p>Michelle O&#8217;Neill</p>`},
		{"beside a less-than sign that starts no tag", "rates", `<p>inflation < 2% and rates</p>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			topic := interest.Topic{Name: c.term, Terms: []string{c.term}}
			if !topic.Touches(c.content) {
				t.Errorf("the term %q does not touch %q", c.term, c.content)
			}
		})
	}
}

// A Span touches a Topic by the Topic's terms alone.
func TestASpanTouchesATopicByItsTermsAsWholeWordsOfTheTextAReaderSees(t *testing.T) {
	reform := interest.Topic{Name: "Reform UK", Terms: []string{"Reform UK", "Farage"}}

	cases := []struct {
		name string
		span string
		want bool
	}{
		{"a term", "Nigel Farage, leader of Reform UK.", true},
		{"a term in another case, split by punctuation", "Reform-UK's response, said Mr FARAGE.", true},
		{"a term in markup", `Said <b>Farage</b>.`, true},
		{"a term only in the markup", `See <a href="https://example.org/farage">the statement</a>.`, false},
		{"a term with an entity decoded", "Nigel&#160;Farage spoke.", true},
		{"part of a longer word", "Farageist politics.", false},
		{"no term", "On reform of the NHS.", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := reform.Touches(c.span); got != c.want {
				t.Errorf("Touches(%q) = %v, want %v", c.span, got, c.want)
			}
		})
	}
}
