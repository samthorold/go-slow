package editor

import (
	"html"
	"regexp"
	"strings"
	"unicode"
)

var (
	// blockTag is a tag that ends a block of text: a paragraph, a heading, a
	// list item, a line break.
	blockTag = regexp.MustCompile(`(?i)</?(p|div|h[1-6]|li|ul|ol|br|blockquote|table|tr)\b[^>]*>`)
	anyTag   = regexp.MustCompile(`<[^>]*>`)
	blankRun = regexp.MustCompile(`\n\s*\n\s*`)
)

// plainText is an Item's HTML content as text: tags dropped, blocks on lines
// of their own, entities decoded. Collected content is HTML as its publisher
// wrote it (GOV.UK's govspeak), and the model is shown, and Facts are traced
// to, the words in it rather than its markup.
func plainText(s string) string {
	s = blockTag.ReplaceAllString(s, "\n")
	s = anyTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = blankRun.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// words lowercases s and turns every run of characters that are not letters
// or digits into one space, with a space at each end, so that a passage
// appears in a text, whatever its punctuation, quotes or spacing, exactly
// when words(passage) is a substring of words(text).
func words(s string) string {
	f := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return " " + strings.Join(f, " ") + " "
}

// sharesContentWord says whether a statement and its quote share a content
// word: lowercased, stopwords dropped, compared by a crude suffix stem so
// "walked" matches "walks". A statement sharing none says nothing its quote
// says, however genuine the quote; one sharing any passes, so an honest
// paraphrase does.
func sharesContentWord(statement, quote string) bool {
	in := map[string]bool{}
	for _, w := range strings.Fields(words(quote)) {
		if !stopwords[w] {
			in[stem(w)] = true
		}
	}
	for _, w := range strings.Fields(words(statement)) {
		if !stopwords[w] && in[stem(w)] {
			return true
		}
	}
	return false
}

// stem is a word cut to a crude stem: one inflectional suffix, then a
// trailing e, each only where at least three letters are left, so "uses",
// "used", "using" and "use" share one, as do "walks" and "walked".
func stem(w string) string {
	for _, suffix := range []string{"ing", "ed", "es", "s"} {
		if s, ok := strings.CutSuffix(w, suffix); ok && len(s) >= 3 {
			w = s
			break
		}
	}
	if s, ok := strings.CutSuffix(w, "e"); ok && len(s) >= 3 {
		w = s
	}
	return w
}

// stopwords are English words that carry no content of their own, as
// words gives them (an apostrophe splits "it's" into "it" and "s").
var stopwords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`a about above after again against all am an and any are as at
		be because been before being below between both but by can could did do does doing down
		during each few for from further had has have having he her here hers herself him himself
		his how i if in into is it its itself just me more most my myself no nor not now of off on
		once only or other our ours ourselves out over own same she should so some such than that
		the their theirs them themselves then there these they this those through to too under
		until up very was we were what when where which while who whom why will with would you
		your yours yourself yourselves s t d ll m re ve`) {
		m[w] = true
	}
	return m
}()
