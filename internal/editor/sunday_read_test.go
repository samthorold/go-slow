package editor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// sundayMorning is the Sunday after saturday, past the Briefing's time.
var sundayMorning = saturday.AddDate(0, 0, 1).Add(time.Hour)

// offer is an Item published before the speech: S1 "The PM will argue for
// more powers for the regions." and S2 "Scotland is one of the most
// centralised nations in Europe."
var offer = collect.Item{
	SourceID: number10.ID, EntryID: "offer", Title: "PM to set out offer to Holyrood",
	Link:      "https://www.gov.uk/offer",
	Content:   "<p>The PM will argue for more powers for the regions. Scotland is one of the most centralised nations in Europe.</p>",
	Published: time.Date(2026, time.October, 14, 9, 0, 0, 0, time.UTC), Attribution: collect.OGL, CollectedAt: saturday,
}

const offerReading = `{"names": [{"at": {"span": 2, "words": "Scotland"}, "kind": "place", "name": "Scotland", "office": ""}],
  "refers_to": [], "rejected": [], "speakers": []}`

// byInput answers each request with the output kept under a word its input
// holds.
type byInput map[string]string

func (m byInput) Complete(_ context.Context, r editor.Request) (editor.Response, error) {
	for word, out := range m {
		if strings.Contains(r.Input, word) {
			return editor.Response{Output: json.RawMessage(out), Model: "fake"}, nil
		}
	}
	return editor.Response{}, fmt.Errorf("no answer for:\n%s", r.Input)
}

// teller writes the Briefing telling, in one statement saying text, the
// Span shown with the words tell, its Story gathered of the Spans shown
// with the words in story.
type teller struct {
	text, tell string
	story      []string
}

var shownToTell = regexp.MustCompile(`(?m)^([SE]\d+)(?: \(spoken by [^)]*\))?: (.*)$`)

func (f teller) Complete(_ context.Context, req editor.Request) (editor.Response, error) {
	ref := func(words string) (string, error) {
		for _, m := range shownToTell.FindAllStringSubmatch(req.Input, -1) {
			if strings.Contains(m[2], words) {
				return m[1], nil
			}
		}
		return "", fmt.Errorf("no Span shown with %q:\n%s", words, req.Input)
	}
	told, err := ref(f.tell)
	if err != nil {
		return editor.Response{}, err
	}
	story := []string{}
	for _, w := range f.story {
		r, err := ref(w)
		if err != nil {
			return editor.Response{}, err
		}
		story = append(story, r)
	}
	out, err := json.Marshal(map[string]any{"statements": []any{map[string]any{"spans": []string{told},
		"stories": []any{map[string]any{"span": told, "story": story}}, "text": f.text}}})
	return editor.Response{Output: out, Model: "fake"}, err
}

// addressIn is the address of the Span of it whose words hold words.
func addressIn(t *testing.T, sourceID, entryID, content, words string) editor.Address {
	t.Helper()
	for _, sp := range cut.Sentences(content) {
		if strings.Contains(cut.Words(content, sp), words) {
			return editor.Address{SourceID: sourceID, EntryID: entryID, Span: sp}
		}
	}
	t.Fatalf("no Span of %s holds %q", entryID, words)
	return editor.Address{}
}

// The Spans the told week rests on.
var (
	backInScotland = func(t *testing.T) editor.Address {
		return addressIn(t, speech.SourceID, speech.EntryID, speech.Content, "back in Scotland")
	}
	centralised = func(t *testing.T) editor.Address {
		return addressIn(t, offer.SourceID, offer.EntryID, offer.Content, "centralised")
	}
)

// toldWeek is a Record whose Saturday Briefing told the speech's "It’s good
// to be back in Scotland, to be here in Motherwell.", which follows the offer's "Scotland is one of the
// most centralised nations in Europe.", its Story gathered with it. Both
// Items are read, naming Scotland.
func toldWeek(t *testing.T) *sqlite.Store {
	t.Helper()
	ctx := t.Context()
	store := record(t)
	if _, err := store.AddItems(ctx, []collect.Item{offer}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(store).CutNew(ctx, saturday); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	reading := byInput{"Motherwell": goodReading, "centralised": offerReading}
	if err := editor.New(editor.Deps{Store: store, Model: reading, Log: log}).ReadWaiting(ctx, saturday); err != nil {
		t.Fatal(err)
	}
	if err := editor.NewLookups(editor.LookupDeps{Store: store, Wikidata: noMatch{}, Log: log}).LookUpWaiting(ctx, saturday); err != nil {
		t.Fatal(err)
	}
	r, _, err := store.NewestReading(ctx, speech.SourceID, speech.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.KeepRelating(ctx, editor.Relating{SourceID: speech.SourceID, EntryID: speech.EntryID, ReadingID: r.ID,
		Judge: editor.Judge{Act: "relating", Version: 1}, At: saturday,
		Relations: []editor.Relation{{Kind: editor.Follows, From: backInScotland(t), To: centralised(t)}}}); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Deps{Store: store, Briefings: store, Log: log,
		BriefingModel: teller{text: "The Prime Minister was back in Scotland.", tell: "back in Scotland", story: []string{"centralised"}}})
	if err := ed.Brief(ctx, saturday.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if b, found, err := store.Briefing(ctx, "2026-10-24"); err != nil || !found || len(b.Statements) != 1 {
		t.Fatalf("the week's Briefing: %+v %v %v", b, found, err)
	}
	return store
}

// fakeAgent stands in for a model behind the conversation port: it answers
// each turn with the next of its scripted turns, and keeps the requests it
// was sent. Run out of script, it ends its turn saying nothing.
type fakeAgent struct {
	turns []editor.Turn
	// then, if set, is every turn after the script's.
	then *editor.Turn
	sent []editor.ConversationRequest
}

func (f *fakeAgent) Turn(_ context.Context, r editor.ConversationRequest) (editor.Turn, error) {
	f.sent = append(f.sent, r)
	n := len(f.sent) - 1
	if n >= len(f.turns) && f.then == nil {
		return editor.Turn{Model: "fake-agent"}, nil
	}
	var t editor.Turn
	if n < len(f.turns) {
		t = f.turns[n]
	} else {
		t = *f.then
	}
	if t.Model == "" {
		t.Model = "fake-agent"
	}
	return t, nil
}

// calls is a turn that calls tools, each given as name and JSON input.
func calls(nameInput ...string) editor.Turn {
	var t editor.Turn
	for i := 0; i+1 < len(nameInput); i += 2 {
		t.Calls = append(t.Calls, editor.ToolCall{
			ID: "call-" + strconv.Itoa(i/2+1), Name: nameInput[i], Input: json.RawMessage(nameInput[i+1]),
		})
	}
	t.Usage = editor.Usage{InputTokens: 1000, OutputTokens: 100}
	return t
}

// submits is a turn that submits the read.
func submits(read string) editor.Turn { return calls("submit_read", read) }

// reader is an Editor over store that writes the Sunday read with agent.
func reader(store *sqlite.Store, agent editor.Conversation) (*editor.Editor, *bytes.Buffer) {
	return readerWith(store, agent, &fakeWikidata{})
}

// readerWith is reader looking things up in wd.
func readerWith(store *sqlite.Store, agent editor.Conversation, wd editor.Wikidata) (*editor.Editor, *bytes.Buffer) {
	var logs bytes.Buffer
	return editor.New(editor.Deps{
		Store:           store,
		SundayReads:     store,
		SundayReadModel: agent,
		Wikidata:        wd,
		Log:             slog.New(slog.NewTextHandler(&logs, nil)),
	}), &logs
}

// citesTheTold is a read citing the Span the week's Briefing told: the
// first shown, P1.
const citesTheTold = `{"statements": [{"text": "Scotland's money came with an argument about centralisation.", "cites": ["P1"], "stories": []}],
	"assessments": []}`

const uncited = `{"statements": [{"text": "Scotland is the true north.", "cites": [], "stories": []}], "assessments": []}`

const citesWhatItWasNotShown = `{"statements": [{"text": "Scotland gets £2 billion.", "cites": ["P99"], "stories": []}], "assessments": []}`

// toolResult is what the agent was told of its turn n's call i (from 0).
func toolResult(t *testing.T, agent *fakeAgent, n, i int) editor.ToolResult {
	t.Helper()
	if len(agent.sent) <= n {
		t.Fatalf("the agent took %d turns", len(agent.sent))
	}
	msgs := agent.sent[n].Messages
	res := msgs[len(msgs)-1].Results
	if len(res) <= i {
		t.Fatalf("results = %+v", res)
	}
	return res[i]
}

func TestOnSundayTheReadIsWrittenFromWhatTheWeeksBriefingsToldAndKept(t *testing.T) {
	store := toldWeek(t)
	agent := &fakeAgent{turns: []editor.Turn{submits(citesTheTold)}}
	ed, logs := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	if len(agent.sent) == 0 {
		t.Fatal("the agent was never called")
	}
	in := agent.sent[0].Messages[0].User
	for _, want := range []string{"Saturday 24 October 2026", "The Prime Minister was back in Scotland.",
		"P1", "It’s good to be back in Scotland", "PM speech in Scotland: 16 October 2026", "Scotland"} {
		if !strings.Contains(in, want) {
			t.Errorf("the agent is not shown %q:\n%s", want, in)
		}
	}
	r, found, err := store.SundayRead(t.Context(), "2026-10-25")
	if err != nil || !found {
		t.Fatalf("SundayRead: %v %v\n%s", found, err, logs)
	}
	if !r.WrittenAt.Equal(sundayMorning) || r.Judge.Model != "fake-agent" || r.Judge.Act != "sunday_read" ||
		r.Judge.Version != editor.SundayReadVersion {
		t.Errorf("written at %v by %+v", r.WrittenAt, r.Judge)
	}
	if len(r.Statements) != 1 || r.Statements[0].Text != "Scotland's money came with an argument about centralisation." {
		t.Fatalf("statements = %+v", r.Statements)
	}
	// The statement traces to the Span it cites, at its address in its Item.
	cites := r.Statements[0].Cites
	if len(cites) != 1 || cites[0].Kind != editor.CitesSpan || cites[0].Span.At != backInScotland(t) ||
		cites[0].Span.Item.Title != speech.Title || cites[0].Span.ReadingID == 0 {
		t.Errorf("cites = %+v", cites)
	}
}
