// Command yield reports how much each of the Editor's acts drew from each
// fixture in two sets of recorded runs, before → after, so a re-recording's
// shift in yield is seen without diffing JSON. It is a dev tool, not part of
// the app: it reads the repository's recordings, never the Record.
//
//	go run ./internal/editor/yield <before> <after>
//
// Relating is counted by its relations' kinds; a sample of an Item related
// with no call counts none.
//
// Each argument is a recordings directory (internal/editor/testdata/recorded,
// or an older copy of it, e.g. extracted with git archive). A fixture
// recorded as several samples (<fixture>/<k>.json) is counted by each
// sample's last attempt, and shown as the range its samples drew (least–most);
// a total is the range from every fixture's least to its most. Frozen copies
// of upstream recordings (inputs/) are not counted. Reading's Spans are those
// the sentence cutter makes of the fixture Item, from the fixtures beside
// either directory (../govuk). It reports, it never fails on a change in
// yield.
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
	before, err := load(args[0], govuk)
	if err != nil {
		return err
	}
	after, err := load(args[1], govuk)
	if err != nil {
		return err
	}
	var b strings.Builder
	report(&b, before, after)
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

// about is what a recording's count may need beyond its output: for
// reading, the Spans of its fixture's Cut.
type about struct {
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
	{"briefing", []string{"Statements", "Spans cited", "Stories gathered"}, countBriefing},
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

// countBriefing counts the Briefing's statements, the distinct Spans they
// cite and the Stories it gathered (those the model gathered: a Span told
// with no relations has an empty Story the act keeps without asking).
func countBriefing(out json.RawMessage, _ about) ([]int, error) {
	var r struct {
		Statements []struct {
			Spans   []string          `json:"spans"`
			Stories []json.RawMessage `json:"stories"`
		} `json:"statements"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, err
	}
	cited := map[string]bool{}
	gathered := 0
	for _, st := range r.Statements {
		gathered += len(st.Stories)
		for _, s := range st.Spans {
			cited[s] = true
		}
	}
	return []int{len(r.Statements), len(cited), gathered}, nil
}

// countReading counts a reading of Spans: the Spans its fixture was shown
// as, those it Rejected, the names and speakers it gave and the stretches it
// said refer to others.
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

func load(dir, govuk string) (counts, error) {
	c := counts{}
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
			if a.name == "reading" {
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
		}
		return nil
	})
	return c, err
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
