package editor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The Sunday read's tools. Each reads the Record or a Reference source; none
// writes, and none can make a Fact (system design: The Sunday read writes
// Stories and Assessments, never Facts). Everything a tool shows is shown by
// its ref and registered as seen, so a statement can cite it.

// errTool is a call the agent got wrong (an unknown tool, a malformed input,
// a ref to nothing): told to the agent as an error, so it can try again,
// not a failure of the read.
var errTool = errors.New("tool call failed")

func toolError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errTool, fmt.Sprintf(format, args...))
}

// submitRead is the tool the agent submits its read with.
const submitRead = "submit_read"

// factsShownAbout is how many Facts about an Entity one call shows: the
// latest first, and an earlier page by a date to go back before.
const factsShownAbout = 25

// assessmentsShown is how many of the latest Assessments the agent is shown.
const assessmentsShown = 50

func (r *sundayRun) tools() []Tool {
	return []Tool{
		{
			Name: "story",
			Description: "A Story's whole Timeline, back to its start, with the cause Connections to and from its " +
				"Developments and the Stories connected to it through shared Developments or Entities in the same role.",
			Schema: object(map[string]any{"story": refSchema("The Story's ref, such as S3.")}),
		},
		{
			Name: "facts_about",
			Description: fmt.Sprintf("The Facts in the Record involving an Entity, by its name, the latest first, at most %d, "+
				"each with the Stories it is a Development in. Give before (YYYY-MM-DD) to go further back, or an empty string "+
				"for the latest.", factsShownAbout),
			Schema: object(map[string]any{
				"entity": refSchema("The Entity's name, as the Record gives it."),
				"before": refSchema("Only Facts as at a date before this one (YYYY-MM-DD), or an empty string."),
			}),
		},
		{
			Name: "assessments",
			Description: fmt.Sprintf("The Assessments earlier Sunday reads reached, the latest first, at most %d, current and "+
				"superseded, each with the Facts it rests on. Start from them rather than from scratch; a new Assessment "+
				"that revises a current one supersedes it.", assessmentsShown),
			Schema: object(map[string]any{}),
		},
		{
			Name:        submitRead,
			Description: "Submit the finished Sunday read. It is checked before it is published; a read that fails the checks is returned with the reason, to mend and submit again.",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"statements": array(object(map[string]any{
						"text":  str(),
						"cites": array(str()),
					})),
					"assessments": array(object(map[string]any{
						"ref":        str(),
						"statement":  str(),
						"facts":      array(str()),
						"supersedes": str(),
					})),
					"stories": array(object(map[string]any{
						"title":        str(),
						"developments": array(str()),
					})),
				},
				"required":             []string{"assessments", "statements", "stories"},
				"additionalProperties": false,
			},
		},
	}
}

func refSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

// call runs one of the tools that read.
func (r *sundayRun) call(ctx context.Context, c ToolCall) (string, error) {
	switch c.Name {
	case "story":
		var in struct {
			Story string `json:"story"`
		}
		if err := decodeInput(c.Input, &in); err != nil {
			return "", err
		}
		return r.storyTool(ctx, in.Story)
	case "facts_about":
		var in struct {
			Entity string `json:"entity"`
			Before string `json:"before"`
		}
		if err := decodeInput(c.Input, &in); err != nil {
			return "", err
		}
		return r.factsAboutTool(ctx, in.Entity, in.Before)
	case "assessments":
		if err := decodeInput(c.Input, &struct{}{}); err != nil {
			return "", err
		}
		return r.assessmentsTool(ctx)
	}
	return "", toolError("no such tool: %s", c.Name)
}

func decodeInput(input json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(input))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return toolError("the input does not match the tool's schema: %v", err)
	}
	return nil
}

func (r *sundayRun) storyTool(ctx context.Context, ref string) (string, error) {
	id, ok := parseRef(ref, "S")
	if !ok {
		return "", toolError("%q is not a Story's ref, such as S3", ref)
	}
	s, found, err := r.store.Story(ctx, id)
	if err != nil {
		return "", err
	}
	if !found {
		return "", toolError("there is no Story %s", ref)
	}
	connected, err := r.store.ConnectedStories(ctx, id)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n", ref, s.Title)
	if len(s.Entities) > 0 {
		fmt.Fprintf(&b, "Entities: %s\n", strings.Join(s.Entities, "; "))
	}
	b.WriteString("Timeline:\n")
	for _, d := range s.Timeline {
		fmt.Fprintf(&b, "  %s\n", r.showDevelopment(s, d))
	}
	if len(s.Causes) > 0 {
		b.WriteString("Cause Connections:\n")
		for _, c := range s.Causes {
			fmt.Fprintf(&b, "  %s led to %s\n", r.showFact(c.Cause), r.showFact(c.Effect))
		}
	}
	if len(connected) > 0 {
		b.WriteString("Connected Stories:\n")
		for _, c := range connected {
			other := Story{ID: c.ID, Title: c.Title}
			fmt.Fprintf(&b, "  %s: %s\n", storyRefOf(c.ID), c.Title)
			for _, d := range c.Shared {
				fmt.Fprintf(&b, "    shares %s\n", r.showDevelopment(other, d))
			}
			for _, e := range c.Entities {
				fmt.Fprintf(&b, "    %s as %s here in %s and there in %s\n", e.Entity, e.Role,
					r.showDevelopment(s, e.Here), r.showDevelopment(other, e.There))
			}
		}
	}
	return b.String(), nil
}

func (r *sundayRun) factsAboutTool(ctx context.Context, entity, before string) (string, error) {
	var until time.Time
	if before != "" {
		t, err := time.Parse(time.DateOnly, before)
		if err != nil {
			return "", toolError("before must be a date, YYYY-MM-DD, or empty: %q", before)
		}
		until = t
	}
	facts, err := r.store.FactsAbout(ctx, entity, until, factsShownAbout)
	if err != nil {
		return "", err
	}
	if len(facts) == 0 {
		return fmt.Sprintf("The Record holds no Facts involving %q%s.", entity, beforeText(before)), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Facts involving %q%s, the latest first:\n", entity, beforeText(before))
	for _, f := range facts {
		fmt.Fprintf(&b, "  %s\n", r.showFact(f.Fact))
		for _, s := range f.Stories {
			fmt.Fprintf(&b, "    a Development in %s: %s, as %s\n", storyRefOf(s.ID), s.Title, r.registerDevelopment(s, f.Fact))
		}
	}
	return b.String(), nil
}

func (r *sundayRun) assessmentsTool(ctx context.Context) (string, error) {
	as, err := r.store.Assessments(ctx, assessmentsShown)
	if err != nil {
		return "", err
	}
	if len(as) == 0 {
		return "No Sunday read has reached an Assessment yet.", nil
	}
	var b strings.Builder
	b.WriteString("Assessments, the latest first:\n")
	for _, a := range as {
		ref := assessmentRefOf(a.ID)
		r.seen[ref] = Citation{Kind: CitesAssessment, Assessment: a}
		fmt.Fprintf(&b, "  %s (as at %s): %s\n", ref, a.AsAt.Format(time.DateOnly), a.Statement)
		if a.SupersededBy != 0 {
			fmt.Fprintf(&b, "    superseded by %s\n", assessmentRefOf(a.SupersededBy))
		}
		if a.Supersedes != 0 {
			fmt.Fprintf(&b, "    supersedes %s\n", assessmentRefOf(a.Supersedes))
		}
		for _, f := range a.Facts {
			fmt.Fprintf(&b, "    rests on %s\n", r.showFact(f))
		}
	}
	return b.String(), nil
}

func beforeText(before string) string {
	if before == "" {
		return ""
	}
	return " as at a date before " + before
}

// showFact is a Fact as the agent is shown it, by its ref, registered as
// seen.
func (r *sundayRun) showFact(f Development) string {
	ref := factRefOf(f.ID)
	r.seen[ref] = Citation{Kind: CitesFact, Fact: f}
	return fmt.Sprintf("%s (as at %s, from %q): %s", ref, f.AsAt.Format(time.DateOnly), f.ItemTitle, f.Statement)
}

// registerDevelopment registers a Fact as a Development in its Story as
// seen, and is its ref.
func (r *sundayRun) registerDevelopment(s Story, d Development) string {
	ref := storyRefOf(s.ID) + ":" + factRefOf(d.ID)
	r.seen[ref] = Citation{Kind: CitesDevelopment, Fact: d, Story: Story{ID: s.ID, Title: s.Title}}
	if _, ok := r.seen[factRefOf(d.ID)]; !ok {
		r.seen[factRefOf(d.ID)] = Citation{Kind: CitesFact, Fact: d}
	}
	return ref
}

// showDevelopment is a Development as the agent is shown it, by its ref,
// registered as seen: as a Development, and as its Fact.
func (r *sundayRun) showDevelopment(s Story, d Development) string {
	ref := r.registerDevelopment(s, d)
	return fmt.Sprintf("%s (as at %s, from %q): %s", ref, d.AsAt.Format(time.DateOnly), d.ItemTitle, d.Statement)
}

// parseRef is the id in a ref such as S3, with its prefix.
func parseRef(ref, prefix string) (int64, bool) {
	n, ok := strings.CutPrefix(ref, prefix)
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(n, 10, 64)
	return id, err == nil && id > 0
}

func storyRefOf(id int64) string { return "S" + strconv.FormatInt(id, 10) }

func factRefOf(id int64) string { return "F" + strconv.FormatInt(id, 10) }

func assessmentRefOf(id int64) string { return "A" + strconv.FormatInt(id, 10) }
