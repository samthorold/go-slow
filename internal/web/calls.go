package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/samthorold/go-slow/internal/editor"
)

// Calls answers the queries a kept call's page needs.
type Calls interface {
	// Call is one kept call, reporting whether the Record holds it.
	Call(ctx context.Context, id int64) (editor.Call, bool, error)
	// CallsOn are the calls an act made on a piece of work, in the order
	// they were kept.
	CallsOn(ctx context.Context, act, key string) ([]editor.Call, error)
	// StoryBriefingCitations are the Developments the day's Briefing over
	// Stories cites as it was written, each once, with the calls behind
	// them.
	StoryBriefingCitations(ctx context.Context, day string) ([]editor.BriefingCitation, error)
	// BriefingTrail is each Span the day's Briefing over Spans cites, as
	// written, with the reading and relating calls behind it.
	BriefingTrail(ctx context.Context, day string) ([]editor.CitedSpan, error)
	// Conversation is one kept conversation with its turns, reporting
	// whether the Record holds it.
	Conversation(ctx context.Context, id int64) (editor.KeptConversation, bool, error)
}

// callTmpl is a kept call's page.
var callTmpl = page(noIndex, "call.html", "outcome.html")

// callPage is the view model for a kept call's page: its request, its
// response, what the act's checks said, and every attempt at the same work.
type callPage struct {
	Act      string
	Work     string
	WorkPage string
	At       string
	Batch    string
	Model    string
	Hash     string
	// The request as sent.
	Instructions string
	Schema       string
	Input        string
	Precedents   string
	// The response as returned, if one came.
	Answered      bool
	ReportedModel string
	StopReason    string
	Tokens        tokens
	Output        string
	Raw           string
	Outcome       outcome
	Why           string
	Attempts      []attemptRow
	// AttemptTokens are every attempt's tokens, summed.
	AttemptTokens tokens
}

// tokens are a call's usage as shown: tokens, never money, since prices
// change.
type tokens struct {
	Input, Output, CacheRead, Total int64
}

func tokensOf(u editor.Usage) tokens {
	return tokens{u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.InputTokens + u.OutputTokens + u.CacheReadTokens}
}

func (t tokens) add(u tokens) tokens {
	return tokens{t.Input + u.Input, t.Output + u.Output, t.CacheRead + u.CacheRead, t.Total + u.Total}
}

// outcome is what came of a call or a turn as shown: its word, and the
// status hue of its verdict (Hue, the class suffix after "status-"), which
// the stylesheet's header maps: applied is supported, refused (by the act's
// checks) undecided, failed refuted. A turn that continued the conversation
// is no verdict, and takes no hue.
type outcome struct {
	Word, Hue string
}

func outcomeOf(o editor.Outcome) outcome {
	hue := map[editor.Outcome]string{editor.Applied: "supported", editor.Refused: "undecided", editor.Failed: "refuted"}[o]
	return outcome{Word: string(o), Hue: hue}
}

type attemptRow struct {
	Page    string
	At      string
	Outcome outcome
	Why     string
	// This is the attempt the page is about.
	This bool
}

// callPath is a kept call's page; empty for none.
func callPath(id int64) string {
	if id == 0 {
		return ""
	}
	return "/calls/" + strconv.FormatInt(id, 10)
}

// briefingAct is the act of writing the Briefing, whose work is keyed by its
// day.
const briefingAct = "briefing"

// workPage is the page of the work an act's call was on: the day's Briefing
// for writing it; otherwise an Item, keyed by its Source and entry a line
// apart, as the acts on Items key their work.
func workPage(act, key string) string {
	if act == briefingAct {
		return briefingPath(key)
	}
	source, entry, ok := strings.Cut(key, "\n")
	if !ok {
		return ""
	}
	return itemPath(source, entry)
}

// pretty is JSON indented to read, or as it is if it is not JSON.
func pretty(b []byte) string {
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		return string(b)
	}
	return out.String()
}

// call is a kept call's page. Checking how something was made is not
// reading it, so opening it records no open.
func (a *app) call(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, found, err := a.Calls.Call(r.Context(), id)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	attempts, err := a.Calls.CallsOn(r.Context(), c.Act, c.Key)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	loc := a.Now().Location()
	schema, _ := json.Marshal(c.Request.Schema)
	p := callPage{
		Act: c.Act, Work: strings.ReplaceAll(c.Key, "\n", " "), WorkPage: workPage(c.Act, c.Key),
		At: when(c.At, loc), Batch: c.BatchID, Model: c.Model, Hash: c.RequestHash,
		Instructions: c.Request.Instructions, Schema: pretty(schema), Input: c.Request.Input,
		Outcome: outcomeOf(c.Outcome), Why: c.Why,
	}
	var ps []string
	for _, f := range c.Precedents {
		ps = append(ps, strconv.FormatInt(f, 10))
	}
	p.Precedents = strings.Join(ps, ", ")
	if res := c.Response; res != nil {
		p.Answered, p.ReportedModel, p.StopReason = true, res.Model, res.StopReason
		p.Tokens, p.Output, p.Raw = tokensOf(res.Usage), pretty(res.Output), pretty(res.Raw)
	}
	for _, at := range attempts {
		row := attemptRow{Page: callPath(at.ID), At: when(at.At, loc), Outcome: outcomeOf(at.Outcome), Why: at.Why, This: at.ID == c.ID}
		p.Attempts = append(p.Attempts, row)
		if at.Response != nil {
			p.AttemptTokens = p.AttemptTokens.add(tokensOf(at.Response.Usage))
		}
	}
	a.render(w, r, callTmpl, p)
}
