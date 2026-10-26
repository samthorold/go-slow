package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// Agreement is how alike a fixture's samples are in the Entities each Fact
// involves: for each Fact, the Jaccard index of two samples' sets (two empty
// sets agree), averaged over every pair of samples and every Fact they
// share. It is reported for connecting, whose samples share their Facts
// (F1, F2...), by Entity and Role and by Entity alone, and for the same
// fixtures' readings, whose per-Fact lists name Entities with no Role, in
// readings recorded before reading gave the Item's mentions once.
// Reading's samples draw their own Facts, so each Fact of one sample is
// paired with the statement of the other sharing the most of its words, at
// least half of them (Jaccard over lower-case words), one to one; a Fact with
// no such pair is left out, since its list has nothing to agree with.
// Reading's samples are also compared on what their Item mentions: the
// Jaccard index of two samples' sets of names, over every pair of samples
// (for an earlier reading, the names its lists give).

// outputs are, per act and fixture, each sample's output, by its last
// attempt.
type outputs map[string]map[string][]json.RawMessage

// minWordOverlap is how alike two of reading's statements must be, by the
// Jaccard index of their words, to be taken for one Fact.
const minWordOverlap = 0.5

type set map[string]bool

func jaccard(a, b set) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1
	}
	shared := 0
	for k := range a {
		if b[k] {
			shared++
		}
	}
	return float64(shared) / float64(len(a)+len(b)-shared)
}

// mean is the mean of xs, and whether there were any.
func mean(xs []float64) (float64, bool) {
	if len(xs) == 0 {
		return 0, false
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs)), true
}

// connectingSets are a connecting sample's sets per Fact ref: of Entity and
// Role when withRoles, otherwise of Entity alone.
func connectingSets(out json.RawMessage, withRoles bool) (map[string]set, error) {
	var r struct {
		Facts []struct {
			Fact  string `json:"fact"`
			Roles []struct {
				Entity string `json:"entity"`
				Role   string `json:"role"`
			} `json:"roles"`
		} `json:"facts"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, err
	}
	sets := map[string]set{}
	for _, f := range r.Facts {
		s := set{}
		for _, role := range f.Roles {
			k := role.Entity
			if withRoles {
				k += " as " + role.Role
			}
			s[k] = true
		}
		sets[f.Fact] = s
	}
	return sets, nil
}

// connectingAgreement is the agreement of connecting's samples.
func connectingAgreement(samples []json.RawMessage, withRoles bool) (float64, bool, error) {
	var all []map[string]set
	for _, s := range samples {
		sets, err := connectingSets(s, withRoles)
		if err != nil {
			return 0, false, err
		}
		all = append(all, sets)
	}
	var xs []float64
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			refs := set{}
			for ref := range all[i] {
				refs[ref] = true
			}
			for ref := range all[j] {
				refs[ref] = true
			}
			for ref := range refs {
				xs = append(xs, jaccard(all[i][ref], all[j][ref]))
			}
		}
	}
	m, ok := mean(xs)
	return m, ok, nil
}

// readFact is one of reading's Facts: its statement's words and its
// Entities' names.
type readFact struct{ words, entities set }

func words(s string) set {
	out := set{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		out[w] = true
	}
	return out
}

// readFacts are a reading's Facts with their lists, and whether it gives
// lists: one that gives the Item's mentions once gives none.
func readFacts(out json.RawMessage) ([]readFact, bool, error) {
	var r readingOutput
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, false, err
	}
	if r.Mentions != nil {
		return nil, false, nil
	}
	var fs []readFact
	for _, f := range r.Facts {
		es := set{}
		for _, e := range f.Entities {
			es[e.Name] = true
		}
		fs = append(fs, readFact{words(f.Statement), es})
	}
	return fs, true, nil
}

// pairedAgreements are the Jaccard indexes of the Entities of a's and b's
// Facts paired one to one, the most alike statements first.
func pairedAgreements(a, b []readFact) []float64 {
	type pair struct {
		i, j  int
		alike float64
	}
	var pairs []pair
	for i := range a {
		for j := range b {
			if alike := jaccard(a[i].words, b[j].words); alike >= minWordOverlap {
				pairs = append(pairs, pair{i, j, alike})
			}
		}
	}
	sort.SliceStable(pairs, func(x, y int) bool { return pairs[x].alike > pairs[y].alike })
	usedA, usedB := map[int]bool{}, map[int]bool{}
	var xs []float64
	for _, p := range pairs {
		if usedA[p.i] || usedB[p.j] {
			continue
		}
		usedA[p.i], usedB[p.j] = true, true
		xs = append(xs, jaccard(a[p.i].entities, b[p.j].entities))
	}
	return xs
}

// readingAgreement is the agreement of reading's samples' per-Fact lists.
func readingAgreement(samples []json.RawMessage) (float64, bool, error) {
	var all [][]readFact
	for _, s := range samples {
		fs, lists, err := readFacts(s)
		if err != nil {
			return 0, false, err
		}
		if lists {
			all = append(all, fs)
		}
	}
	var xs []float64
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			xs = append(xs, pairedAgreements(all[i], all[j])...)
		}
	}
	m, ok := mean(xs)
	return m, ok, nil
}

// mentionsAgreement is the agreement of reading's samples on the names
// their Item mentions.
func mentionsAgreement(samples []json.RawMessage) (float64, bool, error) {
	var all []set
	for _, s := range samples {
		var r readingOutput
		if err := json.Unmarshal(s, &r); err != nil {
			return 0, false, err
		}
		names := set{}
		for e := range r.mentions() {
			names[e.Name] = true
		}
		all = append(all, names)
	}
	var xs []float64
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			xs = append(xs, jaccard(all[i], all[j]))
		}
	}
	m, ok := mean(xs)
	return m, ok, nil
}

// agreements are a fixture's four agreements on one side: reading's per-Fact
// lists, reading's mentions, connecting's by Entity and Role, and
// connecting's by Entity. A column is missing (nil) where there are fewer
// than two samples to compare.
type agreements [4][]float64

func agreementsOf(o outputs, fixture string) (agreements, error) {
	var a agreements
	conn := o["connecting"]["connecting/"+fixture]
	for i, f := range []func() (float64, bool, error){
		func() (float64, bool, error) { return readingAgreement(o["reading"]["reading/"+fixture]) },
		func() (float64, bool, error) { return mentionsAgreement(o["reading"]["reading/"+fixture]) },
		func() (float64, bool, error) { return connectingAgreement(conn, true) },
		func() (float64, bool, error) { return connectingAgreement(conn, false) },
	} {
		m, ok, err := f()
		if err != nil {
			return a, err
		}
		if ok {
			a[i] = []float64{m}
		}
	}
	return a, nil
}

// cell is one column of a side: its mean, or a dash where it has none.
func (a agreements) cell(i int) string {
	m, ok := mean(a[i])
	if !ok {
		return "–"
	}
	return fmt.Sprintf("%.2f", m)
}

func (a agreements) add(o agreements) agreements {
	for i := range a {
		a[i] = append(a[i], o[i]...)
	}
	return a
}

// reportAgreement writes the agreement table, before → after, for every
// fixture connecting has a recording of on either side.
func reportAgreement(b *strings.Builder, before, after outputs) error {
	fixtures := set{}
	for _, o := range []outputs{before, after} {
		for f := range o["connecting"] {
			fixtures[strings.TrimPrefix(f, "connecting/")] = true
		}
	}
	if len(fixtures) == 0 {
		return nil
	}
	names := make([]string, 0, len(fixtures))
	for f := range fixtures {
		names = append(names, f)
	}
	sort.Strings(names)
	b.WriteString("### agreement\n\n| Fixture | Reading (Entities) | Reading (mentions) | Connecting (Entities and Roles) | Connecting (Entities) |\n|---|---|---|---|---|\n")
	row := func(label string, was, now agreements) {
		fmt.Fprintf(b, "| %s |", label)
		for i := range was {
			fmt.Fprintf(b, " %s → %s |", was.cell(i), now.cell(i))
		}
		b.WriteString("\n")
	}
	var allBefore, allAfter agreements
	for _, f := range names {
		was, err := agreementsOf(before, f)
		if err != nil {
			return fmt.Errorf("agreement of %s: %w", f, err)
		}
		now, err := agreementsOf(after, f)
		if err != nil {
			return fmt.Errorf("agreement of %s: %w", f, err)
		}
		row(f, was, now)
		allBefore, allAfter = allBefore.add(was), allAfter.add(now)
	}
	row("**Mean**", allBefore, allAfter)
	b.WriteString("\n")
	return nil
}
