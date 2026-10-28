package editor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/feedback"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// sundayMorning is the Sunday after saturday, past the Briefing's time.
var sundayMorning = saturday.AddDate(0, 0, 1).Add(time.Hour)

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

const citesTheSpeech = `{"statements": [
	{"text": "The Prime Minister took his case to Motherwell.", "cites": ["S1:F1"]}]}`

func TestOnSundayTheReadIsWrittenFromTheWeeksStoriesAndKept(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	agent := &fakeAgent{turns: []editor.Turn{submits(citesTheSpeech)}}
	ed, logs := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	if len(agent.sent) == 0 {
		t.Fatal("the agent was never called")
	}
	in := agent.sent[0].Messages[0].User
	for _, want := range []string{"S1: Andy Burnham in Scotland", "S1:F1", "Andy Burnham gave a speech in Motherwell."} {
		if !strings.Contains(in, want) {
			t.Errorf("the agent is not shown %q:\n%s", want, in)
		}
	}
	r, found, err := store.SundayRead(t.Context(), "2026-10-25")
	if err != nil || !found {
		t.Fatalf("SundayRead: %v %v\n%s", found, err, logs)
	}
	if !r.WrittenAt.Equal(sundayMorning) || r.Model != "fake-agent" {
		t.Errorf("written at %v by %q", r.WrittenAt, r.Model)
	}
	if len(r.Statements) != 1 || r.Statements[0].Text != "The Prime Minister took his case to Motherwell." {
		t.Fatalf("statements = %+v", r.Statements)
	}
	// The statement traces to the Development it cites, its Fact and Item.
	cites := r.Statements[0].Cites
	if len(cites) != 1 || cites[0].Kind != editor.CitesDevelopment || cites[0].Story.ID != 1 ||
		cites[0].Fact.Statement != "Andy Burnham gave a speech in Motherwell." || cites[0].Fact.ItemTitle != speech.Title {
		t.Errorf("cites = %+v", cites)
	}
}

// earlierWeek is when the call readout was placed: before the week the
// Sunday read covers.
var earlierWeek = saturday.AddDate(0, 0, -10)

// withAnEarlierCall adds to the Record a Fact from the call readout, placed
// in the speech's Story a week and more before the Sunday read: in the
// Record, but not among the week's Developments.
func withAnEarlierCall(t *testing.T, store *sqlite.Store) {
	t.Helper()
	placeTheCall(t, store, earlierWeek)
}

// placeTheCall reads the call readout into a Fact, F2, placed at placedAt in
// the speech's Story.
func placeTheCall(t *testing.T, store *sqlite.Store, placedAt time.Time) {
	t.Helper()
	ctx := t.Context()
	// A Topic on Finland ties the readout, so its Story is selected.
	if _, errs, err := interest.New(store).Add(ctx, interest.Draft{Name: "Finland", Depth: "following", Terms: "Finland"}); err != nil || errs != nil {
		t.Fatalf("add Topic: %v %v", errs, err)
	}
	if err := store.SaveReading(ctx, editor.Reading{
		SourceID: number10.ID, EntryID: "call",
		Entities: []editor.Entity{{Name: "Andy Burnham", Kind: "person"}, {Name: "Finland", Kind: "place"}},
		Facts: []editor.Fact{{
			SourceID: number10.ID, EntryID: "call", Statement: "Andy Burnham spoke to the President of Finland.",
			AsAt: oct16.AddDate(0, 0, -2), Quote: "PM call",
		}},
		Model: "fake",
	}, placedAt); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlacing(ctx, editor.Placing{
		SourceID: number10.ID, EntryID: "call", Facts: []int64{2},
		Developments: []editor.Placement{{FactID: 2, StoryID: 1}}, Model: "fake",
	}, placedAt); err != nil {
		t.Fatal(err)
	}
}

func TestTheAgentsToolsQueryTheRecordBackThroughTime(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	withAnEarlierCall(t, store)
	connect(t, store, speech.EntryID, 1, editor.Role{Entity: "Andy Burnham", Role: "actor"})
	connect(t, store, "call", 2, editor.Role{Entity: "Andy Burnham", Role: "actor"})
	agent := &fakeAgent{turns: []editor.Turn{
		calls("story", `{"story": "S1"}`, "facts_about", `{"entity": "Andy Burnham", "before": "2026-10-16"}`),
		submits(`{"statements": [{"text": "Before Motherwell, the Prime Minister called Finland.", "cites": ["S1:F2", "F2"]}]}`),
	}}
	ed, logs := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	// The week's Developments are what the agent starts from; the earlier
	// call is not among them.
	if in := agent.sent[0].Messages[0].User; strings.Contains(in, "Finland") {
		t.Errorf("the agent is shown a Development from before the week:\n%s", in)
	}
	if len(agent.sent) < 2 {
		t.Fatalf("the agent took %d turns", len(agent.sent))
	}
	msgs := agent.sent[1].Messages
	res := msgs[len(msgs)-1].Results
	if len(res) != 2 {
		t.Fatalf("results = %+v, want one per call", res)
	}
	// Its Story's whole Timeline, and the Facts about an Entity before a
	// date, newest first.
	for i, want := range []string{"S1:F2 (as at 2026-10-14", "F2 (as at 2026-10-14"} {
		if res[i].CallID != "call-"+strconv.Itoa(i+1) || res[i].IsError || !strings.Contains(res[i].Content, want) {
			t.Errorf("result %d = %+v, want it to show %q", i+1, res[i], want)
		}
	}
	if strings.Contains(res[1].Content, "Motherwell") {
		t.Errorf("Facts about an Entity before a date include a later one:\n%s", res[1].Content)
	}
	r, found, err := store.SundayRead(t.Context(), "2026-10-25")
	if err != nil || !found {
		t.Fatalf("SundayRead: %v %v\n%s", found, err, logs)
	}
	if cites := r.Statements[0].Cites; len(cites) != 2 || cites[0].Kind != editor.CitesDevelopment ||
		cites[1].Kind != editor.CitesFact || cites[1].Fact.Statement != "Andy Burnham spoke to the President of Finland." {
		t.Errorf("cites = %+v", cites)
	}
}

// connect gives each Entity named in roles its role in the Fact id, as
// connecting would: the Entities the Fact involves.
func connect(t *testing.T, store *sqlite.Store, entryID string, id int64, roles ...editor.Role) {
	t.Helper()
	for i := range roles {
		roles[i].FactID = id
	}
	if err := store.SaveConnecting(t.Context(), editor.Connecting{
		SourceID: number10.ID, EntryID: entryID, Facts: []int64{id}, Roles: roles, Model: "fake",
	}, saturday); err != nil {
		t.Fatal(err)
	}
}

func TestFactsAboutAnEntityAreTheFactsInvolvingItInARoleNotThoseMentioningIt(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	withAnEarlierCall(t, store)
	// The speech's Item mentions Motherwell, but connecting gave it no role
	// in the speech: the speech involves Andy Burnham alone.
	connect(t, store, speech.EntryID, 1, editor.Role{Entity: "Andy Burnham", Role: "actor"})
	// And the call, mentioning both, involves Finland alone.
	connect(t, store, "call", 2, editor.Role{Entity: "Finland", Role: "undergoer"})
	agent := &fakeAgent{turns: []editor.Turn{
		calls("facts_about", `{"entity": "Motherwell", "before": ""}`, "facts_about", `{"entity": "Andy Burnham", "before": ""}`,
			"facts_about", `{"entity": "Finland", "before": ""}`),
		submits(citesTheSpeech),
	}}
	ed, logs := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatalf("%v\n%s", err, logs)
	}

	msgs := agent.sent[1].Messages
	res := msgs[len(msgs)-1].Results
	if len(res) != 3 {
		t.Fatalf("results = %+v, want one per call", res)
	}
	if !strings.Contains(res[0].Content, "no Facts involving") {
		t.Errorf("Facts about an Entity only mentioned include one:\n%s", res[0].Content)
	}
	if !strings.Contains(res[1].Content, "Andy Burnham gave a speech in Motherwell.") {
		t.Errorf("Facts about Andy Burnham lack the speech, which involves him:\n%s", res[1].Content)
	}
	if strings.Contains(res[1].Content, "Andy Burnham spoke to the President of Finland.") {
		t.Errorf("Facts about Andy Burnham include the call, which only mentions him:\n%s", res[1].Content)
	}
	if !strings.Contains(res[2].Content, "Andy Burnham spoke to the President of Finland.") {
		t.Errorf("Facts about Finland lack the call, which involves it:\n%s", res[2].Content)
	}
}

func TestTheStoryToolShowsEachFactWithTheEntitiesItInvolvesInTheirRoles(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	withAnEarlierCall(t, store)
	// The speech mentions Andy Burnham and Motherwell but involves him
	// alone; the call mentions Andy Burnham and Finland but involves Finland
	// alone.
	connect(t, store, speech.EntryID, 1, editor.Role{Entity: "Andy Burnham", Role: "actor"})
	connect(t, store, "call", 2, editor.Role{Entity: "Finland", Role: "undergoer"})
	agent := &fakeAgent{turns: []editor.Turn{calls("story", `{"story": "S1"}`), submits(citesTheSpeech)}}
	ed, logs := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatalf("%v\n%s", err, logs)
	}

	if in := agent.sent[0].Messages[0].User; !strings.Contains(in, "Andy Burnham gave a speech in Motherwell. [Andy Burnham: actor]\n") {
		t.Errorf("the week's Development is not shown with what it involves:\n%s", in)
	}
	msgs := agent.sent[1].Messages
	story := msgs[len(msgs)-1].Results[0].Content
	for _, want := range []string{
		"Entities: Andy Burnham; Finland\n",
		"Andy Burnham gave a speech in Motherwell. [Andy Burnham: actor]\n",
		"Andy Burnham spoke to the President of Finland. [Finland: undergoer]\n",
	} {
		if !strings.Contains(story, want) {
			t.Errorf("the story tool does not show %q:\n%s", want, story)
		}
	}
}

func TestFactsAboutCausesAndAssessmentsShowEachFactWithTheEntitiesItInvolvesInTheirRoles(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	ed, logs := reader(store, &fakeAgent{turns: []editor.Turn{submits(assessesTheSpeech)}})
	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatalf("%v\n%s", err, logs)
	}
	nextSunday := sundayMorning.AddDate(0, 0, 7)
	placeTheCall(t, store, nextSunday.Add(-24*time.Hour))
	connect(t, store, speech.EntryID, 1, editor.Role{Entity: "Andy Burnham", Role: "actor"})
	if err := store.SaveConnecting(t.Context(), editor.Connecting{
		SourceID: number10.ID, EntryID: "call", Facts: []int64{2}, Model: "fake",
		Roles:  []editor.Role{{FactID: 2, Entity: "Finland", Role: "undergoer"}},
		Causes: []editor.Cause{{CauseID: 2, EffectID: 1}},
	}, saturday); err != nil {
		t.Fatal(err)
	}
	agent := &fakeAgent{turns: []editor.Turn{
		calls("story", `{"story": "S1"}`, "facts_about", `{"entity": "Finland", "before": ""}`, "assessments", `{}`),
		submits(citesTheSpeech),
	}}
	ed, logs = reader(store, agent)

	if err := ed.SundayRead(t.Context(), nextSunday); err != nil {
		t.Fatalf("%v\n%s", err, logs)
	}

	msgs := agent.sent[1].Messages
	res := msgs[len(msgs)-1].Results
	if len(res) != 3 {
		t.Fatalf("results = %+v, want one per call", res)
	}
	speechShown := "Andy Burnham gave a speech in Motherwell. [Andy Burnham: actor]"
	callShown := "Andy Burnham spoke to the President of Finland. [Finland: undergoer]"
	for i, want := range []string{
		callShown + " led to F1 (as at 2026-10-16, from \"PM speech in Scotland: 16 October 2026\"): " + speechShown + "\n",
		callShown + "\n",
		"rests on F1 (as at 2026-10-16, from \"PM speech in Scotland: 16 October 2026\"): " + speechShown + "\n",
	} {
		if !strings.Contains(res[i].Content, want) {
			t.Errorf("result %d does not show %q:\n%s", i+1, want, res[i].Content)
		}
	}
}

func TestStoriesAnnouncedTogetherShowTheItemThatConnectsThem(t *testing.T) {
	// An update's two Facts: one moves on the speech's Story, the other
	// starts a Story of its own.
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	update := collect.Item{SourceID: number10.ID, EntryID: "update", Title: "Cost of Living update",
		Link: "https://www.gov.uk/update", Published: laterItem.Published, Attribution: collect.OGL, CollectedAt: saturday}
	if _, err := store.AddItems(t.Context(), []collect.Item{update}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveReading(t.Context(), editor.Reading{
		SourceID: update.SourceID, EntryID: update.EntryID, Model: "fake",
		Entities: []editor.Entity{{Name: "Andy Burnham", Kind: "person"}},
		Facts: []editor.Fact{
			{Statement: "Andy Burnham announced a Scottish fund.", AsAt: oct16.AddDate(0, 0, 1), Quote: "fund"},
			{Statement: "Carer's Allowance was reformed.", AsAt: oct16.AddDate(0, 0, 1), Quote: "reformed"},
		},
	}, saturday); err != nil {
		t.Fatal(err)
	}
	ed, logs := placer(store, &fakeModel{output: `{"new_stories": [{"ref": "N1", "title": "Carer's Allowance reform"}],
	  "facts": [{"fact": "F1", "developments_in": ["S1"]}, {"fact": "F2", "developments_in": ["N1"]}]}`})
	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	agent := &fakeAgent{turns: []editor.Turn{calls("story", `{"story": "S1"}`), submits(citesTheSpeech)}}
	ed, _ = reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatalf("%v\n%s", err, logs)
	}

	msgs := agent.sent[1].Messages
	story := msgs[len(msgs)-1].Results[0].Content
	_, connected, _ := strings.Cut(story, "Connected Stories:\n")
	if want := "  S2: Carer's Allowance reform\n    announced together in \"Cost of Living update\"\n"; !strings.Contains(connected, want) {
		t.Errorf("the connected Stories do not show %q:\n%s\n%s", want, story, logs)
	}
}

func TestConnectedStoriesShowEachFactWithTheEntitiesItInvolvesInTheirRoles(t *testing.T) {
	// The readout is in a Story of its own; a visit, F3, moves on both
	// Stories, which it and Andy Burnham as actor connect.
	store := twoStories(t, inANewStory)
	visit := collect.Item{SourceID: number10.ID, EntryID: "visit", Title: "PM visit to Glasgow",
		Link: "https://www.gov.uk/visit", Published: laterItem.Published.Add(time.Hour), Attribution: collect.OGL, CollectedAt: saturday}
	if _, err := store.AddItems(t.Context(), []collect.Item{visit}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveReading(t.Context(), editor.Reading{
		SourceID: visit.SourceID, EntryID: visit.EntryID, Model: "fake",
		Entities: []editor.Entity{{Name: "Andy Burnham", Kind: "person"}, {Name: "Glasgow", Kind: "place"}},
		Facts: []editor.Fact{{Statement: "Andy Burnham visited Glasgow.", AsAt: oct16.AddDate(0, 0, 2),
			Quote: "visited Glasgow"}},
	}, saturday); err != nil {
		t.Fatal(err)
	}
	ed, logs := placer(store, &fakeModel{output: `{"new_stories": [], "facts": [{"fact": "F1", "developments_in": ["S1", "S2"]}]}`})
	if err := ed.PlaceWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	connect(t, store, laterItem.EntryID, 2, editor.Role{Entity: "Andy Burnham", Role: "actor"}, editor.Role{Entity: "Edinburgh", Role: "place"})
	connect(t, store, visit.EntryID, 3, editor.Role{Entity: "Andy Burnham", Role: "actor"})
	otherStories(t, store, 2)
	agent := &fakeAgent{turns: []editor.Turn{calls("story", `{"story": "S1"}`), submits(citesTheSpeech)}}
	ed, _ = reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatalf("%v\n%s", err, logs)
	}

	msgs := agent.sent[1].Messages
	story := msgs[len(msgs)-1].Results[0].Content
	_, connected, _ := strings.Cut(story, "Connected Stories:\n")
	for _, want := range []string{
		"shares S2:F3 (as at 2026-10-18, from \"PM visit to Glasgow\"): Andy Burnham visited Glasgow. [Andy Burnham: actor]\n",
		"Andy Burnham as actor here in S1:F3 (as at 2026-10-18, from \"PM visit to Glasgow\"): Andy Burnham visited Glasgow. [Andy Burnham: actor] and there in S2:F2 ",
		"Andy Burnham met the First Minister in Edinburgh. [Andy Burnham: actor; Edinburgh: place]\n",
	} {
		if !strings.Contains(connected, want) {
			t.Errorf("the connected Stories do not show %q:\n%s\n%s", want, story, logs)
		}
	}
}

func TestAFactInvolvingNoEntityIsShownWithNone(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	agent := &fakeAgent{turns: []editor.Turn{calls("story", `{"story": "S1"}`), submits(citesTheSpeech)}}
	ed, logs := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatalf("%v\n%s", err, logs)
	}

	msgs := agent.sent[1].Messages
	story := msgs[len(msgs)-1].Results[0].Content
	if !strings.Contains(story, "Andy Burnham gave a speech in Motherwell.\n") || strings.Contains(story, "Entities:") {
		t.Errorf("a Story whose Facts involve no Entity is shown with some:\n%s", story)
	}
}

func TestTheAgentLooksThingsUpInWikidataAndCitesTheLookup(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	wd := &fakeWikidata{
		search: map[string][]string{"Andy Burnham": {"Q123"}},
		items: map[string]editor.WikidataItem{"Q123": {
			ID: "Q123", Label: "Andy Burnham", Aliases: []string{"Andrew Burnham"}, InstanceOf: []string{"Q5"}, Revision: 42,
			Positions: []editor.Tenure{{Holder: "Q123", Office: "Q14211", Start: time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC), StartToTheDay: true}},
		}},
		labels: map[string]string{"Q123": "Andy Burnham", "Q14211": "Prime Minister of the United Kingdom", "Q5": "human"},
	}
	agent := &fakeAgent{turns: []editor.Turn{
		calls("wikidata_search", `{"text": "Andy Burnham"}`),
		calls("wikidata_item", `{"id": "Q123"}`),
		submits(`{"statements": [{"text": "Andy Burnham has been Prime Minister since July.", "cites": ["L1", "S1:F1"]}],
			"assessments": [], "stories": []}`),
	}}
	ed, logs := readerWith(store, agent, wd)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	result := func(turn int) string {
		msgs := agent.sent[turn].Messages
		return msgs[len(msgs)-1].Results[0].Content
	}
	if got := result(1); !strings.Contains(got, "Q123: Andy Burnham") {
		t.Errorf("the search shows:\n%s", got)
	}
	got := result(2)
	for _, want := range []string{"L1", "Q123", "revision 42", "human", "Prime Minister of the United Kingdom", "2026-07-20"} {
		if !strings.Contains(got, want) {
			t.Errorf("the item does not show %q:\n%s", want, got)
		}
	}
	r, found, err := store.SundayRead(t.Context(), "2026-10-25")
	if err != nil || !found {
		t.Fatalf("SundayRead: %v %v\n%s", found, err, logs)
	}
	// The lookup is kept, as the item and revision seen, under its licence.
	c := r.Statements[0].Cites[0]
	if c.Kind != editor.CitesLookup || c.Lookup.ID == 0 || c.Lookup.ItemID != "Q123" || c.Lookup.Revision != 42 ||
		c.Lookup.Reference != "Wikidata" || c.Lookup.Licence != editor.WikidataLicence || !c.Lookup.At.Equal(sundayMorning) {
		t.Errorf("cites %+v, want the lookup", c)
	}
}

func TestTheAgentIsNeverShownWhatFeedbackWithdrew(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	withAnEarlierCall(t, store)
	connect(t, store, "call", 2, editor.Role{Entity: "Andy Burnham", Role: "actor"})
	if _, errs, err := feedback.New(store).Give(t.Context(), feedback.Draft{Kind: "wrong", Level: "fact", Fact: "2"}, saturday); err != nil || errs != nil {
		t.Fatalf("give Feedback: %v %v", errs, err)
	}
	agent := &fakeAgent{turns: []editor.Turn{
		calls("story", `{"story": "S1"}`, "facts_about", `{"entity": "Andy Burnham", "before": ""}`),
		submits(`{"statements": [{"text": "The Prime Minister called Finland.", "cites": ["F2"]}], "assessments": [], "stories": []}`),
	}}
	ed, _ := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	msgs := agent.sent[1].Messages
	for _, res := range msgs[len(msgs)-1].Results {
		if strings.Contains(res.Content, "Finland") {
			t.Errorf("a withdrawn Fact is shown:\n%s", res.Content)
		}
	}
	// So it can't be cited either.
	if _, found, _ := store.SundayRead(t.Context(), "2026-10-25"); found {
		t.Error("a read citing a withdrawn Fact was published")
	}
}

func TestTheAgentHasNoToolThatWritesAFact(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	agent := &fakeAgent{turns: []editor.Turn{submits(citesTheSpeech)}}
	ed, _ := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	// It reads the Record and Wikidata; the one tool that writes is the
	// read's own submission, which holds no Facts.
	var names []string
	for _, tool := range agent.sent[0].Tools {
		names = append(names, tool.Name)
	}
	want := []string{"story", "facts_about", "assessments", "wikidata_search", "wikidata_item", "submit_read"}
	if !slices.Equal(names, want) {
		t.Errorf("tools = %v, want %v", names, want)
	}
	for _, tool := range agent.sent[0].Tools {
		if tool.Name != "submit_read" {
			continue
		}
		props := tool.Schema["properties"].(map[string]any)
		if _, ok := props["facts"]; ok {
			t.Error("the read can submit Facts")
		}
	}
}

func TestTheReadCanStartAStoryGatheringExistingDevelopments(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	withAnEarlierCall(t, store)
	agent := &fakeAgent{turns: []editor.Turn{
		calls("story", `{"story": "S1"}`),
		submits(`{"statements": [{"text": "The Prime Minister's month has been one of travel.", "cites": ["S1:F1", "S1:F2"]}],
			"assessments": [], "stories": [{"title": "The Prime Minister's travels", "developments": ["S1:F2", "S1:F1"]}]}`),
	}}
	ed, logs := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	r, found, err := store.SundayRead(t.Context(), "2026-10-25")
	if err != nil || !found {
		t.Fatalf("SundayRead: %v %v\n%s", found, err, logs)
	}
	if len(r.Stories) != 1 || r.Stories[0].Title != "The Prime Minister's travels" {
		t.Fatalf("Stories started = %+v", r.Stories)
	}
	// A wider story is a Story like any other, sharing its Developments.
	s, found, err := store.Story(t.Context(), r.Stories[0].ID)
	if err != nil || !found || len(s.Timeline) != 2 || s.Timeline[0].ID != 2 || s.Timeline[1].ID != 1 {
		t.Errorf("the new Story = %+v, %v, %v; want the call and the speech on its Timeline", s, found, err)
	}
	if old, _, _ := store.Story(t.Context(), 1); len(old.Timeline) != 2 {
		t.Errorf("the Story gathered from lost its Developments: %+v", old.Timeline)
	}
}

func TestAStoryTheReadStartsGathersOnlyDevelopmentsItWasShown(t *testing.T) {
	for name, stories := range map[string]string{
		"no Developments":            `[{"title": "Travels", "developments": []}]`,
		"no title":                   `[{"title": " ", "developments": ["S1:F1"]}]`,
		"a Development it not shown": `[{"title": "Travels", "developments": ["S1:F2"]}]`,
		"a Fact, not a Development":  `[{"title": "Travels", "developments": ["F1"]}]`,
	} {
		t.Run(name, func(t *testing.T) {
			store := record(t)
			readAndPlace(t, store, &fakeModel{output: startsAStory})
			withAnEarlierCall(t, store)
			ed, _ := reader(store, &fakeAgent{turns: []editor.Turn{submits(`{"statements": [{"text": "x", "cites": ["S1:F1"]}],
				"assessments": [], "stories": ` + stories + `}`)}})

			if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
				t.Fatal(err)
			}

			if _, found, _ := store.SundayRead(t.Context(), "2026-10-25"); found {
				t.Error("a read starting a bad Story was published")
			}
			if all, _ := store.ListStories(t.Context()); len(all) != 1 {
				t.Errorf("Stories = %+v, want none started", all)
			}
		})
	}
}

const assessesTheSpeech = `{"statements": [
	{"text": "The Prime Minister is courting Scotland.", "cites": ["N1", "S1:F1"]}],
	"assessments": [{"ref": "N1", "statement": "The Prime Minister is courting Scottish voters.", "facts": ["F1"], "supersedes": ""}],
	"stories": []}`

func TestAnAssessmentIsKeptWithTheFactsItCitesAndALaterOneSupersedesIt(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	ed, logs := reader(store, &fakeAgent{turns: []editor.Turn{submits(assessesTheSpeech)}})
	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	first, err := store.Assessments(t.Context(), 10)
	if err != nil || len(first) != 1 {
		t.Fatalf("Assessments = %+v, %v\n%s", first, err, logs)
	}
	a := first[0]
	// Kept with the Facts it cites, and dated: as at the Sunday it was made.
	if a.Statement != "The Prime Minister is courting Scottish voters." || a.AsAt.Format(time.DateOnly) != "2026-10-25" ||
		len(a.Facts) != 1 || a.Facts[0].Statement != "Andy Burnham gave a speech in Motherwell." || a.Day != "2026-10-25" {
		t.Errorf("Assessment = %+v", a)
	}
	r, _, _ := store.SundayRead(t.Context(), "2026-10-25")
	if c := r.Statements[0].Cites[0]; c.Kind != editor.CitesAssessment || c.Assessment.ID != a.ID {
		t.Errorf("the statement cites %+v, want the Assessment", c)
	}

	// A week on, the call to Finland moves the Story on, and the next read
	// finds the earlier Assessment and supersedes it.
	nextSunday := sundayMorning.AddDate(0, 0, 7)
	placeTheCall(t, store, nextSunday.Add(-24*time.Hour))
	agent := &fakeAgent{turns: []editor.Turn{
		calls("assessments", `{}`),
		submits(`{"statements": [{"text": "The Prime Minister looks abroad as well as north.", "cites": ["N1", "A1"]}],
			"assessments": [{"ref": "N1", "statement": "The Prime Minister is courting Scottish voters and Nordic allies.",
				"facts": ["F1", "F2"], "supersedes": "A1"}], "stories": []}`),
	}}
	ed, logs = reader(store, agent)
	if err := ed.SundayRead(t.Context(), nextSunday); err != nil {
		t.Fatal(err)
	}
	if msgs := agent.sent[1].Messages; !strings.Contains(msgs[len(msgs)-1].Results[0].Content, "A1 (as at 2026-10-25): The Prime Minister is courting Scottish voters.") {
		t.Errorf("the earlier Assessment is not shown:\n%+v", msgs[len(msgs)-1].Results)
	}

	all, err := store.Assessments(t.Context(), 10)
	if err != nil || len(all) != 2 {
		t.Fatalf("Assessments = %+v, %v\n%s", all, err, logs)
	}
	later, earlier := all[0], all[1]
	// The earlier is kept as it was, superseded, never rewritten.
	if earlier.ID != a.ID || earlier.Statement != a.Statement || !earlier.AsAt.Equal(a.AsAt) || len(earlier.Facts) != 1 ||
		earlier.SupersededBy != later.ID {
		t.Errorf("earlier = %+v, want it as it was, superseded by %d", earlier, later.ID)
	}
	if later.Supersedes != a.ID || later.AsAt.Format(time.DateOnly) != "2026-11-01" || len(later.Facts) != 2 {
		t.Errorf("later = %+v", later)
	}
}

func TestAWithdrawnAssessmentIsNotShownToALaterRead(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	ed, _ := reader(store, &fakeAgent{turns: []editor.Turn{submits(assessesTheSpeech)}})
	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}
	if _, errs, err := feedback.New(store).Give(t.Context(), feedback.Draft{Kind: "wrong", Level: "assessment", Assessment: "1"}, sundayMorning); err != nil || errs != nil {
		t.Fatalf("give Feedback: %v %v", errs, err)
	}
	nextSunday := sundayMorning.AddDate(0, 0, 7)
	placeTheCall(t, store, nextSunday.Add(-24*time.Hour))
	agent := &fakeAgent{turns: []editor.Turn{calls("assessments", `{}`)}}
	ed, _ = reader(store, agent)

	if err := ed.SundayRead(t.Context(), nextSunday); err != nil {
		t.Fatal(err)
	}

	msgs := agent.sent[1].Messages
	if got := msgs[len(msgs)-1].Results[0].Content; strings.Contains(got, "courting") {
		t.Errorf("a withdrawn Assessment is shown:\n%s", got)
	}
}

func TestAnAssessmentThatFailsTheChecksRejectsTheRead(t *testing.T) {
	for name, read := range map[string]string{
		"citing no Fact": `{"statements": [{"text": "x", "cites": ["N1"]}],
			"assessments": [{"ref": "N1", "statement": "Courting.", "facts": [], "supersedes": ""}], "stories": []}`,
		"citing a Fact not shown": `{"statements": [{"text": "x", "cites": ["N1"]}],
			"assessments": [{"ref": "N1", "statement": "Courting.", "facts": ["F99"], "supersedes": ""}], "stories": []}`,
		"superseding one not shown": `{"statements": [{"text": "x", "cites": ["N1"]}],
			"assessments": [{"ref": "N1", "statement": "Courting.", "facts": ["F1"], "supersedes": "A7"}], "stories": []}`,
		"cited but not made": `{"statements": [{"text": "x", "cites": ["N2"]}],
			"assessments": [{"ref": "N1", "statement": "Courting.", "facts": ["F1"], "supersedes": ""}], "stories": []}`,
	} {
		t.Run(name, func(t *testing.T) {
			store := record(t)
			readAndPlace(t, store, &fakeModel{output: startsAStory})
			ed, _ := reader(store, &fakeAgent{turns: []editor.Turn{submits(read)}})

			if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
				t.Fatal(err)
			}

			if _, found, _ := store.SundayRead(t.Context(), "2026-10-25"); found {
				t.Error("a read with a bad Assessment was published")
			}
			if as, _ := store.Assessments(t.Context(), 10); len(as) != 0 {
				t.Errorf("Assessments kept: %+v", as)
			}
		})
	}
}

const uncited = `{"statements": [{"text": "Scotland is the true north.", "cites": []}]}`

const citesWhatItWasNotShown = `{"statements": [{"text": "The Prime Minister spoke in Motherwell.", "cites": ["F99"]}]}`

func TestTheAgentsLoopIsBoundedInTurnsAndTokens(t *testing.T) {
	looking := calls("story", `{"story": "S1"}`)
	expensive := looking
	expensive.Usage = editor.Usage{InputTokens: editor.SundayReadTokenBudget / 3, OutputTokens: 1000}
	for name, tc := range map[string]struct {
		turn  editor.Turn
		turns int
	}{
		"turns":  {looking, editor.SundayReadMaxTurns},
		"tokens": {expensive, 3},
	} {
		t.Run(name, func(t *testing.T) {
			store := record(t)
			readAndPlace(t, store, &fakeModel{output: startsAStory})
			agent := &fakeAgent{then: &tc.turn}
			ed, _ := reader(store, agent)

			if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
				t.Fatal(err)
			}

			if len(agent.sent) != tc.turns {
				t.Errorf("the agent took %d turns, want it stopped after %d", len(agent.sent), tc.turns)
			}
			fs, err := store.SundayReadFailures(t.Context(), "2026-10-25")
			if err != nil || len(fs) != 1 || !strings.Contains(fs[0].Reason, name) {
				t.Errorf("failures = %+v, %v; want one naming the %s bound", fs, err, name)
			}
		})
	}
}

func TestAnAgentThatStopsWithoutSubmittingIsAskedToSubmit(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	agent := &fakeAgent{turns: []editor.Turn{{Text: "I have read the week."}, submits(citesTheSpeech)}}
	ed, _ := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	msgs := agent.sent[1].Messages
	if last := msgs[len(msgs)-1]; !strings.Contains(last.User, "submit_read") {
		t.Errorf("the agent was told %+v, want to be asked to submit", last)
	}
	if _, found, _ := store.SundayRead(t.Context(), "2026-10-25"); !found {
		t.Error("the read submitted once asked was not written")
	}
}

func TestAWeekWithNothingMovedOnIsWrittenEmptyWithNoCall(t *testing.T) {
	store := record(t)
	agent := &fakeAgent{}
	ed, _ := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	if len(agent.sent) != 0 {
		t.Errorf("the agent was called %d times with nothing to read", len(agent.sent))
	}
	r, found, err := store.SundayRead(t.Context(), "2026-10-25")
	if err != nil || !found || len(r.Statements) != 0 {
		t.Errorf("read = %+v, %v, %v; want one written empty", r, found, err)
	}
}

func TestAReadWithAnUncitedStatementIsRejectedBeforeItIsPublished(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	agent := &fakeAgent{turns: []editor.Turn{submits(uncited), submits(citesWhatItWasNotShown)}}
	ed, logs := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	if _, found, _ := store.SundayRead(t.Context(), "2026-10-25"); found {
		t.Fatal("a read that fails its checks was published")
	}
	// The agent is told why each submission was rejected, so it can mend it.
	if len(agent.sent) < 3 {
		t.Fatalf("the agent took %d turns, want it told of each rejection", len(agent.sent))
	}
	for i, want := range []string{"uncited", "F99"} {
		msgs := agent.sent[i+1].Messages
		res := msgs[len(msgs)-1].Results
		if len(res) != 1 || !res[0].IsError || res[0].CallID != "call-1" || !strings.Contains(res[0].Content, want) {
			t.Errorf("after submission %d the agent was told %+v, want an error saying %q", i+1, res, want)
		}
	}
	// It never submitted a read that passes: the failure is kept.
	fs, err := store.SundayReadFailures(t.Context(), "2026-10-25")
	if err != nil || len(fs) != 1 || !fs[0].At.Equal(sundayMorning) || fs[0].Reason == "" {
		t.Errorf("failures = %+v, %v; want one kept with its time and reason", fs, err)
	}
	if !strings.Contains(logs.String(), "level=ERROR") {
		t.Errorf("the failure was not logged:\n%s", logs)
	}
}

func TestARejectedReadCanBeMendedAndResubmitted(t *testing.T) {
	store := record(t)
	readAndPlace(t, store, &fakeModel{output: startsAStory})
	agent := &fakeAgent{turns: []editor.Turn{submits(uncited), submits(citesTheSpeech)}}
	ed, _ := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	r, found, err := store.SundayRead(t.Context(), "2026-10-25")
	if err != nil || !found {
		t.Fatalf("SundayRead: %v %v", found, err)
	}
	if len(r.Statements) != 1 || r.Statements[0].Text != "The Prime Minister took his case to Motherwell." || r.Turns != 2 {
		t.Errorf("read = %+v, want the mended one, in two turns", r)
	}
}
