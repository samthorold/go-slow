package web

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/samthorold/go-slow/internal/editor"
)

// inPlace is a hydrated Span's pieces with each inserted stretch's words
// cased as they would be written where they land: the reading's words keep
// their antecedent's case, which reads wrongly in place. Only the page's
// words change; Was keeps the words the stretch was. The possessive is the
// reading's own (editor.Piece), shared with what weaving is shown.
func inPlace(pieces []editor.Piece) []editor.Piece {
	out := make([]editor.Piece, len(pieces))
	var before strings.Builder
	for i, p := range pieces {
		if p.Was != "" {
			switch start := sentenceStart(before.String()); {
			case start && startsUpper(p.Was):
				p.Words = upperFirst(p.Words)
			case !start && strings.HasPrefix(p.Words, "The "):
				p.Words = "the " + strings.TrimPrefix(p.Words, "The ")
			}
		}
		before.WriteString(pieces[i].Words)
		out[i] = p
	}
	return out
}

// sentenceStart is whether words written after before begin a sentence:
// nothing but space or quotes before them, or a sentence's end.
func sentenceStart(before string) bool {
	rest := strings.TrimRight(before, " \t\n“”\"‘’'(")
	return rest == "" || strings.ContainsAny(rest[len(rest)-1:], ".!?")
}

// startsUpper is whether words begin with a capital letter.
func startsUpper(words string) bool {
	r, _ := utf8.DecodeRuneInString(words)
	return unicode.IsUpper(r)
}

// upperFirst is words with its first letter a capital.
func upperFirst(words string) string {
	r, n := utf8.DecodeRuneInString(words)
	return string(unicode.ToUpper(r)) + words[n:]
}
