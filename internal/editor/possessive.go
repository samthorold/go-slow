package editor

import (
	"slices"
	"strings"
)

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
