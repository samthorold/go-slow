package web

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/samthorold/go-slow/internal/editor"
)

// inPlace is a hydrated Span's pieces with each inserted stretch's words
// read as they would be written where they land: the reading's words keep
// their antecedent's case and form, which reads wrongly in place. Only the
// page's words change; Was keeps the words the stretch was, and the
// reading's own Hydrated, which relating is shown, is left as it is.
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
			if possessive(p.Was) {
				p.Words = possessed(p.Words)
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

// possessives are the determiners that stand for whose something is.
// "her" is also an object ("told her"); it is read as possessive, as the
// readings' stretches seen so far have it.
var possessives = []string{"its", "their", "his", "her", "our", "my", "your"}

// possessive is whether the stretch words was says whose something is: its
// last word a possessive determiner, or ending in 's.
func possessive(was string) bool {
	fields := strings.Fields(was)
	if len(fields) == 0 {
		return false
	}
	last := strings.ToLower(fields[len(fields)-1])
	return slices.Contains(possessives, last) || endsApostropheS(last)
}

// possessed is words made possessive, unless it already is: ’s, or just ’
// after an s.
func possessed(words string) string {
	switch {
	case endsApostropheS(words):
		return words
	case strings.HasSuffix(words, "s"):
		return words + "’"
	default:
		return words + "’s"
	}
}

// endsApostropheS is whether words ends in 's, either apostrophe.
func endsApostropheS(words string) bool {
	return strings.HasSuffix(words, "'s") || strings.HasSuffix(words, "’s")
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
