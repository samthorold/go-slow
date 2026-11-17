package editor

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
)

// WeavingVersion is the version of weaving's instructions, schema and
// checks, and of how its input is shown. A new version is a new Judge (system
// design: Every Judgement carries its Judge). Change it with
// weavingInstructions, weavingSchema, checkWeaving or weavingInput.
const WeavingVersion = 2

// weavingAct names weaving, as its requests, calls and work do.
const weavingAct = "weaving"

// CutStory is a Story a Cut made: an Item's Spans as its Cut listed them,
// with the Item whose text they address.
type CutStory struct {
	// CutID is the Cut's event in the Record.
	CutID int64
	Item  Item
	Spans []cut.Span
}

// Cited is a Span an Editor's Span cites, its words as read, when it was
// published, and either its address in its Item's text with its Item's
// title, or, for an earlier Editor's Span, Of, its address among the
// Editor's Spans.
type Cited struct {
	Address Address
	// Of is the earlier Editor's Span cited, when it is one; zero when the
	// Span cited is a Source's.
	Of    EditorSpanAt
	Words string
	Title string
	// Published is its Item's publication, or for an Editor's Span the
	// Judgement's that wrote it (CONTEXT.md: Timeline).
	Published time.Time
}

// EditorSpanAt is an Editor's Span's address: the Story that wrote it, and
// its place among that Story's Editor's Spans, numbered from 1 in the order
// written. The Story is an event that never changes, so the address always
// holds the same words.
type EditorSpanAt struct {
	Story int64
	N     int
}

// IsZero reports whether a is no Editor's Span's address.
func (a EditorSpanAt) IsZero() bool { return a.Story == 0 }

// EditorSpan is a Span the Editor wrote: its words, and the Spans it cites,
// in the order it cites them.
type EditorSpan struct {
	Body  string
	Cites []Cited
}

// Story is a Story the Editor made: one Judgement, holding the Editor's
// Spans it wrote, in the order written, and what they cite (system design:
// An opinion is a Story, answered as Editor's Spans with their citations).
type Story struct {
	// ID is its event's in the Record, once kept.
	ID int64
	// Weaving is the run that made it, and Judge and At its run's.
	Weaving int64
	Judge   Judge
	At      time.Time
	Spans   []EditorSpan
	// Call is the kept call that made it, found by its Judge's request hash;
	// zero if none was kept.
	Call int64
}

// CitingSpan is an Editor's Span citing a Span: its Story, its number in
// it from 1, its words and when it was written, with its Story's opening
// words.
type CitingSpan struct {
	Story   int64
	N       int
	Body    string
	At      time.Time
	Opening string
}

// SpanHeld is what holds a Span: the Cuts whose Stories list it, oldest
// first, and the Editor's Spans citing it, in the order written.
type SpanHeld struct {
	Cuts   []int64
	Citing []CitingSpan
}

// Timeline is the Spans the Story's Editor's Spans cite, each once, in the
// order they were published, an earlier Editor's Span by the Judgement that
// wrote it, and those published together in the order their Item or Story
// has them (CONTEXT.md: Timeline). Its own Editor's Spans are not in it:
// they are the argument shown above it, all published last, by the
// Judgement that made it.
func (s Story) Timeline() []Cited {
	var out []Cited
	for _, es := range s.Spans {
		for _, c := range es.Cites {
			if !slices.ContainsFunc(out, func(o Cited) bool { return o.Address == c.Address && o.Of == c.Of }) {
				out = append(out, c)
			}
		}
	}
	slices.SortStableFunc(out, func(a, b Cited) int {
		return cmp.Or(a.Published.Compare(b.Published),
			cmp.Compare(a.Address.SourceID, b.Address.SourceID), cmp.Compare(a.Address.EntryID, b.Address.EntryID),
			cmp.Compare(a.Address.Span.Start, b.Address.Span.Start), cmp.Compare(a.Address.Span.End, b.Address.Span.End),
			cmp.Compare(a.Of.Story, b.Of.Story), cmp.Compare(a.Of.N, b.Of.N))
	})
	return out
}

// Weaving is a weaving run whose result was written: the Stories it made,
// none for a run with nothing substantive to say, with its Judge. It is kept
// as an event, and each Story as an event of its own tied to it.
type Weaving struct {
	ID int64
	// Key is the run's work key, the day it was submitted on.
	Key string
	// ThroughCut is the newest Cut whose Story the run was shown: the next run
	// is shown the Stories cut after it.
	ThroughCut int64
	Judge      Judge
	At         time.Time
	Stories    []Story
	// Usage is what the call cost, for the logs; not kept with the run.
	Usage Usage
}

// WeavingStore is the part of the Record weaving reads and writes.
type WeavingStore interface {
	// StoriesToWeave are the Stories cut since the last weaving run whose
	// result was written, of Items collected after the switch, oldest Cut
	// first.
	StoriesToWeave(ctx context.Context) ([]CutStory, error)
	// StoriesMade are the Stories the Editor made, newest first, each with
	// its Editor's Spans and what they cite.
	StoriesMade(ctx context.Context) ([]Story, error)
	// WeavingRan reports whether the run keyed key has had its result
	// written or been marked failed.
	WeavingRan(ctx context.Context, key string) (bool, error)
	// PendingBatches are the batches not yet ended, among which a weaving
	// run's may be.
	PendingBatches(ctx context.Context) ([]PendingBatch, error)
	// KeepWeaving keeps a run and each Story it made, all at once.
	KeepWeaving(ctx context.Context, w Weaving) error
}

// weavingInstructions are weaving's instructions: the Owner's, one sentence
// saying what a Span marked the Editor's is and that it may be cited, and one
// stating the checks the Owner's leave unsaid (that an Editor's Span has
// words, and cites only Spans shown).
const weavingInstructions = `You are the editor of a slow-news publication. Your publication exists to see the stories behind the stories, and to take a zoomed-out view of the long-term causes. Below are Stories, each a group of numbered Spans: stretches of text as their publishers wrote them. What each Story says is already in it; write what none of them says. Write your substantive editorial opinion on one or more of them. Each opinion is a new Story: your own words, in one or more editor_spans, each citing by number the Spans it rests on, at least one. Spans marked the Editor's are your own earlier words, and you may cite them too. Each editor_span's body holds its words, and its cites only the numbers of Spans shown below. You don't have to write anything: if you have nothing substantive to say, return no stories.`

// weavingSchema is weaving's output schema: stories, each editor_spans, each
// its words (body) and then the numbers of the Spans it cites (cites). The
// fields are named so they are written in that order, since a schema's
// properties go out in name order.
var weavingSchema = object(map[string]any{
	"stories": array(object(map[string]any{
		"editor_spans": array(object(map[string]any{
			"body":  str(),
			"cites": array(integer()),
		})),
	})),
})

// weavingOutput is weaving's answer as the model gives it.
type weavingOutput struct {
	Stories *[]struct {
		EditorSpans []struct {
			Body  string `json:"body"`
			Cites []int  `json:"cites"`
		} `json:"editor_spans"`
	} `json:"stories"`
}

// Weaving is the weaving act as it runs in batches (system design: Weaving
// runs at most once a day): at most one run a day, keyed by the day, on the
// first run of the batch runner at or after the time of day at (in now's
// location), unless that day's run has already been written or failed,
// nothing has been cut since the last run written, or a run's batch is still
// pending. A run is shown every Story the Editor has made and every Story
// cut since the last run written (system design: A weaving run is shown what
// is new, and everything the Editor has made), and its result is applied to
// what it was shown, however the Record has moved on since.
func (e *Editor) Weaving(at time.Duration) BatchAct {
	return BatchAct{
		Name: weavingAct,
		Waiting: func(ctx context.Context, now time.Time) ([]Job, error) {
			key, pending, err := e.pendingWeaving(ctx)
			if err != nil {
				return nil, err
			}
			if !pending {
				if now.Before(DueAt(now, at)) {
					return nil, nil
				}
				key = now.Format(time.DateOnly)
				ran, err := e.Weavings.WeavingRan(ctx, key)
				if err != nil || ran {
					return nil, err
				}
			}
			stories, err := e.Weavings.StoriesToWeave(ctx)
			if err != nil || len(stories) == 0 {
				return nil, err
			}
			made, err := e.Weavings.StoriesMade(ctx)
			if err != nil {
				return nil, err
			}
			slices.Reverse(made)
			return []Job{{
				Key:     key,
				Request: weavingRequest(made, stories),
				ApplySent: func(ctx context.Context, sent Request, res Response, now time.Time) error {
					shown, ok := shownIn(made, stories, sent)
					if !ok {
						return errors.New("weaving: the Stories its request showed no longer await weaving")
					}
					w, err := checkWeaving(shown, sent, modelOf(e.ModelFor, sent.Act), res)
					if err != nil {
						return err
					}
					w.Key, w.At = key, now
					if err := e.Weavings.KeepWeaving(ctx, w); err != nil {
						return err
					}
					e.Log.InfoContext(ctx, "woven", "key", key, "shown", len(shown.stories), "stories", len(w.Stories),
						"model", w.Judge.Model, "input_tokens", w.Usage.InputTokens, "output_tokens", w.Usage.OutputTokens)
					return nil
				},
			}}, nil
		},
	}
}

// pendingWeaving is the key of the weaving run in a pending batch, if any.
func (e *Editor) pendingWeaving(ctx context.Context) (string, bool, error) {
	pending, err := e.Weavings.PendingBatches(ctx)
	if err != nil {
		return "", false, err
	}
	for _, p := range pending {
		for _, r := range p.Requests {
			if r.Act == weavingAct {
				return r.Key, true, nil
			}
		}
	}
	return "", false, nil
}

// weavingRequest is a weaving run's request, shown the Stories the Editor
// made, oldest first, then the Stories cut since the last run.
func weavingRequest(made []Story, stories []CutStory) Request {
	return Request{Act: weavingAct, Instructions: weavingInstructions, Input: weavingInput(made, stories),
		Schema: weavingSchema, Effort: EffortMedium}
}

// weavingInput is what a run is shown: the Stories the Editor made, then
// those cut since, numbering their Spans S1, S2… across them all.
func weavingInput(made []Story, stories []CutStory) string {
	sh := showMade(made)
	for _, s := range stories {
		sh.cutStory(s)
	}
	return sh.input.String()
}

// woven is what a weaving run is shown: its input as written so far, each
// Span it numbers in number order, the number of each, and the Stories cut
// since the last run among them.
type woven struct {
	input   strings.Builder
	spans   []Cited
	numbers map[citedAt]int
	stories []CutStory
}

// citedAt is a Span's address, whether a Source's or an Editor's.
type citedAt struct {
	Address Address
	Of      EditorSpanAt
}

// showMade starts a run's input with the Stories the Editor made, oldest
// first, each under the date it was made, its Editor's Spans in the order
// written, each after the Spans it cites not yet shown. An Editor's Span is
// marked as the Editor's, with the numbers it cites, so the model never
// takes its own words for a Source's; a Source's Span is shown with its
// Item's title. Each Span is numbered once, however many Stories cite it.
func showMade(made []Story) *woven {
	sh := &woven{numbers: map[citedAt]int{}}
	for _, st := range made {
		fmt.Fprintf(&sh.input, "Story the Editor made, %s\n", st.At.UTC().Format(time.DateOnly))
		for i, es := range st.Spans {
			var cites []string
			for _, c := range es.Cites {
				n, ok := sh.numbers[citedAt{c.Address, c.Of}]
				if !ok {
					n = sh.number(c)
					if c.Of.IsZero() {
						fmt.Fprintf(&sh.input, "S%d (from %q): %s\n", n, c.Title, c.Words)
					} else {
						fmt.Fprintf(&sh.input, "S%d (the Editor's): %s\n", n, c.Words)
					}
				}
				cites = append(cites, fmt.Sprintf("S%d", n))
			}
			n := sh.number(Cited{Of: EditorSpanAt{Story: st.ID, N: i + 1}, Words: es.Body})
			fmt.Fprintf(&sh.input, "S%d (the Editor's, citing %s): %s\n", n, strings.Join(cites, ", "), es.Body)
		}
		sh.input.WriteString("\n")
	}
	return sh
}

// number numbers c as the next Span shown.
func (sh *woven) number(c Cited) int {
	sh.spans = append(sh.spans, c)
	n := len(sh.spans)
	sh.numbers[citedAt{c.Address, c.Of}] = n
	return n
}

// cutStory shows one Story a Cut made, under its Item's title and date, its
// Spans as written, numbered on from those shown before it.
func (sh *woven) cutStory(s CutStory) {
	fmt.Fprintf(&sh.input, "Story from %q", s.Item.Title)
	if !s.Item.Published.IsZero() {
		fmt.Fprintf(&sh.input, ", published %s", s.Item.Published.UTC().Format(time.DateOnly))
	}
	sh.input.WriteString("\n")
	for _, sp := range s.Spans {
		words := cut.Words(s.Item.Content, sp)
		n := sh.number(Cited{
			Address: Address{SourceID: s.Item.SourceID, EntryID: s.Item.EntryID, Span: sp},
			Words:   words, Title: s.Item.Title,
		})
		fmt.Fprintf(&sh.input, "S%d: %s\n", n, words)
	}
	sh.input.WriteString("\n")
	sh.stories = append(sh.stories, s)
}

// shownIn is what a request was sent showing: the Stories the Editor made,
// and those of stories, now awaiting weaving, whose input it is. Only
// weaving makes Stories, and no run is sent while one is pending, so the
// Stories made are those the request was shown; Stories are cut in order and
// only added to, so those shown are the first of the Stories now awaiting.
func shownIn(made []Story, stories []CutStory, sent Request) (*woven, bool) {
	sh := showMade(made)
	for _, s := range stories {
		sh.cutStory(s)
		switch {
		case sh.input.Len() == len(sent.Input):
			return sh, sh.input.String() == sent.Input
		case sh.input.Len() > len(sent.Input):
			return nil, false
		}
	}
	return nil, false
}

// checkWeaving decodes the model's answer to req, asked of the model named
// asked, shown sh, and runs weaving's checks over it: each citation is a
// Span shown (a Source's or an earlier Editor's), each Editor's Span has
// words and cites at least one Span, and each Story has at least one
// Editor's Span. A result that fails them is ErrFailedChecks.
func checkWeaving(sh *woven, req Request, asked string, res Response) (Weaving, error) {
	refuse := func(format string, args ...any) (Weaving, error) {
		return Weaving{}, fmt.Errorf("weaving: %w: %s", ErrFailedChecks, fmt.Sprintf(format, args...))
	}
	var out weavingOutput
	dec := json.NewDecoder(bytes.NewReader(res.Output))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return refuse("output does not match the schema: %v", err)
	}
	if out.Stories == nil {
		return refuse("output does not match the schema: no stories")
	}
	spans := sh.spans
	w := Weaving{
		ThroughCut: sh.stories[len(sh.stories)-1].CutID, Usage: res.Usage,
		Judge: Judge{Act: req.Act, Version: WeavingVersion, Model: res.Model, RequestHash: RequestHash(req, asked)},
	}
	for i, s := range *out.Stories {
		if len(s.EditorSpans) == 0 {
			return refuse("story %d has no editor_spans", i+1)
		}
		var story Story
		for j, es := range s.EditorSpans {
			body := strings.TrimSpace(es.Body)
			if body == "" {
				return refuse("story %d, editor_span %d has no words", i+1, j+1)
			}
			if len(es.Cites) == 0 {
				return refuse("story %d, editor_span %d cites no Span", i+1, j+1)
			}
			span := EditorSpan{Body: body}
			var seen []int
			for _, c := range es.Cites {
				if c < 1 || c > len(spans) {
					return refuse("story %d, editor_span %d cites S%d, which was not shown: the Spans shown are S1 to S%d",
						i+1, j+1, c, len(spans))
				}
				if slices.Contains(seen, c) {
					continue
				}
				seen = append(seen, c)
				span.Cites = append(span.Cites, spans[c-1])
			}
			story.Spans = append(story.Spans, span)
		}
		w.Stories = append(w.Stories, story)
	}
	return w, nil
}
