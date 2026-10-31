package editor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The Sunday read's tools. Each reads the Record or a Reference source; none
// writes (system design: The read's citations are refs to what its agent
// was shown). Everything a tool shows is shown by its ref and registered as
// seen, so a statement can cite it: a Span as P1, P2 and so on, an
// Assessment as A and its id, a lookup as L1, L2 and so on.

// errTool is a call the agent got wrong (an unknown tool, a malformed input,
// a ref to nothing): told to the agent as an error, so it can try again,
// not a failure of the read.
var errTool = errors.New("tool call failed")

func toolError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errTool, fmt.Sprintf(format, args...))
}

// submitRead is the tool the agent submits its read with.
const submitRead = "submit_read"

// spansShownNaming is how many Spans naming an Entity one call shows: those
// of the latest Items first, and an earlier page by a date to go back
// before.
const spansShownNaming = 25

// assessmentsShown is how many of the latest Assessments the agent is shown.
const assessmentsShown = 50

func (r *sundayRun) tools() []Tool {
	return []Tool{
		{
			Name: "story",
			Description: "A Span's Story: the Spans its latest gathering holds, that move on what it says or that it moves on, " +
				"with the Briefing or Sunday read that gathered it, then its relations judged since (follows, followed by, " +
				"same as, contradicts), each to a Span of another Item.",
			Schema: object(map[string]any{"span": refSchema("The Span's ref, such as P3.")}),
		},
		{
			Name: "spans_naming",
			Description: fmt.Sprintf("The Spans in the Record naming an Entity, by its name as a Span shown to you names it, "+
				"those of the latest Items first, at most %d. Give before (YYYY-MM-DD) to go further back, or an empty string "+
				"for the latest.", spansShownNaming),
			Schema: object(map[string]any{
				"entity": refSchema("The Entity's name, as a Span shown to you names it."),
				"before": refSchema("Only Spans of Items published before this date (YYYY-MM-DD), or an empty string."),
			}),
		},
		{
			Name: "assessments",
			Description: fmt.Sprintf("The Assessments earlier Sunday reads reached, the latest first, at most %d, current and "+
				"superseded, each with the Spans and Assessments it rests on. Start from them rather than from scratch; a new "+
				"Assessment that revises a current one supersedes it.", assessmentsShown),
			Schema: object(map[string]any{}),
		},
		{
			Name:        "wikidata_search",
			Description: "Search Wikidata, a Reference source, for items whose label or alias matches the text: their ids and labels, best first.",
			Schema:      object(map[string]any{"text": refSchema("What to search for, such as a person's or an office's name.")}),
		},
		{
			Name: "wikidata_item",
			Description: "Look a Wikidata item up by its id: its label, aliases, what it is an instance of, the positions " +
				"a person has held and the people who have held an office, with their dates. Each lookup is kept, " +
				"as the item and the revision seen, under a ref (L1, L2 and so on) a statement can cite.",
			Schema: object(map[string]any{"id": refSchema("The item's id, such as Q14211.")}),
		},
		{
			Name:        submitRead,
			Description: "Submit the finished Sunday read. It is checked before it is published; a read that fails the checks is returned with the reason, to mend and submit again.",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"assessments": array(object(map[string]any{
						"assessments": array(str()),
						"ref":         str(),
						"spans":       array(str()),
						"statement":   str(),
						"supersedes":  str(),
					})),
					"statements": array(object(map[string]any{
						"cites": array(str()),
						"stories": array(object(map[string]any{
							"span":  str(),
							"story": array(str()),
						})),
						"text": str(),
					})),
				},
				"required":             []string{"assessments", "statements"},
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
			Span string `json:"span"`
		}
		if err := decodeInput(c.Input, &in); err != nil {
			return "", err
		}
		return r.storyTool(ctx, in.Span)
	case "spans_naming":
		var in struct {
			Entity string `json:"entity"`
			Before string `json:"before"`
		}
		if err := decodeInput(c.Input, &in); err != nil {
			return "", err
		}
		return r.spansNamingTool(ctx, in.Entity, in.Before)
	case "assessments":
		if err := decodeInput(c.Input, &struct{}{}); err != nil {
			return "", err
		}
		return r.assessmentsTool(ctx)
	case "wikidata_search":
		var in struct {
			Text string `json:"text"`
		}
		if err := decodeInput(c.Input, &in); err != nil {
			return "", err
		}
		return r.wikidataSearch(ctx, in.Text)
	case "wikidata_item":
		var in struct {
			ID string `json:"id"`
		}
		if err := decodeInput(c.Input, &in); err != nil {
			return "", err
		}
		return r.wikidataItem(ctx, in.ID)
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
	c, ok := r.seen[ref]
	if !ok || c.Kind != CitesSpan {
		return "", toolError("%q is not the ref of a Span this run has been shown, such as P3", ref)
	}
	at := c.Span.At
	g, gathered, err := r.store.LatestGathering(ctx, at)
	if err != nil {
		return "", err
	}
	rels, err := r.store.RelationsOf(ctx, at)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if gathered {
		fmt.Fprintf(&b, "%s's Story, as %s gathered it:\n", ref, gatheredBy(g))
		if len(g.Spans) == 0 {
			b.WriteString("  nothing: no Span moves on what it says\n")
		}
		for _, o := range g.Spans {
			fmt.Fprintf(&b, "  %s\n", r.showSpan(Citation{Kind: CitesSpan, Span: o}))
		}
	}
	var since []SpanRelation
	for _, rel := range rels {
		if gathered && rel.ID < g.ID {
			continue // judged before the latest gathering, which weighed it
		}
		since = append(since, rel)
	}
	switch {
	case len(since) > 0 && gathered:
		b.WriteString("Its relations judged since:\n")
	case len(since) > 0:
		fmt.Fprintf(&b, "%s's Story has not been gathered. Its relations:\n", ref)
	case !gathered:
		fmt.Fprintf(&b, "%s's Story has not been gathered, and it relates to no Span.\n", ref)
	}
	for _, rel := range since {
		how := howRelatedFrom(rel.Relation, at)
		if rel.Relation.Corroborates() {
			how += " (corroborated)"
		}
		fmt.Fprintf(&b, "  %s %s\n", how, r.showSpan(Citation{Kind: CitesSpan, Span: rel.Other}))
	}
	return b.String(), nil
}

// gatheredBy names the pass that gathered g.
func gatheredBy(g GatheredStory) string {
	if g.ByRead {
		return "the Sunday read for " + g.Day
	}
	return "the Briefing for " + g.Day
}

func (r *sundayRun) spansNamingTool(ctx context.Context, entity, before string) (string, error) {
	e, ok := r.entities[strings.ToLower(strings.TrimSpace(entity))]
	if !ok {
		return "", toolError("%q is not an Entity a Span shown to you names; give its name as a Span shows it", entity)
	}
	var until time.Time
	if before != "" {
		t, err := time.Parse(time.DateOnly, before)
		if err != nil {
			return "", toolError("before must be a date, YYYY-MM-DD, or empty: %q", before)
		}
		until = t
	}
	spans, err := r.store.SpansMentioning(ctx, []NamedEntity{e}, math.MaxInt64)
	if err != nil {
		return "", err
	}
	spans = slices.DeleteFunc(spans, func(o OtherSpan) bool { return !until.IsZero() && !o.Item.Published.Before(until) })
	slices.SortStableFunc(spans, func(a, b OtherSpan) int { return b.Item.Published.Compare(a.Item.Published) })
	spans = spans[:min(len(spans), spansShownNaming)]
	if len(spans) == 0 {
		return fmt.Sprintf("The Record holds no Span naming %s%s.", e.Name, beforeText(before)), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Spans naming %s%s, the latest Items first:\n", e.Name, beforeText(before))
	for _, o := range spans {
		fmt.Fprintf(&b, "  %s\n", r.showSpan(Citation{Kind: CitesSpan, Span: o}))
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
		for _, e := range a.Earlier {
			fmt.Fprintf(&b, "    rests on %s\n", assessmentRefOf(e.ID))
		}
		for _, o := range a.Spans {
			fmt.Fprintf(&b, "    rests on %s\n", r.showSpan(Citation{Kind: CitesSpan, Span: o}))
		}
	}
	return b.String(), nil
}

// holdersShown is how many of an office's holders an item lookup shows, the
// latest first.
const holdersShown = 20

func (r *sundayRun) wikidataSearch(ctx context.Context, text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", toolError("search for something")
	}
	ids, err := r.wikidata.Search(ctx, text)
	if err != nil {
		return "", toolError("Wikidata did not answer: %v", err)
	}
	if len(ids) == 0 {
		return fmt.Sprintf("Wikidata has no item matching %q.", text), nil
	}
	labels, err := r.wikidata.Labels(ctx, ids)
	if err != nil {
		return "", toolError("Wikidata did not answer: %v", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Wikidata items matching %q, best first (look one up with wikidata_item to cite it):\n", text)
	for _, id := range ids {
		fmt.Fprintf(&b, "  %s: %s\n", id, labels[id])
	}
	return b.String(), nil
}

func (r *sundayRun) wikidataItem(ctx context.Context, id string) (string, error) {
	if _, ok := parseRef(id, "Q"); !ok {
		return "", toolError("%q is not a Wikidata item's id, such as Q14211", id)
	}
	items, err := r.wikidata.Items(ctx, []string{id})
	if err != nil {
		return "", toolError("Wikidata did not answer: %v", err)
	}
	if len(items) == 0 {
		return "", toolError("Wikidata has no item %s", id)
	}
	it := items[0]
	holders := slices.Clone(it.Holders)
	slices.SortFunc(holders, func(a, b Tenure) int { return b.Start.Compare(a.Start) })
	holders = holders[:min(len(holders), holdersShown)]
	ids := slices.Clone(it.InstanceOf)
	for _, t := range it.Positions {
		ids = append(ids, t.Office)
	}
	for _, t := range holders {
		ids = append(ids, t.Holder)
	}
	labels := map[string]string{}
	if len(ids) > 0 {
		if labels, err = r.wikidata.Labels(ctx, ids); err != nil {
			return "", toolError("Wikidata did not answer: %v", err)
		}
	}
	r.lookups++
	ref := "L" + strconv.Itoa(r.lookups)
	r.seen[ref] = Citation{Kind: CitesLookup, Lookup: Lookup{
		Reference: "Wikidata", ItemID: it.ID, Revision: it.Revision, At: r.now, Licence: WikidataLicence,
	}}
	named := func(id string) string {
		if l := labels[id]; l != "" {
			return l + " (" + id + ")"
		}
		return id
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: Wikidata %s, revision %d, looked up %s: %s\n", ref, it.ID, it.Revision, r.now.Format(time.DateOnly), it.Label)
	if len(it.Aliases) > 0 {
		fmt.Fprintf(&b, "  also known as: %s\n", strings.Join(it.Aliases, "; "))
	}
	for _, c := range it.InstanceOf {
		fmt.Fprintf(&b, "  instance of: %s\n", named(c))
	}
	for _, t := range it.Positions {
		fmt.Fprintf(&b, "  position held: %s%s\n", named(t.Office), tenureText(t))
	}
	for _, t := range holders {
		fmt.Fprintf(&b, "  held by: %s%s\n", named(t.Holder), tenureText(t))
	}
	return b.String(), nil
}

// tenureText is a tenure's dates, as Wikidata gives them.
func tenureText(t Tenure) string {
	var s string
	if !t.Start.IsZero() {
		if t.StartToTheDay {
			s += " from " + t.Start.Format(time.DateOnly)
		} else {
			s += " from " + t.Start.Format("2006")
		}
	}
	if !t.End.IsZero() {
		s += " until " + t.End.Format(time.DateOnly)
	}
	return s
}

func beforeText(before string) string {
	if before == "" {
		return ""
	}
	return " published before " + before
}

// showSpan is a Span as the agent is shown it, by its ref, registered as
// seen, and the Entities it names registered for spans_naming: its words,
// whose they are, its Item and its date, and the Entities it names.
func (r *sundayRun) showSpan(c Citation) string {
	o := c.Span
	if r.spans == nil {
		r.spans, r.entities = map[Address]string{}, map[string]NamedEntity{}
	}
	ref, ok := r.spans[o.At]
	if !ok {
		ref = "P" + strconv.Itoa(len(r.spans)+1)
		r.spans[o.At] = ref
		r.seen[ref] = Citation{Kind: CitesSpan, Span: o}
	}
	var note []string
	note = append(note, fmt.Sprintf("from %q", o.Item.Title))
	if !o.Item.Published.IsZero() {
		note = append(note, "published "+o.Item.Published.UTC().Format(time.DateOnly))
	}
	if o.Speaker != "" {
		note = append(note, "spoken by "+o.Speaker)
	}
	if o.ReadingID == 0 {
		note = append(note, "not yet read")
	}
	var names []string
	for _, e := range o.Entities {
		r.entities[strings.ToLower(e.Name)] = e
		if !slices.Contains(names, e.Name) {
			names = append(names, e.Name)
		}
	}
	if len(names) > 0 {
		note = append(note, "names "+strings.Join(names, "; "))
	}
	return fmt.Sprintf("%s (%s): %s", ref, strings.Join(note, "; "), o.Words)
}

// parseRef is the id in a ref such as A3, with its prefix.
func parseRef(ref, prefix string) (int64, bool) {
	n, ok := strings.CutPrefix(ref, prefix)
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(n, 10, 64)
	return id, err == nil && id > 0
}

func assessmentRefOf(id int64) string { return "A" + strconv.FormatInt(id, 10) }
