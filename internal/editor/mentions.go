package editor

import (
	"slices"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
)

// Mentions are the names of what the Span s mentions, by r, each once, in
// the order r judged them: a Span mentions what a Span inside it names, or
// what a Span inside it refers to names, followed as far as the references
// go without coming back on themselves (system design: Entities find
// candidates; they don't say who did what). It is worked out, never
// written. Which Entity each name is comes from its look-up (KeyOf).
func (r Reading) Mentions(s cut.Span) []Name {
	var reached []cut.Span
	reach := func(sp cut.Span) bool {
		if slices.Contains(reached, sp) {
			return false
		}
		reached = append(reached, sp)
		return true
	}
	reach(s)
	for _, ref := range r.RefersTo {
		if !inside(ref.From.Span, s) {
			continue
		}
		for to := ref.To; reach(to.Span); {
			i := slices.IndexFunc(r.RefersTo, func(next RefersTo) bool { return next.From == to })
			if i < 0 {
				break
			}
			to = r.RefersTo[i].To
		}
	}
	var out []Name
	for _, n := range r.Names {
		if !slices.ContainsFunc(reached, func(sp cut.Span) bool { return inside(n.At.Span, sp) }) {
			continue
		}
		if !slices.ContainsFunc(out, func(o Name) bool { return o.Name == n.Name && o.Kind == n.Kind && o.Office == n.Office }) {
			out = append(out, n)
		}
	}
	return out
}

// NamedEntity is an Entity as the Record's look-ups have it: a Wikidata
// item, or one the Editor made for a name with no match, until a later
// look-up merges it into an item (system design: Entities come from
// Wikidata first, otherwise from the Editor). Two are the same Entity
// exactly when they are equal.
type NamedEntity struct {
	// WikidataID is the Wikidata item it is; empty for the Editor's own.
	WikidataID string
	// EditorID is the Editor's Entity's id in the Record; zero for a
	// Wikidata item, merged ones included.
	EditorID int64
	// Name is the item's English label, or the name the Editor's Entity was
	// made for.
	Name, Kind string
}

// Keys are the keys of every name r judged, in an Item published on
// published, each once.
func (r Reading) Keys(published time.Time) []LookupKey {
	var out []LookupKey
	for _, n := range r.Names {
		if k := KeyOf(n, published); !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

// Entities are the Entities the Span s mentions, by r, in an Item published
// on published, each once: the Entity each name it mentions is, by is, the
// Entities the Record's look-ups have for each key. A name not yet looked
// up is in none.
func (r Reading) Entities(s cut.Span, published time.Time, is map[LookupKey]NamedEntity) []NamedEntity {
	var out []NamedEntity
	for _, n := range r.Mentions(s) {
		if e, ok := is[KeyOf(n, published)]; ok && !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	return out
}

// inside is whether the Span a lies within the Span s.
func inside(a, s cut.Span) bool { return a.Start >= s.Start && a.End <= s.End }
