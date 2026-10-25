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
	"github.com/anthropics/anthropic-sdk-go/shared/constant"

	"github.com/samthorold/go-slow/internal/editor"
)

// The Editor's one act with tools, the Sunday read, runs its own loop over
// this adapter's Turn: one call per turn, its tools defined by the act. A
// manual loop rather than the SDK's tool runner, since the act owns the loop
// (its bounds, its checks on a submitted read, what it records), and the
// tool runner would put Anthropic's types into the act.

var _ editor.Conversation = (*Model)(nil)

// turnMaxTokens bounds one turn, thinking included: a turn is a few tool
// calls or the submitted read, with room for adaptive thinking at high
// effort.
const turnMaxTokens = 32000

// turnTimeout bounds one turn, retries included: a long turn at high effort
// runs for minutes.
const turnTimeout = 15 * time.Minute

// turnEffort is how hard the model thinks on each turn. The Sunday read is
// weekly long-context reasoning over the Record, the work effort repays,
// and Claude Opus 5.5 defaults to medium.
const turnEffort = anthropic.BetaOutputConfigEffortHigh

// Turn runs one turn of an act's conversation: its instructions as the
// system prompt, its tools as strict tools the model may call, and the
// conversation so far, the model's own turns passed back as they came.
// Thinking is adaptive (Claude Opus 5.5 can't turn it off); a refusal is
// retried server-side on the model Anthropic recommends for its category;
// the conversation's prefix is cached. A refusal or a turn cut off at its
// length is an error, never output, returned with the turn that came back.
func (m *Model) Turn(ctx context.Context, r editor.ConversationRequest) (editor.Turn, error) {
	model := m.ModelFor(r.Act)
	params, err := turnParams(model, r)
	if err != nil {
		return editor.Turn{}, fmt.Errorf("%s on %s: %w", r.Act, model, err)
	}
	msg, err := m.client.Beta.Messages.New(ctx, params, option.WithRequestTimeout(turnTimeout))
	if err != nil {
		return editor.Turn{}, fmt.Errorf("%s on %s: %w", r.Act, model, err)
	}
	t, err := turnOf(msg)
	if err != nil {
		return t, fmt.Errorf("%s on %s: %w", r.Act, model, err)
	}
	return t, nil
}

func turnParams(model string, r editor.ConversationRequest) (anthropic.BetaMessageNewParams, error) {
	tools := make([]anthropic.BetaToolUnionParam, 0, len(r.Tools))
	for _, t := range r.Tools {
		props, _ := t.Schema["properties"]
		required, _ := t.Schema["required"].([]string)
		extra := map[string]any{}
		for k, v := range t.Schema {
			if k != "type" && k != "properties" && k != "required" {
				extra[k] = v
			}
		}
		tools = append(tools, anthropic.BetaToolUnionParam{OfTool: &anthropic.BetaToolParam{
			Name:        t.Name,
			Description: param.NewOpt(t.Description),
			Strict:      param.NewOpt(true),
			InputSchema: anthropic.BetaToolInputSchemaParam{Properties: props, Required: required, ExtraFields: extra},
		}})
	}
	msgs := make([]anthropic.BetaMessageParam, 0, len(r.Messages))
	for _, m := range r.Messages {
		switch {
		case m.Assistant != nil:
			var msg anthropic.BetaMessage
			if err := json.Unmarshal(m.Assistant.Raw, &msg); err != nil {
				return anthropic.BetaMessageNewParams{}, fmt.Errorf("a turn not given back as it came: %w", err)
			}
			msgs = append(msgs, msg.ToParam())
		case m.Results != nil:
			blocks := make([]anthropic.BetaContentBlockParamUnion, 0, len(m.Results))
			for _, res := range m.Results {
				blocks = append(blocks, anthropic.NewBetaToolResultBlock(res.CallID, res.Content, res.IsError))
			}
			msgs = append(msgs, anthropic.NewBetaUserMessage(blocks...))
		default:
			msgs = append(msgs, anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(m.User)))
		}
	}
	return anthropic.BetaMessageNewParams{
		Model:        anthropic.Model(model),
		MaxTokens:    turnMaxTokens,
		System:       []anthropic.BetaTextBlockParam{{Text: r.Instructions}},
		Tools:        tools,
		Messages:     msgs,
		Thinking:     anthropic.BetaThinkingConfigParamUnion{OfAdaptive: &anthropic.BetaThinkingConfigAdaptiveParam{}},
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: turnEffort},
		Fallbacks:    anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()},
		CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
		Betas:        []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
	}, nil
}

// turnOf is a message as the Editor's conversation port gives it: its text,
// its tool calls, and the message itself to be passed back unchanged. A
// refusal or a cut-off turn is an error, returned with what came back, so
// the act can keep it.
func turnOf(msg *anthropic.BetaMessage) (editor.Turn, error) {
	t := editor.Turn{
		Raw:        json.RawMessage(msg.RawJSON()),
		Model:      string(msg.Model),
		StopReason: string(msg.StopReason),
		Usage: editor.Usage{
			InputTokens:     msg.Usage.InputTokens + msg.Usage.CacheCreationInputTokens,
			OutputTokens:    msg.Usage.OutputTokens,
			CacheReadTokens: msg.Usage.CacheReadInputTokens,
		},
	}
	switch msg.StopReason {
	case anthropic.BetaStopReasonRefusal:
		return t, fmt.Errorf("%w (%s)", editor.ErrRefused, msg.StopDetails.Category)
	case anthropic.BetaStopReasonMaxTokens:
		return t, editor.ErrTruncated
	}
	var text strings.Builder
	for _, block := range msg.Content {
		switch b := block.AsAny().(type) {
		case anthropic.BetaTextBlock:
			text.WriteString(b.Text)
		case anthropic.BetaToolUseBlock:
			t.Calls = append(t.Calls, editor.ToolCall{ID: b.ID, Name: b.Name, Input: json.RawMessage(b.JSON.Input.Raw())})
		}
	}
	t.Text = text.String()
	return t, nil
}
