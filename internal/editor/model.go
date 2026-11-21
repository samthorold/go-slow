package editor

import (
	"context"
	"encoding/json"
	"errors"
)

// The errors an adapter reports for an answer with no usable output.
var (
	// ErrRefused is a model declining to answer.
	ErrRefused = errors.New("the model refused")
	// ErrTruncated is an answer cut off at its length limit.
	ErrTruncated = errors.New("the model's answer was cut off")
)

// Model is the port every act that calls a model goes through. It is
// provider-neutral: an act's instructions, input and output schema go in,
// and the model's output, as JSON meant to match the schema, comes out. The
// act decodes and checks it; an adapter only reports what the model said.
type Model interface {
	Complete(ctx context.Context, r Request) (Response, error)
}

// Request is one act's call on a model.
type Request struct {
	// Act names the act, for logs and recordings.
	Act string
	// Instructions are the act's own, the same on every call.
	Instructions string
	// Input is what this call is about, such as an Item's text.
	Input string
	// Schema is the JSON Schema the output must match.
	Schema map[string]any
	// Effort is how much effort the act asks the model to spend on its
	// answer (EffortLow, EffortMedium, EffortHigh); empty leaves it to the
	// model's own default. It is part of the request, as its model is.
	Effort string
	// Tools are the tools the model may call, for an act that is a
	// conversation (weaving); none for an act that answers at once.
	Tools []Tool
	// Messages are the conversation after Input, in order: each turn the
	// model took (Message.Assistant, as it came back) and what the act sent
	// back (Message.Results of the tools it called). Empty on a
	// conversation's first turn.
	Messages []Message
}

// The efforts an act can ask a model for, provider-neutral: an adapter maps
// each to its provider's own setting.
const (
	EffortLow    = "low"
	EffortMedium = "medium"
	EffortHigh   = "high"
)

// Response is what the model answered. An adapter that reports an error for
// an answer with no usable output (ErrRefused, ErrTruncated) still returns
// what came back, so the answer can be kept.
type Response struct {
	// Output is the model's JSON output: its text, which is the answer when
	// it calls no tool.
	Output json.RawMessage
	// Calls are the tools the model called, in order: a conversation's turn
	// that is not yet its answer.
	Calls []ToolCall
	// Raw is the whole response exactly as the provider returned it, in its
	// own form (thinking and refusals included), for the kept call.
	Raw json.RawMessage
	// Model is the model that answered, as its provider names it.
	Model string
	// StopReason is why the model stopped, as its provider says it.
	StopReason string
	Usage      Usage
}

// Usage is what a call cost, in tokens.
type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	// CacheReadTokens are input tokens read from the provider's prompt
	// cache, apart from InputTokens: a conversation sends its whole history
	// on every turn, and most of it is read from the cache.
	CacheReadTokens int64 `json:"cache_read_input_tokens,omitempty"`
}

func (u Usage) add(v Usage) Usage {
	return Usage{u.InputTokens + v.InputTokens, u.OutputTokens + v.OutputTokens, u.CacheReadTokens + v.CacheReadTokens}
}
