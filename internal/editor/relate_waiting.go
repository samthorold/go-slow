package editor

import (
	"cmp"
	"context"
	"slices"
	"time"
)

// candidatesPerSpan is the most candidates a Span is weighed against, and
// candidatesPerItem the most an Item's Spans are shown between them: without
// caps an Entity in nearly everything would make most of the Record a
// candidate, and the call's cost grow with it (system design: Entities find
// candidates; they don't say who did what).
const (
	candidatesPerSpan = 10
	candidatesPerItem = 200
)

// withCandidates is tr with each of its Spans' candidates: Spans of Items
// read before it that mention an Entity it mentions, ranked by the Entities
// each shares with it, each weighted by how rarely it is mentioned, and
// capped. They are not narrowed by recency.
func (e *Editor) withCandidates(ctx context.Context, tr ToRelate) (ToRelate, error) {
	var es []NamedEntity
	for _, sp := range tr.Spans {
		for _, en := range sp.Entities {
			if !slices.Contains(es, en) {
				es = append(es, en)
			}
		}
	}
	if len(es) == 0 {
		return tr, nil
	}
	others, err := e.Store.SpansMentioning(ctx, es, tr.ReadOrder)
	if err != nil {
		return tr, err
	}
	mentioned, err := e.Store.ItemsMentioning(ctx, es, tr.ReadOrder)
	if err != nil {
		return tr, err
	}
	return candidatesOf(tr, others, mentioned), nil
}

// candidatesOf is tr with each Span's candidates chosen from others: those
// of another Item sharing an Entity with it, best first by the sum over
// what they share of one over how many Items mention it, then the most
// recently published, then in order; at most candidatesPerSpan each, and
// candidatesPerItem between them, the Spans in order filling it.
func candidatesOf(tr ToRelate, others []OtherSpan, mentioned map[NamedEntity]int) ToRelate {
	pool := map[Address]bool{}
	spans := slices.Clone(tr.Spans)
	for i, sp := range spans {
		type scored struct {
			o     OtherSpan
			score float64
		}
		var ranked []scored
		for _, o := range others {
			if o.At.SourceID == tr.Item.SourceID && o.At.EntryID == tr.Item.EntryID {
				continue
			}
			var shares []NamedEntity
			score := 0.0
			for _, en := range o.Entities {
				if slices.Contains(sp.Entities, en) && !slices.Contains(shares, en) {
					shares = append(shares, en)
					score += 1 / float64(max(mentioned[en], 1))
				}
			}
			if len(shares) == 0 {
				continue
			}
			o.Shares = shares
			ranked = append(ranked, scored{o, score})
		}
		slices.SortStableFunc(ranked, func(a, b scored) int {
			return cmp.Or(cmp.Compare(b.score, a.score), b.o.Item.Published.Compare(a.o.Item.Published))
		})
		spans[i].Candidates = nil
		for _, r := range ranked {
			if len(spans[i].Candidates) == candidatesPerSpan {
				break
			}
			if !pool[r.o.At] && len(pool) == candidatesPerItem {
				continue
			}
			pool[r.o.At] = true
			spans[i].Candidates = append(spans[i].Candidates, r.o)
		}
	}
	tr.Spans = spans
	return tr
}

// Relating is the relating act as it runs in batches: the Items awaiting
// relating, each with its candidates, related into the Record when its
// result passes the checks. An Item whose Spans have no candidates is
// related at once, with no call. Relating creates nothing, only relations
// between Spans already in the Record, so a batch needs no ordering: an
// Item's candidates are of the Items read before it, so of two Items read in
// one batch (in the order they were published) the later-published judges
// the pair (system design: Relating a Span).
func (e *Editor) Relating() BatchAct {
	return BatchAct{
		Name: "relating",
		Waiting: func(ctx context.Context) ([]Job, error) {
			waiting, err := e.Store.ItemsToRelate(ctx)
			if err != nil {
				return nil, err
			}
			jobs := make([]Job, 0, len(waiting))
			for _, tr := range waiting {
				tr, err := e.withCandidates(ctx, tr)
				if err != nil {
					return nil, err
				}
				job := Job{Key: itemKey(tr.Item)}
				if tr.hasCandidates() {
					job.Request = relatingRequest(tr)
				}
				req := job.Request
				job.Apply = func(ctx context.Context, res Response, now time.Time) error {
					r := unshownRelating(tr)
					if tr.hasCandidates() {
						var err error
						if r, err = checkRelating(tr, req, modelOf(e.ModelFor, req.Act), res); err != nil {
							return err
						}
					}
					return e.keepRelating(ctx, r, now)
				}
				jobs = append(jobs, job)
			}
			return jobs, nil
		},
	}
}

// RelateWaiting relates every Item awaiting relating, at now, in real time,
// in the order they were first read: the synchronous path, for when no
// batch interface is available. A relating that fails is logged and writes
// nothing, so its Item stays awaiting relating; it does not stop the
// others. Only a failing Record is an error.
func (e *Editor) RelateWaiting(ctx context.Context, now time.Time) error {
	waiting, err := e.Store.ItemsToRelate(ctx)
	if err != nil {
		return err
	}
	for _, tr := range waiting {
		tr, err := e.withCandidates(ctx, tr)
		if err != nil {
			return err
		}
		if !tr.hasCandidates() {
			if err := e.keepRelating(ctx, unshownRelating(tr), now); err != nil {
				return err
			}
			continue
		}
		req := relatingRequest(tr)
		var r Relating
		call, err := e.callNow(ctx, e.RelatingModel, itemKey(tr.Item), req, nil, now, func(res Response) (err error) {
			r, err = checkRelating(tr, req, modelOf(e.ModelFor, req.Act), res)
			return err
		})
		if err != nil {
			if err := e.Store.KeepCall(ctx, call); err != nil {
				return err
			}
			e.Log.ErrorContext(ctx, "relating failed", "source", tr.Item.SourceID, "entry", tr.Item.EntryID, "error", err)
			continue
		}
		if err := e.keepRelating(ctx, r, now); err != nil {
			return err
		}
		if err := e.Store.KeepCall(ctx, call); err != nil {
			return err
		}
	}
	return nil
}

// keepRelating keeps a relating judged at now, and logs it.
func (e *Editor) keepRelating(ctx context.Context, r Relating, now time.Time) error {
	r.At = now
	if err := e.Store.KeepRelating(ctx, r); err != nil {
		return err
	}
	counts := map[string]int{}
	for _, rel := range r.Relations {
		counts[rel.Kind]++
	}
	e.Log.InfoContext(ctx, "related", "source", r.SourceID, "entry", r.EntryID,
		"follows", counts[Follows], "same_as", counts[SameAs], "contradicts", counts[Contradicts],
		"model", r.Judge.Model, "input_tokens", r.Usage.InputTokens, "output_tokens", r.Usage.OutputTokens)
	return nil
}
