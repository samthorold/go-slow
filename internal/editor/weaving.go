package editor

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
)

// WeavingVersion is the version of weaving's instructions, schema, tools and
// checks, and of how its input and its asks' answers are shown. A new
// version is a new Judge (system design: Every Judgement carries its Judge).
// Change it with weavingInstructions, weavingSchema, weavingTools,
// checkWeaving, weavingInput or what the asks answer (weaving_asks.go).
const WeavingVersion = 8

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

// Cited is a Span an Editor's Span cites, its words as read, when it was
// published, and either its address in its Item's text with its Item's
// title, or, for an earlier Editor's Span, Of, its address among the
// Editor's Spans.
type Cited struct {
	Address Address
	// Of is the earlier Editor's Span cited, when it is one; zero when the
	// Span cited is a Source's.
	Of    EditorSpanAt
	Words string
	Title string
	// Published is its Item's publication, or for an Editor's Span the
	// Judgement's that wrote it (CONTEXT.md: Timeline).
	Published time.Time
}

// EditorSpanAt is an Editor's Span's address: the Story that wrote it, and
// its place among that Story's Editor's Spans, numbered from 1 in the order
// written. The Story is an event that never changes, so the address always
// holds the same words.
type EditorSpanAt struct {
	Story int64
	N     int
}

// IsZero reports whether a is no Editor's Span's address.
func (a EditorSpanAt) IsZero() bool { return a.Story == 0 }

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
	// Name is its Name, the newest naming of it; its Text is empty until it
	// has one.
	Name StoryName
}

// CitingSpan is an Editor's Span citing a Span: its Story, its number in
// it from 1, its words and when it was written, with its Story's opening
// words and its Story's newest Name, empty until it has one.
type CitingSpan struct {
	Story   int64
	N       int
	Body    string
	At      time.Time
	Opening string
	Name    string
}

// SpanHeld is what holds a Span: the Cuts whose Stories list it, oldest
// first, and the Editor's Spans citing it, in the order written.
type SpanHeld struct {
	Cuts   []int64
	Citing []CitingSpan
}

// Timeline is the Spans the Story's Editor's Spans cite, each once, in the
// order they were published, an earlier Editor's Span by the Judgement that
// wrote it, and those published together in the order their Item or Story
// has them (CONTEXT.md: Timeline). Its own Editor's Spans are not in it:
// they are the argument shown above it, all published last, by the
// Judgement that made it.
func (s Story) Timeline() []Cited {
	var out []Cited
	for _, es := range s.Spans {
		for _, c := range es.Cites {
			if !slices.ContainsFunc(out, func(o Cited) bool { return o.Address == c.Address && o.Of == c.Of }) {
				out = append(out, c)
			}
		}
	}
	slices.SortStableFunc(out, func(a, b Cited) int {
		return cmp.Or(a.Published.Compare(b.Published),
			cmp.Compare(a.Address.SourceID, b.Address.SourceID), cmp.Compare(a.Address.EntryID, b.Address.EntryID),
			cmp.Compare(a.Address.Span.Start, b.Address.Span.Start), cmp.Compare(a.Address.Span.End, b.Address.Span.End),
			cmp.Compare(a.Of.Story, b.Of.Story), cmp.Compare(a.Of.N, b.Of.N))
	})
	return out
}

// Weaving is a weaving run whose result was written: the Stories it made,
// none for a run with nothing substantive to say, with its Judge. It is kept
// as an event, and each Story as an event of its own tied to it.
type Weaving struct {
	ID int64
	// Key is the run's work key, the day it was submitted on.
	Key string
	// ThroughCut is the newest Cut whose Story the run was shown as new: the
	// next run is shown the Stories cut after it.
	ThroughCut int64
	Judge      Judge
	At         time.Time
	Stories    []Story
	// Usage is what the call cost, for the logs; not kept with the run.
	Usage Usage
}

// WeavingTurns is a weaving run's conversation as it stands after a turn
// that asked the Record (system design: Weaving runs at most once a day; a
// run's conversation is held in batches, a turn at a time): what the next
// turn sends, and every Span the run has been shown, by number, so the
// answer's citations are read against what it was shown, wherever it was
// shown it.
type WeavingTurns struct {
	// Key is the run's work key, its day.
	Key string
	// Input is the run's opening as sent, the Stories new to it, and
	// Messages the conversation since, each turn the model took and the
	// answers to its asks.
	Input    string
	Messages []Message
	// Shown is each Span shown, by its number from 1, and Left the numbers
	// that stand for no Span shown (a Rejected Span left out).
	Shown []Cited
	Left  []int
	// New are the Cuts of the Stories new to the run, oldest first.
	New []int64
}

// WeavingStore is the part of the Record weaving reads and writes.
type WeavingStore interface {
	// StoriesToWeave are the Stories cut since the last weaving run whose
	// result was written, of Items collected after the switch, oldest Cut
	// first.
	StoriesToWeave(ctx context.Context) ([]CutStory, error)
	// StoriesCutThrough are the Stories cut through the Cut through, oldest
	// Cut first, as StoriesToWeave reads them: those a run's asks are
	// answered from, beside the Stories the Editor made.
	StoriesCutThrough(ctx context.Context, through int64) ([]CutStory, error)
	// StoriesMade are the Stories the Editor made, newest first, each with
	// its Editor's Spans and what they cite.
	StoriesMade(ctx context.Context) ([]Story, error)
	// NewestHydration is the hydration of the Item a Projection reads, its
	// hydration, reporting whether the Item has one.
	NewestHydration(ctx context.Context, sourceID, entryID string) (Hydration, bool, error)
	// KeptItem is the Item, with its text as kept, reporting whether the
	// Record holds it.
	KeptItem(ctx context.Context, sourceID, entryID string) (KeptItem, bool, error)
	// WeavingRan reports whether the run keyed key has had its result
	// written or been marked failed.
	WeavingRan(ctx context.Context, key string) (bool, error)
	// EntitiesNamed is the Entity each key names, as the Record's look-ups
	// have it now (an Editor's Entity merged into a Wikidata item read as
	// the item); a key not yet looked up is left out.
	EntitiesNamed(ctx context.Context, keys []LookupKey) (map[LookupKey]NamedEntity, error)
	// PendingBatches are the batches not yet ended, among which a weaving
	// run's may be.
	PendingBatches(ctx context.Context) ([]PendingBatch, error)
	// WeavingInProgress is the conversation of the run whose turns are
	// under way (it has asked, and has neither been written nor marked
	// failed), as its last turn kept it, reporting whether there is one.
	WeavingInProgress(ctx context.Context) (WeavingTurns, bool, error)
	// WeavingTurnsOf is the conversation of the run keyed key as its last
	// kept turn left it, under way or marked failed, reporting whether it
	// kept a turn.
	WeavingTurnsOf(ctx context.Context, key string) (WeavingTurns, bool, error)
	// KeepWeavingTurns keeps a run's conversation as it stands after a turn,
	// at at.
	KeepWeavingTurns(ctx context.Context, t WeavingTurns, at time.Time) error
	// KeepWeaving keeps a run and each Story it made, all at once.
	KeepWeaving(ctx context.Context, w Weaving) error
	// WeavingCap is the most the run keyed key may spend over all its turns,
	// in dollars at batch rates, reporting whether it has a cap: one the
	// Owner asked for by the weave command.
	WeavingCap(ctx context.Context, key string) (float64, bool, error)
	// CallsOn are the kept calls an act made on a piece of work, in the
	// order kept: a run's turns so far.
	CallsOn(ctx context.Context, act, key string) ([]Call, error)
	// PastUsage is what the act's applied calls answering its current
	// judgements cost, to estimate a turn from.
	PastUsage(ctx context.Context, act string) (PastUsage, error)
}

// weavingInstructions are weaving's instructions: the Owner's, a sentence
// saying that earlier Stories are reached by asking, and how, one saying
// how a Span says which Entities it mentions (entityOpen, entityClose), so
// the model can ask by them, one saying what a Span marked the Editor's is and that it may be cited, one stating
// the checks the Owner's leave unsaid (that an Editor's Span has words, and
// cites only Spans shown, wherever shown), and a clause saying that words
// between ⟦ and ⟧ (hydratedOpen, hydratedClose) are a hydrated Span's,
// standing in for the publisher's words that refer to them, so the model
// never takes them for the publisher's.
const weavingInstructions = `You are the editor of a slow-news publication. Your publication exists to see the stories behind the stories, and to take a zoomed-out view of the long-term causes. Below are the Stories new since your last run, each a group of numbered Spans: stretches of text as their publishers wrote them, except that words between ⟦ and ⟧ stand in for words that refer to them. What each Story says is already in it; write what none of them says. Earlier Stories, the publishers' and your own, however old, are not shown: find them by asking for words with find, for an Entity a Span shown to you mentions with find_entity, or for what cites an earlier Story shown to you with find_citing, each of which answers with a catalogue, and read one whole with open_story. A Span says which Entities it mentions, each by its name and an id between { and }, which find_entity asks by. Ask as many times as you need before you answer. Write your substantive editorial opinion on one or more Stories, new or earlier. Each opinion is a new Story: your own words, in one or more editor_spans, each citing by number the Spans it rests on, at least one. Spans marked the Editor's are your own earlier words, and you may cite them too. Each editor_span's body holds its words, and its cites only the numbers of Spans shown to you, below or in what find, find_entity, find_citing and open_story answered. You don't have to write anything: if you have nothing substantive to say, return no stories.`

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

// DueAt is when the day that holds day, in day's location, reaches at, as a
// time of day (6h is 06:00), on that day's clock.
func DueAt(day time.Time, at time.Duration) time.Time {
	y, m, d := day.Date()
	return time.Date(y, m, d, int(at/time.Hour), int(at%time.Hour/time.Minute), 0, 0, day.Location())
}

// Weaving is the weaving act as it runs in batches (system design: Weaving
// runs at most once a day): at most one run a day, keyed by the day, on the
// first run of the batch runner at or after the time of day at (in now's
// location), unless that day's run has already been written or failed,
// nothing has been cut since the last run written, or a run is under way
// (its turn in a pending batch, or its conversation kept between turns). A
// run is shown only the Stories cut since the last run written, and reaches
// earlier ones by asking (system design: A weaving run is shown what is new,
// and finds for itself what bears on it). Each turn is a request of its own:
// a turn that asks is answered from the Record as it stands when its result
// is applied, and the next turn is the run's work from then on, submitted
// on the same turn of the runner. Its answer is applied to what the run was
// shown, however the Record has moved on since.
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
			run, err := e.weavingRun(ctx, key)
			if err != nil || run.Job.Key == "" {
				return nil, err
			}
			return []Job{run.Job}, nil
		},
	}
}

// WeavingRun is a weaving run's next turn as it is submitted: its key, the
// Stories new to it (on its first turn) and its Job.
type WeavingRun struct {
	Key     string
	Stories []CutStory
	Job     Job
}

// weavingRun is the run keyed key's next turn as the Record stands: its
// conversation's next, if it has kept a turn; otherwise its first, shown the
// Stories cut since the last run written, and no Job when nothing has been.
func (e *Editor) weavingRun(ctx context.Context, key string) (WeavingRun, error) {
	conv, kept, err := e.Weavings.WeavingTurnsOf(ctx, key)
	if err != nil {
		return WeavingRun{}, err
	}
	if kept {
		req := conv.request()
		return WeavingRun{Key: key, Job: Job{Key: key, Request: req,
			ApplySent: func(ctx context.Context, sent Request, res Response, now time.Time) error {
				if RequestHash(sent, "") != RequestHash(req, "") {
					return fmt.Errorf("weaving: %w: the run's conversation has moved on since the turn was sent", ErrFailedChecks)
				}
				return e.weavingTurn(ctx, conv, sent, res, now)
			},
		}}, nil
	}
	stories, err := e.Weavings.StoriesToWeave(ctx)
	if err != nil || len(stories) == 0 {
		return WeavingRun{Key: key}, err
	}
	h, err := e.hydratedItems(ctx, nil, stories)
	if err != nil {
		return WeavingRun{}, err
	}
	return WeavingRun{Key: key, Stories: stories, Job: Job{
		Key:     key,
		Request: weavingRequest(stories, h),
		ApplySent: func(ctx context.Context, sent Request, res Response, now time.Time) error {
			shown, ok := shownIn(stories, sent)
			if !ok {
				return errors.New("weaving: the Stories its request showed no longer await weaving")
			}
			conv := WeavingTurns{Key: key, Input: sent.Input, Shown: shown.spans, Left: shown.left, New: shown.newCuts}
			return e.weavingTurn(ctx, conv, sent, res, now)
		},
	}}, nil
}

// request is the run's next turn: its opening and the conversation since.
func (t WeavingTurns) request() Request {
	return Request{Act: weavingAct, Instructions: weavingInstructions, Input: t.Input, Schema: weavingSchema,
		Effort: EffortMedium, Tools: weavingTools, Messages: t.Messages}
}

// woven is what the run has been shown so far, to number on from.
func (t WeavingTurns) woven() *woven {
	sh := &woven{numbers: map[citedAt]int{}, left: slices.Clone(t.Left), newCuts: t.New, entities: t.entitiesShown(),
		stories: t.storiesShown()}
	for _, c := range t.Shown {
		sh.number(c)
	}
	return sh
}

// ErrContinued is a conversation's turn applied without its answer: it
// asked, its asks are answered, and the conversation goes on in its next
// turn. The batch runner keeps its call as Continued, and counts no attempt.
var ErrContinued = errors.New("the conversation goes on")

// weavingTurn applies the model's turn res, answering sent, to the run whose
// conversation stood at conv when it was sent: an answer (it called no
// tool) is checked and kept as the run's result; a turn that asked has its
// asks answered from the Record as it stands, and the conversation is kept
// with them for the next turn (ErrContinued).
func (e *Editor) weavingTurn(ctx context.Context, conv WeavingTurns, sent Request, res Response, now time.Time) error {
	sh := conv.woven()
	if len(res.Calls) == 0 {
		w, err := checkWeaving(sh, sent, modelOf(e.ModelFor, sent.Act), res)
		if err != nil {
			return err
		}
		w.Key, w.At = conv.Key, now
		if err := e.Weavings.KeepWeaving(ctx, w); err != nil {
			return err
		}
		e.Log.InfoContext(ctx, "woven", "key", conv.Key, "shown", len(sh.newCuts), "turns", len(conv.Messages)/2+1,
			"stories", len(w.Stories), "model", w.Judge.Model,
			"input_tokens", w.Usage.InputTokens, "output_tokens", w.Usage.OutputTokens)
		return nil
	}
	rec, err := e.earlierRecord(ctx, sh.newCuts)
	if err != nil {
		return err
	}
	results := make([]ToolResult, 0, len(res.Calls))
	for _, c := range res.Calls {
		content, isErr := rec.answer(sh, c)
		results = append(results, ToolResult{CallID: c.ID, Content: content, IsError: isErr})
	}
	conv.Messages = append(slices.Clone(conv.Messages), Message{Assistant: turnOf(res)}, Message{Results: results})
	conv.Shown, conv.Left = sh.spans, sh.left
	if err := e.withinCap(ctx, conv, sent.Act, res.Usage); err != nil {
		return err
	}
	if err := e.Weavings.KeepWeavingTurns(ctx, conv, now); err != nil {
		return err
	}
	e.Log.InfoContext(ctx, "weaving asked", "key", conv.Key, "turn", len(conv.Messages)/2, "asks", len(res.Calls),
		"input_tokens", res.Usage.InputTokens, "output_tokens", res.Usage.OutputTokens)
	return ErrContinued
}

// withinCap is nil unless the run conv is of has a cap (the Owner's
// --max-usd) that its next turn would take it over: what its turns have
// cost so far, this one's usage included, and the next turn's estimate, at
// batch rates, priced on the model act runs on (weavingCost). Over it, the
// run is given up (ErrGiveUp).
func (e *Editor) withinCap(ctx context.Context, conv WeavingTurns, act string, turn Usage) error {
	if e.PriceOf == nil {
		return nil
	}
	maxUSD, capped, err := e.Weavings.WeavingCap(ctx, conv.Key)
	if err != nil || !capped {
		return err
	}
	usd, next, err := e.weavingCost(ctx, conv.Key, act, conv.request(), turn)
	if errors.Is(err, errNoPrice) {
		return fmt.Errorf("weaving: %w: %w to hold the run to its cap", ErrGiveUp, err)
	}
	if err != nil {
		return err
	}
	if usd+next.USD > maxUSD {
		return fmt.Errorf("weaving: %w: its next turn would take it over its cap of $%.2f: $%.2f spent, the next turn ~$%.2f",
			ErrGiveUp, maxUSD, usd, next.USD)
	}
	return nil
}

// errNoPrice is a model with no price known.
var errNoPrice = errors.New("no price known")

// weavingCost is what the run keyed key has spent, its kept calls' usage and
// turn's, and next's estimate, its next turn's, in dollars at batch rates on
// the model act runs on (Deps.PriceOf). Input read from the provider's cache
// is priced at a tenth of the input rate, as Anthropic charges it.
func (e *Editor) weavingCost(ctx context.Context, key, act string, next Request, turn Usage) (float64, Estimate, error) {
	model := modelOf(e.ModelFor, act)
	if e.PriceOf == nil {
		return 0, Estimate{}, fmt.Errorf("%w for model %q", errNoPrice, model)
	}
	price, ok := e.PriceOf(model)
	if !ok {
		return 0, Estimate{}, fmt.Errorf("%w for model %q", errNoPrice, model)
	}
	price = price.AtBatchRates()
	calls, err := e.Weavings.CallsOn(ctx, weavingAct, key)
	if err != nil {
		return 0, Estimate{}, err
	}
	spent := turn
	for _, c := range calls {
		if c.Response != nil {
			spent = spent.add(c.Response.Usage)
		}
	}
	usd := price.cost(spent.InputTokens, spent.OutputTokens) + float64(spent.CacheReadTokens)/1e6*price.InputPerMTok/10
	past, err := e.Weavings.PastUsage(ctx, weavingAct)
	if err != nil {
		return 0, Estimate{}, err
	}
	est, err := EstimateOf(weavingAct, model, []Request{next}, past, price, BatchRates)
	return usd, est, err
}

// GivenUp is a weaving run marked failed, as the Owner is shown it before
// resuming it: its next turn, what it has spent, its next turn's estimate,
// both in dollars at batch rates, and its cap, if the Owner gave it one.
type GivenUp struct {
	Run      WeavingRun
	SpentUSD float64
	Next     Estimate
	MaxUSD   float64
	Capped   bool
}

// OverCap reports whether the run's next turn would take it over its cap:
// resumed under it, it would be given up again.
func (g GivenUp) OverCap() bool { return g.Capped && g.SpentUSD+g.Next.USD > g.MaxUSD }

// GivenUpWeaving is the weaving run keyed key, marked failed, as it would
// resume once its failed mark is cleared (system design: Work that keeps
// failing is given up after three attempts, and taken up again by hand): its
// next turn is its last kept turn's next, or its first again if it kept none,
// shown the Stories cut since the last run written. It calls no model and
// writes nothing. ErrNothingToWeave is a run with no turn kept and nothing
// awaiting weaving.
func (e *Editor) GivenUpWeaving(ctx context.Context, key string) (GivenUp, error) {
	run, err := e.weavingRun(ctx, key)
	if err != nil {
		return GivenUp{}, err
	}
	if run.Job.Key == "" {
		return GivenUp{}, ErrNothingToWeave
	}
	g := GivenUp{Run: run}
	if g.MaxUSD, g.Capped, err = e.Weavings.WeavingCap(ctx, key); err != nil {
		return GivenUp{}, err
	}
	g.SpentUSD, g.Next, err = e.weavingCost(ctx, key, weavingAct, run.Job.Request, Usage{})
	return g, err
}

// The reasons the Owner's ask for today's weaving run is refused.
var (
	ErrWovenToday     = errors.New("today's weaving run has already been written or marked failed")
	ErrWeavingPending = errors.New("a weaving run is under way")
	ErrNothingToWeave = errors.New("nothing has been cut since the last weaving run written")
)

// WeaveToday is today's weaving run, keyed by the day holding now in now's
// location, as the Owner asks for it, whatever the time of day (system
// design: Weaving runs at most once a day): the first turn the timetable
// would submit at its time, asking the same, and applied by the batch
// runner as that is once its batch ends, the runner submitting any turns
// after it. It is refused, as ErrWovenToday, ErrWeavingPending or
// ErrNothingToWeave, where the timetable would submit no run either.
func (e *Editor) WeaveToday(ctx context.Context, now time.Time) (WeavingRun, error) {
	if _, pending, err := e.pendingWeaving(ctx); err != nil || pending {
		return WeavingRun{}, cmp.Or(err, ErrWeavingPending)
	}
	key := now.Format(time.DateOnly)
	if ran, err := e.Weavings.WeavingRan(ctx, key); err != nil || ran {
		return WeavingRun{}, cmp.Or(err, ErrWovenToday)
	}
	run, err := e.weavingRun(ctx, key)
	if err == nil && len(run.Stories) == 0 {
		err = ErrNothingToWeave
	}
	return run, err
}

// WeavingUnderWay is the key of the weaving run under way, if any: its
// conversation kept between turns, or its turn in a pending batch.
func (e *Editor) WeavingUnderWay(ctx context.Context) (string, bool, error) {
	return e.pendingWeaving(ctx)
}

// pendingWeaving is the key of the weaving run under way, if any: its
// conversation kept between turns, or its turn in a pending batch.
func (e *Editor) pendingWeaving(ctx context.Context) (string, bool, error) {
	conv, under, err := e.Weavings.WeavingInProgress(ctx)
	if err != nil || under {
		return conv.Key, under, err
	}
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

// hydratedItems is each Item's hydration a run reads, by its item key: its
// newest hydration, with the text its addresses are into. An Item with none
// is not in it, and its Spans are read as written.
type hydratedItems map[string]hydrated

// hydrated is an Item's newest hydration, the Item's text as kept and when
// it was published, and the Entity each name its hydration judged is, as the
// Record's look-ups have it.
type hydrated struct {
	text      string
	published time.Time
	newest    Hydration
	is        map[LookupKey]NamedEntity
}

// hydratedItems is the hydration of the Items a run is shown Spans of, those
// of the Stories made and of stories, with the Entities their names are, as
// the Record holds them now: nothing waits for an Item to be hydrated, or a
// name to be looked up.
func (e *Editor) hydratedItems(ctx context.Context, made []Story, stories []CutStory) (hydratedItems, error) {
	h := hydratedItems{}
	add := func(sourceID, entryID string, item func() (Item, bool, error)) error {
		key := sourceID + "\n" + entryID
		if _, done := h[key]; done {
			return nil
		}
		r, ok, err := e.Weavings.NewestHydration(ctx, sourceID, entryID)
		if err != nil || !ok {
			return err
		}
		it, ok, err := item()
		if err != nil || !ok {
			return err
		}
		h[key] = hydrated{text: it.Content, published: it.Published, newest: r}
		return nil
	}
	for _, s := range stories {
		if err := add(s.Item.SourceID, s.Item.EntryID, func() (Item, bool, error) {
			return s.Item, true, nil
		}); err != nil {
			return nil, err
		}
	}
	for _, st := range made {
		for _, es := range st.Spans {
			for _, c := range es.Cites {
				if !c.Of.IsZero() {
					continue
				}
				a := c.Address
				if err := add(a.SourceID, a.EntryID, func() (Item, bool, error) {
					it, ok, err := e.Weavings.KeptItem(ctx, a.SourceID, a.EntryID)
					return it.Item, ok, err
				}); err != nil {
					return nil, err
				}
			}
		}
	}
	var keys []LookupKey
	seen := map[LookupKey]bool{}
	for _, hy := range h {
		for _, k := range hy.newest.Keys(hy.published) {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	if len(keys) == 0 {
		return h, nil
	}
	is, err := e.Weavings.EntitiesNamed(ctx, keys)
	if err != nil {
		return nil, err
	}
	for key, hy := range h {
		hy.is = is
		h[key] = hy
	}
	return h, nil
}

// entity is the Entity whose id (NamedEntity.handle) is id, among those the
// Items' names are, reporting whether there is one.
func (h hydratedItems) entity(id string) (NamedEntity, bool) {
	for _, hy := range h {
		// Every Item's names are read from the one map of the Record's.
		for _, e := range hy.is {
			if e.handle() == id {
				return e, true
			}
		}
		break
	}
	return NamedEntity{}, false
}

// mentions are the Entities the Span at a mentions, by its hydration, as
// the Record's look-ups have them: none for a Span not hydrated, or whose
// names are not yet looked up.
func (h hydratedItems) mentions(a Address) []NamedEntity {
	hy, ok := h[a.SourceID+"\n"+a.EntryID]
	if !ok {
		return nil
	}
	return hy.newest.Entities(a.Span, hy.published, hy.is)
}

// rejected reports whether the Span at a is Rejected by its hydration.
func (h hydratedItems) rejected(a Address) bool {
	hy, ok := h[a.SourceID+"\n"+a.EntryID]
	return ok && slices.Contains(hy.newest.Rejected, a)
}

// hydratedOpen and hydratedClose mark a hydrated stretch as weaving is shown
// it: white square brackets, which no publisher writes, so a publisher's own
// square brackets ("yesterday [TUE]") are shown as written and never read
// as hydration's (system design: Hydration is shown in marks no publisher
// writes).
const (
	hydratedOpen  = "⟦"
	hydratedClose = "⟧"
)

// read is the Span at a as its hydration reads it: who speaks it, as the
// words naming them read, empty for none judged, and its words with each
// stretch that refers to another read, between hydratedOpen and
// hydratedClose, as the words of what it refers to. With no hydration, it is
// words, as written.
func (h hydratedItems) read(a Address, words string) (speakers, read string) {
	hy, ok := h[a.SourceID+"\n"+a.EntryID]
	if !ok {
		return "", words
	}
	r, s := hy.newest, a.Span
	var by []string
	for _, sp := range r.Speakers {
		if sp.Of.Span.Start >= s.Start && sp.Of.Span.End <= s.End {
			if who := r.Words(hy.text, sp.By); !slices.Contains(by, who) {
				by = append(by, who)
			}
		}
	}
	var b strings.Builder
	for _, p := range r.Hydrated(hy.text, s) {
		if p.Was != "" {
			b.WriteString(hydratedOpen + p.Words + hydratedClose)
			continue
		}
		b.WriteString(p.Words)
	}
	return strings.Join(by, " and "), b.String()
}

// show is the Span at a as a run is shown it after its number, its words as
// written words: what is said of it (its title, for a Span cited by an
// Editor's Span, who speaks it, and the Entities it mentions, each by its
// name and its id, which find_entity asks by), in brackets, then its words
// as read.
func (h hydratedItems) show(a Address, words, title string) string {
	speakers, read := h.read(a, words)
	var said []string
	if title != "" {
		said = append(said, fmt.Sprintf("from %q", title))
	}
	if speakers != "" {
		said = append(said, "spoken by "+speakers)
	}
	if es := h.mentions(a); len(es) > 0 {
		named := make([]string, 0, len(es))
		for _, e := range es {
			named = append(named, e.Name+" "+entityOpen+e.handle()+entityClose)
		}
		said = append(said, "mentioning "+strings.Join(named, ", "))
	}
	if len(said) == 0 {
		return ": " + read
	}
	return " (" + strings.Join(said, ", ") + "): " + read
}

// weavingRequest is a weaving run's first turn, shown the Stories cut since
// the last run, each Span as h hydrates it, with the tools it asks by.
func weavingRequest(stories []CutStory, h hydratedItems) Request {
	return WeavingTurns{Input: weavingInput(stories, h)}.request()
}

// weavingInput is what a run is shown at its start: the Stories cut since
// the last run, numbering their Spans S1, S2… across them all, each as h
// hydrates it.
func weavingInput(stories []CutStory, h hydratedItems) string {
	sh := &woven{numbers: map[citedAt]int{}}
	var b strings.Builder
	for _, s := range stories {
		sh.cutStory(&b, s, h)
	}
	return b.String()
}

// woven is what a weaving run has been shown: each Span it numbers in number
// order, the number of each, the numbers left out (Spans Rejected), the
// Cuts of the Stories new to it, and, before its latest turn, the ids of the
// Entities it was shown a Span mentioning and the numbers of the earlier
// Stories its asks' answers named.
type woven struct {
	spans    []Cited
	numbers  map[citedAt]int
	left     []int
	newCuts  []int64
	entities []string
	stories  []int64
}

// citedAt is a Span's address, whether a Source's or an Editor's.
type citedAt struct {
	Address Address
	Of      EditorSpanAt
}

// number numbers c as the next Span shown.
func (sh *woven) number(c Cited) int {
	sh.spans = append(sh.spans, c)
	n := len(sh.spans)
	sh.numbers[citedAt{c.Address, c.Of}] = n
	return n
}

// shown is c's number, numbering it if it has none yet, and shown: no
// longer among those left out.
func (sh *woven) shown(c Cited) int {
	n, ok := sh.numbers[citedAt{c.Address, c.Of}]
	if !ok {
		return sh.number(c)
	}
	sh.left = slices.DeleteFunc(sh.left, func(l int) bool { return l == n })
	return n
}

// leftOut is c's number, numbering it if it has none yet, left out unless it
// has been shown.
func (sh *woven) leftOut(c Cited) {
	if _, ok := sh.numbers[citedAt{c.Address, c.Of}]; ok {
		return
	}
	sh.left = append(sh.left, sh.number(c))
}

// cutStory writes to b one new Story a Cut made, under its Item's title and
// date, its Spans numbered on from those shown before it, each as h
// hydrates it. A Span h Rejects is left out, and keeps its number, so every
// Span has the same number however it is hydrated.
func (sh *woven) cutStory(b *strings.Builder, s CutStory, h hydratedItems) {
	b.WriteString(cutStoryHeader(s))
	for _, sp := range s.Spans {
		words := cut.Words(s.Item.Content, sp)
		a := Address{SourceID: s.Item.SourceID, EntryID: s.Item.EntryID, Span: sp}
		n := sh.number(Cited{Address: a, Words: words, Title: s.Item.Title, Published: s.Item.Published})
		if h.rejected(a) {
			sh.left = append(sh.left, n)
			continue
		}
		fmt.Fprintf(b, "S%d%s\n", n, h.show(a, words, ""))
	}
	b.WriteString("\n")
	sh.newCuts = append(sh.newCuts, s.CutID)
}

// cutStoryHeader is the line a new Story a Cut made is shown under: its
// Item's title and date.
func cutStoryHeader(s CutStory) string {
	if s.Item.Published.IsZero() {
		return fmt.Sprintf("Story from %q\n", s.Item.Title)
	}
	return fmt.Sprintf("Story from %q, published %s\n", s.Item.Title, s.Item.Published.UTC().Format(time.DateOnly))
}

// shownIn is what a run's first turn was sent showing: those of stories, now
// awaiting weaving, whose input it is, each Span by the number it was shown
// under, and the numbers it left out. No run is sent while one is under way,
// and Stories are cut in order and only added to, so those shown are the
// first of the Stories now awaiting. A Span's number is the same however it
// is hydrated, so the numbers are worked out again here; which Spans were
// left out, Rejected, and how each was shown, are read from the request as
// sent, since a Span hydrated after it went is shown otherwise now (system
// design: Construction judges a Span from the Story its Cut made; nothing
// waits for hydration).
func shownIn(stories []CutStory, sent Request) (*woven, bool) {
	sh := &woven{numbers: map[citedAt]int{}}
	in := sent.Input
	pos := 0
	var discard strings.Builder
	for _, s := range stories {
		if !strings.HasPrefix(in[pos:], cutStoryHeader(s)) {
			break
		}
		end := strings.Index(in[pos:], "\n\n")
		if end < 0 {
			return nil, false
		}
		block := in[pos : pos+end]
		pos += end + 2
		first := len(sh.spans) + 1
		sh.cutStory(&discard, s, nil)
		for n := first; n <= len(sh.spans); n++ {
			if !strings.Contains(block, fmt.Sprintf("\nS%d:", n)) && !strings.Contains(block, fmt.Sprintf("\nS%d (", n)) {
				sh.left = append(sh.left, n)
			}
		}
	}
	if len(sh.newCuts) == 0 || pos != len(in) {
		return nil, false
	}
	return sh, true
}

// checkWeaving decodes the model's answer to req, asked of the model named
// asked, shown sh, and runs weaving's checks over it: each citation is a
// Span shown (a Source's or an earlier Editor's, new or reached by an ask),
// each Editor's Span has words and cites at least one Span, and each Story
// has at least one Editor's Span. A result that fails them is
// ErrFailedChecks.
func checkWeaving(sh *woven, req Request, asked string, res Response) (Weaving, error) {
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
	spans := sh.spans
	w := Weaving{
		ThroughCut: sh.newCuts[len(sh.newCuts)-1], Usage: res.Usage,
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
				if slices.Contains(sh.left, c) {
					return refuse("story %d, editor_span %d cites S%d, which was not shown: it is left out, Rejected",
						i+1, j+1, c)
				}
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
