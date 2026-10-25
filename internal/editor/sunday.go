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

// SundayRead is the Sunday read written for a Sunday: statements that put
// the week's Stories into wider context, each citing what it rests on.
type SundayRead struct {
	// ID is the read's own, stable once it is kept.
	ID int64
	// Day is the Sunday it is for, YYYY-MM-DD, in the Owner's time zone.
	Day        string
	WrittenAt  time.Time
	Statements []ReadStatement
	// Assessments are those the read made, in order.
	Assessments []Assessment
	// Stories are those the read started, each gathering existing
	// Developments onto its Timeline.
	Stories []Story
	Model   string
	Usage   Usage
	// Turns is how many turns the agent took to write it.
	Turns int
}

// ReadStatement is one statement in the Sunday read, with what it cites.
type ReadStatement struct {
	// ID is the statement's own, stable once it is kept.
	ID    int64
	Text  string
	Cites []Citation
}

// CitationKind is what a statement in the Sunday read cites.
type CitationKind string

// The kinds of thing a statement in the Sunday read may cite.
const (
	CitesFact        CitationKind = "fact"
	CitesDevelopment CitationKind = "development"
	CitesAssessment  CitationKind = "assessment"
	CitesLookup      CitationKind = "lookup"
)

// Citation is one thing a statement rests on: a Fact, a Fact as a
// Development in its Story, an Assessment, or a lookup of a Reference
// source.
type Citation struct {
	Kind CitationKind
	// Fact is the Fact cited, traced to its Item, for a Fact or a
	// Development.
	Fact Development
	// Story is the Story a cited Development is in.
	Story Story
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
// over Facts, citing the Facts it rests on, as at a date. It is never
// rewritten: a later Assessment supersedes it.
type Assessment struct {
	// ID is the Assessment's own, stable once it is kept.
	ID        int64
	Statement string
	// AsAt is the date it holds as at: the Sunday it was made.
	AsAt  time.Time
	Facts []Development
	// Supersedes is the earlier Assessment this one supersedes, and
	// SupersededBy the later one that supersedes this; zero for none.
	Supersedes   int64
	SupersededBy int64
	// Day is the Sunday of the read that made it.
	Day string
	// Withdrawn is set once Feedback said it was wrong. The agent never sees
	// a withdrawn Assessment, or one resting on a withdrawn Fact; the Record
	// keeps it, marked.
	Withdrawn bool
}

// RecordFact is a Fact as the Record holds it, traced to its Item, with the
// Stories it is a Development in.
type RecordFact struct {
	Fact    Development
	Stories []Story
}

// SundayReadFailure is a Sunday read that failed to be written.
type SundayReadFailure struct {
	Day    string
	At     time.Time
	Reason string
}

// SundayStore is the part of the Record the Sunday read is written from and
// kept in.
type SundayStore interface {
	// LastSundayReadWritten is when the latest Sunday read was written, or
	// zero if none has been.
	LastSundayReadWritten(ctx context.Context) (time.Time, error)
	// ToBrief are the Stories the Owner's Topics select with Developments
	// placed after after and up to upTo: the week's Stories.
	ToBrief(ctx context.Context, after, upTo time.Time) ([]ToBrief, error)
	// StoryToRead is one Story with its whole Timeline and its cause
	// Connections, as the acts see it, without what Feedback withdrew,
	// reporting whether it is in the Record.
	StoryToRead(ctx context.Context, id int64) (Story, bool, error)
	// ConnectedStories are the Stories connected to the Story id, each with
	// the shared Facts that connect them.
	ConnectedStories(ctx context.Context, id int64) ([]StoryConnection, error)
	// FactsAbout are at most limit Facts involving the named Entity, as at
	// a date before before (any date, if it is zero), the latest first, each
	// with the Stories it is a Development in; none Feedback withdrew.
	FactsAbout(ctx context.Context, entity string, before time.Time, limit int) ([]RecordFact, error)
	// Assessments are at most limit Assessments, current and superseded,
	// the latest first, each with the Facts it cites; none Feedback
	// withdrew, or that rests on a Fact it withdrew.
	Assessments(ctx context.Context, limit int) ([]Assessment, error)
	// SaveSundayRead keeps a Sunday read, all at once.
	SaveSundayRead(ctx context.Context, r SundayRead) error
	// SaveSundayReadFailure keeps a failure to write a Sunday's read.
	SaveSundayReadFailure(ctx context.Context, f SundayReadFailure) error
}

// week is how far back the first Sunday read looks, with no earlier read
// to start from.
const week = 7 * 24 * time.Hour

// SundayRead writes the Sunday read for the day that holds now, at now. It
// covers the Stories the Owner's Topics select that moved on since the last
// read was written (the first, the week before it). A read that fails is not
// written: the failure is logged and kept. Only a failing Record is an
// error.
func (e *Editor) SundayRead(ctx context.Context, now time.Time) error {
	after, err := e.SundayReads.LastSundayReadWritten(ctx)
	if err != nil {
		return err
	}
	if after.IsZero() {
		after = now.Add(-week)
	}
	stories, err := e.SundayReads.ToBrief(ctx, after, now)
	if err != nil {
		return err
	}
	run := &sundayRun{model: e.SundayReadModel, store: e.SundayReads, wikidata: e.Wikidata, now: now, seen: map[string]Citation{}}
	r, err := run.write(ctx, after, stories)
	day := now.Format(time.DateOnly)
	if err != nil {
		e.Log.ErrorContext(ctx, "writing the Sunday read failed", "day", day, "error", err)
		return e.SundayReads.SaveSundayReadFailure(ctx, SundayReadFailure{Day: day, At: now, Reason: err.Error()})
	}
	if err := e.SundayReads.SaveSundayRead(ctx, r); err != nil {
		return err
	}
	e.Log.InfoContext(ctx, "Sunday read written", "day", r.Day, "statements", len(r.Statements), "turns", r.Turns,
		"model", r.Model, "input_tokens", r.Usage.InputTokens, "output_tokens", r.Usage.OutputTokens)
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
	// lookups is how many lookups the run has made, which numbers them.
	lookups int
}

// sundayReadInstructions are the Sunday read's own instructions.
const sundayReadInstructions = `You are the Editor of a private slow-news briefing, writing this week's Sunday read for its one reader, the Owner, who follows the news closely and reads the daily Briefing.

The daily Briefing says what moved on each day. The Sunday read does something different: it puts the week's Stories into wider context. Look for what the week's Developments mean together and over time: what led to them, how they bear on each other and on other Stories, what pattern or change they show, what earlier judgements they support or undercut. Aim to show the Owner something they would not have seen from the daily Briefings alone.

The Record is everything collected and the editorial work done on it:
- A Story is an ongoing subject followed across days (S3). Its Timeline is its Developments in order.
- A Fact (F12) is a proposition drawn from a collected Item and corroborated, holding as at a date. A Development (S3:F12) is a Fact that moved a Story on; one Fact can be a Development in several Stories.
- A cause Connection says one Fact led to another. Stories are connected through the Developments they share and the Entities their Facts involve in the same role.
- An Assessment (A4) is a judgement an earlier Sunday read reached by reasoning over Facts, as at its date.
Wikidata is a Reference source of background facts: who holds an office and since when, what something is.

You are given the week's Stories with their Developments this week. Use the tools to look into what the argument needs: a Story's whole Timeline and its Connections, the Facts about an Entity back through time, the earlier Assessments, Wikidata. Start from the earlier Assessments rather than from scratch. Investigate the few threads that matter most rather than everything; the read is at most a few hundred words.

Then submit the read with submit_read:
- statements: the read itself, in order, each a sentence or two of plain prose. Every statement cites, by ref, what it rests on: Facts (F12), Developments (S3:F12), Assessments (A4, or one this read makes, N1), or Wikidata lookups (L1). Cite only refs you have been shown in this conversation. Say nothing the cited things do not support; where the statement is your own reasoning, keep it as an Assessment and cite that.
- assessments: what this read works out, as Assessments, refs N1, N2 and so on, each a judgement in one sentence citing the Facts it rests on (F refs, or S:F refs for Developments). Where one revises a current earlier Assessment, it supersedes it: give that Assessment's ref, or an empty string. An Assessment is never rewritten; a later one supersedes it.
- stories: any wider Story the week shows, which no Story yet follows, gathering existing Developments (S3:F12 refs) as its Timeline. Usually none.

Never present your own reasoning as a Fact: Facts come only from collected Items. The read is checked before it is published; if it fails, you are told why, and can mend it and submit again.`

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
func (r *sundayRun) write(ctx context.Context, after time.Time, stories []ToBrief) (SundayRead, error) {
	read := SundayRead{Day: r.now.Format(time.DateOnly), WrittenAt: r.now}
	if len(stories) == 0 {
		// Nothing moved on: the read is written empty, and says so, with no
		// model call.
		return read, nil
	}
	req := ConversationRequest{
		Act:          "sunday_read",
		Instructions: sundayReadInstructions,
		Tools:        r.tools(),
		Messages:     []Message{{User: r.weekInput(after, stories)}},
	}
	asked := false
	for {
		if read.Turns >= SundayReadMaxTurns {
			return SundayRead{}, fmt.Errorf("writing the Sunday read for %s: stopped at the bound of %d turns with no read that passes the checks",
				read.Day, SundayReadMaxTurns)
		}
		if spent := read.Usage.total(); spent > SundayReadTokenBudget {
			return SundayRead{}, fmt.Errorf("writing the Sunday read for %s: stopped at the bound of %d tokens (spent %d) with no read that passes the checks",
				read.Day, SundayReadTokenBudget, spent)
		}
		t, err := r.model.Turn(ctx, req)
		if err != nil {
			return SundayRead{}, fmt.Errorf("writing the Sunday read for %s: %w", read.Day, err)
		}
		read.Turns++
		read.Model = t.Model
		read.Usage = read.Usage.add(t.Usage)
		req.Messages = append(req.Messages, Message{Assistant: &t})
		if len(t.Calls) == 0 {
			// The model can't be made to call a tool, only asked: it is
			// asked once to submit, then the read fails.
			if asked {
				return SundayRead{}, fmt.Errorf("writing the Sunday read for %s: the agent stopped without submitting a read", read.Day)
			}
			asked = true
			req.Messages = append(req.Messages, Message{User: askToSubmit})
			continue
		}
		var results []ToolResult
		for _, c := range t.Calls {
			if c.Name == submitRead {
				checked, err := r.check(c.Input)
				if err == nil {
					read.Statements, read.Assessments, read.Stories = checked.Statements, checked.Assessments, checked.Stories
					return read, nil
				}
				results = append(results, ToolResult{CallID: c.ID, Content: err.Error(), IsError: true})
				continue
			}
			out, err := r.call(ctx, c)
			if errors.Is(err, errTool) {
				results = append(results, ToolResult{CallID: c.ID, Content: err.Error(), IsError: true})
				continue
			}
			if err != nil {
				return SundayRead{}, err
			}
			results = append(results, ToolResult{CallID: c.ID, Content: out})
		}
		req.Messages = append(req.Messages, Message{Results: results})
	}
}

type submittedRead struct {
	Statements []struct {
		Text  string   `json:"text"`
		Cites []string `json:"cites"`
	} `json:"statements"`
	Assessments []struct {
		Ref        string   `json:"ref"`
		Statement  string   `json:"statement"`
		Facts      []string `json:"facts"`
		Supersedes string   `json:"supersedes"`
	} `json:"assessments"`
	Stories []struct {
		Title        string   `json:"title"`
		Developments []string `json:"developments"`
	} `json:"stories"`
}

// checkedRead is what a submitted read makes once it passes the checks.
type checkedRead struct {
	Statements  []ReadStatement
	Assessments []Assessment
	Stories     []Story
}

func failedRead(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrFailedChecks, fmt.Sprintf(format, args...))
}

// check decodes a submitted read and runs the act's checks. Everything
// traces: every statement cites at least one thing, and every Assessment at
// least one Fact, each a thing the agent was shown in this run or an
// Assessment the read makes; an Assessment supersedes only a current one it
// was shown.
func (r *sundayRun) check(input json.RawMessage) (checkedRead, error) {
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
		if len(a.Facts) == 0 {
			return out, failedRead("Assessment %s cites no Fact: %q", a.Ref, statement)
		}
		as := Assessment{Statement: statement, AsAt: r.day(), Day: r.now.Format(time.DateOnly)}
		for _, ref := range a.Facts {
			c, ok := r.seen[ref]
			if !ok || (c.Kind != CitesFact && c.Kind != CitesDevelopment) {
				return out, failedRead("Assessment %s cites %s, which is not a Fact this run has been shown", a.Ref, ref)
			}
			if !slices.ContainsFunc(as.Facts, func(f Development) bool { return f.ID == c.Fact.ID }) {
				as.Facts = append(as.Facts, c.Fact)
			}
		}
		if a.Supersedes != "" {
			c, ok := r.seen[a.Supersedes]
			if !ok || c.Kind != CitesAssessment || c.Assessment.ID == 0 {
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
	for _, s := range in.Stories {
		title := strings.TrimSpace(s.Title)
		if title == "" {
			return out, failedRead("a Story started with no title")
		}
		if len(s.Developments) == 0 {
			return out, failedRead("Story %q gathers no Development", title)
		}
		st := Story{Title: title}
		for _, ref := range s.Developments {
			c, ok := r.seen[ref]
			if !ok || c.Kind != CitesDevelopment {
				return out, failedRead("Story %q gathers %s, which is not a Development this run has been shown (give it as S1:F2)", title, ref)
			}
			if !slices.ContainsFunc(st.Timeline, func(d Development) bool { return d.ID == c.Fact.ID }) {
				st.Timeline = append(st.Timeline, c.Fact)
			}
		}
		out.Stories = append(out.Stories, st)
	}
	if len(in.Statements) == 0 {
		return out, failedRead("the read has no statements")
	}
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
		out.Statements = append(out.Statements, st)
	}
	return out, nil
}

// day is the start of the day the read is written on.
func (r *sundayRun) day() time.Time {
	y, m, d := r.now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// weekInput is the week's Stories as the agent is first shown them, each
// Development registered as seen.
func (r *sundayRun) weekInput(after time.Time, stories []ToBrief) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The Sunday read for %s, covering the Stories that moved on since %s.\n",
		r.now.Format("Monday 2 January 2006"), after.Format("Monday 2 January 2006"))
	for _, tb := range stories {
		fmt.Fprintf(&b, "\n%s: %s\n", storyRefOf(tb.Story.ID), tb.Story.Title)
		fmt.Fprintf(&b, "  Followed at %s Depth (Topics: %s)\n", tb.Depth, strings.Join(tb.Topics, "; "))
		for _, d := range tb.Today {
			fmt.Fprintf(&b, "  %s\n", r.showDevelopment(tb.Story, d))
		}
	}
	return b.String()
}
