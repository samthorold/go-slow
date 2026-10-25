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
)

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
	b, err := json.Marshal(struct {
		Instructions string         `json:"instructions"`
		Input        string         `json:"input"`
		Model        string         `json:"model"`
		Schema       map[string]any `json:"schema"`
	}{r.Instructions, r.Input, model, r.Schema})
	if err != nil {
		// A schema is built of JSON values; one that can't be encoded is a
		// programming error, never input.
		panic("editor: request schema is not JSON: " + err.Error())
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
