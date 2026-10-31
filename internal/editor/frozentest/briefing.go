package frozentest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
)

// StoryBriefingStore is the part of the Record a Briefing over Stories is
// written to: the earlier Briefing's shape, which the Record still holds
// for the days it wrote, and the pages still show.
type StoryBriefingStore interface {
	LastBriefingWritten(ctx context.Context) (time.Time, error)
	ToBrief(ctx context.Context, after, upTo time.Time) ([]editor.ToBrief, error)
	SaveStoryBriefing(ctx context.Context, b editor.StoryBriefing) error
	SaveBriefingFailure(ctx context.Context, f editor.BriefingFailure) error
	editor.CallKeeper
}

// BriefDeps are what a Briefing over Stories is written with: the Record,
// the model answering in the earlier act's schema, and the log, which takes
// "writing the Briefing failed" for an answer its checks refused.
type BriefDeps struct {
	Store StoryBriefingStore
	Model editor.Model
	Log   *slog.Logger
}

// storyBriefingAct is the earlier Briefing act's name, under which its calls
// were kept, as the new act's are.
const storyBriefingAct = "briefing"

// Brief writes the Briefing over Stories for the day that holds now, as the
// retired act did: the Stories the Topics select with Developments placed
// since the last Briefing, told in statements citing those Developments. A
// Briefing that fails its checks is not written, and the failure is kept.
func Brief(ctx context.Context, d BriefDeps, now time.Time) error {
	after, err := d.Store.LastBriefingWritten(ctx)
	if err != nil {
		return err
	}
	if after.IsZero() {
		after = now.Add(-24 * time.Hour)
	}
	stories, err := d.Store.ToBrief(ctx, after, now)
	if err != nil {
		return err
	}
	day := now.Format(time.DateOnly)
	b := editor.StoryBriefing{Day: day, WrittenAt: now}
	if len(stories) > 0 {
		refs := refsFor(stories)
		req := editor.Request{Act: storyBriefingAct, Instructions: storyBriefingInstructions,
			Input: storyBriefingInput(now, stories, refs), Schema: storyBriefingSchema(refs)}
		call := editor.Call{Act: req.Act, Key: day, At: now, Request: req, RequestHash: editor.RequestHash(req, "")}
		res, err := d.Model.Complete(ctx, req)
		call.Response = &res
		if err == nil {
			b.Model = res.Model
			b, err = checkStoryBriefing(b, stories, refs, res)
			if err != nil {
				call.Outcome, call.Why = editor.Refused, err.Error()
			}
		} else {
			call.Outcome, call.Why = editor.Failed, err.Error()
		}
		if err != nil {
			d.Log.ErrorContext(ctx, "writing the Briefing failed", "day", day, "error", err)
			if err := d.Store.KeepCall(ctx, call); err != nil {
				return err
			}
			return d.Store.SaveBriefingFailure(ctx, editor.BriefingFailure{Day: day, At: now, Reason: err.Error()})
		}
		b.WrittenAt = now
		if err := d.Store.SaveStoryBriefing(ctx, b); err != nil {
			return err
		}
		call.Outcome = editor.Applied
		return d.Store.KeepCall(ctx, call)
	}
	return d.Store.SaveStoryBriefing(ctx, b)
}

const storyBriefingInstructions = `You are the Editor of a private slow-news briefing, writing today's Briefing over Stories, each told in statements citing its Developments.`

func storyBriefingSchema(refs briefingRefs) map[string]any {
	enum := func(values []string) map[string]any { return map[string]any{"type": "string", "enum": values} }
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"stories": map[string]any{"type": "array", "items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"story": enum(refs.stories),
					"statements": map[string]any{"type": "array", "items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"text":         map[string]any{"type": "string"},
							"developments": map[string]any{"type": "array", "items": enum(refs.developments)},
						},
					}},
				},
			}},
		},
	}
}

type storyBriefingOutput struct {
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
	story        map[string]int
	development  map[string][2]int
}

func refsFor(stories []editor.ToBrief) briefingRefs {
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

// checkStoryBriefing runs the retired act's checks: every statement cites
// at least one Development of its own Story; and at following Depth or
// closer, every Development is in the Briefing.
func checkStoryBriefing(b editor.StoryBriefing, stories []editor.ToBrief, refs briefingRefs, res editor.Response) (editor.StoryBriefing, error) {
	refuse := func(format string, args ...any) (editor.StoryBriefing, error) {
		return editor.StoryBriefing{}, fmt.Errorf("writing the Briefing for %s: %w: %s", b.Day, editor.ErrFailedChecks, fmt.Sprintf(format, args...))
	}
	var out storyBriefingOutput
	dec := json.NewDecoder(bytes.NewReader(res.Output))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return refuse("output does not match the schema: %v", err)
	}
	told := make([]bool, len(stories))
	cited := map[string]bool{}
	for _, s := range out.Stories {
		i, ok := refs.story[s.Story]
		if !ok {
			return refuse("a Story not given: %q", s.Story)
		}
		if told[i] {
			return refuse("%s told twice", s.Story)
		}
		told[i] = true
		tb := stories[i]
		bs := editor.BriefedStory{Story: tb.Story}
		for _, st := range s.Statements {
			text := strings.TrimSpace(st.Text)
			if text == "" {
				return refuse("a statement in %s with no text", s.Story)
			}
			if len(st.Developments) == 0 {
				return refuse("an untraced statement in %s, citing no Development: %q", s.Story, text)
			}
			stmt := editor.StoryStatement{Text: text}
			for _, ref := range st.Developments {
				at, ok := refs.development[ref]
				if !ok || at[0] != i {
					return refuse("a statement in %s cites %s, which is not one of its Developments: %q", s.Story, ref, text)
				}
				d := tb.Today[at[1]]
				if !slices.ContainsFunc(stmt.Developments, func(x editor.Development) bool { return x.ID == d.ID }) {
					stmt.Developments = append(stmt.Developments, d)
				}
				cited[ref] = true
			}
			bs.Statements = append(bs.Statements, stmt)
		}
		if len(bs.Statements) == 0 {
			return refuse("%s is included with no statement", s.Story)
		}
		b.Stories = append(b.Stories, bs)
	}
	for _, ref := range refs.developments {
		at := refs.development[ref]
		if tb := stories[at[0]]; tb.Depth >= interest.Following && !cited[ref] {
			return refuse("%s of %q, followed at %s Depth, is left out", ref, tb.Story.Title, tb.Depth)
		}
	}
	return b, nil
}

// storyBriefingInput is the Stories to brief as the model was shown them.
func storyBriefingInput(day time.Time, stories []editor.ToBrief, refs briefingRefs) string {
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
