package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Agreement is how alike a fixture's samples are in the Entities each Fact
// involves: for each Fact, the Jaccard index of two samples' sets (two empty
// sets agree), averaged over every pair of samples and every Fact they
// share. It is reported for connecting, whose samples share their Facts
// (F1, F2...), by Entity and Role and by Entity alone. Reading judges Spans
// and draws no Facts, so it has none.

// outputs are, per act and fixture, each sample's output, by its last
// attempt.
type outputs map[string]map[string][]json.RawMessage

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

// agreements are a fixture's two agreements on one side: connecting's by
// Entity and Role, and by Entity. A column is missing (nil) where there are
// fewer than two samples to compare.
type agreements [2][]float64

func agreementsOf(o outputs, fixture string) (agreements, error) {
	var a agreements
	conn := o["connecting"]["connecting/"+fixture]
	for i, withRoles := range []bool{true, false} {
		m, ok, err := connectingAgreement(conn, withRoles)
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
	b.WriteString("### agreement\n\n| Fixture | Connecting (Entities and Roles) | Connecting (Entities) |\n|---|---|---|\n")
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
