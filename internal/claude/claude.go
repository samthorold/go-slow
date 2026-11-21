// Package claude is the model adapter for Claude, through Anthropic's
// Messages API. It answers the Editor's model port with structured output
// against the act's own schema; nothing of Anthropic's types leaves it. A
// conversation's turn (weaving's) is a request like any other, carrying
// strict tools beside its output format and the turns before it, passed
// back as they came (Model.params), and answered with the tools it called
// beside its text (answer).
//
// It speaks the Messages API and the Message Batches API (Options.ActModels
// sends each request of a batch to its act's model) through
// anthropic-sdk-go. Its prices (prices.go: PriceOf, dated
// PricesAsOf, with PricesSource) stay inside it too, used only by the
// estimates of the rejudge and weave commands; read them again when a model changes or before
// a run whose cap matters. A test fails if a model in .env.example has none.
//
// Every call has the SDK's retries but one: creating a Message Batch is made
// once (Submit says why), since it is not idempotent and a retried create can
// submit a second, paid batch. Polling a batch, reading its results and the
// real-time calls are safe to repeat, and keep them.
package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"

	"github.com/samthorold/go-slow/internal/editor"
)

// maxTokens bounds one answer, the model's thinking included: Claude Haiku
// 5.5 thinks unless told not to (adaptive thinking, its default), and the
// thinking counts against this bound as the output does. Hydrating a long
// Item thinks about as much as it writes, and a limit sized for the output
// alone cut hydrations off (issue #90). The thinking is left as the model's
// default, since it is part of how each act was recorded. An answer cut off
// at the bound is reported, not used. Twice the most a hydration has used, and
// a quarter of the model's own ceiling, so a call stays well inside timeout.
const maxTokens = 32000

// timeout bounds each attempt of a call; a call the SDK retries can take up
// to its attempts times this, plus the backoff between them.
const timeout = 5 * time.Minute

// Options configure the adapter. main passes them from the config.
type Options struct {
	APIKey string
	// Model is the Claude model the act runs on, such as claude-haiku-5-5.
	Model string
	// ActModels names, by act, a model other than Model for that act's
	// requests, so one batch can carry every act's requests, each on its
	// own act's model.
	ActModels map[string]string
	// BaseURL replaces Anthropic's API, for tests; empty is the real one.
	BaseURL string
}

// Model is one Claude model behind the Editor's model port.
type Model struct {
	client    anthropic.Client
	model     string
	actModels map[string]string
}

// ModelFor is the model an act's requests run on: the act's own in ActModels, otherwise Model.
func (m *Model) ModelFor(act string) string {
	if am, ok := m.actModels[act]; ok {
		return am
	}
	return m.model
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
	return &Model{client: anthropic.NewClient(opts...), model: o.Model, actModels: o.ActModels}
}

// Complete runs one act's call: its instructions as the system prompt, its
// input as the user's turn, and its schema as the required output format.
// A refusal or an answer cut off at its length is an error, never output.
func (m *Model) Complete(ctx context.Context, r editor.Request) (editor.Response, error) {
	p, err := m.params(r)
	if err != nil {
		return editor.Response{}, fmt.Errorf("%s on %s: %w", r.Act, m.ModelFor(r.Act), err)
	}
	msg, err := m.client.Messages.New(ctx, p)
	if err != nil {
		return editor.Response{}, fmt.Errorf("%s on %s: %w", r.Act, m.ModelFor(r.Act), err)
	}
	res, err := answer(msg)
	if err != nil {
		return res, fmt.Errorf("%s on %s: %w", r.Act, m.ModelFor(r.Act), err)
	}
	return res, nil
}

// params are an act's call as a message: its instructions as the system
// prompt, its input as the user's turn, its schema as the output format.
// A conversation's turn (weaving's) also carries its tools, as strict tools
// beside the output format, and the conversation after its input: each turn
// the model took, passed back exactly as it came (its thinking included, as
// the API requires), and the tools' results as the user's turn after it. It
// is cached, the top-level marker placing the breakpoint on its last block,
// since each turn sends everything the turn before it sent; a request that
// is no conversation is sent as it always was.
func (m *Model) params(r editor.Request) (anthropic.MessageNewParams, error) {
	p := anthropic.MessageNewParams{
		Model:     anthropic.Model(m.ModelFor(r.Act)),
		MaxTokens: maxTokens,
		System:    []anthropic.TextBlockParam{{Text: r.Instructions}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(r.Input)),
		},
		OutputConfig: anthropic.OutputConfigParam{
			Format: anthropic.JSONOutputFormatParam{Schema: r.Schema},
			// The port's efforts are named as the API names them; empty sends
			// none, leaving the model's default.
			Effort: anthropic.OutputConfigEffort(r.Effort),
		},
	}
	if len(r.Tools) == 0 && len(r.Messages) == 0 {
		return p, nil
	}
	for _, t := range r.Tools {
		props := t.Schema["properties"]
		required, _ := t.Schema["required"].([]string)
		extra := map[string]any{}
		for k, v := range t.Schema {
			if k != "type" && k != "properties" && k != "required" {
				extra[k] = v
			}
		}
		p.Tools = append(p.Tools, anthropic.ToolUnionParam{OfTool: &anthropic.ToolParam{
			Name:        t.Name,
			Description: param.NewOpt(t.Description),
			Strict:      param.NewOpt(true),
			InputSchema: anthropic.ToolInputSchemaParam{Properties: props, Required: required, ExtraFields: extra},
		}})
	}
	for _, msg := range r.Messages {
		switch {
		case msg.Assistant != nil:
			var turn anthropic.Message
			if err := json.Unmarshal(msg.Assistant.Raw, &turn); err != nil {
				return p, fmt.Errorf("a turn not given back as it came: %w", err)
			}
			p.Messages = append(p.Messages, turn.ToParam())
		case msg.Results != nil:
			blocks := make([]anthropic.ContentBlockParamUnion, 0, len(msg.Results))
			for _, res := range msg.Results {
				blocks = append(blocks, anthropic.NewToolResultBlock(res.CallID, res.Content, res.IsError))
			}
			p.Messages = append(p.Messages, anthropic.NewUserMessage(blocks...))
		default:
			p.Messages = append(p.Messages, anthropic.NewUserMessage(anthropic.NewTextBlock(msg.User)))
		}
	}
	p.CacheControl = anthropic.NewCacheControlEphemeralParam()
	return p, nil
}

// answer is a message as the Editor's port gives it: its text output and
// the tools it called, or an error for a refusal or an answer cut off at
// its length. Either way the message as returned comes with it, for the
// kept call, and for a conversation's next turn. Input tokens written to the
// cache count as input, those read from it apart.
func answer(msg *anthropic.Message) (editor.Response, error) {
	res := editor.Response{
		Model:      string(msg.Model),
		StopReason: string(msg.StopReason),
		Usage: editor.Usage{
			InputTokens:     msg.Usage.InputTokens + msg.Usage.CacheCreationInputTokens,
			OutputTokens:    msg.Usage.OutputTokens,
			CacheReadTokens: msg.Usage.CacheReadInputTokens,
		},
	}
	if raw := msg.RawJSON(); raw != "" {
		res.Raw = json.RawMessage(raw)
	}
	switch msg.StopReason {
	case anthropic.StopReasonRefusal:
		return res, fmt.Errorf("%w (%s)", editor.ErrRefused, msg.StopDetails.Category)
	case anthropic.StopReasonMaxTokens:
		return res, editor.ErrTruncated
	}
	var text strings.Builder
	for _, block := range msg.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			text.WriteString(b.Text)
		case anthropic.ToolUseBlock:
			res.Calls = append(res.Calls, editor.ToolCall{ID: b.ID, Name: b.Name, Input: b.Input})
		}
	}
	res.Output = json.RawMessage(text.String())
	return res, nil
}
