package claude

import (
	"context"
	"errors"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/samthorold/go-slow/internal/editor"
)

// Submit submits acts' requests as one Message Batch, each made as Complete
// makes it and identified by its request's ID (the batch's custom_id).
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
	b, err := m.client.Messages.Batches.New(ctx, anthropic.MessageBatchNewParams{Requests: requests})
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
