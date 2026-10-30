// Package cut is the Cut: listing an Item's Spans before anything else is
// judged of them, in plain code (system design: The grain belongs to the
// judgement, not to the Span). A Cut is a Judgement, kept with its Judge (the
// cutter and its version) and never changed; a Span is an address into the
// Item's text as kept, not a thing with an identity of its own.
package cut

import (
	"html"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Span is a stretch of an Item's text, known by where it starts and ends:
// byte offsets into the text as kept, Start inclusive, End exclusive. Two
// Judgements about the same words have equal Spans.
type Span struct {
	Start, End int
}

// blockTag is a tag that ends a block of text: a paragraph, a heading, a
// list item, a line break. No sentence runs across one.
var blockTag = regexp.MustCompile(`(?i)^</?(p|div|h[1-6]|li|ul|ol|br|blockquote|table|tr|td|th)\b[^>]*>`)

// char is one character of an Item's text as read, with the bytes of the
// text as kept it was read from: an entity is one character from several
// bytes, and a tag is none.
type char struct {
	r          rune
	start, end int
}

// Sentences is the sentence cutter: it lists the sentences of an Item's
// text, which is HTML as its publisher wrote it, in order. A sentence never
// crosses a block (a paragraph, a heading, a list item) or a blank line; one
// ends at a full stop, question or exclamation mark, with any closing quote
// or bracket after it, where what follows begins with a capital, a digit or
// an opening quote or bracket. Each Span starts at the sentence's first
// character and ends after its last, so it holds no surrounding space.
func Sentences(text string) []Span {
	var spans []Span
	for _, block := range blocks(text) {
		start := 0
		for i := 0; i < len(block); i++ {
			if !strings.ContainsRune(".!?", block[i].r) {
				continue
			}
			end := i + 1
			for end < len(block) && strings.ContainsRune(`"'”’)]`, block[end].r) {
				end++
			}
			next := end
			for next < len(block) && unicode.IsSpace(block[next].r) {
				next++
			}
			if next == end || next == len(block) || !opens(block[next].r) {
				continue
			}
			spans = appendSpan(spans, block[start:end])
			start, i = next, next-1
		}
		spans = appendSpan(spans, block[start:])
	}
	return spans
}

// opens reports whether r can begin a sentence.
func opens(r rune) bool {
	return unicode.IsUpper(r) || unicode.IsDigit(r) || strings.ContainsRune(`"'“‘([`, r)
}

// appendSpan appends the Span of chars, trimmed of space, unless nothing is
// left.
func appendSpan(spans []Span, chars []char) []Span {
	for len(chars) > 0 && unicode.IsSpace(chars[0].r) {
		chars = chars[1:]
	}
	for len(chars) > 0 && unicode.IsSpace(chars[len(chars)-1].r) {
		chars = chars[:len(chars)-1]
	}
	if len(chars) == 0 {
		return spans
	}
	return append(spans, Span{Start: chars[0].start, End: chars[len(chars)-1].end})
}

// blocks reads text into its blocks of characters: tags dropped, entities
// decoded, and a new block at every block tag and blank line.
func blocks(text string) [][]char {
	var out [][]char
	var cur []char
	newlines := 0
	flush := func() {
		if len(cur) > 0 {
			out = append(out, cur)
		}
		cur, newlines = nil, 0
	}
	for i := 0; i < len(text); {
		switch {
		case text[i] == '<' && strings.IndexByte(text[i:], '>') > 0:
			end := i + strings.IndexByte(text[i:], '>') + 1
			if blockTag.MatchString(text[i:end]) {
				flush()
			}
			i = end
			continue
		case text[i] == '&':
			if semi := strings.IndexByte(text[i:], ';'); semi > 1 && semi < 12 {
				if dec := html.UnescapeString(text[i : i+semi+1]); dec != text[i:i+semi+1] {
					for _, r := range dec {
						cur = append(cur, char{r: r, start: i, end: i + semi + 1})
					}
					newlines = 0
					i += semi + 1
					continue
				}
			}
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == '\n' {
			newlines++
			if newlines == 2 {
				flush()
				i += size
				continue
			}
		} else if !unicode.IsSpace(r) {
			newlines = 0
		}
		cur = append(cur, char{r: r, start: i, end: i + size})
		i += size
	}
	flush()
	return out
}

// space is a run of white space.
var space = regexp.MustCompile(`\s+`)

// anyTag is any tag.
var anyTag = regexp.MustCompile(`<[^>]*>`)

// Words is the words the Span of text holds, as read: tags dropped, entities
// decoded and each run of space one space.
func Words(text string, s Span) string {
	return strings.TrimSpace(Stretch(text, s))
}

// Stretch is the words of the Span s of text as a stretch of a longer Span:
// as Words, but with the space at either end kept, as one space, so the
// stretches of a Span read as the Span when put back together.
func Stretch(text string, s Span) string {
	if s.Start < 0 || s.End > len(text) || s.Start >= s.End {
		return ""
	}
	// A Span crosses no block tag, so what tags it holds are inline ones,
	// inside words or between them.
	w := anyTag.ReplaceAllString(text[s.Start:s.End], "")
	w = html.UnescapeString(w)
	return space.ReplaceAllString(w, " ")
}
