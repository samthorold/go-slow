package editor

import "encoding/json"

// The shapes of a kept conversation (KeptConversation): one an act had with
// a model, a turn at a time, the model calling tools. Only the retired
// Sunday read held conversations, and no act holds one now; they are kept
// for good, so these shapes stay to read them.

// Tool is a tool the model could call, by name, with input matching Schema.
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
// calls, the model ended its turn.
type Turn struct {
	Text  string
	Calls []ToolCall
	// Raw is the turn in the provider's own form.
	Raw   json.RawMessage
	Model string
	// StopReason is why the model stopped, as the provider says it.
	StopReason string
	Usage      Usage
}

// ToolCall is the model calling a tool. ID matches it to its result.
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResult is what running a tool gave, for the call with CallID, an error
// told to the model as one.
type ToolResult struct {
	CallID  string
	Content string
	IsError bool
}
