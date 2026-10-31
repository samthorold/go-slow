package editor

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// BriefingVersion is the version of the Briefing's instructions and schema.
// A new version is a new Judge (system design: Every judgement carries its
// judge). Version 1 was the Briefing over Stories; 2 tells Spans. Change it
// with briefingInstructions or briefingSchema.
const BriefingVersion = 2

// ItemToTell is an Item with Spans the Briefing is shown: those its Item's
// newest reading did not reject, or, for an Item only cut, every Span of its
// Cut (system design: The Briefing doesn't wait for construction).
type ItemToTell struct {
	Item Item
	// Read is whether the Item has been read: an Item only cut is shown as
	// written, with nothing judged of it.
	Read  bool
	Spans []SpanToTell
}

// SpanToTell is a Span the Briefing is shown, as its Item's newest reading
// reads it, with the relations judged of it, either way.
type SpanToTell struct {
	OtherSpan
	Relations []SpanRelation
}

// Briefing is the Briefing written for a day: what the Editor judged worth
// telling of the Spans read since the last, in statements each citing the
// Spans it rests on, and the Story it gathered from each Span it told. It is
// one Judgement, with its Judge, and each statement and gathering is a
// Judgement of its own within it.
type Briefing struct {
	// ID is the Briefing's in the Record, once it is kept there.
	ID int64
	// Day is the day it is for, YYYY-MM-DD, in the Owner's time zone.
	Day       string
	WrittenAt time.Time
	// Judge is what wrote it; its Model is empty for a Briefing written
	// empty, with no call.
	Judge      Judge
	Statements []Statement
	Gathered   []Gathering
	// Usage is what the call cost, for the logs; not kept with it.
	Usage Usage
	// Call is the kept call that wrote it, as a view finds it; zero if none.
	Call int64
}

// Statement is one statement of the Briefing, in the Editor's words, citing
// every Span it rests on.
type Statement struct {
	ID    int64
	Text  string
	Cites []OtherSpan
}

// Gathering is the Story the Briefing gathered from a Span it told (system
// design: A Story is gathered by the pass that tells it): the Spans its
// relations reach that move on what From says, or that it moves on.
type Gathering struct {
	ID    int64
	From  Address
	Story []Address
}

// GatheredStory is a Story as a Span's page shows its latest gathering:
// the gathering, the day of the Briefing that gathered it, when, its Judge
// and the call that wrote it (zero if none was kept), and the Spans it holds.
type GatheredStory struct {
	Gathering
	Day   string
	At    time.Time
	Judge Judge
	Call  int64
	Spans []OtherSpan
}

// CitedSpan is a Span a Briefing cites, as it was written, with the kept
// calls behind it: ReadingCall, the call that made the reading it was shown
// as (zero if its Item was only cut, or none was kept), and RelatingCalls,
// the relating calls on its Item made before the Briefing.
type CitedSpan struct {
	Span          OtherSpan
	ReadingCall   int64
	RelatingCalls []int64
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
	// SpansToTell are the Items first cut after after and up to upTo, each
	// with its Spans not Rejected and their relations.
	SpansToTell(ctx context.Context, after, upTo time.Time) ([]ItemToTell, error)
	// KeepBriefing keeps a Briefing, all at once.
	KeepBriefing(ctx context.Context, b Briefing) error
	// SaveBriefingFailure keeps a failure to write a day's Briefing.
	SaveBriefingFailure(ctx context.Context, f BriefingFailure) error
	CallKeeper
}

// firstWindow is how far back the first Briefing ever written looks, with no
// earlier Briefing to start from.
const firstWindow = 24 * time.Hour

// Brief writes the Briefing for the day that holds now, in now's location,
// at now, over what the Record holds then: the Spans of the Items cut since
// the last Briefing was written, so each Span is weighed for exactly one
// Briefing, whatever the Collection runs between, and whether or not its
// Item has been read or related yet; the first Briefing covers the day
// before it. A Briefing that fails is not written: the failure is logged
// and kept, to be shown, and writing is tried again later. Only a failing
// Record is an error.
func (e *Editor) Brief(ctx context.Context, now time.Time) error {
	after, err := e.Briefings.LastBriefingWritten(ctx)
	if err != nil {
		return err
	}
	if after.IsZero() {
		after = now.Add(-firstWindow)
	}
	items, err := e.Briefings.SpansToTell(ctx, after, now)
	if err != nil {
		return err
	}
	b, call, err := writeBriefing(ctx, e.BriefingModel, modelOf(e.ModelFor, briefingAct), now, items)
	if err != nil {
		day := now.Format(time.DateOnly)
		e.Log.ErrorContext(ctx, "writing the Briefing failed", "day", day, "error", err)
		if err := e.Briefings.KeepCall(ctx, *call); err != nil {
			return err
		}
		return e.Briefings.SaveBriefingFailure(ctx, BriefingFailure{Day: day, At: now, Reason: err.Error()})
	}
	b.WrittenAt = now
	if err := e.Briefings.KeepBriefing(ctx, b); err != nil {
		return err
	}
	// A Briefing written empty made no call, and keeps none.
	if call != nil {
		if err := e.Briefings.KeepCall(ctx, *call); err != nil {
			return err
		}
	}
	e.Log.InfoContext(ctx, "Briefing written", "day", b.Day, "statements", len(b.Statements), "gathered", len(b.Gathered),
		"model", b.Judge.Model, "input_tokens", b.Usage.InputTokens, "output_tokens", b.Usage.OutputTokens)
	return nil
}

// briefingAct is the act's name, under which its calls are kept, keyed by
// the day.
const briefingAct = "briefing"

// briefingInstructions are the act of writing the Briefing's own
// instructions.
const briefingInstructions = `You are the Editor of a private slow-news briefing, writing today's Briefing for its one reader, the Owner.

You are shown the Spans read since the last Briefing, under the Items they are from, numbered S1, S2 and so on. A Span is a stretch of an Item's text as its publisher wrote it; a stretch that refers to something in other words is shown as what it refers to, and a Span that is someone's words is shown with who speaks it. Under a Span are its relations to Spans of other Items: one it follows (it moves on what that one said), one it is followed by, one it is the same as (the two say the same thing; corroborated where the other is from another Source), and one it contradicts. Spans of earlier Items that today's are related to come after them, numbered E1, E2 and so on. An Item not yet read is shown as written, with nothing judged of it.

Judge which of today's Spans are worth the Owner's reading, and tell those. Tell what was done, decided, announced, found or argued, and what moves on something told before; leave out what adds nothing to it, such as greetings, background the Owner would know, contact details and repetition. Spans the same as one another are told once, in one statement citing them all, naming each speaker where different people say it.

statements: what you tell, most consequential first, in short statements of plain, neutral prose, as a well-informed colleague would. For each statement:
- spans: the refs of the Spans whose words it tells, every one, and only those. A Span a told Span is related to is not cited for that; cite it only if the statement tells what it says too.
- speakers: who speaks the Spans in spans, each exactly as shown after "spoken by", once; none if no Span in spans is shown with a speaker.
- stories: one for each of today's Spans (S) in spans: its Story, the Spans its relations reach that move on what this Span says, or that it moves on, by their refs. Leave out a related Span that is about something else than what this Span says. A Span with no relations has an empty Story.
- text: the statement, naming each of its speakers as listed. Say nothing its Spans do not say: no background, no speculation, no judgement of your own.
- A Span shown as spoken by someone is told as theirs, naming them by the words shown after "spoken by": a Span "(spoken by Andy Burnham)" is told as "Andy Burnham said that...", even where the Item calls him the Prime Minister. What a speaker claims or how they describe something ("a genuine offer", "fantastic news") stays theirs ("what he called a genuine offer"), never in your own voice. A statement telling Spans of several speakers names each.
- What something will do, cause or be worth (an estimate, a forecast, a promise of jobs or money) is told as the projection it is ("is expected to", "the government says it will"), never as having happened. An arrangement is told as arranged, and an intention as intended.
- Tell each Span as at its Item's date, not as of today.`

// briefingSchema is the act's output schema, over the refs it was shown. Its
// refs are enumerated, so citations are structural: the checks trace each
// statement by its refs, not by reading its prose. A statement's fields are
// in name order (spans, speakers, stories, text; span, story), so the model
// cites what a statement tells, notes who speaks it, gathers the Story of
// each Span it tells beside it, and only then writes.
func briefingSchema(today, shown []string) map[string]any {
	enum := func(values []string) map[string]any { return map[string]any{"type": "string", "enum": values} }
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"statements": array(object(map[string]any{
				"spans":    array(enum(shown)),
				"speakers": array(str()),
				"stories": array(object(map[string]any{
					"span":  enum(today),
					"story": array(enum(shown)),
				})),
				"text": str(),
			})),
		},
		"required":             []string{"statements"},
		"additionalProperties": false,
	}
}

type briefingOutput struct {
	Statements *[]struct {
		Spans []string `json:"spans"`
		// Speakers is the model's own note of who speaks what it tells,
		// written before the text, to name them by; not kept.
		Speakers []string `json:"speakers"`
		Stories  []struct {
			Span  string   `json:"span"`
			Story []string `json:"story"`
		} `json:"stories"`
		Text string `json:"text"`
	} `json:"statements"`
}

// briefingShown is how the Spans to tell are shown: today's numbered S1, S2
// in order, then the Spans of other Items their relations reach, E1, E2,
// grouped by Item.
type briefingShown struct {
	today   []SpanToTell
	earlier []OtherSpan
}

func shownToTell(items []ItemToTell) briefingShown {
	var s briefingShown
	for _, it := range items {
		s.today = append(s.today, it.Spans...)
	}
	var order []string
	for _, sp := range s.today {
		for _, rel := range sp.Relations {
			o := rel.Other
			if s.todayIndex(o.At) >= 0 || slices.ContainsFunc(s.earlier, func(e OtherSpan) bool { return e.At == o.At }) {
				continue
			}
			s.earlier = append(s.earlier, o)
			if k := itemKey(o.Item); !slices.Contains(order, k) {
				order = append(order, k)
			}
		}
	}
	slices.SortStableFunc(s.earlier, func(a, b OtherSpan) int {
		return cmp.Or(cmp.Compare(slices.Index(order, itemKey(a.Item)), slices.Index(order, itemKey(b.Item))),
			cmp.Compare(a.At.Span.Start, b.At.Span.Start))
	})
	return s
}

func (s briefingShown) todayIndex(a Address) int {
	return slices.IndexFunc(s.today, func(sp SpanToTell) bool { return sp.At == a })
}

// ref is the ref the Span at a is shown as, or "".
func (s briefingShown) ref(a Address) string {
	if i := s.todayIndex(a); i >= 0 {
		return "S" + strconv.Itoa(i+1)
	}
	if i := slices.IndexFunc(s.earlier, func(e OtherSpan) bool { return e.At == a }); i >= 0 {
		return "E" + strconv.Itoa(i+1)
	}
	return ""
}

// span is the Span shown as ref, reporting whether one is.
func (s briefingShown) span(ref string) (OtherSpan, bool) {
	n, err := strconv.Atoi(ref[min(1, len(ref)):])
	switch {
	case err != nil || n < 1:
	case strings.HasPrefix(ref, "S") && n <= len(s.today):
		return s.today[n-1].OtherSpan, true
	case strings.HasPrefix(ref, "E") && n <= len(s.earlier):
		return s.earlier[n-1], true
	}
	return OtherSpan{}, false
}

// refs are today's refs, and every ref shown.
func (s briefingShown) refs() (today, shown []string) {
	for i := range s.today {
		today = append(today, "S"+strconv.Itoa(i+1))
	}
	shown = slices.Clone(today)
	for i := range s.earlier {
		shown = append(shown, "E"+strconv.Itoa(i+1))
	}
	return today, shown
}

// WriteBriefing is the act of writing the Briefing for the day that holds
// day, in day's location, on the model m, asked for as the model named
// asked, from the Items with Spans to tell. With none, the Briefing is
// written empty, and no model is called. A result that fails the act's
// checks, such as a statement that cites no Span, is ErrFailedChecks, and
// none of it is returned.
func WriteBriefing(ctx context.Context, m Model, asked string, day time.Time, items []ItemToTell) (Briefing, error) {
	b, _, err := writeBriefing(ctx, m, asked, day, items)
	return b, err
}

// writeBriefing is WriteBriefing with its call, keyed by the day and ended
// as it came out. A Briefing written empty has no call; any other has one,
// whether it was written or failed.
func writeBriefing(ctx context.Context, m Model, asked string, day time.Time, items []ItemToTell) (Briefing, *Call, error) {
	b := Briefing{Day: day.Format(time.DateOnly), Judge: Judge{Act: briefingAct, Version: BriefingVersion}}
	s := shownToTell(items)
	if len(s.today) == 0 {
		return b, nil, nil
	}
	today, shown := s.refs()
	req := Request{
		Act:          briefingAct,
		Instructions: briefingInstructions,
		Input:        briefingInput(day, items, s),
		Schema:       briefingSchema(today, shown),
	}
	call, err := callModel(ctx, m, asked, b.Day, "writing the Briefing for "+b.Day, req, nil, day,
		func(res Response) (err error) {
			b.Usage = res.Usage
			b.Judge.Model, b.Judge.RequestHash = res.Model, RequestHash(req, asked)
			b, err = checkBriefing(b, s, res)
			return err
		})
	if err != nil {
		return Briefing{}, &call, err
	}
	return b, &call, nil
}

// checkBriefing decodes the model's Briefing and runs the act's checks:
// everything traces, so every statement cites at least one Span it was
// shown; each Story is gathered beside a statement, from a Span of today's
// it tells, of Spans that Span's relations reach; and every Span of today's
// told that has relations is gathered. A Span gathered beside two
// statements keeps the first Story. A result that fails them is refused
// whole.
func checkBriefing(b Briefing, s briefingShown, res Response) (Briefing, error) {
	refuse := func(format string, args ...any) (Briefing, error) {
		return Briefing{}, fmt.Errorf("writing the Briefing for %s: %w: %s", b.Day, ErrFailedChecks, fmt.Sprintf(format, args...))
	}
	var out briefingOutput
	dec := json.NewDecoder(bytes.NewReader(res.Output))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return refuse("output does not match the schema: %v", err)
	}
	if out.Statements == nil {
		return refuse("output does not match the schema: no statements")
	}
	told, gathered := map[string]bool{}, map[string]bool{}
	for _, st := range *out.Statements {
		text := strings.TrimSpace(st.Text)
		if text == "" {
			return refuse("a statement with no text")
		}
		if len(st.Spans) == 0 {
			return refuse("an untraced statement, citing no Span: %q", text)
		}
		stmt := Statement{Text: text}
		for _, ref := range st.Spans {
			sp, ok := s.span(ref)
			if !ok {
				return refuse("a statement cites %s, which it was not shown: %q", ref, text)
			}
			if !slices.ContainsFunc(stmt.Cites, func(c OtherSpan) bool { return c.At == sp.At }) {
				stmt.Cites = append(stmt.Cites, sp)
			}
			if strings.HasPrefix(ref, "S") {
				told[ref] = true
			}
		}
		for _, g := range st.Stories {
			sp, ok := s.span(g.Span)
			switch {
			case !ok || !strings.HasPrefix(g.Span, "S"):
				return refuse("a Story gathered from %s, which is not one of today's Spans", g.Span)
			case !slices.Contains(st.Spans, g.Span):
				return refuse("%s's Story is gathered beside a statement that does not tell it: %q", g.Span, text)
			case gathered[g.Span]:
				continue
			}
			gathered[g.Span] = true
			reach := s.today[s.todayIndex(sp.At)].Relations
			gt := Gathering{From: sp.At}
			for _, ref := range g.Story {
				o, ok := s.span(ref)
				if !ok || !slices.ContainsFunc(reach, func(r SpanRelation) bool { return r.Other.At == o.At }) {
					return refuse("%s's Story holds %s, which none of its relations reaches", g.Span, ref)
				}
				if !slices.Contains(gt.Story, o.At) {
					gt.Story = append(gt.Story, o.At)
				}
			}
			b.Gathered = append(b.Gathered, gt)
		}
		b.Statements = append(b.Statements, stmt)
	}
	for i, sp := range s.today {
		ref := "S" + strconv.Itoa(i+1)
		switch {
		case !told[ref] || gathered[ref]:
		case len(sp.Relations) > 0:
			return refuse("%s is told, but its Story is not gathered", ref)
		default:
			// Nothing reaches a Span with no relations: its Story is empty,
			// with no judgement to ask for.
			b.Gathered = append(b.Gathered, Gathering{From: sp.At})
		}
	}
	return b, nil
}

// howRelatedFrom is how rel relates the Span at a, worded from its side.
func howRelatedFrom(rel Relation, a Address) string {
	switch {
	case rel.Kind == Follows && rel.From == a:
		return "follows"
	case rel.Kind == Follows:
		return "followed by"
	case rel.Kind == SameAs:
		return "same as"
	}
	return "contradicts"
}

// briefingInput is the Spans to tell as the model is shown them.
func briefingInput(day time.Time, items []ItemToTell, s briefingShown) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The Briefing for %s.\n", day.Format("Monday 2 January 2006"))
	dated := func(it Item, note string) {
		fmt.Fprintf(&b, "\nItem: %s%s\n", it.Title, note)
		if !it.Published.IsZero() {
			fmt.Fprintf(&b, "Published: %s\n", it.Published.UTC().Format(time.DateOnly))
		}
	}
	for _, it := range items {
		note := ""
		if !it.Read {
			note = " (not yet read)"
		}
		dated(it.Item, note)
		for _, sp := range it.Spans {
			fmt.Fprintf(&b, "%s%s: %s\n", s.ref(sp.At), spokenBy(sp.Speaker), sp.Words)
			var seen []string
			for _, rel := range sp.Relations {
				line := howRelatedFrom(rel.Relation, sp.At) + " " + s.ref(rel.Other.At)
				if rel.Relation.Corroborates() {
					line += ", corroborated"
				}
				if !slices.Contains(seen, line) {
					seen = append(seen, line)
					fmt.Fprintf(&b, "  %s\n", line)
				}
			}
		}
	}
	if len(s.earlier) > 0 {
		b.WriteString("\nEarlier Spans today's are related to:\n")
		for i, o := range s.earlier {
			if i == 0 || itemKey(o.Item) != itemKey(s.earlier[i-1].Item) {
				dated(o.Item, "")
			}
			fmt.Fprintf(&b, "%s%s: %s\n", s.ref(o.At), spokenBy(o.Speaker), o.Words)
		}
	}
	return b.String()
}
