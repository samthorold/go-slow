package editor

import (
	"context"
	"encoding/json"
)

// Conversation is the port the one act with tools goes through, the Sunday
// read (system design: The Sunday read is the one place the Editor is an
// agent with tools). Apart from the single-call Model port, it is one turn
// of a conversation in which the model may call tools: the act's
// instructions, its tools and the conversation so far go in, and the model's
// next turn comes out. The act runs the loop, runs the tools and checks what
// comes back; an adapter only reports what the model said. It is
// provider-neutral: nothing of a provider's types crosses it.
type Conversation interface {
	Turn(ctx context.Context, r ConversationRequest) (Turn, error)
}

// ConversationRequest is one turn's call on a model.
type ConversationRequest struct {
	// Act names the act, for logs and recordings.
	Act string
	// Instructions are the act's own, the same on every turn.
	Instructions string
	// Tools are the tools the model may call, the same on every turn.
	Tools []Tool
	// Messages are the conversation so far, oldest first, starting with the
	// act's own message.
	Messages []Message
}

// Tool is a tool the model may call, by name, with input matching Schema.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
}

// Message is one message of a conversation: the act's (User, or Results of
// the tools the model called), or the model's own turn (Assistant).
type Message struct {
	User      string
	Results   []ToolResult
	Assistant *Turn
}

// Turn is the model's turn: what it said, and the tools it called. With no
// calls, the model has ended its turn.
type Turn struct {
	Text  string
	Calls []ToolCall
	// Raw is the turn in the provider's own form, opaque to the act, which
	// the adapter is given back unchanged in the conversation's later turns
	// (a model's reasoning in a turn is valid only as it was given).
	Raw   json.RawMessage
	Model string
	Usage Usage
}

// ToolCall is the model calling a tool. ID matches it to its result.
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResult is what running a tool gave, for the call with CallID. An
// error is told to the model as one, so it can try another way.
type ToolResult struct {
	CallID  string
	Content string
	IsError bool
}
