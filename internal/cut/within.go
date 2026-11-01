package cut

import (
	"html"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Within is the address of words quoted within the Span s of text, the
// first place they occur in it, reporting whether they do. Words are matched
// as a reader sees them (tags dropped, entities decoded, each run of space
// one space, curled quotes the same as straight ones), exactly otherwise,
// and only as whole words: a match starting or ending inside a run of
// letters or digits is no match, so words found only inside longer words
// ("he" in "The") have no address. Empty words are the whole Span. This is how a judge shown an Item as
// numbered Spans cites a finer one: by its number and its words, which code
// turns into an address (system design: The grain belongs to the judgement,
// not to the Span).
func Within(text string, s Span, words string) (Span, bool) {
	if s.Start < 0 || s.End > len(text) || s.Start >= s.End {
		return Span{}, false
	}
	read := readable(text, s)
	want := fold(words)
	if len(want) == 0 {
		return s, true
	}
	got := make([]rune, len(read))
	for i, c := range read {
		got[i] = c.r
	}
	for i := 0; i+len(want) <= len(got); i++ {
		if runesEqual(got[i:i+len(want)], want) && boundary(got, i) && boundary(got, i+len(want)) {
			return Span{Start: read[i].start, End: read[i+len(want)-1].end}, true
		}
	}
	return Span{}, false
}

// readable is the characters of the Span s of text as a reader sees them,
// folded as fold folds words: tags dropped, entities decoded, each run of
// space one space with none at either end, and quotes straight. Each keeps
// the bytes of text it was read from.
func readable(text string, s Span) []char {
	var out []char
	space := false
	for i := s.Start; i < s.End; {
		if text[i] == '<' {
			if end := strings.IndexByte(text[i:s.End], '>'); end > 0 {
				i += end + 1
				continue
			}
		}
		var dec string
		size := 0
		if text[i] == '&' {
			if semi := strings.IndexByte(text[i:s.End], ';'); semi > 1 && semi < 12 {
				if d := html.UnescapeString(text[i : i+semi+1]); d != text[i:i+semi+1] {
					dec, size = d, semi+1
				}
			}
		}
		if size == 0 {
			r, n := utf8.DecodeRuneInString(text[i:])
			dec, size = string(r), n
		}
		for _, r := range dec {
			if unicode.IsSpace(r) {
				space = len(out) > 0
				continue
			}
			if space {
				out = append(out, char{r: ' ', start: i, end: i + size})
				space = false
			}
			out = append(out, char{r: straight(r), start: i, end: i + size})
		}
		i += size
	}
	return out
}

// fold is words as readable reads a Span.
func fold(words string) []rune {
	var out []rune
	for _, f := range strings.Fields(words) {
		if len(out) > 0 {
			out = append(out, ' ')
		}
		for _, r := range f {
			out = append(out, straight(r))
		}
	}
	return out
}

// straight is r with a curled quote made straight.
func straight(r rune) rune {
	switch r {
	case '‘', '’', '‛', '′':
		return '\''
	case '“', '”', '„', '″':
		return '"'
	}
	return r
}

// boundary reports whether i, a place between the characters of got, is not
// inside a run of letters or digits: a word starts or ends there.
func boundary(got []rune, i int) bool {
	return i == 0 || i == len(got) || !wordChar(got[i-1]) || !wordChar(got[i])
}

func wordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func runesEqual(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
