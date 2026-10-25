package editor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Batcher is the port to a provider's batch interface (system design: The
// daily acts run in batches). Like Model it is provider-neutral: acts'
// requests go in, each with its own identifier, and once the batch has ended
// its results come out, each carrying the identifier of its request. Results
// may come in any order; they are matched by identifier, never by position.
type Batcher interface {
	// Submit submits the requests as one batch and returns its identifier.
	Submit(ctx context.Context, rs []BatchRequest) (string, error)
	// Poll reports whether the batch has ended and, once it has, its results.
	Poll(ctx context.Context, batchID string) (BatchPoll, error)
}

// BatchRequest is one act's request in a batch.
type BatchRequest struct {
	// ID identifies the request within its batch.
	ID      string
	Request Request
}

// BatchResult is what came back for one request in a batch.
type BatchResult struct {
	// ID is the identifier of the request it answers.
	ID       string
	Response Response
	// Err is a request that got no usable answer: it errored or expired, or
	// the model refused or was cut off.
	Err error
}

// BatchPoll is a batch's state when polled.
type BatchPoll struct {
	// Ended is whether every request in the batch has its result.
	Ended   bool
	Results []BatchResult
}

// BatchAct is an editorial act as it runs in batches: what awaits it, each
// piece of work with its request, and how a result is checked and written
// back. Reading is one; placing and connecting are built the same way.
type BatchAct struct {
	// Name names the act, as its requests do.
	Name string
	// Waiting is the work awaiting the act in the Record. Work stays waiting
	// until its result is written, so a batch's results are applied to the
	// work as it is when they arrive, which may be after a restart.
	Waiting func(ctx context.Context) ([]Job, error)
}

// Job is one piece of work awaiting an act.
type Job struct {
	// Key identifies the work within its act, such as an Item.
	Key     string
	Request Request
	// Apply checks a model's answer with the act's own checks and writes
	// what passes to the Record, at now. A result that fails the checks is
	// ErrFailedChecks and writes nothing; any other error is the Record's.
	Apply func(ctx context.Context, res Response, now time.Time) error
}

// PendingBatch is a batch submitted and not yet ended, as the Record keeps
// it so that a restart picks it up.
type PendingBatch struct {
	ID          string
	SubmittedAt time.Time
	Requests    []PendingRequest
}

// PendingRequest is one request in a pending batch: its identifier, and the
// act and work it is for.
type PendingRequest struct {
	ID  string
	Act string
	Key string
}

// MaxAttempts is how many times a piece of work is attempted in batches
// before it is marked failed and no longer submitted (system design: The
// daily acts run in batches). Each attempt costs a model call, so work that
// always fails would otherwise cost one on every run, without bound.
const MaxAttempts = 3

// BatchDeadline is how long a batch is waited for before it is given up and
// each piece of work in it counts a failed attempt: one that has disappeared
// at the provider, or never ends. Twice the day within which providers end
// a batch (Anthropic's expire after 24 hours), so a batch slow to end is
// not given up while its results may still come.
const BatchDeadline = 48 * time.Hour

// WorkFailure is a failed attempt at a piece of work: when and why, and
// whether it was the last attempt, marking the work failed.
type WorkFailure struct {
	Act, Key string
	At       time.Time
	Why      string
	Failed   bool
}

// FailedWork is a piece of work marked failed, as the Owner is shown it: the
// act, the Item it was on, and when and why it last failed.
type FailedWork struct {
	Act               string
	SourceID, EntryID string
	Title             string
	Attempts          int
	LastFailedAt      time.Time
	Why               string
}

// BatchStore is the part of the Record that keeps the pending batches.
type BatchStore interface {
	PendingBatches(ctx context.Context) ([]PendingBatch, error)
	SaveBatch(ctx context.Context, b PendingBatch) error
	// Attempts is how many attempts at the act's work with key have failed.
	Attempts(ctx context.Context, act, key string) (int, error)
	// EndBatch removes an ended batch, keeping its failed attempts and
	// saving the batches its failed requests were resubmitted in, all at
	// once. Work a failure marks failed no longer awaits its act.
	EndBatch(ctx context.Context, batchID string, failures []WorkFailure, resubmitted []PendingBatch) error
}

// BatchDeps are the batch runner's dependencies.
type BatchDeps struct {
	Store   BatchStore
	Batcher Batcher
	// Acts are the acts run in batches.
	Acts []BatchAct
	Log  *slog.Logger
}

// Batches runs the daily acts in batches.
type Batches struct {
	BatchDeps
}

// NewBatches builds the batch runner.
func NewBatches(d BatchDeps) *Batches { return &Batches{BatchDeps: d} }

// ReadWaiting is the batch runner's turn after a Collection run, at now.
// It never waits on a batch: it applies the results of every batch that has
// ended, resubmitting each failed request alone until its work has failed
// MaxAttempts times, and gives up each batch past BatchDeadline as failed;
// then it submits the work awaiting each act, and not already in a batch,
// as one batch. Work marked failed no longer awaits its act. Only a
// failing Record is an error; a failing batch interface is logged, and its
// work tried again on the next run.
func (b *Batches) ReadWaiting(ctx context.Context, now time.Time) error {
	pending, err := b.Store.PendingBatches(ctx)
	if err != nil {
		return err
	}
	jobs, err := b.waiting(ctx)
	if err != nil {
		return err
	}
	inBatch := map[string]bool{}
	for _, p := range pending {
		s, err := b.settle(ctx, p, jobs, now)
		if err != nil {
			return err
		}
		if !s.ended {
			for _, r := range p.Requests {
				inBatch[r.ID] = true
			}
			continue
		}
		for _, rb := range s.resubmitted {
			for _, r := range rb.Requests {
				inBatch[r.ID] = true
			}
		}
		if err := b.Store.EndBatch(ctx, p.ID, s.failures, s.resubmitted); err != nil {
			return err
		}
	}
	// Results applied above are no longer waiting.
	if jobs, err = b.waiting(ctx); err != nil {
		return err
	}
	var batch PendingBatch
	var requests []BatchRequest
	for _, act := range b.Acts {
		for _, j := range jobs[act.Name] {
			id := requestID(act.Name, j.Key)
			if inBatch[id] {
				continue
			}
			requests = append(requests, BatchRequest{ID: id, Request: j.Request})
			batch.Requests = append(batch.Requests, PendingRequest{ID: id, Act: act.Name, Key: j.Key})
		}
	}
	if len(requests) == 0 {
		return nil
	}
	if batch.ID, err = b.Batcher.Submit(ctx, requests); err != nil {
		b.Log.ErrorContext(ctx, "batch not submitted", "requests", len(requests), "error", err)
		return nil
	}
	batch.SubmittedAt = now
	b.Log.InfoContext(ctx, "batch submitted", "batch", batch.ID, "requests", len(requests))
	return b.Store.SaveBatch(ctx, batch)
}

// settled is what settling a pending batch came to: whether it has ended,
// its failed attempts, and the batches its failures were resubmitted in.
type settled struct {
	ended       bool
	failures    []WorkFailure
	resubmitted []PendingBatch
}

// settle polls a pending batch and, if it has ended, applies its results,
// counting each failed request's attempt and resubmitting it alone unless
// that was its last.
func (b *Batches) settle(ctx context.Context, p PendingBatch, jobs map[string]map[string]Job, now time.Time) (settled, error) {
	var s settled
	// lost is why the batch is given up, if it is: it could not be polled,
	// or has not ended, by the deadline.
	var lost error
	poll, err := b.Batcher.Poll(ctx, p.ID)
	switch {
	case err != nil:
		b.Log.ErrorContext(ctx, "batch not polled", "batch", p.ID, "error", err)
		lost = fmt.Errorf("batch %s not polled by its deadline: %w", p.ID, err)
	case !poll.Ended:
		lost = fmt.Errorf("batch %s not ended by its deadline", p.ID)
	}
	if lost != nil {
		if now.Sub(p.SubmittedAt) < BatchDeadline {
			return s, nil
		}
		b.Log.ErrorContext(ctx, "batch given up", "batch", p.ID, "error", lost)
		poll.Results = nil
	}
	s.ended = true
	results := map[string]BatchResult{}
	for _, r := range poll.Results {
		results[r.ID] = r
	}
	for _, r := range p.Requests {
		j, ok := jobs[r.Act][r.Key]
		if !ok {
			// No longer awaiting the act (already done, or its Topic
			// dropped): the result has nothing to apply to.
			continue
		}
		res, ok := results[r.ID]
		switch {
		case lost != nil:
			res.Err = lost
		case !ok:
			res.Err = errors.New("no result for the request")
		}
		if res.Err == nil {
			err := j.Apply(ctx, res.Response, now)
			if err == nil {
				b.Log.InfoContext(ctx, "batch result applied", "batch", p.ID, "act", r.Act, "key", r.Key,
					"model", res.Response.Model, "input_tokens", res.Response.Usage.InputTokens,
					"output_tokens", res.Response.Usage.OutputTokens)
				continue
			}
			if !errors.Is(err, ErrFailedChecks) {
				return settled{}, err
			}
			res.Err = err
		}
		prior, err := b.Store.Attempts(ctx, r.Act, r.Key)
		if err != nil {
			return settled{}, err
		}
		f := WorkFailure{Act: r.Act, Key: r.Key, At: now, Why: res.Err.Error(), Failed: prior+1 >= MaxAttempts}
		s.failures = append(s.failures, f)
		if f.Failed {
			b.Log.ErrorContext(ctx, "work failed", "batch", p.ID, "act", r.Act, "key", r.Key,
				"attempts", prior+1, "error", res.Err)
			continue
		}
		b.Log.ErrorContext(ctx, "batch request failed", "batch", p.ID, "act", r.Act, "key", r.Key,
			"attempts", prior+1, "error", res.Err)
		id, err := b.Batcher.Submit(ctx, []BatchRequest{{ID: r.ID, Request: j.Request}})
		if err != nil {
			// Not in a batch once this one ends, so submitted with the
			// rest on the next run.
			b.Log.ErrorContext(ctx, "batch request not resubmitted", "act", r.Act, "key", r.Key, "error", err)
			continue
		}
		s.resubmitted = append(s.resubmitted, PendingBatch{ID: id, SubmittedAt: now, Requests: []PendingRequest{r}})
	}
	return s, nil
}

// waiting is the work awaiting each act, by act and key.
func (b *Batches) waiting(ctx context.Context) (map[string]map[string]Job, error) {
	out := map[string]map[string]Job{}
	for _, act := range b.Acts {
		jobs, err := act.Waiting(ctx)
		if err != nil {
			return nil, fmt.Errorf("work awaiting %s: %w", act.Name, err)
		}
		out[act.Name] = map[string]Job{}
		for _, j := range jobs {
			out[act.Name][j.Key] = j
		}
	}
	return out, nil
}

// requestID is a request's identifier: its act and a digest of its work's
// key, so it is the same wherever the work is submitted, and short and plain
// enough for any provider (Anthropic's allow 64 letters, digits, - and _).
func requestID(act, key string) string {
	sum := sha256.Sum256([]byte(key))
	return act + "-" + hex.EncodeToString(sum[:16])
}

// Reading is the reading act as it runs in batches: the Items awaiting
// reading, each read into the Record when its result passes the checks.
func (e *Editor) Reading() BatchAct {
	return BatchAct{
		Name: "reading",
		Waiting: func(ctx context.Context) ([]Job, error) {
			items, err := e.Store.ItemsToRead(ctx)
			if err != nil {
				return nil, err
			}
			jobs := make([]Job, 0, len(items))
			for _, it := range items {
				ps, err := e.Store.ReadingPrecedents(ctx, it, precedentsShown)
				if err != nil {
					return nil, err
				}
				req, err := readingRequest(it, ps)
				if err != nil {
					continue
				}
				jobs = append(jobs, Job{
					Key:     it.SourceID + "\n" + it.EntryID,
					Request: req,
					Apply: func(ctx context.Context, res Response, now time.Time) error {
						return e.keep(ctx, it, ps, res, now)
					},
				})
			}
			return jobs, nil
		},
	}
}
