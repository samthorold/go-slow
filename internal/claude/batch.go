package claude

import (
	"context"
	"errors"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/samthorold/go-slow/internal/editor"
)

// Submit submits acts' requests as one Message Batch, each made as Complete
// makes it and identified by its request's ID (the batch's custom_id).
//
// The create is one request, never retried: creating a batch is not
// idempotent, so a create that times out on the client after the API has
// accepted it, retried, would submit a second, paid batch the Record never
// learns of. A failed create needs no retry here: its work stays awaiting
// its act, and the next Collection run submits it again (the weave command,
// submitting on demand, reports the failure instead). Poll keeps the SDK's
// retries, since reading a batch and its results is safe to repeat.
func (m *Model) Submit(ctx context.Context, rs []editor.BatchRequest) (string, error) {
	requests := make([]anthropic.MessageBatchNewParamsRequest, 0, len(rs))
	for _, r := range rs {
		p := m.params(r.Request)
		requests = append(requests, anthropic.MessageBatchNewParamsRequest{
			CustomID: r.ID,
			Params: anthropic.MessageBatchNewParamsRequestParams{
				Model: p.Model, MaxTokens: p.MaxTokens, System: p.System,
				Messages: p.Messages, OutputConfig: p.OutputConfig,
			},
		})
	}
	b, err := m.client.Messages.Batches.New(ctx, anthropic.MessageBatchNewParams{Requests: requests},
		option.WithMaxRetries(0))
	if err != nil {
		return "", fmt.Errorf("batch of %d on %s: %w", len(rs), m.model, err)
	}
	return b.ID, nil
}

// Poll reports whether a Message Batch has ended and, once it has, each
// request's result under its custom_id. The results come in any order.
func (m *Model) Poll(ctx context.Context, batchID string) (editor.BatchPoll, error) {
	b, err := m.client.Messages.Batches.Get(ctx, batchID, anthropic.MessageBatchGetParams{})
	if err != nil {
		return editor.BatchPoll{}, fmt.Errorf("batch %s: %w", batchID, err)
	}
	if b.ProcessingStatus != anthropic.MessageBatchProcessingStatusEnded {
		return editor.BatchPoll{}, nil
	}
	stream := m.client.Messages.Batches.ResultsStreaming(ctx, batchID, anthropic.MessageBatchResultsParams{})
	defer func() { _ = stream.Close() }()
	poll := editor.BatchPoll{Ended: true}
	for stream.Next() {
		line := stream.Current()
		res := editor.BatchResult{ID: line.CustomID}
		switch v := line.Result.AsAny().(type) {
		case anthropic.MessageBatchSucceededResult:
			res.Response, res.Err = answer(&v.Message)
		case anthropic.MessageBatchErroredResult:
			res.Err = fmt.Errorf("%s: %s", v.Error.Error.Type, v.Error.Error.Message)
		case anthropic.MessageBatchExpiredResult:
			res.Err = errors.New("the request expired before it was processed")
		case anthropic.MessageBatchCanceledResult:
			res.Err = errors.New("the request was canceled")
		default:
			res.Err = fmt.Errorf("a result of no known type %q", line.Result.Type)
		}
		if res.Err != nil {
			res.Err = fmt.Errorf("batch %s request %s on %s: %w", batchID, line.CustomID, m.model, res.Err)
		}
		poll.Results = append(poll.Results, res)
	}
	if err := stream.Err(); err != nil {
		return editor.BatchPoll{}, fmt.Errorf("batch %s results: %w", batchID, err)
	}
	return poll, nil
}
