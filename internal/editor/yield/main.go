// Command yield reports how much each of the Editor's acts drew from each
// fixture in two sets of recorded runs, before → after, so a re-recording's
// shift in yield is seen without diffing JSON. It is a dev tool, not part of
// the app: it reads the repository's recordings, never the Record.
//
//	go run ./internal/editor/yield <before> <after>
//
// It counts hydration, weaving and naming. Hydration is counted by the Spans its
// fixture is cut into and what it judged of them; weaving by the Stories it
// made, their Editor's Spans and the distinct Spans they cite; naming by the
// words of the Name it gave its Story. An act that calls tools (weaving) has
// a second table, its tools: per fixture, each tool's asks over every attempt
// of every sample, and how many of its answers held anything (named a Story,
// and were no error), with each tool's total; ? where the tree keeps no
// answer to some ask (one recorded before samples kept their tool results).
// Recordings of any
// other act (a retired act's, in an older tree) are not counted: the report
// ends by naming those acts.
//
// Each argument is a recordings directory (internal/editor/testdata/recorded,
// or an older copy of it, e.g. extracted with git archive). A fixture
// recorded as several samples (<fixture>/<k>.json) is counted by each
// sample's last attempt, and shown as the range its samples drew (least–most);
// a total is the range from every fixture's least to its most. Frozen copies
// of upstream recordings (inputs/) are not counted. Hydration's Spans are those
// the sentence cutter makes of the fixture Item, from the fixtures beside
// either directory (../govuk). It reports, it never fails on a change in
// yield.
package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
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

// recording is the little of a kept response the report needs: a sample's
// last attempt, with the attempts before it, and of a turn that called
// tools, its calls and what the tools returned (none kept in an older tree).
type recording struct {
	Act     string          `json:"act"`
	Output  json.RawMessage `json:"output"`
	Calls   []toolCall      `json:"calls"`
	Results []toolResult    `json:"results"`
	Earlier []recording     `json:"earlier_attempts"`
}

type toolCall struct {
	ID   string `json:"ID"`
	Name string `json:"Name"`
}

type toolResult struct {
	CallID  string `json:"CallID"`
	Content string `json:"Content"`
	IsError bool   `json:"IsError"`
}

// namesAStory is a line of a tool's answer naming a Story, a catalogue's
// entry or the Story opened: an answer holds anything when it names one and
// is no error.
var namesAStory = regexp.MustCompile(`(?m)^\s*Story \d+,`)

// asked is what a tool was asked in a fixture's samples: its asks, and how
// many of its answers held anything; unknown when some ask's answer was not
// kept.
type asked struct {
	asks, held int
	unknown    bool
}

func (a asked) add(o asked) asked {
	return asked{asks: a.asks + o.asks, held: a.held + o.held, unknown: a.unknown || o.unknown}
}

// heldCell is how many answers held anything, or ? where not every answer
// was kept.
func (a asked) heldCell() string {
	if a.unknown {
		return "?"
	}
	return strconv.Itoa(a.held)
}

// toolsAsked counts each tool rec's attempts called: their asks, and the
// answers that held anything.
func toolsAsked(rec recording) map[string]asked {
	out := map[string]asked{}
	for _, a := range append(slices.Clone(rec.Earlier), rec) {
		for _, c := range a.Calls {
			n := out[c.Name]
			n.asks++
			i := slices.IndexFunc(a.Results, func(r toolResult) bool { return r.CallID == c.ID })
			switch {
			case i < 0:
				n.unknown = true
			case !a.Results[i].IsError && namesAStory.MatchString(a.Results[i].Content):
				n.held++
			}
			out[c.Name] = n
		}
	}
	return out
}

// about is what a recording's count may need beyond its output: for
// hydration, the Spans of its fixture's Cut.
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
	{"hydration", []string{"Spans", "Rejected", "Names", "Speakers", "Refers to"}, countHydration},
	{"weaving", []string{"Stories", "Editor's Spans", "Spans cited"}, countWeaving},
	{"naming", []string{"Words"}, countNaming},
}

// countNaming counts the words of the Name naming gave a Story: none when it
// gave none.
func countNaming(out json.RawMessage, _ about) ([]int, error) {
	var r struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, err
	}
	return []int{len(strings.Fields(r.Name))}, nil
}

// countWeaving counts the Stories a weaving run made, their Editor's Spans
// and the distinct Spans they cite, by the numbers the run was shown them as.
func countWeaving(out json.RawMessage, _ about) ([]int, error) {
	var r struct {
		Stories []struct {
			EditorSpans []struct {
				Cites []int `json:"cites"`
			} `json:"editor_spans"`
		} `json:"stories"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, err
	}
	spans, cited := 0, map[int]bool{}
	for _, s := range r.Stories {
		spans += len(s.EditorSpans)
		for _, es := range s.EditorSpans {
			for _, c := range es.Cites {
				cited[c] = true
			}
		}
	}
	return []int{len(r.Stories), spans, len(cited)}, nil
}

// countHydration counts a hydration of Spans: the Spans its fixture was shown
// as, those it Rejected, the names and speakers it gave and the stretches it
// said refer to others.
func countHydration(out json.RawMessage, a about) ([]int, error) {
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

// oldHydration is hydration's name in a tree recorded before it was called
// hydration (reading/<fixture>/, act "reading"): its recordings are counted
// as hydration's, so a report against such a tree compares like with like.
const oldHydration = "reading"

// counts is one tree's yield: per act, each fixture's counts, one per sample
// (a fixture recorded once has one), and the acts it holds recordings of but
// the tool doesn't count (retired acts, whose recordings an older tree may
// still hold).
type counts struct {
	acts      map[string]map[string][][]int
	uncounted map[string]bool
	// tools are, per act and fixture, what each tool was asked over the
	// fixture's samples, for an act that calls tools.
	tools map[string]map[string]map[string]asked
}

// addTools adds what the tools were asked in one of fixture's samples of act.
func (c counts) addTools(act, fixture string, tools map[string]asked) {
	for tool, n := range tools {
		if c.tools[act] == nil {
			c.tools[act] = map[string]map[string]asked{}
		}
		if c.tools[act][fixture] == nil {
			c.tools[act][fixture] = map[string]asked{}
		}
		c.tools[act][fixture][tool] = c.tools[act][fixture][tool].add(n)
	}
}

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
	c := counts{acts: map[string]map[string][][]int{}, uncounted: map[string]bool{}, tools: map[string]map[string]map[string]asked{}}
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
		// are not counted; acts with no counts below are named as such.
		var rec recording
		if json.Unmarshal(data, &rec) != nil {
			return nil
		}
		if rec.Act == "" && (rec.Output == nil || string(rec.Output) == "null") {
			// A sample of work that made no call is its act's, by where it
			// is kept: <act>/<fixture>/<k>.json.
			if rel, err := filepath.Rel(dir, path); err == nil {
				if act, _, found := strings.Cut(filepath.ToSlash(rel), "/"); found {
					rec.Act = act
				}
			}
		}
		if rec.Act == oldHydration {
			rec.Act = "hydration"
		}
		counted := false
		for _, a := range acts {
			if a.name != rec.Act {
				continue
			}
			counted = true
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if rest, ok := strings.CutPrefix(rel, oldHydration+"/"); ok {
				rel = "hydration/" + rest
			}
			f := fixtureOf(strings.TrimSuffix(rel, ".json"))
			var ab about
			if a.name == "hydration" {
				if ab.spans, err = fixtureSpans(govuk, strings.TrimPrefix(f, "hydration/")); err != nil {
					return err
				}
			}
			n, err := a.count(rec.Output, ab)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if c.acts[a.name] == nil {
				c.acts[a.name] = map[string][][]int{}
			}
			c.acts[a.name][f] = append(c.acts[a.name][f], n)
			c.addTools(a.name, f, toolsAsked(rec))
		}
		if !counted && rec.Act != "" {
			c.uncounted[rec.Act] = true
		}
		return nil
	})
	return c, err
}

func report(b *strings.Builder, before, after counts) {
	for _, a := range acts {
		fixtures := map[string]bool{}
		for f := range before.acts[a.name] {
			fixtures[f] = true
		}
		for f := range after.acts[a.name] {
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
			was, inBefore := before.acts[a.name][f]
			now, inAfter := after.acts[a.name][f]
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
		reportTools(b, a.name, before, after)
	}
	uncounted := map[string]bool{}
	for act := range before.uncounted {
		uncounted[act] = true
	}
	for act := range after.uncounted {
		uncounted[act] = true
	}
	if len(uncounted) > 0 {
		names := make([]string, 0, len(uncounted))
		for act := range uncounted {
			names = append(names, act)
		}
		sort.Strings(names)
		fmt.Fprintf(b, "Not counted: %s\n", strings.Join(names, ", "))
	}
}

// reportTools writes, for an act that called tools in either tree, each
// fixture's asks of each tool and the answers that held anything, before →
// after, summed over the fixture's samples, and each tool's total.
// A fixture a tree has no recording of shows a dash on that side.
func reportTools(b *strings.Builder, act string, before, after counts) {
	rows := map[[2]string]bool{}
	tools := map[string]bool{}
	for _, side := range []counts{before, after} {
		for f, byTool := range side.tools[act] {
			for tool := range byTool {
				rows[[2]string{f, tool}] = true
				tools[tool] = true
			}
		}
	}
	if len(rows) == 0 {
		return
	}
	keys := make([][2]string, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(x, y [2]string) int { return cmp.Or(cmp.Compare(x[0], y[0]), cmp.Compare(x[1], y[1])) })
	fmt.Fprintf(b, "### %s's tools\n\n| Fixture | Tool | Asks | Held anything |\n|---|---|---|---|\n", act)
	totalBefore, totalAfter := map[string]asked{}, map[string]asked{}
	cellOf := func(side counts, total map[string]asked, f, tool string) (string, string) {
		if _, recorded := side.acts[act][f]; !recorded {
			return "–", "–"
		}
		n := side.tools[act][f][tool]
		total[tool] = total[tool].add(n)
		return strconv.Itoa(n.asks), n.heldCell()
	}
	for _, k := range keys {
		wasAsks, wasHeld := cellOf(before, totalBefore, k[0], k[1])
		nowAsks, nowHeld := cellOf(after, totalAfter, k[0], k[1])
		fmt.Fprintf(b, "| %s | %s | %s → %s | %s → %s |\n", k[0], k[1], wasAsks, nowAsks, wasHeld, nowHeld)
	}
	names := make([]string, 0, len(tools))
	for tool := range tools {
		names = append(names, tool)
	}
	sort.Strings(names)
	for _, tool := range names {
		was, now := totalBefore[tool], totalAfter[tool]
		fmt.Fprintf(b, "| **Total** | %s | %d → %d | %s → %s |\n", tool, was.asks, now.asks, was.heldCell(), now.heldCell())
	}
	b.WriteString("\n")
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
