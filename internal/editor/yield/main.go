// Command yield reports how much each of the Editor's acts drew from each
// fixture in two sets of recorded runs, before → after, so a re-recording's
// shift in yield is seen without diffing JSON. It is a dev tool, not part of
// the app: it reads the repository's recordings, never the Record.
//
//	go run ./internal/editor/yield <before> <after>
//
// Relating is counted by its relations' kinds; a sample of an Item related
// with no call counts none. Placing and connecting, retired, are counted
// from recordings made before they were.
//
// Each argument is a recordings directory (internal/editor/testdata/recorded,
// or an older copy of it, e.g. extracted with git archive). A fixture
// recorded as several samples (<fixture>/<k>.json) is counted by each
// sample's last attempt, and shown as the range its samples drew (least–most);
// a total is the range from every fixture's least to its most. Frozen copies
// of upstream recordings (inputs/) are not counted; connecting's copies of
// readings are read only for the mentions the act was shown. Reading's Spans
// are those the sentence cutter makes of the fixture Item, from the fixtures
// beside either directory (../govuk). It reports, it never fails on a change
// in yield.
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

	"github.com/samthorold/go-slow/internal/cut"
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
	govuk := fixturesBeside(args[1], args[0])
	before, beforeOut, err := load(args[0], govuk)
	if err != nil {
		return err
	}
	after, afterOut, err := load(args[1], govuk)
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

// fixturesBeside is the fixture Items' directory beside the first of dirs
// that has one, or empty if none has.
func fixturesBeside(dirs ...string) string {
	for _, d := range dirs {
		g := filepath.Join(d, "..", "govuk")
		if info, err := os.Stat(g); err == nil && info.IsDir() {
			return g
		}
	}
	return ""
}

// recording is the little of a kept response the report needs.
type recording struct {
	Act    string          `json:"act"`
	Output json.RawMessage `json:"output"`
}

// entity is an Entity a reading of Facts names, by kind and name. Readings
// recorded before Entities carried a kind name them as a plain string, read
// as a name with no kind.
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

// about is what a recording's count may need beyond its output: for
// connecting, the mentions it was shown; for reading, the Spans of its
// fixture's Cut.
type about struct {
	shown set
	spans int
}

// act is one recorded act's columns and how a recording's output is counted.
type act struct {
	name    string
	columns []string
	count   func(out json.RawMessage, a about) ([]int, error)
}

var acts = []act{
	{"reading", []string{"Spans", "Rejected", "Names", "Speakers", "Refers to"}, countReading},
	{"relating", []string{"Follows", "Same as", "Contradicts"}, countRelating},
	{"placing", []string{"Developments", "New Stories"}, countPlacing},
	{"connecting", []string{"Connections", "Mentions involved in no Fact", "Facts involving no Entity"}, countConnecting},
	{"briefing", []string{"Statements", "Stories"}, countBriefing},
}

// countRelating counts the relations a relating judged, by kind. A sample of
// an Item related with no call (its Spans had no candidates) judged none.
func countRelating(out json.RawMessage, _ about) ([]int, error) {
	var r struct {
		Relations []struct {
			Verdict string `json:"verdict"`
		} `json:"relations"`
	}
	if len(out) > 0 && string(out) != "null" {
		if err := json.Unmarshal(out, &r); err != nil {
			return nil, err
		}
	}
	n := []int{0, 0, 0}
	for _, rel := range r.Relations {
		switch rel.Verdict {
		case "follows":
			n[0]++
		case "same_as":
			n[1]++
		case "contradicts":
			n[2]++
		}
	}
	return n, nil
}

// countPlacing counts each Fact's Developments (one per Story it is placed
// in) and the Stories the placing started.
func countPlacing(out json.RawMessage, _ about) ([]int, error) {
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

// countConnecting counts the cause Connections the act made, and what it
// left out: the mentions it was shown (by name) that it gave no Role in any
// Fact, and the Facts it gave no Roles. What it leaves out is counted, never
// judged.
func countConnecting(out json.RawMessage, a about) ([]int, error) {
	var r struct {
		Causes []json.RawMessage `json:"causes"`
		Facts  []struct {
			Roles []struct {
				Entity string `json:"entity"`
			} `json:"roles"`
		} `json:"facts"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, err
	}
	involved := set{}
	involvingNone := 0
	for _, f := range r.Facts {
		if len(f.Roles) == 0 {
			involvingNone++
		}
		for _, role := range f.Roles {
			involved[role.Entity] = true
		}
	}
	inNoFact := 0
	for name := range a.shown {
		if !involved[name] {
			inNoFact++
		}
	}
	return []int{len(r.Causes), inNoFact, involvingNone}, nil
}

// shownMentions are the names a connecting fixture's Item mentions, as the
// act was shown them: those of its frozen copy of the fixture's reading of
// Facts, under <dir>/connecting/inputs/reading/. A fixture with no such copy
// (one recorded before acts kept their inputs) was shown none the report can
// see.
func shownMentions(dir, fixture string) (set, error) {
	name := strings.TrimPrefix(fixture, "connecting/")
	data, err := os.ReadFile(filepath.Join(dir, "connecting", "inputs", "reading", name+".json")) //nolint:gosec // a dev tool reading the directory it was given
	if errors.Is(err, fs.ErrNotExist) {
		return set{}, nil
	}
	if err != nil {
		return nil, err
	}
	var rec struct {
		Output factReading `json:"output"`
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("%s's frozen reading: %w", fixture, err)
	}
	names := set{}
	for e := range rec.Output.mentions() {
		names[e.Name] = true
	}
	return names, nil
}

// countBriefing counts the Briefing's statements and the Stories they are in.
func countBriefing(out json.RawMessage, _ about) ([]int, error) {
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

// factReading is the little of the earlier reading act's output (Facts,
// Claims and mentions), as connecting's frozen copies keep it, the report
// needs. One in its earlier schema gave Entities with each Fact and Claim,
// which together are what its Item mentions.
type factReading struct {
	Facts []struct {
		Entities []entity `json:"entities"`
	} `json:"facts"`
	Claims []struct {
		Entities []entity `json:"entities"`
	} `json:"claims"`
	Mentions []entity `json:"mentions"`
}

// mentions are the distinct Entities the reading's Item mentions.
func (r factReading) mentions() map[entity]bool {
	distinct := map[entity]bool{}
	for _, e := range r.Mentions {
		distinct[e] = true
	}
	for _, f := range r.Facts {
		for _, e := range f.Entities {
			distinct[e] = true
		}
	}
	for _, c := range r.Claims {
		for _, e := range c.Entities {
			distinct[e] = true
		}
	}
	return distinct
}

// countReading counts a reading of Spans: the Spans its fixture was shown
// as, those it Rejected, the names and speakers it gave and the stretches it
// said refer to others. A reading by the earlier act, which drew Facts,
// judged none of these, and counts none.
func countReading(out json.RawMessage, a about) ([]int, error) {
	var r struct {
		Names    []json.RawMessage `json:"names"`
		RefersTo []json.RawMessage `json:"refers_to"`
		Rejected []json.RawMessage `json:"rejected"`
		Speakers []json.RawMessage `json:"speakers"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, err
	}
	return []int{a.spans, len(r.Rejected), len(r.Names), len(r.Speakers), len(r.RefersTo)}, nil
}

// fixtureSpans is how many Spans the sentence cutter makes of the fixture
// Item name in govuk (a GOV.UK Content API document), or 0 if there is no
// such fixture.
func fixtureSpans(govuk, name string) (int, error) {
	if govuk == "" {
		return 0, nil
	}
	data, err := os.ReadFile(filepath.Join(govuk, name+".json")) //nolint:gosec // a dev tool reading the fixtures beside the directory it was given
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var doc struct {
		Details struct {
			Body string `json:"body"`
		} `json:"details"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return 0, fmt.Errorf("fixture %s: %w", name, err)
	}
	return len(cut.Sentences(doc.Details.Body)), nil
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

func load(dir, govuk string) (counts, outputs, error) {
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
		if rec.Act == "" && (rec.Output == nil || string(rec.Output) == "null") {
			// A sample of work that made no call is its act's, by where it
			// is kept: <act>/<fixture>/<k>.json.
			if rel, err := filepath.Rel(dir, path); err == nil {
				rec.Act, _, _ = strings.Cut(filepath.ToSlash(rel), "/")
			}
		}
		for _, a := range acts {
			if a.name != rec.Act {
				continue
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			f := fixtureOf(strings.TrimSuffix(filepath.ToSlash(rel), ".json"))
			var ab about
			switch a.name {
			case "connecting":
				if ab.shown, err = shownMentions(dir, f); err != nil {
					return err
				}
			case "reading":
				if ab.spans, err = fixtureSpans(govuk, strings.TrimPrefix(f, "reading/")); err != nil {
					return err
				}
			}
			n, err := a.count(rec.Output, ab)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if c[a.name] == nil {
				c[a.name] = map[string][][]int{}
			}
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
