package editor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/samthorold/go-slow/internal/interest"
)

// ToBrief is a Story the Owner's Topics select for the Briefing, with the
// Developments it has had since the last Briefing.
type ToBrief struct {
	Story Story
	// Today are the Developments the Briefing is to cover, in order.
	Today []Development
	// Topics are the names of the Topics whose Items the Developments came
	// from, and Depth the closest Depth among them.
	Topics []string
	Depth  interest.Depth
}

// Briefing is the Briefing written for a day: the Stories that moved on, in
// the order the Editor put them, each told in statements that cite the
// Developments they rest on.
type Briefing struct {
	// Day is the day it is for, YYYY-MM-DD, in the Owner's time zone.
	Day       string
	WrittenAt time.Time
	Stories   []BriefedStory
	Model     string
	Usage     Usage
}

// BriefedStory is one Story as the Briefing tells it.
type BriefedStory struct {
	Story      Story
	Statements []Statement
}

// Statement is one statement in the Briefing, with the Developments it
// rests on: each traces to its Fact and the Item it was drawn from.
type Statement struct {
	Text         string
	Developments []Development
}

// BriefingFailure is a Briefing that failed to be written: its call failed,
// or its result failed the act's checks.
type BriefingFailure struct {
	Day    string
	At     time.Time
	Reason string
}

// BriefingStore is the part of the Record the Briefing is written from and
// kept in.
type BriefingStore interface {
	// LastBriefingWritten is when the latest Briefing was written, or zero
	// if none has been.
	LastBriefingWritten(ctx context.Context) (time.Time, error)
	// ToBrief are the Stories with Developments placed after after and up
	// to upTo, from Items a Topic ties, each with only those Developments.
	ToBrief(ctx context.Context, after, upTo time.Time) ([]ToBrief, error)
	// SaveBriefing keeps a Briefing, all at once.
	SaveBriefing(ctx context.Context, b Briefing) error
	// SaveBriefingFailure keeps a failure to write a day's Briefing.
	SaveBriefingFailure(ctx context.Context, f BriefingFailure) error
}

// firstWindow is how far back the first Briefing ever written looks for
// Developments, with no earlier Briefing to start from.
const firstWindow = 24 * time.Hour

// Brief writes the Briefing for the day that holds now, in now's location,
// at now. It covers the Developments placed since the last Briefing was
// written, so each is in exactly one Briefing, whatever the Collection runs
// between; the first Briefing covers the day before it. A Briefing that
// fails is not written: the failure is logged and kept, to be shown, and
// writing is tried again later. Only a failing Record is an error.
func (e *Editor) Brief(ctx context.Context, now time.Time) error {
	after, err := e.Briefings.LastBriefingWritten(ctx)
	if err != nil {
		return err
	}
	if after.IsZero() {
		after = now.Add(-firstWindow)
	}
	stories, err := e.Briefings.ToBrief(ctx, after, now)
	if err != nil {
		return err
	}
	b, err := WriteBriefing(ctx, e.BriefingModel, now, stories)
	if err != nil {
		day := now.Format(time.DateOnly)
		e.Log.ErrorContext(ctx, "writing the Briefing failed", "day", day, "error", err)
		return e.Briefings.SaveBriefingFailure(ctx, BriefingFailure{Day: day, At: now, Reason: err.Error()})
	}
	b.WrittenAt = now
	if err := e.Briefings.SaveBriefing(ctx, b); err != nil {
		return err
	}
	e.Log.InfoContext(ctx, "Briefing written", "day", b.Day, "stories", len(b.Stories), "model", b.Model,
		"input_tokens", b.Usage.InputTokens, "output_tokens", b.Usage.OutputTokens)
	return nil
}

// briefingInstructions are the act of writing the Briefing's own
// instructions.
const briefingInstructions = `You are the Editor of a private slow-news briefing, writing today's Briefing for its one reader, the Owner.

You are given the Stories that moved on since the last Briefing. A Story is an ongoing subject followed across days; each comes with its Developments (D1, D2 and so on): Facts that moved it on, each holding as at a date. Each Story is followed at a Depth, from the Owner's Topics:
- passing: only what moves the Story on in a big way. Leave out a Development that does not, and leave out the Story altogether if none does.
- following: every one of the Story's Developments, plainly told.
- close: every one of its Developments, with its detail.

Tell each Story you include in one or more short statements, in plain, neutral prose, as a well-informed colleague would: what happened, who did it, when. Every statement rests on Developments of its own Story and cites, by ref, each one it draws on. Say nothing that its cited Developments do not say: no background, no speculation, no judgement of your own. Several Developments may be told in one statement where they belong together. At following and close Depth, every Development of the Story must be cited by at least one statement.

Answer with the Stories you include, most consequential first, each by its ref (S1, S2 and so on as given).`

// briefingSchema is the act's output schema. Its refs are enumerated, the
// Stories given and their Developments, so citations are structural: the
// checks trace each statement by its refs, not by reading its prose.
func briefingSchema(refs briefingRefs) map[string]any {
	enum := func(values []string) map[string]any { return map[string]any{"type": "string", "enum": values} }
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"stories": array(object(map[string]any{
				"story": enum(refs.stories),
				"statements": array(object(map[string]any{
					"text":         str(),
					"developments": array(enum(refs.developments)),
				})),
			})),
		},
		"required":             []string{"stories"},
		"additionalProperties": false,
	}
}

type briefingOutput struct {
	Stories []struct {
		Story      string `json:"story"`
		Statements []struct {
			Text         string   `json:"text"`
			Developments []string `json:"developments"`
		} `json:"statements"`
	} `json:"stories"`
}

// briefingRefs are the refs the model is given: S<id> for each Story, and
// D1, D2... for the Developments, numbered across the Stories in order.
type briefingRefs struct {
	stories      []string
	developments []string
	// story and development resolve a ref to its Story (an index into the
	// Stories to brief) and Development (an index into that Story's Today).
	story       map[string]int
	development map[string][2]int
}

func refsFor(stories []ToBrief) briefingRefs {
	r := briefingRefs{story: map[string]int{}, development: map[string][2]int{}}
	for i, tb := range stories {
		ref := "S" + strconv.FormatInt(tb.Story.ID, 10)
		r.stories = append(r.stories, ref)
		r.story[ref] = i
		for j := range tb.Today {
			ref := "D" + strconv.Itoa(len(r.developments)+1)
			r.developments = append(r.developments, ref)
			r.development[ref] = [2]int{i, j}
		}
	}
	return r
}

// WriteBriefing is the act of writing the Briefing for the day that holds
// day, in day's location, from the Stories the Owner's Topics select. With
// none, the Briefing is written empty, and no model is called: it says that
// nothing moved on. A result that fails the act's checks, such as a
// statement that cites no Development, is ErrFailedChecks, and none of it
// is returned.
func WriteBriefing(ctx context.Context, m Model, day time.Time, stories []ToBrief) (Briefing, error) {
	b := Briefing{Day: day.Format(time.DateOnly)}
	if len(stories) == 0 {
		return b, nil
	}
	refs := refsFor(stories)
	res, err := m.Complete(ctx, Request{
		Act:          "briefing",
		Instructions: briefingInstructions,
		Input:        briefingInput(day, stories, refs),
		Schema:       briefingSchema(refs),
	})
	if err != nil {
		return Briefing{}, fmt.Errorf("writing the Briefing for %s: %w", b.Day, err)
	}
	b.Model, b.Usage = res.Model, res.Usage
	return checkBriefing(b, stories, refs, res)
}

// checkBriefing decodes the model's Briefing and runs the act's checks:
// everything traces, so every statement cites at least one Development of
// its own Story; and at following Depth or closer, every Development is in
// the Briefing.
func checkBriefing(b Briefing, stories []ToBrief, refs briefingRefs, res Response) (Briefing, error) {
	var out briefingOutput
	dec := json.NewDecoder(bytes.NewReader(res.Output))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return Briefing{}, failedBriefing(b, "output does not match the schema: %v", err)
	}
	told := make([]bool, len(stories))
	cited := map[string]bool{}
	for _, s := range out.Stories {
		i, ok := refs.story[s.Story]
		if !ok {
			return Briefing{}, failedBriefing(b, "a Story not given: %q", s.Story)
		}
		if told[i] {
			return Briefing{}, failedBriefing(b, "%s told twice", s.Story)
		}
		told[i] = true
		tb := stories[i]
		bs := BriefedStory{Story: tb.Story}
		for _, st := range s.Statements {
			text := strings.TrimSpace(st.Text)
			if text == "" {
				return Briefing{}, failedBriefing(b, "a statement in %s with no text", s.Story)
			}
			if len(st.Developments) == 0 {
				return Briefing{}, failedBriefing(b, "an untraced statement in %s, citing no Development: %q", s.Story, text)
			}
			stmt := Statement{Text: text}
			for _, ref := range st.Developments {
				at, ok := refs.development[ref]
				if !ok || at[0] != i {
					return Briefing{}, failedBriefing(b, "a statement in %s cites %s, which is not one of its Developments: %q", s.Story, ref, text)
				}
				d := tb.Today[at[1]]
				if !slices.ContainsFunc(stmt.Developments, func(x Development) bool { return x.ID == d.ID }) {
					stmt.Developments = append(stmt.Developments, d)
				}
				cited[ref] = true
			}
			bs.Statements = append(bs.Statements, stmt)
		}
		if len(bs.Statements) == 0 {
			return Briefing{}, failedBriefing(b, "%s is included with no statement", s.Story)
		}
		b.Stories = append(b.Stories, bs)
	}
	for _, ref := range refs.developments {
		at := refs.development[ref]
		if tb := stories[at[0]]; tb.Depth >= interest.Following && !cited[ref] {
			return Briefing{}, failedBriefing(b, "%s of %q, followed at %s Depth, is left out", ref, tb.Story.Title, tb.Depth)
		}
	}
	return b, nil
}

func failedBriefing(b Briefing, format string, args ...any) error {
	return fmt.Errorf("writing the Briefing for %s: %w: %s", b.Day, ErrFailedChecks, fmt.Sprintf(format, args...))
}

// briefingInput is the Stories to brief as the model is shown them.
func briefingInput(day time.Time, stories []ToBrief, refs briefingRefs) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The Briefing for %s.\n", day.Format("Monday 2 January 2006"))
	n := 0
	for i, tb := range stories {
		fmt.Fprintf(&b, "\n%s: %s\n", refs.stories[i], tb.Story.Title)
		fmt.Fprintf(&b, "  Depth: %s (Topics: %s)\n", tb.Depth, strings.Join(tb.Topics, "; "))
		for _, d := range tb.Today {
			fmt.Fprintf(&b, "  %s (as at %s, from %q): %s\n", refs.developments[n], d.AsAt.Format(time.DateOnly), d.ItemTitle, d.Statement)
			n++
		}
	}
	return b.String()
}
