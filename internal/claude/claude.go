// Package claude is the model adapter for Claude, through Anthropic's
// Messages API. It answers the Editor's model port with structured output
// against the act's own schema; nothing of Anthropic's types leaves it.
package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/samthorold/go-slow/internal/editor"
)

// maxTokens bounds one answer. An act's output is a bounded extraction, far
// below this; one cut off at it is reported, not used.
const maxTokens = 16000

// timeout bounds one call, retries included.
const timeout = 5 * time.Minute

// Options configure the adapter. main passes them from the config.
type Options struct {
	APIKey string
	// Model is the Claude model the act runs on, such as claude-haiku-5-5.
	Model string
	// BaseURL replaces Anthropic's API, for tests; empty is the real one.
	BaseURL string
}

// Model is one Claude model behind the Editor's model port.
type Model struct {
	client anthropic.Client
	model  string
}

// New builds the adapter. It takes nothing from the environment: the key
// and model come in Options.
func New(o Options) *Model {
	opts := []option.RequestOption{
		option.WithoutEnvironmentDefaults(),
		option.WithAPIKey(o.APIKey),
		option.WithRequestTimeout(timeout),
	}
	if o.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(o.BaseURL))
	}
	return &Model{client: anthropic.NewClient(opts...), model: o.Model}
}

// Complete runs one act's call: its instructions as the system prompt, its
// input as the user's turn, and its schema as the required output format.
// A refusal or an answer cut off at its length is an error, never output.
func (m *Model) Complete(ctx context.Context, r editor.Request) (editor.Response, error) {
	msg, err := m.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(m.model),
		MaxTokens: maxTokens,
		System:    []anthropic.TextBlockParam{{Text: r.Instructions}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(r.Input)),
		},
		OutputConfig: anthropic.OutputConfigParam{
			Format: anthropic.JSONOutputFormatParam{Schema: r.Schema},
		},
	})
	if err != nil {
		return editor.Response{}, fmt.Errorf("%s on %s: %w", r.Act, m.model, err)
	}
	switch msg.StopReason {
	case anthropic.StopReasonRefusal:
		return editor.Response{}, fmt.Errorf("%s on %s: %w (%s)", r.Act, m.model, editor.ErrRefused, msg.StopDetails.Category)
	case anthropic.StopReasonMaxTokens:
		return editor.Response{}, fmt.Errorf("%s on %s: %w", r.Act, m.model, editor.ErrTruncated)
	}
	var text strings.Builder
	for _, block := range msg.Content {
		if b, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(b.Text)
		}
	}
	return editor.Response{
		Output: json.RawMessage(text.String()),
		Model:  string(msg.Model),
		Usage: editor.Usage{
			InputTokens:  msg.Usage.InputTokens,
			OutputTokens: msg.Usage.OutputTokens,
		},
	}, nil
}
