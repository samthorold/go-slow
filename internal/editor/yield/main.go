// Command yield reports how much each of the Editor's acts drew from each
// fixture in two sets of recorded runs, before → after, so a re-recording's
// shift in yield is seen without diffing JSON. It is a dev tool, not part of
// the app: it reads the repository's recordings, never the Record.
//
//	go run ./internal/editor/yield <before> <after>
//
// Each argument is a recordings directory (internal/editor/testdata/recorded,
// or an older copy of it, e.g. extracted with git archive). A fixture
// recorded as several samples (<fixture>/<k>.json) is counted by each
// sample's last attempt, and shown as the range its samples drew (least–most);
// a total is the range from every fixture's least to its most. It reports,
// it never fails on a change in yield.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "yield:", err)
		os.Exit(1)
	}
}

func run(args []string, w io.Writer) error {
	if len(args) != 2 {
		return errors.New("usage: yield <before> <after>")
	}
	before, beforeOut, err := load(args[0])
	if err != nil {
		return err
	}
	after, afterOut, err := load(args[1])
	if err != nil {
		return err
	}
	var b strings.Builder
	report(&b, before, after)
	if err := reportAgreement(&b, beforeOut, afterOut); err != nil {
		return err
	}
	_, err = io.WriteString(w, b.String())
	return err
}

// recording is the little of a kept response the report needs.
type recording struct {
	Act    string          `json:"act"`
	Output json.RawMessage `json:"output"`
}

// entity is an Entity a reading names, by kind and name. Readings recorded
// before Entities carried a kind name them as a plain string, read as a name
// with no kind.
type entity struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

func (e *entity) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		*e = entity{}
		return json.Unmarshal(data, &e.Name)
	}
	type plain entity
	return json.Unmarshal(data, (*plain)(e))
}

// act is one recorded act's columns and how a recording's output is counted.
type act struct {
	name    string
	columns []string
	count   func(json.RawMessage) ([]int, error)
}

var acts = []act{
	{"reading", []string{"Facts", "Claims", "Entities"}, countReading},
	{"placing", []string{"Developments", "New Stories"}, countPlacing},
	{"connecting", []string{"Connections"}, countConnecting},
	{"briefing", []string{"Statements", "Stories"}, countBriefing},
}

// countPlacing counts each Fact's Developments (one per Story it is placed
// in) and the Stories the placing started.
func countPlacing(out json.RawMessage) ([]int, error) {
	var r struct {
		Facts []struct {
			DevelopmentsIn []string `json:"developments_in"`
		} `json:"facts"`
		NewStories []json.RawMessage `json:"new_stories"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, err
	}
	developments := 0
	for _, f := range r.Facts {
		developments += len(f.DevelopmentsIn)
	}
	return []int{developments, len(r.NewStories)}, nil
}

// countConnecting counts the cause Connections the act made.
func countConnecting(out json.RawMessage) ([]int, error) {
	var r struct {
		Causes []json.RawMessage `json:"causes"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, err
	}
	return []int{len(r.Causes)}, nil
}

// countBriefing counts the Briefing's statements and the Stories they are in.
func countBriefing(out json.RawMessage) ([]int, error) {
	var r struct {
		Stories []struct {
			Statements []json.RawMessage `json:"statements"`
		} `json:"stories"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, err
	}
	statements := 0
	for _, s := range r.Stories {
		statements += len(s.Statements)
	}
	return []int{statements, len(r.Stories)}, nil
}

// readingOutput is the little of a reading the report needs. A reading
// gives the Item's mentions once; one in the earlier schema gave Entities
// with each Fact and Claim, which together are what its Item mentions.
type readingOutput struct {
	Facts    []readStatement `json:"facts"`
	Claims   []readStatement `json:"claims"`
	Mentions []entity        `json:"mentions"`
}

type readStatement struct {
	Statement string   `json:"statement"`
	Entities  []entity `json:"entities"`
}

// mentions are the distinct Entities the reading's Item mentions.
func (r readingOutput) mentions() map[entity]bool {
	distinct := map[entity]bool{}
	for _, e := range r.Mentions {
		distinct[e] = true
	}
	for _, s := range append(append([]readStatement{}, r.Facts...), r.Claims...) {
		for _, e := range s.Entities {
			distinct[e] = true
		}
	}
	return distinct
}

func countReading(out json.RawMessage) ([]int, error) {
	var r readingOutput
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, err
	}
	return []int{len(r.Facts), len(r.Claims), len(r.mentions())}, nil
}

// counts is, per act, each fixture's counts, one per sample: a fixture
// recorded once has one.
type counts map[string]map[string][][]int

// fixtureOf is the fixture a recording at rel (with no extension) is of: a
// sample, kept as <fixture>/<k>, is of its directory.
func fixtureOf(rel string) string {
	dir, base := path.Split(rel)
	if _, err := strconv.Atoi(base); err == nil && dir != "" {
		return strings.TrimSuffix(dir, "/")
	}
	return rel
}

func load(dir string) (counts, outputs, error) {
	c, o := counts{}, outputs{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// A downstream act's frozen copies of the recordings it builds
			// on are not its own yield.
			if d.Name() == "inputs" && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".json" {
			return nil
		}
		data, err := os.ReadFile(path) //nolint:gosec // a dev tool reading the directory it was given
		if err != nil {
			return err
		}
		// Files that are not a kept response (a recorded Wikidata answer)
		// are not counted, nor are acts with no counts below.
		var rec recording
		if json.Unmarshal(data, &rec) != nil {
			return nil
		}
		for _, a := range acts {
			if a.name != rec.Act {
				continue
			}
			n, err := a.count(rec.Output)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			if c[a.name] == nil {
				c[a.name] = map[string][][]int{}
			}
			f := fixtureOf(strings.TrimSuffix(filepath.ToSlash(rel), ".json"))
			c[a.name][f] = append(c[a.name][f], n)
			if o[a.name] == nil {
				o[a.name] = map[string][]json.RawMessage{}
			}
			o[a.name][f] = append(o[a.name][f], rec.Output)
		}
		return nil
	})
	return c, o, err
}

func report(b *strings.Builder, before, after counts) {
	for _, a := range acts {
		fixtures := map[string]bool{}
		for f := range before[a.name] {
			fixtures[f] = true
		}
		for f := range after[a.name] {
			fixtures[f] = true
		}
		names := make([]string, 0, len(fixtures))
		for f := range fixtures {
			names = append(names, f)
		}
		if len(names) == 0 {
			continue
		}
		sort.Strings(names)
		fmt.Fprintf(b, "### %s\n\n| Fixture | %s |\n|%s\n", a.name, strings.Join(a.columns, " | "), strings.Repeat("---|", len(a.columns)+1))
		totalBefore, totalAfter := make([]span, len(a.columns)), make([]span, len(a.columns))
		for _, f := range names {
			was, inBefore := before[a.name][f]
			now, inAfter := after[a.name][f]
			label := f
			switch {
			case !inBefore:
				label += " (added)"
			case !inAfter:
				label += " (removed)"
			}
			cells := make([]string, len(a.columns))
			for i := range a.columns {
				cells[i] = cell(was, i) + " → " + cell(now, i)
				if inBefore {
					totalBefore[i] = totalBefore[i].add(spanOf(was, i))
				}
				if inAfter {
					totalAfter[i] = totalAfter[i].add(spanOf(now, i))
				}
			}
			fmt.Fprintf(b, "| %s | %s |\n", label, strings.Join(cells, " | "))
		}
		cells := make([]string, len(a.columns))
		for i := range a.columns {
			cells[i] = totalBefore[i].String() + " → " + totalAfter[i].String()
		}
		fmt.Fprintf(b, "| **Total** | %s |\n\n", strings.Join(cells, " | "))
	}
}

// span is the least and most a fixture's samples drew in a column.
type span struct{ least, most int }

func spanOf(samples [][]int, i int) span {
	s := span{samples[0][i], samples[0][i]}
	for _, n := range samples[1:] {
		s.least, s.most = min(s.least, n[i]), max(s.most, n[i])
	}
	return s
}

func (s span) add(o span) span { return span{s.least + o.least, s.most + o.most} }

func (s span) String() string {
	if s.least == s.most {
		return fmt.Sprint(s.least)
	}
	return fmt.Sprintf("%d–%d", s.least, s.most)
}

// cell is one side's count in column i, the range its samples drew, or a
// dash where the fixture has no recording on that side.
func cell(samples [][]int, i int) string {
	if samples == nil {
		return "–"
	}
	return spanOf(samples, i).String()
}
