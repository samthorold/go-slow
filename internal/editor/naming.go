package editor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// NamingVersion is the version of naming's instructions, schema and checks,
// and of how its input is shown. A new version is a new Judge (system design:
// Every Judgement carries its Judge). Change it with namingInstructions,
// namingSchema, checkNaming or namingInput.
const NamingVersion = 2

// namingAct names naming, as its requests, calls and work do.
const namingAct = "naming"

// MaxNameWords is the most words a Name may have, as namingInstructions
// state it.
const MaxNameWords = 12

// StoryName is a Story's Name: a short subject for the Owner to recognise it
// by in a list, kept as a Judgement of its own with its Judge (system
// design: Every Story the Editor makes has a Name, and nothing reads it but
// the Owner). Not Name, which is what a Span names.
type StoryName struct {
	// Story is the Story it names.
	Story int64
	Text  string
	Judge Judge
	At    time.Time
	// Call is the kept call that made it, found by its Judge's request hash;
	// zero if none was kept.
	Call int64
	// Usage is what the call cost, for the logs; not kept with the Name.
	Usage Usage
}

// NamingStore is the part of the Record naming reads and writes.
type NamingStore interface {
	// StoriesToName are the Stories the Editor made that have no Name and
	// whose naming has not been marked failed, oldest first, each with its
	// Editor's Spans.
	StoriesToName(ctx context.Context) ([]Story, error)
	// KeepName keeps a Name.
	KeepName(ctx context.Context, n StoryName) error
}

// namingInstructions are naming's instructions. They state every standard
// checkNaming holds a Name to: that it is given, on one line, in at most
// MaxNameWords (twelve) words.
const namingInstructions = `Below is a Story from a slow-news publication: its editor's words, in numbered parts, each followed by the passages from published articles it cites. Give the Story a name: a short subject by which a reader scanning a list of Stories recognises this one. Say what the Story is about, in plain words. The name is a single line of at most twelve words.`

// namingSchema is naming's output schema: the name alone.
var namingSchema = object(map[string]any{"name": str()})

// namingOutput is naming's answer as the model gives it.
type namingOutput struct {
	Name *string `json:"name"`
}

// Naming is the naming act as it runs in batches: each Story the Editor
// made with no Name, named once its result passes the checks. A Story never
// changes once made, so its request is the same however late its result.
func (e *Editor) Naming() BatchAct {
	return BatchAct{
		Name: namingAct,
		Waiting: func(ctx context.Context, _ time.Time) ([]Job, error) {
			stories, err := e.Namings.StoriesToName(ctx)
			if err != nil {
				return nil, err
			}
			jobs := make([]Job, 0, len(stories))
			for _, s := range stories {
				req := Request{Act: namingAct, Instructions: namingInstructions, Input: namingInput(s),
					Schema: namingSchema, Effort: EffortLow}
				jobs = append(jobs, Job{
					Key:     strconv.FormatInt(s.ID, 10),
					Request: req,
					Apply: func(ctx context.Context, res Response, now time.Time) error {
						n, err := checkNaming(req, modelOf(e.ModelFor, req.Act), res)
						if err != nil {
							return err
						}
						n.Story, n.At = s.ID, now
						if err := e.Namings.KeepName(ctx, n); err != nil {
							return err
						}
						e.Log.InfoContext(ctx, "named", "story", s.ID, "model", n.Judge.Model,
							"input_tokens", n.Usage.InputTokens, "output_tokens", n.Usage.OutputTokens)
						return nil
					},
				})
			}
			return jobs, nil
		},
	}
}

// namingInput shows a Story's Editor's Spans in the order written, numbered
// E1, E2…, each followed by the Spans it cites, under their Items' titles,
// or marked the Editor's for an earlier Editor's Span.
// It shows no Name: a Command is never shown one.
func namingInput(s Story) string {
	var b strings.Builder
	for i, es := range s.Spans {
		fmt.Fprintf(&b, "E%d: %s\n", i+1, es.Body)
		for _, c := range es.Cites {
			if !c.Of.IsZero() {
				fmt.Fprintf(&b, "  cites the Editor's earlier words: %s\n", c.Words)
				continue
			}
			fmt.Fprintf(&b, "  cites %q: %s\n", c.Title, c.Words)
		}
	}
	return b.String()
}

// checkNaming decodes the model's answer to req, asked of the model named
// asked, and runs naming's checks over it: the name is given, on one line,
// in at most MaxNameWords words. A result that fails them is
// ErrFailedChecks.
func checkNaming(req Request, asked string, res Response) (StoryName, error) {
	refuse := func(format string, args ...any) (StoryName, error) {
		return StoryName{}, fmt.Errorf("naming: %w: %s", ErrFailedChecks, fmt.Sprintf(format, args...))
	}
	var out namingOutput
	dec := json.NewDecoder(bytes.NewReader(res.Output))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return refuse("output does not match the schema: %v", err)
	}
	if out.Name == nil {
		return refuse("output does not match the schema: no name")
	}
	text := strings.TrimSpace(*out.Name)
	switch words := len(strings.Fields(text)); {
	case text == "":
		return refuse("the name is empty")
	case strings.ContainsAny(text, "\r\n"):
		return refuse("the name runs to more than one line")
	case words > MaxNameWords:
		return refuse("the name has %d words, more than %d", words, MaxNameWords)
	}
	return StoryName{
		Text: text, Usage: res.Usage,
		Judge: Judge{Act: req.Act, Version: NamingVersion, Model: res.Model, RequestHash: RequestHash(req, asked)},
	}, nil
}
