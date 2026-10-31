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
)

// SundayReadVersion is the version of the Sunday read's instructions, tools
// and checks. A new version is a new Judge (system design: Every judgement
// carries its judge). Version 1 read Stories of Facts; 2 reads what the
// week's Briefings told, as Spans.
const SundayReadVersion = 2

// SundayRead is the Sunday read written for a Sunday: statements that put
// what the week's Briefings told into wider context, each citing what it
// rests on, the Assessments it reached and the Stories it gathered. It is
// one Judgement, with its Judge, and each statement, Assessment and
// gathering is a Judgement of its own within it.
type SundayRead struct {
	// ID is the read's own in the Record, once it is kept.
	ID int64
	// Day is the Sunday it is for, YYYY-MM-DD, in the Owner's time zone.
	Day       string
	WrittenAt time.Time
	// Judge is what wrote it; its Model is empty for a read written empty,
	// with no call.
	Judge       Judge
	Statements  []ReadStatement
	Assessments []Assessment
	// Gathered are the Stories of the Spans its statements tie together.
	Gathered []Gathering
	Usage    Usage
	// Turns is how many turns the agent took to write it.
	Turns int
	// Conversation is the agent's conversation that wrote it, turn by turn,
	// to be kept with it; nil for a read written with no model call. Once
	// kept, ConversationID is its id in the Record (zero for none).
	Conversation   *KeptConversation
	ConversationID int64
}

// ReadStatement is one statement in the Sunday read, with what it cites.
type ReadStatement struct {
	// ID is the statement's own, stable once it is kept.
	ID    int64
	Text  string
	Cites []Citation
	// TakenOutBy are the ids of the Feedback standing that takes it out of
	// its read, as the Record reads it: wrong on an Assessment it cites.
	TakenOutBy []int64 `json:"-"`
}

// CitationKind is what a statement in the Sunday read cites.
type CitationKind string

// The kinds of thing a statement in the Sunday read may cite.
const (
	CitesSpan       CitationKind = "span"
	CitesAssessment CitationKind = "assessment"
	CitesLookup     CitationKind = "lookup"
)

// Citation is one thing a statement rests on: a Span, an Assessment, or a
// lookup of a Reference source.
type Citation struct {
	Kind CitationKind
	// Span is the Span cited, as it was shown.
	Span OtherSpan
	// Assessment is the Assessment cited. One the same read made has no ID
	// until it is kept: NewAssessment is then its place in the read's
	// Assessments, counting from 1.
	Assessment    Assessment
	NewAssessment int
	// Lookup is a lookup of a Reference source the read's agent made: the
	// item and the revision of it seen. It has no ID until it is kept.
	Lookup Lookup
}

// Assessment is a judgement the Editor reached in a Sunday read by reasoning
// over Spans, citing the Spans and earlier Assessments it rests on, as at a
// date. It is never rewritten: a later Assessment supersedes it.
type Assessment struct {
	// ID is the Assessment's own, stable once it is kept.
	ID        int64
	Statement string
	// AsAt is the date it holds as at: the Sunday it was made.
	AsAt time.Time
	// Spans are the Spans it rests on, and Earlier the earlier Assessments
	// (each without what it rests on).
	Spans   []OtherSpan
	Earlier []Assessment
	// Supersedes is the earlier Assessment this one supersedes, and
	// SupersededBy the later one that supersedes this; zero for none.
	Supersedes   int64
	SupersededBy int64
	// Day is the Sunday of the read that made it.
	Day string
	// Withdrawn is set while wrong stands on it. No later read is shown a
	// withdrawn Assessment; the Record keeps it.
	Withdrawn bool
}

// SundayReadFailure is a Sunday read that failed to be written.
type SundayReadFailure struct {
	Day    string
	At     time.Time
	Reason string
	// Conversation is the agent's conversation in the attempt that failed,
	// to be kept with it; nil when it made no model call. Once kept,
	// ConversationID is its id in the Record (zero for none).
	Conversation   *KeptConversation
	ConversationID int64
}

// SundayStore is the part of the Record the Sunday read is written from and
// kept in.
type SundayStore interface {
	// LastSundayReadWritten is when the latest Sunday read was written, or
	// zero if none has been.
	LastSundayReadWritten(ctx context.Context) (time.Time, error)
	// BriefingsWritten are the Briefings over Spans written after after and
	// up to upTo, in the order written, each with its statements and the
	// Spans they cite, and what Feedback took out marked.
	BriefingsWritten(ctx context.Context, after, upTo time.Time) ([]Briefing, error)
	// LatestGathering is the latest Story gathered from the Span at a,
	// reporting whether one was.
	LatestGathering(ctx context.Context, a Address) (GatheredStory, bool, error)
	// RelationsOf are the relations of the Span at a, either way.
	RelationsOf(ctx context.Context, a Address) ([]SpanRelation, error)
	// SpansMentioning are the Spans not Rejected of the Items first read
	// before readBefore that mention one of es.
	SpansMentioning(ctx context.Context, es []NamedEntity, readBefore int64) ([]OtherSpan, error)
	// Assessments are at most limit Assessments, current and superseded,
	// the latest first, each with what it rests on; none under wrong.
	Assessments(ctx context.Context, limit int) ([]Assessment, error)
	// KeepSundayRead keeps a Sunday read, all at once, with its
	// conversation.
	KeepSundayRead(ctx context.Context, r SundayRead) error
	// SaveSundayReadFailure keeps a failure to write a Sunday's read, with
	// the conversation of the attempt that failed.
	SaveSundayReadFailure(ctx context.Context, f SundayReadFailure) error
}

// week is how far back the first Sunday read looks, with no earlier read
// to start from.
const week = 7 * 24 * time.Hour

// SundayRead writes the Sunday read for the day that holds now, at now. It
// covers what the Briefings written since the last read was written told
// (the first, the week before it). A read that fails is not written: the
// failure is logged and kept. Only a failing Record is an error.
func (e *Editor) SundayRead(ctx context.Context, now time.Time) error {
	after, err := e.SundayReads.LastSundayReadWritten(ctx)
	if err != nil {
		return err
	}
	if after.IsZero() {
		after = now.Add(-week)
	}
	told, err := e.SundayReads.BriefingsWritten(ctx, after, now)
	if err != nil {
		return err
	}
	day := now.Format(time.DateOnly)
	run := &sundayRun{
		model: e.SundayReadModel, store: e.SundayReads, wikidata: e.Wikidata, now: now, seen: map[string]Citation{},
		kept: KeptConversation{Act: sundayReadAct, Key: day, Model: modelOf(e.ModelFor, sundayReadAct)},
	}
	r, err := run.write(ctx, after, told)
	if err != nil {
		e.Log.ErrorContext(ctx, "writing the Sunday read failed", "day", day, "error", err)
		return e.SundayReads.SaveSundayReadFailure(ctx, SundayReadFailure{
			Day: day, At: now, Reason: err.Error(), Conversation: run.conversation(Failed, err),
		})
	}
	r.Conversation = run.conversation(Applied, nil)
	if err := e.SundayReads.KeepSundayRead(ctx, r); err != nil {
		return err
	}
	e.Log.InfoContext(ctx, "Sunday read written", "day", r.Day, "statements", len(r.Statements), "turns", r.Turns,
		"model", r.Judge.Model, "input_tokens", r.Usage.InputTokens, "output_tokens", r.Usage.OutputTokens)
	return nil
}

// sundayRun is one run of the agent writing the Sunday read: the
// conversation, and everything it has been shown, by ref, which is all a
// statement may cite.
type sundayRun struct {
	model    Conversation
	store    SundayStore
	wikidata Wikidata
	now      time.Time
	seen     map[string]Citation
	// spans are the refs of the Spans shown, P1, P2 and so on, by address.
	spans map[Address]string
	// entities are the Entities the Spans shown mention, by name, which the
	// spans_naming tool takes.
	entities map[string]NamedEntity
	// lookups is how many lookups the run has made, which numbers them.
	lookups int
	// kept is the run's conversation as it is kept, turn by turn.
	kept KeptConversation
}

// sundayReadAct is the Sunday read's name as an act.
const sundayReadAct = "sunday_read"

// conversation is the run's conversation as kept, ending with outcome (and
// why, for a failure); nil if the run made no model call.
func (r *sundayRun) conversation(outcome Outcome, why error) *KeptConversation {
	if len(r.kept.Turns) == 0 {
		return nil
	}
	c := r.kept
	c.At, c.Outcome = r.now, outcome
	if why != nil {
		c.Why = why.Error()
	}
	return &c
}

// sending is the next turn of the run's conversation as it is kept, for its
// request req as sent, of which the messages from sentFrom on are new with
// it.
func (r *sundayRun) sending(req ConversationRequest, sentFrom int) KeptTurn {
	r.kept.Instructions, r.kept.Tools = req.Instructions, req.Tools
	return KeptTurn{
		N:           len(r.kept.Turns) + 1,
		Sent:        slices.Clone(req.Messages[sentFrom:]),
		RequestHash: ConversationRequestHash(req, r.kept.Model),
	}
}

// keep keeps a turn of the run's conversation, with t what came back, if
// anything did, and what came of it.
func (r *sundayRun) keep(kt KeptTurn, t Turn, outcome Outcome, why string) {
	kt.At, kt.Outcome, kt.Why = r.now, outcome, why
	if t.Raw != nil || t.Model != "" || len(t.Calls) > 0 || t.Text != "" {
		kt.Response = &t
	}
	r.kept.Turns = append(r.kept.Turns, kt)
}

// sundayReadInstructions are the Sunday read's own instructions.
const sundayReadInstructions = `You are the Editor of a private slow-news briefing, writing this week's Sunday read for its one reader, the Owner, who follows the news closely and reads the daily Briefing.

The daily Briefing tells what moved on each day. The Sunday read does something different: it puts what the week's Briefings told into wider context. Look for what it means together and over time: what led to it, how its stories bear on each other, what pattern or change they show, what earlier judgements they support or undercut. Aim to show the Owner something they would not have seen from the daily Briefings alone.

The Record is everything collected and the editorial work done on it:
- A Span (P12) is a stretch of a collected Item's text, as its publisher wrote it: a stretch that refers to something in other words is shown as what it refers to, and a Span that is someone's words is shown with who speaks them. Each is shown with the Entities it names.
- Spans of different Items are related: one follows another (it moves on what that one said), is the same as another (the two say the same thing), or contradicts another.
- A Span's Story is the Spans it moves on from and those that move it on, as a Briefing gathered them, and its relations judged since.
- An Assessment (A4) is a judgement an earlier Sunday read reached by reasoning over Spans, as at its date.
Wikidata is a Reference source of background facts: who holds an office and since when, what something is.

You are given what the week's Briefings told: their statements, each with the Spans it cites. Use the tools to look into what the argument needs: a Span's Story, the Spans naming an Entity back through time, the earlier Assessments, Wikidata. Start from the earlier Assessments rather than from scratch. Investigate the few threads that matter most rather than everything; the read is at most a few hundred words.

Then submit the read with submit_read:
- assessments: what this read works out, as Assessments, refs N1, N2 and so on, each a judgement in one sentence, citing the Spans it rests on (P refs, at least one) and any earlier Assessments it builds on (A refs). Where one revises a current earlier Assessment, it supersedes it: give that Assessment's ref, or an empty string. An Assessment is never rewritten; a later one supersedes it.
- statements: the read itself, in order, each a sentence or two of plain prose. Every statement cites, by ref, what it rests on: Spans (P12), Assessments (A4, or one this read makes, N1), or Wikidata lookups (L1). Cite only refs you have been shown in this conversation. Say nothing the cited things do not support; where the statement is your own reasoning, keep it as an Assessment and cite that. A Span that is someone's words is told as theirs, never in your own voice, and as at its Item's date.
- stories: beside a statement that ties Spans of different stories together, the Story of each Span it cites whose Story it tells: the Spans its relations reach that move on what that Span says, or that it moves on, by their P refs. Leave it empty where the statement tells no Story.

The read is checked before it is published; if it fails, you are told why, and can mend it and submit again.`

// The bounds on the agent's loop. A read that passes the checks within them
// is published; one that does not fails, and is kept as a failure. The
// read is weekly and its questions open, so they are generous: an
// investigation of a dozen Stories, with a few lookups each and a mended
// submission, takes well under half of them, while a loop that never
// converges is stopped before it costs more than a few pounds.
const (
	// SundayReadMaxTurns is the most turns (model calls) a read may take.
	SundayReadMaxTurns = 40
	// SundayReadTokenBudget is the most tokens a read may spend over all its
	// turns, input (cached or not) and output together, checked before
	// each turn.
	SundayReadTokenBudget = 4_000_000
)

// askToSubmit is what the agent is told when it ends its turn without
// submitting a read.
const askToSubmit = "Submit the read with submit_read now; it is not published otherwise."

// write runs the agent until it submits a read that passes the checks.
func (r *sundayRun) write(ctx context.Context, after time.Time, told []Briefing) (SundayRead, error) {
	read := SundayRead{Day: r.now.Format(time.DateOnly), WrittenAt: r.now, Judge: Judge{Act: sundayReadAct, Version: SundayReadVersion}}
	input := r.weekInput(after, told)
	if input == "" {
		// Nothing was told: the read is written empty, and says so, with no
		// model call.
		return read, nil
	}
	req := ConversationRequest{
		Act:          sundayReadAct,
		Instructions: sundayReadInstructions,
		Tools:        r.tools(),
		Messages:     []Message{{User: input}},
	}
	asked := false
	// sentFrom is where the messages not yet sent start.
	sentFrom := 0
	for {
		if read.Turns >= SundayReadMaxTurns {
			return SundayRead{}, fmt.Errorf("writing the Sunday read for %s: stopped at the bound of %d turns with no read that passes the checks",
				read.Day, SundayReadMaxTurns)
		}
		if spent := read.Usage.total(); spent > SundayReadTokenBudget {
			return SundayRead{}, fmt.Errorf("writing the Sunday read for %s: stopped at the bound of %d tokens (spent %d) with no read that passes the checks",
				read.Day, SundayReadTokenBudget, spent)
		}
		kt := r.sending(req, sentFrom)
		t, err := r.model.Turn(ctx, req)
		if err != nil {
			// What came back with a refusal or a cut-off turn is kept too.
			r.keep(kt, t, Failed, err.Error())
			return SundayRead{}, fmt.Errorf("writing the Sunday read for %s: %w", read.Day, err)
		}
		read.Turns++
		read.Judge.Model = t.Model
		read.Usage = read.Usage.add(t.Usage)
		req.Messages = append(req.Messages, Message{Assistant: &t})
		sentFrom = len(req.Messages)
		if len(t.Calls) == 0 {
			// The model can't be made to call a tool, only asked: it is
			// asked once to submit, then the read fails.
			if asked {
				err := fmt.Errorf("writing the Sunday read for %s: the agent stopped without submitting a read", read.Day)
				r.keep(kt, t, Failed, err.Error())
				return SundayRead{}, err
			}
			r.keep(kt, t, Continued, "")
			asked = true
			req.Messages = append(req.Messages, Message{User: askToSubmit})
			continue
		}
		var results []ToolResult
		var refused []string
		for _, c := range t.Calls {
			if c.Name == submitRead {
				checked, err := r.check(ctx, c.Input)
				if err == nil {
					r.keep(kt, t, Applied, "")
					read.Statements, read.Assessments, read.Gathered = checked.Statements, checked.Assessments, checked.Gathered
					return read, nil
				}
				if !errors.Is(err, ErrFailedChecks) {
					r.keep(kt, t, Failed, err.Error())
					return SundayRead{}, err
				}
				refused = append(refused, err.Error())
				results = append(results, ToolResult{CallID: c.ID, Content: err.Error(), IsError: true})
				continue
			}
			out, err := r.call(ctx, c)
			if errors.Is(err, errTool) {
				results = append(results, ToolResult{CallID: c.ID, Content: err.Error(), IsError: true})
				continue
			}
			if err != nil {
				r.keep(kt, t, Failed, err.Error())
				return SundayRead{}, err
			}
			results = append(results, ToolResult{CallID: c.ID, Content: out})
		}
		if refused != nil {
			r.keep(kt, t, Refused, strings.Join(refused, "\n"))
		} else {
			r.keep(kt, t, Continued, "")
		}
		req.Messages = append(req.Messages, Message{Results: results})
	}
}

type submittedRead struct {
	Assessments []struct {
		Assessments []string `json:"assessments"`
		Ref         string   `json:"ref"`
		Spans       []string `json:"spans"`
		Statement   string   `json:"statement"`
		Supersedes  string   `json:"supersedes"`
	} `json:"assessments"`
	Statements []struct {
		Cites   []string `json:"cites"`
		Stories []struct {
			Span  string   `json:"span"`
			Story []string `json:"story"`
		} `json:"stories"`
		Text string `json:"text"`
	} `json:"statements"`
}

// checkedRead is what a submitted read makes once it passes the checks.
type checkedRead struct {
	Statements  []ReadStatement
	Assessments []Assessment
	Gathered    []Gathering
}

func failedRead(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrFailedChecks, fmt.Sprintf(format, args...))
}

// check decodes a submitted read and runs the act's checks. Everything
// traces: every statement cites at least one thing, each a thing the agent
// was shown in this run or an Assessment the read makes; every Assessment
// rests on at least one Span shown, and supersedes only a current
// Assessment shown; a Story is gathered from a Span its statement cites, of
// Spans shown that its relations reach. A refused read is ErrFailedChecks;
// any other error is the Record's.
func (r *sundayRun) check(ctx context.Context, input json.RawMessage) (checkedRead, error) {
	var in submittedRead
	var out checkedRead
	dec := json.NewDecoder(bytes.NewReader(input))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return out, failedRead("the read does not match the schema: %v", err)
	}
	made := map[string]Citation{}
	superseded := map[int64]bool{}
	for i, a := range in.Assessments {
		statement := strings.TrimSpace(a.Statement)
		if _, ok := parseRef(a.Ref, "N"); !ok || made[a.Ref].NewAssessment != 0 {
			return out, failedRead("an Assessment's ref must be N1, N2 and so on, each once: %q", a.Ref)
		}
		if statement == "" {
			return out, failedRead("Assessment %s has no statement", a.Ref)
		}
		if len(a.Spans) == 0 {
			return out, failedRead("Assessment %s cites no Span: %q", a.Ref, statement)
		}
		as := Assessment{Statement: statement, AsAt: r.day(), Day: r.now.Format(time.DateOnly)}
		for _, ref := range a.Spans {
			c, ok := r.seen[ref]
			if !ok || c.Kind != CitesSpan {
				return out, failedRead("Assessment %s cites %s, which is not a Span this run has been shown", a.Ref, ref)
			}
			if !slices.ContainsFunc(as.Spans, func(o OtherSpan) bool { return o.At == c.Span.At }) {
				as.Spans = append(as.Spans, c.Span)
			}
		}
		for _, ref := range a.Assessments {
			c, ok := r.seen[ref]
			if !ok || c.Kind != CitesAssessment {
				return out, failedRead("Assessment %s rests on %s, which is not an earlier Assessment this run has been shown", a.Ref, ref)
			}
			if !slices.ContainsFunc(as.Earlier, func(e Assessment) bool { return e.ID == c.Assessment.ID }) {
				as.Earlier = append(as.Earlier, c.Assessment)
			}
		}
		if a.Supersedes != "" {
			c, ok := r.seen[a.Supersedes]
			if !ok || c.Kind != CitesAssessment {
				return out, failedRead("Assessment %s supersedes %s, which is not an Assessment this run has been shown", a.Ref, a.Supersedes)
			}
			if c.Assessment.SupersededBy != 0 || superseded[c.Assessment.ID] {
				return out, failedRead("Assessment %s supersedes %s, which is already superseded", a.Ref, a.Supersedes)
			}
			superseded[c.Assessment.ID] = true
			as.Supersedes = c.Assessment.ID
		}
		out.Assessments = append(out.Assessments, as)
		made[a.Ref] = Citation{Kind: CitesAssessment, Assessment: as, NewAssessment: i + 1}
	}
	if len(in.Statements) == 0 {
		return out, failedRead("the read has no statements")
	}
	gathered := map[Address]bool{}
	for _, s := range in.Statements {
		text := strings.TrimSpace(s.Text)
		if text == "" {
			return out, failedRead("a statement with no text")
		}
		if len(s.Cites) == 0 {
			return out, failedRead("an uncited statement: %q", text)
		}
		st := ReadStatement{Text: text}
		for _, ref := range s.Cites {
			c, ok := r.seen[ref]
			if !ok {
				c, ok = made[ref]
			}
			if !ok {
				return out, failedRead("%q cites %s, which this run has not been shown and the read does not make", text, ref)
			}
			st.Cites = append(st.Cites, c)
		}
		for _, g := range s.Stories {
			c, ok := r.seen[g.Span]
			switch {
			case !ok || c.Kind != CitesSpan:
				return out, failedRead("a Story gathered from %s, which is not a Span this run has been shown", g.Span)
			case !slices.Contains(s.Cites, g.Span):
				return out, failedRead("%s's Story is gathered beside a statement that does not cite it: %q", g.Span, text)
			case gathered[c.Span.At]:
				continue
			}
			rels, err := r.store.RelationsOf(ctx, c.Span.At)
			if err != nil {
				return out, err
			}
			gt := Gathering{From: c.Span.At}
			for _, ref := range g.Story {
				o, ok := r.seen[ref]
				if !ok || o.Kind != CitesSpan || !slices.ContainsFunc(rels, func(rel SpanRelation) bool { return rel.Other.At == o.Span.At }) {
					return out, failedRead("%s's Story holds %s, which none of its relations reaches", g.Span, ref)
				}
				if !slices.Contains(gt.Story, o.Span.At) {
					gt.Story = append(gt.Story, o.Span.At)
				}
			}
			gathered[c.Span.At] = true
			out.Gathered = append(out.Gathered, gt)
		}
		out.Statements = append(out.Statements, st)
	}
	return out, nil
}

// day is the start of the day the read is written on.
func (r *sundayRun) day() time.Time {
	y, m, d := r.now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// weekInput is what the week's Briefings told, as the agent is first shown
// it, each Span cited registered as seen; empty if they told nothing that
// stands.
func (r *sundayRun) weekInput(after time.Time, told []Briefing) string {
	var b strings.Builder
	some := false
	for _, br := range told {
		var standing []Statement
		for _, st := range br.Statements {
			if len(st.TakenOutBy) == 0 {
				standing = append(standing, st)
			}
		}
		if len(standing) == 0 {
			continue
		}
		some = true
		day, err := time.Parse(time.DateOnly, br.Day)
		name := br.Day
		if err == nil {
			name = day.Format("Monday 2 January 2006")
		}
		fmt.Fprintf(&b, "\nThe Briefing for %s told:\n", name)
		for _, st := range standing {
			fmt.Fprintf(&b, "- %s\n", st.Text)
			for _, c := range st.Cites {
				fmt.Fprintf(&b, "  %s\n", r.showSpan(Citation{Kind: CitesSpan, Span: c}))
			}
		}
	}
	if !some {
		return ""
	}
	return fmt.Sprintf("The Sunday read for %s, covering what the Briefings told since %s.\n",
		r.now.Format("Monday 2 January 2006"), after.Format("Monday 2 January 2006")) + b.String()
}
