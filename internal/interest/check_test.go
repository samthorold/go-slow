package interest_test

import (
	"testing"

	"github.com/samthorold/go-slow/internal/interest"
)

func TestTheCheckTiesAnItemToATopicByItsTerms(t *testing.T) {
	reform := interest.Topic{Name: "Reform UK", Depth: interest.Close, Terms: []string{"Reform UK", "Farage"}}

	cases := []struct {
		name string
		item interest.Item
		want bool
	}{
		{"a term in the title", interest.Item{Title: "Statement on Reform UK councillors"}, true},
		{"a term in the summary, in another case", interest.Item{Summary: "Mr FARAGE responded."}, true},
		{"a term in the content", interest.Item{Content: "<p>Nigel Farage, leader of Reform UK.</p>"}, true},
		{"a term split by punctuation", interest.Item{Title: "Reform-UK's response"}, true},
		{"part of a longer word", interest.Item{Title: "Farageist politics"}, false},
		{"no term", interest.Item{Title: "PM call with the President", Summary: "On reform of the NHS."}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := reform.Ties(c.item); got != c.want {
				t.Errorf("Ties(%+v) = %v, want %v", c.item, got, c.want)
			}
		})
	}
}

func TestTheCheckMatchesTheTextAReaderSeesNotTheMarkup(t *testing.T) {
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
			if topic.Ties(interest.Item{Content: content}) {
				t.Errorf("the term %q, only in the markup, ties the Item", c.term)
			}
		})
	}
}

func TestTheCheckTiesATermInTheVisibleText(t *testing.T) {
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
			if !topic.Ties(interest.Item{Content: c.content}) {
				t.Errorf("the term %q does not tie %q", c.term, c.content)
			}
		})
	}
}

func TestTheCheckTiesEveryItemFromATopicsSources(t *testing.T) {
	treasury := interest.Topic{Name: "The economy", Depth: interest.Passing, Sources: []string{"govuk-hm-treasury"}}

	if !treasury.Ties(interest.Item{SourceID: "govuk-hm-treasury", Title: "Anything at all"}) {
		t.Error("an Item from the Topic's Source is not tied")
	}
	if treasury.Ties(interest.Item{SourceID: "govuk-number-10", Title: "Anything at all"}) {
		t.Error("an Item from another Source is tied")
	}
}
