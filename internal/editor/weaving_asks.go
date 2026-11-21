package editor

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/samthorold/go-slow/internal/cut"
)

// A weaving run asks the Record for earlier Stories by words, and is
// answered with a catalogue it opens Stories from (system design: A run asks
// by words, Entities and citations, and is answered with a catalogue). What
// a tool answers is shown to the model, so it is part of the act's
// instructions: a change to it is a new WeavingVersion, and a re-record.

// weavingTools are the tools a run asks the Record with: find, by words;
// find_entity, by an Entity a Span it was shown mentions; find_citing, by a
// Story it was shown, for the Stories citing it; and open_story, a Story
// from a catalogue, whole.
var weavingTools = []Tool{
	{
		Name: "find",
		Description: "Find earlier Stories, the publishers' and your own, by words. The answer is a catalogue of the " +
			"Stories with a Span holding the words, newest first: each Story by its number, what it is (a " +
			"publisher's by its title, your own by its first words), when it is from, for a publisher's its " +
			"Source, and how many of your later Spans cite it; then each Span holding the words, numbered so you " +
			"can cite it, between the Spans around it, marked · and unnumbered, for context. Words are matched " +
			"as whole words, whatever their case, in Spans as they read with the words between ⟦ and ⟧, so a " +
			"name finds the Spans that refer to it in other words.",
		Schema: object(map[string]any{"words": str()}),
	},
	{
		Name: "find_entity",
		Description: "Find earlier Stories by an Entity a Span shown to you mentions: a person, organisation, place " +
			"or named occasion, asked for by its id, between { and } after its name (Q and digits for a Wikidata " +
			"item, E and digits for one of the Editor's own). The answer is a catalogue as find's is, of the " +
			"publishers' Stories with a Span mentioning the Entity, whatever words the Span calls it by: a Span " +
			"mentions an Entity when it names it, or refers to words that do.",
		Schema: object(map[string]any{"entity": str()}),
	},
	{
		Name: "find_citing",
		Description: "Find your own earlier Stories that cite an earlier Story shown to you, asked for by its number " +
			"where a catalogue lists it or you opened it: those with an Editor's Span citing one of its Spans, which " +
			"follows your own earlier reasoning to what followed or corrected it. The answer is a catalogue as " +
			"find's is, each Editor's Span citing the Story numbered so you can cite it.",
		Schema: object(map[string]any{"story": integer()}),
	},
	{
		Name: "open_story",
		Description: "Read an earlier Story whole, by its number in a catalogue: every Span of it shown and " +
			"numbered, so you can cite any of them.",
		Schema: object(map[string]any{"story": integer()}),
	},
}

// catalogueSize is the most Stories a catalogue lists, the newest, and
// matchesShown the most Spans it shows of each, the first: a broad ask is
// answered with its newest matches rather than with an answer that grows
// with the Record, and a Story matching throughout is opened to be read.
const (
	catalogueSize = 20
	matchesShown  = 5
)

// earlierRecord is the Record a run's asks are answered from: the Stories
// cut through its newest new one but for its new ones, and the Stories the
// Editor made, each Span read as its hydration reads it.
type earlierRecord struct {
	cuts []CutStory
	made []Story
	h    hydratedItems
}

// earlierRecord is the Record the run whose new Stories are the Cuts
// newCuts asks, as it stands now.
func (e *Editor) earlierRecord(ctx context.Context, newCuts []int64) (earlierRecord, error) {
	var rec earlierRecord
	cuts, err := e.Weavings.StoriesCutThrough(ctx, newCuts[len(newCuts)-1])
	if err != nil {
		return rec, err
	}
	rec.cuts = slices.DeleteFunc(cuts, func(s CutStory) bool { return slices.Contains(newCuts, s.CutID) })
	if rec.made, err = e.Weavings.StoriesMade(ctx); err != nil {
		return rec, err
	}
	rec.h, err = e.hydratedItems(ctx, rec.made, rec.cuts)
	return rec, err
}

// answer is what the Record answers the model's call c, numbering on from
// what sh has been shown: its content, and whether it is an error told to
// the model as one.
func (r earlierRecord) answer(sh *woven, c ToolCall) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(c.Input))
	dec.DisallowUnknownFields()
	switch c.Name {
	case "find":
		var in struct {
			Words *string `json:"words"`
		}
		if err := dec.Decode(&in); err != nil || in.Words == nil || strings.TrimSpace(*in.Words) == "" {
			return `find takes {"words": "..."}, the words to find.`, true
		}
		return r.find(sh, *in.Words), false
	case "find_entity":
		var in struct {
			Entity *string `json:"entity"`
		}
		if err := dec.Decode(&in); err != nil || in.Entity == nil || strings.TrimSpace(*in.Entity) == "" {
			return `find_entity takes {"entity": "..."}, the id of an Entity a Span shown to you mentions.`, true
		}
		return r.findEntity(sh, *in.Entity)
	case "find_citing":
		var in struct {
			Story *int64 `json:"story"`
		}
		if err := dec.Decode(&in); err != nil || in.Story == nil {
			return `find_citing takes {"story": N}, the number of an earlier Story shown to you.`, true
		}
		return r.findCiting(sh, *in.Story)
	case "open_story":
		var in struct {
			Story *int64 `json:"story"`
		}
		if err := dec.Decode(&in); err != nil || in.Story == nil {
			return `open_story takes {"story": N}, a Story's number from a catalogue.`, true
		}
		return r.open(sh, *in.Story)
	}
	return fmt.Sprintf("There is no tool %q: ask with find, find_entity, find_citing or open_story.", c.Name), true
}

// entry is a Story a catalogue lists: when it is from, its heading, and the
// lines under it.
type entry struct {
	at    time.Time
	id    int64
	lines func() string
}

// find is the catalogue of the earlier Stories with a Span holding words.
func (r earlierRecord) find(sh *woven, words string) string {
	return r.catalogue(sh, ask{
		cut:  func(_ Address, read string) bool { return holdsWords(read, words) },
		made: func(es EditorSpan) bool { return holdsWords(es.Body, words) },
		with: fmt.Sprintf("with the words %q", words),
		more: "hold the words",
	})
}

// findEntity is the catalogue of the earlier Stories with a Span mentioning
// the Entity whose id is asked (its marks and case aside), or an error told
// to the model when the run was never shown a Span mentioning it, naming the
// id without its marks, so the error never reads as showing it. An Editor's
// Span mentions no Entity, since nothing hydrates it, so only Stories a Cut
// made are listed.
func (r earlierRecord) findEntity(sh *woven, asked string) (string, bool) {
	id := strings.ToUpper(strings.Trim(strings.TrimSpace(asked), entityOpen+entityClose+" "))
	if !slices.Contains(sh.entities, id) {
		return fmt.Sprintf("No Span shown to you mentions an Entity with the id %q: ask for an Entity by the id "+
			"between { and } after its name where a Span shown to you mentions it.", id), true
	}
	named := entityOpen + id + entityClose
	if e, ok := r.h.entity(id); ok {
		named = e.Name + " " + named
	}
	return r.catalogue(sh, ask{
		cut: func(a Address, _ string) bool {
			return slices.ContainsFunc(r.h.mentions(a), func(e NamedEntity) bool { return e.handle() == id })
		},
		with: "mentioning " + named,
		more: "mention it",
	}), false
}

// findCiting is the catalogue of the Stories holding an Editor's Span that
// cites a Span of the earlier Story id: for a Story a Cut made, one of its
// Spans; for one the Editor made, one of its Editor's Spans. Only the
// Editor's Spans cite, so only Stories the Editor made are listed. It is an
// error told to the model when no answer to the run's asks named the Story,
// as for an Entity it was never shown (findEntity).
func (r earlierRecord) findCiting(sh *woven, id int64) (string, bool) {
	if !slices.Contains(sh.stories, id) {
		return fmt.Sprintf("No Story shown to you is numbered %d: ask what cites an earlier Story by its number "+
			"where a catalogue lists it or you opened it.", id), true
	}
	var cites func(EditorSpan) bool
	if i := slices.IndexFunc(r.cuts, func(s CutStory) bool { return s.CutID == id }); i >= 0 {
		cites = func(es EditorSpan) bool { return citesCut(es, r.cuts[i]) }
	} else {
		cites = func(es EditorSpan) bool { return citesMade(es, id) }
	}
	return r.catalogue(sh, ask{
		made: cites,
		with: fmt.Sprintf("citing a Span of Story %d", id),
		more: "cite it",
	}), false
}

// citesCut reports whether es cites a Span of the Story s a Cut made.
func citesCut(es EditorSpan, s CutStory) bool {
	return slices.ContainsFunc(es.Cites, func(c Cited) bool {
		return c.Of.IsZero() && c.Address.SourceID == s.Item.SourceID && c.Address.EntryID == s.Item.EntryID &&
			slices.Contains(s.Spans, c.Address.Span)
	})
}

// citesMade reports whether es cites an Editor's Span of the Story id the
// Editor made.
func citesMade(es EditorSpan, id int64) bool {
	return slices.ContainsFunc(es.Cites, func(c Cited) bool { return c.Of.Story == id })
}

// ask is what a catalogue lists earlier Stories by: whether a Span of a
// Story a Cut made matches it, by its address and its words as read, and
// whether an Editor's Span does (either nil for none); with, how the answer
// says what Spans matching hold ("with the words ..."), and more, what the
// Spans not shown do ("hold the words").
type ask struct {
	cut        func(a Address, read string) bool
	made       func(es EditorSpan) bool
	with, more string
}

// catalogue is the catalogue of the earlier Stories with a Span matching
// the ask q, newest first, at most catalogueSize: each Span matched numbered
// (shown, so it can be cited), between the Spans around it, unnumbered.
func (r earlierRecord) catalogue(sh *woven, q ask) string {
	var entries []entry
	for _, s := range r.cuts {
		if q.cut == nil {
			break
		}
		var lines []string
		var matched []int
		for i, sp := range s.Spans {
			a := r.address(s, sp)
			_, read := r.h.read(a, cut.Words(s.Item.Content, sp))
			lines = append(lines, read)
			if !r.h.rejected(a) && q.cut(a, read) {
				matched = append(matched, i)
			}
		}
		if len(matched) == 0 {
			continue
		}
		entries = append(entries, entry{at: s.Item.Published, id: s.CutID,
			lines: func() string {
				return r.cutHeading(s) + "\n" + around(matched, len(s.Spans), q.more, func(i int, match bool) string {
					sp := s.Spans[i]
					a := r.address(s, sp)
					if r.h.rejected(a) {
						return ""
					}
					if !match {
						return "  · " + lines[i] + "\n"
					}
					words := cut.Words(s.Item.Content, sp)
					n := sh.shown(Cited{Address: a, Words: words, Title: s.Item.Title, Published: s.Item.Published})
					return fmt.Sprintf("  S%d%s\n", n, r.h.show(a, words, ""))
				})
			}})
	}
	for _, st := range r.made {
		if q.made == nil {
			break
		}
		var matched []int
		for i, es := range st.Spans {
			if q.made(es) {
				matched = append(matched, i)
			}
		}
		if len(matched) == 0 {
			continue
		}
		entries = append(entries, entry{at: st.At, id: st.ID,
			lines: func() string {
				return r.madeHeading(st, true) + "\n" + around(matched, len(st.Spans), q.more, func(i int, match bool) string {
					body := st.Spans[i].Body
					if !match {
						return "  · " + body + "\n"
					}
					n := sh.shown(Cited{Of: EditorSpanAt{Story: st.ID, N: i + 1}, Words: body, Published: st.At})
					return fmt.Sprintf("  S%d (the Editor's): %s\n", n, body)
				})
			}})
	}
	if len(entries) == 0 {
		return fmt.Sprintf("No earlier Story has a Span %s.", q.with)
	}
	slices.SortFunc(entries, func(a, b entry) int { return cmp.Or(b.at.Compare(a.at), cmp.Compare(b.id, a.id)) })
	var b strings.Builder
	switch {
	case len(entries) > catalogueSize:
		fmt.Fprintf(&b, "%d earlier Stories have Spans %s; the %d newest are listed, newest first.\n",
			len(entries), q.with, catalogueSize)
		entries = entries[:catalogueSize]
	case len(entries) == 1:
		fmt.Fprintf(&b, "1 earlier Story has Spans %s.\n", q.with)
	default:
		fmt.Fprintf(&b, "%d earlier Stories have Spans %s, newest first.\n", len(entries), q.with)
	}
	for _, e := range entries {
		b.WriteString("\n" + e.lines())
	}
	return b.String()
}

// open is the Story id shown whole, each of its Spans numbered (shown, so it
// can be cited), or an error told to the model when the Record holds no
// earlier Story id.
func (r earlierRecord) open(sh *woven, id int64) (string, bool) {
	if slices.Contains(sh.newCuts, id) {
		return fmt.Sprintf("Story %d is one of the new Stories shown at the start, its Spans numbered there.", id), false
	}
	var b strings.Builder
	for _, s := range r.cuts {
		if s.CutID != id {
			continue
		}
		b.WriteString(r.cutHeading(s) + "\n")
		for _, sp := range s.Spans {
			a := r.address(s, sp)
			words := cut.Words(s.Item.Content, sp)
			c := Cited{Address: a, Words: words, Title: s.Item.Title, Published: s.Item.Published}
			if r.h.rejected(a) {
				sh.leftOut(c)
				continue
			}
			fmt.Fprintf(&b, "S%d%s\n", sh.shown(c), r.h.show(a, words, ""))
		}
		return b.String(), false
	}
	for _, st := range r.made {
		if st.ID != id {
			continue
		}
		b.WriteString(r.madeHeading(st, false) + "\n")
		var written []citedAt
		for i, es := range st.Spans {
			var cites []string
			for _, c := range es.Cites {
				n := sh.shown(c)
				cites = append(cites, fmt.Sprintf("S%d", n))
				if at := (citedAt{c.Address, c.Of}); !slices.Contains(written, at) {
					written = append(written, at)
					if c.Of.IsZero() {
						fmt.Fprintf(&b, "S%d%s\n", n, r.h.show(c.Address, c.Words, c.Title))
					} else {
						fmt.Fprintf(&b, "S%d (the Editor's): %s\n", n, c.Words)
					}
				}
			}
			n := sh.shown(Cited{Of: EditorSpanAt{Story: st.ID, N: i + 1}, Words: es.Body, Published: st.At})
			fmt.Fprintf(&b, "S%d (the Editor's, citing %s): %s\n", n, strings.Join(cites, ", "), es.Body)
		}
		return b.String(), false
	}
	return fmt.Sprintf("There is no earlier Story %d: open a Story by its number in a catalogue.", id), true
}

// address is the Span sp's address in s's Item.
func (r earlierRecord) address(s CutStory, sp cut.Span) Address {
	return Address{SourceID: s.Item.SourceID, EntryID: s.Item.EntryID, Span: sp}
}

// cutHeading is how a catalogue, or the Story opened, names a Story a Cut
// made: its number, its Item's title, when it was published, its Source,
// and how many later Editor's Spans cite it.
func (r earlierRecord) cutHeading(s CutStory) string {
	published := ""
	if !s.Item.Published.IsZero() {
		published = ", published " + s.Item.Published.UTC().Format(time.DateOnly)
	}
	citing := 0
	for _, st := range r.made {
		for _, es := range st.Spans {
			if citesCut(es, s) {
				citing++
			}
		}
	}
	return fmt.Sprintf("Story %d, from %q%s, from %s; %s", s.CutID, s.Item.Title, published, s.Item.SourceID,
		citedBy(citing))
}

// madeHeading is how a catalogue, or the Story opened, names a Story the
// Editor made: its number, the Editor's, when it was made, in a catalogue
// its first Editor's Span, and how many later Editor's Spans cite it.
func (r earlierRecord) madeHeading(st Story, opening bool) string {
	citing := 0
	for _, other := range r.made {
		for _, es := range other.Spans {
			if citesMade(es, st.ID) {
				citing++
			}
		}
	}
	first := ""
	if opening && len(st.Spans) > 0 {
		first = fmt.Sprintf(": %q", st.Spans[0].Body)
	}
	return fmt.Sprintf("Story %d, the Editor's, made %s%s; %s", st.ID, st.At.UTC().Format(time.DateOnly), first,
		citedBy(citing))
}

// citedBy says how many later Editor's Spans cite a Story.
func citedBy(n int) string {
	switch n {
	case 0:
		return "cited by no later Editor's Span"
	case 1:
		return "cited by 1 later Editor's Span"
	}
	return fmt.Sprintf("cited by %d later Editor's Spans", n)
}

// around is the lines of a Story's Spans a catalogue shows: each matched,
// the first matchesShown of them, and the one before and after each, in
// order, line(i, matched) writing Span i, and "  …" where Spans are skipped
// between them; then how many more match, if any, saying they hold what was
// asked for.
func around(matched []int, spans int, hold string, line func(i int, match bool) string) string {
	more := len(matched) - matchesShown
	if more > 0 {
		matched = matched[:matchesShown]
	}
	var show []int
	for _, m := range matched {
		for i := max(m-1, 0); i <= min(m+1, spans-1); i++ {
			if !slices.Contains(show, i) {
				show = append(show, i)
			}
		}
	}
	slices.Sort(show)
	var b strings.Builder
	for k, i := range show {
		if k > 0 && i > show[k-1]+1 {
			b.WriteString("  …\n")
		}
		b.WriteString(line(i, slices.Contains(matched, i)))
	}
	if more > 0 {
		fmt.Fprintf(&b, "  … and %d more Spans of it %s: open the Story to read them\n", more, hold)
	}
	return b.String()
}

// holdsWords reports whether text, as read with its hydrated stretches'
// marks dropped, holds words as whole words, whatever their case and
// however they are spaced.
func holdsWords(text, words string) bool {
	norm := func(s string) string {
		s = strings.NewReplacer(hydratedOpen, "", hydratedClose, "").Replace(s)
		return strings.Join(strings.Fields(strings.ToLower(s)), " ")
	}
	t, w := norm(text), norm(words)
	if w == "" {
		return false
	}
	for from := 0; ; {
		i := strings.Index(t[from:], w)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(w)
		before, _ := utf8.DecodeLastRuneInString(t[:start])
		after, _ := utf8.DecodeRuneInString(t[end:])
		if (start == 0 || !isWordRune(before)) && (end == len(t) || !isWordRune(after)) {
			return true
		}
		from = start + 1
	}
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// entityOpen and entityClose mark an Entity's id where a run is shown that a
// Span mentions it, after its name: "Andy Burnham {Q269909}".
const (
	entityOpen  = "{"
	entityClose = "}"
)

// handle is the id a run is shown e by, and asks for it by: a Wikidata
// item's own id (an Editor's Entity merged into one included), or E and the
// Editor's Entity's id in the Record.
func (e NamedEntity) handle() string {
	if e.WikidataID != "" {
		return e.WikidataID
	}
	return fmt.Sprintf("E%d", e.EditorID)
}

// shownEntity is an Entity's id as a run is shown it, and shownStory an
// earlier Story's number as an answer names it, heading a catalogue's entry
// or the Story opened, at the start of a line.
var (
	shownEntity = regexp.MustCompile(`\{((?:Q|E)[0-9]+)\}`)
	shownStory  = regexp.MustCompile(`(?m)^Story ([0-9]+), `)
)

// entitiesShown are the ids of the Entities the run has been shown a Span
// mentioning, read from what it was sent (its opening, and the answers to
// its asks), each once: those it may ask for by find_entity. They are read
// from what was sent, as left-out Spans are (shownIn), since a Span looked up
// or hydrated since is shown otherwise now.
func (t WeavingTurns) entitiesShown() []string {
	return t.sent(shownEntity)
}

// storiesShown are the numbers of the earlier Stories the answers to the
// run's asks have named, each once: those it may ask what cites by
// find_citing. Its opening names no Story by number.
func (t WeavingTurns) storiesShown() []int64 {
	var out []int64
	for _, s := range t.sent(shownStory) {
		var id int64
		if _, err := fmt.Sscan(s, &id); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// sent is what each match of re's first group in what the run was sent (its
// opening, and the answers to its asks) holds, each once, in the order sent.
func (t WeavingTurns) sent(re *regexp.Regexp) []string {
	var out []string
	add := func(text string) {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			if !slices.Contains(out, m[1]) {
				out = append(out, m[1])
			}
		}
	}
	add(t.Input)
	for _, m := range t.Messages {
		for _, r := range m.Results {
			add(r.Content)
		}
	}
	return out
}
