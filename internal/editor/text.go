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
