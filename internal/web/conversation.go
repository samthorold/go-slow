package web

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/samthorold/go-slow/internal/editor"
)

// conversationTmpl is a kept conversation's page.
var conversationTmpl = page(noIndex, "conversation.html")

// conversationPath is a kept conversation's page, at turn n if n is not
// zero; empty for no conversation.
func conversationPath(id int64, n int) string {
	if id == 0 {
		return ""
	}
	p := "/conversations/" + strconv.FormatInt(id, 10)
	if n != 0 {
		p += "#" + turnAnchor(n)
	}
	return p
}

func turnAnchor(n int) string { return "turn-" + strconv.Itoa(n) }

// conversationPage is the view model for a kept conversation's page: the
// act's instructions and tools, then each turn, what it sent and what came
// back.
type conversationPage struct {
	Act          string
	Day          string
	ReadPath     string
	At           string
	Model        string
	Outcome      string
	Why          string
	Instructions string
	Tools        string
	Turns        []turnRow
	// Tokens are every turn's tokens, summed.
	Tokens tokens
}

type turnRow struct {
	N       int
	Anchor  string
	At      string
	Hash    string
	Sent    []sentRow
	Outcome string
	Why     string
	// The model's turn, if one came.
	Answered      bool
	ReportedModel string
	StopReason    string
	Tokens        tokens
	Text          string
	Calls         []calledRow
	Raw           string
}

// sentRow is a message sent new with a turn: the act's text, or a tool's
// result.
type sentRow struct {
	Text    string
	CallID  string
	IsError bool
}

type calledRow struct {
	ID, Name, Input string
}

// conversation is a kept conversation's page. Checking how something was
// made is not reading it, so opening it records no open.
func (a *app) conversation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, found, err := a.Calls.Conversation(r.Context(), id)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	loc := a.Now().Location()
	tools, _ := json.Marshal(c.Tools)
	p := conversationPage{
		Act: c.Act, Day: c.Key, At: when(c.At, loc), Model: c.Model, Outcome: string(c.Outcome), Why: c.Why,
		Instructions: c.Instructions, Tools: pretty(tools),
	}
	if c.Act == "sunday_read" {
		p.ReadPath = sundayReadPath(c.Key)
	}
	for _, t := range c.Turns {
		row := turnRow{N: t.N, Anchor: turnAnchor(t.N), At: when(t.At, loc), Hash: t.RequestHash,
			Outcome: string(t.Outcome), Why: t.Why}
		for _, m := range t.Sent {
			if m.Results == nil {
				row.Sent = append(row.Sent, sentRow{Text: m.User})
			}
			for _, res := range m.Results {
				row.Sent = append(row.Sent, sentRow{Text: res.Content, CallID: res.CallID, IsError: res.IsError})
			}
		}
		if res := t.Response; res != nil {
			row.Answered, row.ReportedModel, row.StopReason = true, res.Model, res.StopReason
			row.Tokens, row.Text, row.Raw = tokensOf(res.Usage), res.Text, pretty(res.Raw)
			for _, call := range res.Calls {
				row.Calls = append(row.Calls, calledRow{call.ID, call.Name, pretty(call.Input)})
			}
			p.Tokens = p.Tokens.add(row.Tokens)
		}
		p.Turns = append(p.Turns, row)
	}
	a.render(w, r, conversationTmpl, p)
}

// readConversation is the page of the conversation that wrote read, and of
// the turn that submitted it, which reached its Assessments: the read's
// last turn, since a read is written by the first submission that passes.
func readConversation(read editor.SundayRead) (conversation, reached string) {
	return conversationPath(read.ConversationID, 0), conversationPath(read.ConversationID, read.Turns)
}
