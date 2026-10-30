package editor

import (
	"cmp"
	"slices"
	"strings"

	"github.com/samthorold/go-slow/internal/cut"
)

// Piece is a stretch of a Span's hydrated reading: its words, and for a
// stretch read in the words of what it refers to, the words it was.
type Piece struct {
	Words string
	Was   string
}

// Hydrated is the hydrated reading of the Span s of text, the Item's text
// as kept, by r: each stretch within it that refers to another read as the
// words of what it refers to, followed as far as it goes (system design:
// Hydration). It is worked out, never written.
func (r Reading) Hydrated(text string, s cut.Span) []Piece {
	var within []RefersTo
	for _, ref := range r.RefersTo {
		if ref.From.Span.Start >= s.Start && ref.From.Span.End <= s.End {
			within = append(within, ref)
		}
	}
	slices.SortFunc(within, func(a, b RefersTo) int {
		return cmp.Or(cmp.Compare(a.From.Span.Start, b.From.Span.Start), cmp.Compare(b.From.Span.End, a.From.Span.End))
	})
	var out []Piece
	at := s.Start
	for _, ref := range within {
		if ref.From.Span.Start < at {
			// Inside a stretch already read in other words.
			continue
		}
		if w := cut.Stretch(text, cut.Span{Start: at, End: ref.From.Span.Start}); w != "" {
			out = append(out, Piece{Words: w})
		}
		out = append(out, Piece{Words: r.Words(text, ref.From), Was: cut.Words(text, ref.From.Span)})
		at = ref.From.Span.End
	}
	if w := cut.Stretch(text, cut.Span{Start: at, End: s.End}); w != "" {
		out = append(out, Piece{Words: w})
	}
	if len(out) > 0 {
		out[0].Words = strings.TrimLeft(out[0].Words, " ")
		out[len(out)-1].Words = strings.TrimRight(out[len(out)-1].Words, " ")
	}
	return out
}

// Words is the words the Span at a reads as, by r: the words of what it
// refers to, followed as far as it goes without coming back on itself, or
// its own.
func (r Reading) Words(text string, a Address) string {
	seen := []Address{a}
	for {
		i := slices.IndexFunc(r.RefersTo, func(ref RefersTo) bool { return ref.From == a })
		if i < 0 || slices.Contains(seen, r.RefersTo[i].To) {
			return cut.Words(text, a.Span)
		}
		a = r.RefersTo[i].To
		seen = append(seen, a)
	}
}
