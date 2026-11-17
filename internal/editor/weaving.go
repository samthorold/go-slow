package editor

import (
	"bytes"
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
const WeavingVersion = 1

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

// Cited is a Span an Editor's Span cites: its address, its words as read,
// and its Item's title.
type Cited struct {
	Address Address
	Words   string
	Title   string
}

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
	// WeavingRan reports whether the run keyed key has had its result
	// written or been marked failed.
	WeavingRan(ctx context.Context, key string) (bool, error)
	// PendingBatches are the batches not yet ended, among which a weaving
	// run's may be.
	PendingBatches(ctx context.Context) ([]PendingBatch, error)
	// KeepWeaving keeps a run and each Story it made, all at once.
	KeepWeaving(ctx context.Context, w Weaving) error
}

// weavingInstructions are weaving's instructions: the Owner's, and one
// sentence stating the checks the Owner's leave unsaid (that an Editor's
// Span has words, and cites only Spans shown).
const weavingInstructions = `You are the editor of a slow-news publication. Your publication exists to see the stories behind the stories, and to take a zoomed-out view of the long-term causes. Below are Stories, each a group of numbered Spans: stretches of text as their publishers wrote them. What each Story says is already in it; write what none of them says. Write your substantive editorial opinion on one or more of them. Each opinion is a new Story: your own words, in one or more editor_spans, each citing by number the Spans it rests on, at least one. Each editor_span's body holds its words, and its cites only the numbers of Spans shown below. You don't have to write anything: if you have nothing substantive to say, return no stories.`

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
// pending. A run is shown every Story cut since the last run written, and
// its result is applied to what it was shown, however the Record has moved
// on since.
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
			return []Job{{
				Key:     key,
				Request: weavingRequest(stories),
				ApplySent: func(ctx context.Context, sent Request, res Response, now time.Time) error {
					shown, ok := shownIn(stories, sent)
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
					e.Log.InfoContext(ctx, "woven", "key", key, "shown", len(shown), "stories", len(w.Stories),
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

// weavingRequest is a weaving run's request, shown the Stories.
func weavingRequest(stories []CutStory) Request {
	return Request{Act: weavingAct, Instructions: weavingInstructions, Input: weavingInput(stories),
		Schema: weavingSchema, Effort: EffortMedium}
}

// weavingInput shows the Stories in order, each under its Item's title and
// date, its Spans as written, numbered S1, S2… across them all, so the
// Stories shown first are numbered the same however many follow.
func weavingInput(stories []CutStory) string {
	var b strings.Builder
	n := 0
	for _, s := range stories {
		b.WriteString(storyBlock(s, &n))
	}
	return b.String()
}

// storyBlock is one Story as weaving is shown it, its Spans numbered on
// from n.
func storyBlock(s CutStory, n *int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Story from %q", s.Item.Title)
	if !s.Item.Published.IsZero() {
		fmt.Fprintf(&b, ", published %s", s.Item.Published.UTC().Format(time.DateOnly))
	}
	b.WriteString("\n")
	for _, sp := range s.Spans {
		*n++
		fmt.Fprintf(&b, "S%d: %s\n", *n, cut.Words(s.Item.Content, sp))
	}
	b.WriteString("\n")
	return b.String()
}

// shownIn is the Stories a request was sent showing: those of stories, now
// awaiting weaving, whose input it is. Stories are cut in order and only
// added to, so those shown are the first of the Stories now awaiting.
func shownIn(stories []CutStory, sent Request) ([]CutStory, bool) {
	var b strings.Builder
	n := 0
	for i, s := range stories {
		b.WriteString(storyBlock(s, &n))
		switch {
		case b.Len() == len(sent.Input):
			return stories[:i+1], b.String() == sent.Input
		case b.Len() > len(sent.Input):
			return nil, false
		}
	}
	return nil, false
}

// checkWeaving decodes the model's answer to req, asked of the model named
// asked, shown the Stories shown, and runs weaving's checks over it: each
// citation is a Span shown, each Editor's Span has words and cites at least
// one Span, and each Story has at least one Editor's Span. A result that
// fails them is ErrFailedChecks.
func checkWeaving(shown []CutStory, req Request, asked string, res Response) (Weaving, error) {
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
	var spans []Cited
	for _, s := range shown {
		for _, sp := range s.Spans {
			spans = append(spans, Cited{
				Address: Address{SourceID: s.Item.SourceID, EntryID: s.Item.EntryID, Span: sp},
				Words:   cut.Words(s.Item.Content, sp),
				Title:   s.Item.Title,
			})
		}
	}
	w := Weaving{
		ThroughCut: shown[len(shown)-1].CutID, Usage: res.Usage,
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
