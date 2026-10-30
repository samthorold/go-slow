package editor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Range is a range of the Record to judge again: the Items published from
// From up to, not including, To.
type Range struct {
	From, To time.Time
}

// Candidate is a candidate Judge: a run judging a Range of the Record again
// whose judgements are kept beside the live ones and read by no view until
// it is chosen (system design: Every judgement carries its judge, and a
// changed judge judges forward). It is the act, the version of its
// instructions and the model, as a Judge is, without a request: each of its
// judgements carries its own.
type Candidate struct {
	// ID is the candidate's in the Record, once it is kept there.
	ID      int64
	Act     string
	Version int
	Model   string
	Range   Range
	// At is when its run began.
	At time.Time
	// ChosenAt is when it was chosen, making it the live judge from then
	// on; zero while it is a candidate.
	ChosenAt time.Time
	// Judgements is how many it has made: for reading, its readings.
	Judgements int
}

// RejudgeAct is an act as it judges a Range of the Record again. Reading is
// the one today; an act on Spans that comes later (relating, the Briefing)
// plugs in by giving its own Work.
type RejudgeAct struct {
	// Name names the act, as its requests do, and Version is the version of
	// its instructions this build sends.
	Name    string
	Version int
	// Work is the act's work over the range, asked of the model named
	// model, each piece with its request and how its result is kept.
	Work func(ctx context.Context, r Range, model string) ([]RejudgeJob, error)
}

// RejudgeJob is one piece of an act's work over a range.
type RejudgeJob struct {
	// Key identifies the work within its act, as its calls are kept under.
	Key     string
	Request Request
	// Apply checks a model's answer with the act's own checks and keeps
	// what passes, judged at now, as the candidate's judgement, or as the
	// live judge's when candidate is zero. A result that fails the checks
	// is ErrFailedChecks and keeps nothing.
	Apply func(ctx context.Context, res Response, now time.Time, candidate int64) error
}

// RereadingStore is the part of the Record reading a range again reads and
// writes.
type RereadingStore interface {
	// ItemsIn are the Items cut and published in the range, each with its
	// newest Cut, oldest first, whether read already or not.
	ItemsIn(ctx context.Context, r Range) ([]ToRead, error)
	KeepReading(ctx context.Context, r Reading) error
}

// Rereading is reading as it reads a range of the Record again: each Item
// cut and published in the range, read as it is read in use.
func Rereading(store RereadingStore) RejudgeAct {
	return RejudgeAct{
		Name:    "reading",
		Version: ReadingVersion,
		Work: func(ctx context.Context, rng Range, model string) ([]RejudgeJob, error) {
			items, err := store.ItemsIn(ctx, rng)
			if err != nil {
				return nil, err
			}
			jobs := make([]RejudgeJob, 0, len(items))
			for _, tr := range items {
				req := readingRequest(tr)
				jobs = append(jobs, RejudgeJob{
					Key:     itemKey(tr.Item),
					Request: req,
					Apply: func(ctx context.Context, res Response, now time.Time, candidate int64) error {
						r, err := checkReading(tr, req, model, res)
						if err != nil {
							return err
						}
						r.At, r.Candidate = now, candidate
						return store.KeepReading(ctx, r)
					},
				})
			}
			return jobs, nil
		},
	}
}

// KindCompared is how two readings of one Item compare on one kind of
// judgement: how many each made, and how many both made, alike in every
// part.
type KindCompared struct {
	Kind                  string
	Live, Candidate, Both int
}

// CompareReadings compares a live reading of an Item with a candidate's, a
// kind of judgement at a time, in the order a reading judges them.
func CompareReadings(live, candidate Reading) []KindCompared {
	return []KindCompared{
		compareKind("names", live.Names, candidate.Names),
		compareKind("refers_to", live.RefersTo, candidate.RefersTo),
		compareKind("speakers", live.Speakers, candidate.Speakers),
		compareKind("rejected", live.Rejected, candidate.Rejected),
	}
}

func compareKind[T comparable](kind string, live, candidate []T) KindCompared {
	in := map[T]bool{}
	for _, j := range live {
		in[j] = true
	}
	k := KindCompared{Kind: kind, Live: len(live), Candidate: len(candidate)}
	for _, j := range candidate {
		if in[j] {
			k.Both++
		}
	}
	return k
}

// Price is what a model costs per million tokens in and out, at the rate
// the calls are made at.
type Price struct {
	InputPerMTok, OutputPerMTok float64
	// LongAbove, if set, is the size of a request in input tokens above
	// which the request is charged at LongInputPerMTok and
	// LongOutputPerMTok instead.
	LongAbove                           int64
	LongInputPerMTok, LongOutputPerMTok float64
}

// cost is what a request of in tokens answered in out tokens costs, in
// dollars.
func (p Price) cost(in, out int64) float64 {
	pin, pout := p.InputPerMTok, p.OutputPerMTok
	if p.LongAbove > 0 && in > p.LongAbove {
		pin, pout = p.LongInputPerMTok, p.LongOutputPerMTok
	}
	return float64(in)/1e6*pin + float64(out)/1e6*pout
}

// PastUsage is what an act's applied calls in the Record cost: how many
// there were, the size of their requests in bytes (instructions, schema and
// input) and their tokens in and out.
type PastUsage struct {
	Calls                     int
	RequestBytes              int64
	InputTokens, OutputTokens int64
}

// RejudgeStore is the part of the Record a run judging a range again keeps
// its candidate and its calls in, and estimates from.
type RejudgeStore interface {
	NewCandidate(ctx context.Context, c Candidate) (int64, error)
	// PastUsage is what the act's applied calls answering the act's current
	// judgements cost.
	PastUsage(ctx context.Context, act string) (PastUsage, error)
	CallKeeper
}

// RejudgeDeps are a run's dependencies.
type RejudgeDeps struct {
	Store RejudgeStore
	// Model is the model the run calls, in real time, and ModelName the
	// model it is asked for: the judge's model.
	Model     Model
	ModelName string
	// PriceOf is a model's price at real-time rates, reporting whether it
	// is known.
	PriceOf func(model string) (Price, bool)
	Now     func() time.Time
	Log     *slog.Logger
}

// Rejudge judges a range of the Record again with one judge, priced first
// (system design: Every judgement carries its judge, and a changed judge
// judges forward). Its calls are made in real time, one at a time: a run
// is a deliberate one-off the Owner starts and watches end, and a batch
// would either hold the command open for up to a day or leave its results
// to the app's timetable, which applies only work awaiting an act.
type Rejudge struct {
	RejudgeDeps
}

// NewRejudge builds a run.
func NewRejudge(d RejudgeDeps) *Rejudge { return &Rejudge{RejudgeDeps: d} }

// Rates is the rate a run's calls are charged at: real time, not batch.
const Rates = "real-time"

// The estimate's fallbacks, for an act with no applied call in the Record
// to estimate from: bytes of request per input token (English runs at
// about four; three overestimates, as a price used as a cap should), and
// output tokens per call, thinking included.
const (
	fallbackBytesPerToken = 3
	fallbackOutputTokens  = 4000
)

// Estimate is what a run would send and cost, worked out without calling a
// model.
type Estimate struct {
	Act, Model string
	Requests   int
	// InputTokens and OutputTokens are estimated: input from the size of
	// each request, at the tokens per byte of the act's past calls, and
	// output at their mean per call (FromPast); otherwise by the fallbacks.
	InputTokens, OutputTokens int64
	FromPast                  int
	USD                       float64
	Rates                     string
	jobs                      []RejudgeJob
	costs                     []float64
}

// Price is the dry run: what judging the range again with act would send and
// cost. It calls no model and writes nothing.
func (r *Rejudge) Price(ctx context.Context, act RejudgeAct, rng Range) (Estimate, error) {
	price, ok := r.PriceOf(r.ModelName)
	if !ok {
		return Estimate{}, fmt.Errorf("no price known for model %q", r.ModelName)
	}
	jobs, err := act.Work(ctx, rng, r.ModelName)
	if err != nil {
		return Estimate{}, err
	}
	past, err := r.Store.PastUsage(ctx, act.Name)
	if err != nil {
		return Estimate{}, err
	}
	est := Estimate{Act: act.Name, Model: r.ModelName, Requests: len(jobs), FromPast: past.Calls, Rates: Rates, jobs: jobs}
	for _, j := range jobs {
		size, err := requestBytes(j.Request)
		if err != nil {
			return Estimate{}, err
		}
		in, out := (size+fallbackBytesPerToken-1)/fallbackBytesPerToken, int64(fallbackOutputTokens)
		if past.Calls > 0 && past.RequestBytes > 0 {
			in = (size*past.InputTokens + past.RequestBytes - 1) / past.RequestBytes
			out = (past.OutputTokens + int64(past.Calls) - 1) / int64(past.Calls)
		}
		est.InputTokens += in
		est.OutputTokens += out
		c := price.cost(in, out)
		est.costs = append(est.costs, c)
		est.USD += c
	}
	return est, nil
}

// requestBytes is the size of a request as sent: its instructions, schema
// and input.
func requestBytes(req Request) (int64, error) {
	schema, err := json.Marshal(req.Schema)
	if err != nil {
		return 0, err
	}
	return int64(len(req.Instructions) + len(schema) + len(req.Input)), nil
}

// ErrOverCap is a run whose estimate is over the most it may spend.
var ErrOverCap = errors.New("the estimate is over the most the run may spend")

// RunOptions say how a run judges.
type RunOptions struct {
	// Candidate keeps the run's judgements as a new candidate Judge's, read
	// by no view until it is chosen; otherwise they are the live judge's.
	Candidate bool
	// MaxUSD is the most the run may spend, in dollars at its rates.
	MaxUSD float64
}

// RunOutcome is what a run came to.
type RunOutcome struct {
	Estimate Estimate
	// Candidate is the candidate Judge the run kept, if it ran as one.
	Candidate int64
	// Judged is how many pieces of work it judged, and Failed the keys of
	// those that failed every attempt.
	Judged int
	Failed []string
	// Stopped is whether it stopped before the end because the next piece
	// could take its spending over MaxUSD.
	Stopped bool
	// Usage and USD are what its calls cost, as the provider reported.
	Usage Usage
	USD   float64
}

// Run judges the range again with act, as the live judge or as a candidate.
// It is priced first, and refused as ErrOverCap if the estimate is over
// MaxUSD; while it runs, it stops before a piece whose estimate would take
// what it has spent over MaxUSD. Each piece gets MaxAttempts attempts, as
// in use, and every call is kept. A piece that fails every attempt is
// logged and left; only a failing Record is an error.
func (r *Rejudge) Run(ctx context.Context, act RejudgeAct, rng Range, o RunOptions) (RunOutcome, error) {
	est, err := r.Price(ctx, act, rng)
	if err != nil {
		return RunOutcome{}, err
	}
	out := RunOutcome{Estimate: est}
	if est.USD > o.MaxUSD {
		return out, fmt.Errorf("%w: $%.2f over $%.2f", ErrOverCap, est.USD, o.MaxUSD)
	}
	price, _ := r.PriceOf(r.ModelName)
	if o.Candidate {
		out.Candidate, err = r.Store.NewCandidate(ctx, Candidate{
			Act: act.Name, Version: act.Version, Model: r.ModelName, Range: rng, At: r.Now(),
		})
		if err != nil {
			return out, err
		}
	}
	for i, j := range est.jobs {
		var lastErr error
		for attempt := 1; attempt <= MaxAttempts; attempt++ {
			// Each attempt is a call, so the cap is checked before each.
			if out.USD+est.costs[i] > o.MaxUSD {
				out.Stopped = true
				r.Log.WarnContext(ctx, "run stopped at its cap", "act", act.Name, "spent_usd", out.USD, "max_usd", o.MaxUSD)
				return out, nil
			}
			now := r.Now()
			// notKept is the Record failing to keep a result that passed.
			var notKept error
			call, err := callModel(ctx, r.Model, r.ModelName, j.Key, act.Name, j.Request, nil, now, func(res Response) error {
				err := j.Apply(ctx, res, now, out.Candidate)
				if err != nil && !errors.Is(err, ErrFailedChecks) {
					notKept = err
				}
				return err
			})
			if call.Response != nil {
				u := call.Response.Usage
				out.Usage = out.Usage.add(u)
				out.USD += price.cost(u.InputTokens+u.CacheReadTokens, u.OutputTokens)
			}
			if err := r.Store.KeepCall(ctx, call); err != nil {
				return out, err
			}
			if notKept != nil {
				return out, notKept
			}
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			if lastErr = err; err == nil {
				break
			}
		}
		if lastErr != nil {
			out.Failed = append(out.Failed, j.Key)
			r.Log.ErrorContext(ctx, "work failed", "act", act.Name, "key", j.Key, "error", lastErr)
			continue
		}
		out.Judged++
	}
	return out, nil
}
