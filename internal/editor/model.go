// Package editor is the Editor: the editorial acts done on the Record. An act
// that needs judgement calls a model through the port here; the act owns its
// instructions and output schema, and checks the result before anything is
// written back (system design: The Editor's daily work is a sequence of
// editorial acts; The model is an adapter).
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
}

// Response is what the model answered.
type Response struct {
	// Output is the model's JSON output.
	Output json.RawMessage
	// Model is the model that answered, as its provider names it.
	Model string
	Usage Usage
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

// total is every token, input and output, cached or not.
func (u Usage) total() int64 { return u.InputTokens + u.OutputTokens + u.CacheReadTokens }
