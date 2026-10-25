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
	Usage      Usage
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
)

// Citation is one thing a statement rests on: a Fact, a Fact as a
// Development in its Story, or an Assessment.
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
	// Story is one Story with its whole Timeline and its cause
	// Connections, reporting whether it is in the Record.
	Story(ctx context.Context, id int64) (Story, bool, error)
	// ConnectedStories are the Stories connected to the Story id, each with
	// the shared Facts that connect them.
	ConnectedStories(ctx context.Context, id int64) ([]StoryConnection, error)
	// FactsAbout are at most limit Facts involving the named Entity, as at
	// a date before before (any date, if it is zero), the latest first, each
	// with the Stories it is a Development in.
	FactsAbout(ctx context.Context, entity string, before time.Time, limit int) ([]RecordFact, error)
	// Assessments are at most limit Assessments, current and superseded,
	// the latest first, each with the Facts it cites.
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
	run := &sundayRun{model: e.SundayReadModel, store: e.SundayReads, now: now, seen: map[string]Citation{}}
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
	model Conversation
	store SundayStore
	now   time.Time
	seen  map[string]Citation
}

// sundayReadInstructions are the Sunday read's own instructions.
const sundayReadInstructions = `You are the Editor of a private slow-news briefing, writing this week's Sunday read for its one reader, the Owner.`

// write runs the agent until it submits a read that passes the checks.
func (r *sundayRun) write(ctx context.Context, after time.Time, stories []ToBrief) (SundayRead, error) {
	read := SundayRead{Day: r.now.Format(time.DateOnly), WrittenAt: r.now}
	req := ConversationRequest{
		Act:          "sunday_read",
		Instructions: sundayReadInstructions,
		Tools:        r.tools(),
		Messages:     []Message{{User: r.weekInput(after, stories)}},
	}
	for {
		t, err := r.model.Turn(ctx, req)
		if err != nil {
			return SundayRead{}, fmt.Errorf("writing the Sunday read for %s: %w", read.Day, err)
		}
		read.Turns++
		read.Model = t.Model
		read.Usage.InputTokens += t.Usage.InputTokens
		read.Usage.OutputTokens += t.Usage.OutputTokens
		req.Messages = append(req.Messages, Message{Assistant: &t})
		if len(t.Calls) == 0 {
			return SundayRead{}, fmt.Errorf("writing the Sunday read for %s: the agent stopped without submitting a read", read.Day)
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
