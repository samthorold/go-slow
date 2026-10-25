package editor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Call is a model call an act made, kept as it was sent and as it came back
// (system design: Every model call is kept). Kept calls are a side record:
// they are never rewritten, and no act is shown them.
type Call struct {
	// ID is the call's in the Record, once it is kept there.
	ID int64
	// Act is the act that made the call, and Key the piece of work it was
	// for, as the act keys it (an Item, for reading).
	Act, Key string
	// At is when its outcome was known.
	At time.Time
	// BatchID is the batch the request was sent in; empty for a call made
	// in real time.
	BatchID string
	// Model is the model the call asked for.
	Model string
	// Request is the request exactly as sent.
	Request Request
	// Precedents are the ids of the Feedback the request showed as
	// precedent; they are in its input too.
	Precedents []int64
	// RequestHash is RequestHash of the request and the model asked for.
	RequestHash string
	// Response is the response exactly as returned; nil when none came, as
	// for a request in a batch that ended without answering it.
	Response *Response
	// Outcome is what came of the response, and Why the reason it was not
	// applied.
	Outcome Outcome
	Why     string
}

// Outcome is what the act made of a call's response.
type Outcome string

// The outcomes of a call.
const (
	// Applied is a response that passed the act's checks and was written to
	// the Record.
	Applied Outcome = "applied"
	// Refused is a response the act's checks refused: nothing of it was
	// written.
	Refused Outcome = "refused"
	// Failed is a call with no usable answer (none came, the call errored,
	// the model refused or was cut off), or one whose result could not be
	// written.
	Failed Outcome = "failed"
	// Continued is a turn of a conversation that submitted nothing (it called
	// tools, or ended its turn and was asked to submit), after which the
	// conversation went on.
	Continued Outcome = "continued"
)

// KeptConversation is a conversation an act had with a model, kept turn by
// turn: one attempt at its piece of work, as the Sunday read's run is. Like a
// kept call, it is a side record, never rewritten, that no act is shown.
type KeptConversation struct {
	// ID is the conversation's in the Record, once it is kept there.
	ID int64
	// Act is the act that had it, and Key the piece of work it was for (the
	// Sunday, for the Sunday read).
	Act, Key string
	// At is when it ended.
	At time.Time
	// Model is the model asked for.
	Model string
	// Instructions and Tools are the act's, the same on every turn.
	Instructions string
	Tools        []Tool
	// Turns are its turns in order.
	Turns []KeptTurn
	// Outcome is what came of it, applied or failed, and Why the reason it
	// failed.
	Outcome Outcome
	Why     string
}

// KeptTurn is one turn of a kept conversation: one model call.
type KeptTurn struct {
	// N is its place in the conversation, from 1.
	N int
	// At is when its outcome was known.
	At time.Time
	// Sent are the messages its request added to the conversation: the act's
	// opening message, or the tool results of the turn before, and anything
	// the act said with them. The rest of the request is the conversation's
	// instructions and tools and the turns before, each turn's Sent then its
	// Response.
	Sent []Message
	// RequestHash is ConversationRequestHash of the whole request as sent,
	// and the model asked for.
	RequestHash string
	// Response is the model's turn exactly as returned; nil when none came.
	Response *Turn
	// Outcome is what came of it, and Why what the act's checks told the
	// agent of a refused submission, or why the turn failed.
	Outcome Outcome
	Why     string
}

// RequestHash is the hash of a request as a model is asked it: its
// instructions, schema and input, and the model asked for. The act's name is
// not in it, nor anything the provider adds; two requests with the same hash
// would put the same question to the same model. It is the one meaning of
// "the same request" a kept call and a recorded run share: a recording keeps
// the hash of the request it answered, so whether today's code would still
// send it is a comparison of hashes.
//
// It is the hex SHA-256 of the request's canonical JSON encoding, an object
// with the keys instructions, input, model and schema, in that order, the
// schema encoded with its object keys sorted (as encoding/json does for a
// map), so the same request always hashes the same.
func RequestHash(r Request, model string) string {
	return hashOf(struct {
		Instructions string         `json:"instructions"`
		Input        string         `json:"input"`
		Model        string         `json:"model"`
		Schema       map[string]any `json:"schema"`
	}{r.Instructions, r.Input, model, r.Schema})
}

// ConversationRequestHash is RequestHash for a turn of a conversation: the
// hash of the turn's request as the model is asked it, its instructions, its
// tools, the conversation so far and the model asked for. As with
// RequestHash, the act's name is not in it, nor anything the provider adds.
//
// It is the hex SHA-256 of the request's canonical JSON encoding, an object
// with the keys instructions, messages, model and tools, in that order:
//   - tools, in the order given, each {name, description, schema}, the schema
//     with its object keys sorted;
//   - messages, oldest first, each an object with one key: user (the act's
//     text), results (each {call_id, content, is_error}), or assistant (the
//     model's turn as the provider gave it, Raw, compacted; for a turn with no
//     provider form, {text, calls}, each call {id, name, input}), since what
//     a provider is sent of the model's own turn is that turn as it came.
func ConversationRequestHash(r ConversationRequest, model string) string {
	type tool struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Schema      map[string]any `json:"schema"`
	}
	type result struct {
		CallID  string `json:"call_id"`
		Content string `json:"content"`
		IsError bool   `json:"is_error"`
	}
	type call struct {
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	type turn struct {
		Text  string `json:"text"`
		Calls []call `json:"calls"`
	}
	tools := make([]tool, 0, len(r.Tools))
	for _, t := range r.Tools {
		tools = append(tools, tool(t))
	}
	messages := make([]map[string]any, 0, len(r.Messages))
	for _, m := range r.Messages {
		switch {
		case m.Assistant != nil && m.Assistant.Raw != nil:
			messages = append(messages, map[string]any{"assistant": m.Assistant.Raw})
		case m.Assistant != nil:
			t := turn{Text: m.Assistant.Text, Calls: []call{}}
			for _, c := range m.Assistant.Calls {
				t.Calls = append(t.Calls, call(c))
			}
			messages = append(messages, map[string]any{"assistant": t})
		case m.Results != nil:
			rs := make([]result, 0, len(m.Results))
			for _, res := range m.Results {
				rs = append(rs, result(res))
			}
			messages = append(messages, map[string]any{"results": rs})
		default:
			messages = append(messages, map[string]any{"user": m.User})
		}
	}
	return hashOf(struct {
		Instructions string           `json:"instructions"`
		Messages     []map[string]any `json:"messages"`
		Model        string           `json:"model"`
		Tools        []tool           `json:"tools"`
	}{r.Instructions, messages, model, tools})
}

// hashOf is the hex SHA-256 of v's JSON encoding, the one way a request is
// hashed.
func hashOf(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// A request is built of JSON values; one that can't be encoded is a
		// programming error, never input.
		panic("editor: request is not JSON: " + err.Error())
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// newCall is a call on a piece of work, its request hashed with the model
// asked for.
func newCall(act, key, batchID, model string, req Request, precedents []int64, at time.Time) Call {
	return Call{
		Act: act, Key: key, At: at, BatchID: batchID, Model: model,
		Request: req, Precedents: precedents, RequestHash: RequestHash(req, model),
	}
}

// answered is the call with the response that came back, if one did: a
// response with nothing from the provider in it is none.
func (c Call) answered(res Response) Call {
	if res.Raw != nil || res.Output != nil {
		c.Response = &res
	}
	return c
}

// ended is the call with its outcome.
func (c Call) ended(o Outcome, why error) Call {
	c.Outcome = o
	if why != nil {
		c.Why = why.Error()
	}
	return c
}

// modelOf is the model the act asks for, by modelFor; empty if there is no
// way to know.
func modelOf(modelFor func(string) string, act string) string {
	if modelFor == nil {
		return ""
	}
	return modelFor(act)
}
