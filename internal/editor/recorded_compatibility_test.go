package editor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

// The compatibility check (system design: Each act is recorded over its own
// frozen inputs). Until a refresh, the acts downstream of one re-recorded
// are replayed over what it used to write. So each act's current
// recordings, over its own frozen inputs, are fed through every act
// downstream of it, each on a fake model that answers from its request
// alone, as any model could: their requests must build, the fake's answers
// must pass their checks, and every downstream act must be asked about every
// fixture. A change to an earlier act's output that a later act can't take
// fails here at once, with no recording made.
func TestEachActsCurrentRecordingsAreTakenByTheActsDownstream(t *testing.T) {
	t.Parallel()
	for _, act := range []string{"reading", "placing", "connecting"} {
		t.Run(act, func(t *testing.T) {
			if err := feedDownstream(t, recordedDir, act); err != nil {
				t.Error(err)
			}
		})
	}
}

// A hand-made reading that draws every proposition of an Item as a Claim
// passes reading's checks, but gives placing nothing: placing takes only
// Facts. The compatibility check fails on it.
func TestTheCompatibilityCheckFailsOnAReadingPlacingCannotTake(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(filepath.Join(root, "reading"), os.DirFS(filepath.Join(recordedDir, "reading"))); err != nil {
		t.Fatal(err)
	}
	name := "appointment-of-lord-lieutenant-of-county-tyrone-15-october-2026"
	path := samplePath(filepath.Join(root, "reading"), name, 1)
	sample, err := sampleAt(path)
	if err != nil {
		t.Fatal(err)
	}
	attempts := sample.attempts()
	rec := attempts[len(attempts)-1]
	var out struct {
		Facts  []map[string]any `json:"facts"`
		Claims []map[string]any `json:"claims"`
	}
	if err := json.Unmarshal(rec.Output, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Facts) == 0 {
		t.Fatalf("%s draws no Facts to make Claims of", name)
	}
	for _, f := range out.Facts {
		delete(f, "as_at")
		f["made_by"] = "the Item"
		out.Claims = append(out.Claims, f)
	}
	out.Facts = []map[string]any{}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	rec.Output = b
	attempts[len(attempts)-1] = rec
	keepSample(t, path, attempts)

	err = feedDownstream(t, root, "reading")
	if err == nil || !strings.Contains(err.Error(), "placing was never asked") {
		t.Fatalf("feedDownstream = %v; want placing never asked about %s", err, name)
	}
}

// downstream are the acts that rest on each act's recordings, and the
// frozen inputs each act's own recordings rest on.
var (
	downstream = map[string][]string{
		"reading":    {"placing", "connecting", "briefing", "sunday read"},
		"placing":    {"connecting", "briefing", "sunday read"},
		"connecting": {"briefing", "sunday read"},
	}
	upstream = map[string][]string{
		"reading":    nil,
		"placing":    {"reading"},
		"connecting": {"reading", "placing"},
	}
)

// feedDownstream replays act's current recordings under root (each
// fixture's first sample, as a refresh would copy it), over its own
// frozen inputs there, and runs every act downstream of it on a fake model.
// It is an error if a downstream act is never asked about a fixture, or an
// answer fails its checks.
func feedDownstream(t *testing.T, root, act string) error {
	t.Helper()
	ctx := t.Context()
	store, topics := newRecord(t, briefingTopics)
	fixtures := fixtureItems(t)
	models := map[string]actModel{act: firstSamples(filepath.Join(root, act))}
	for _, up := range upstream[act] {
		models[up] = replayed(inputsOf(root, act, up))
	}
	fakes := map[string]*fakeAct{}
	for _, down := range downstream[act] {
		fakes[down] = &fakeAct{act: down}
		f := fakes[down]
		models[down] = func(string) (editor.Model, error) { return f, nil }
	}
	c := chain{reading: models["reading"], placing: models["placing"], connecting: models["connecting"]}
	if err := c.run(ctx, store, topics, fixtures); err != nil {
		return err
	}
	for _, down := range []string{"placing", "connecting"} {
		f, ok := fakes[down]
		if !ok {
			continue
		}
		for _, fx := range fixtures {
			if !slices.Contains(f.items, fx.item.Title) {
				return fmt.Errorf("%s was never asked about %s", down, fx.name)
			}
		}
	}

	var logs bytes.Buffer
	agent := &fakeSundayAgent{}
	ed := editor.New(editor.Deps{
		Store: store, Briefings: store, BriefingModel: fakes["briefing"],
		SundayReads: store, SundayReadModel: agent, Wikidata: &fakeWikidata{},
		Log: slog.New(slog.NewTextHandler(&logs, nil)),
	})
	at := saturday.Add(time.Hour)
	if err := ed.Brief(ctx, at); err != nil {
		return err
	}
	if strings.Contains(logs.String(), "writing the Briefing failed") {
		return fmt.Errorf("the Briefing fails the act's checks:\n%s", logs.String())
	}
	if len(fakes["briefing"].items) == 0 {
		return fmt.Errorf("the Briefing was never asked: no Story was given it")
	}
	sunday := saturday.AddDate(0, 0, 1).Add(time.Hour)
	if err := ed.SundayRead(ctx, sunday); err != nil {
		return err
	}
	if strings.Contains(logs.String(), "writing the Sunday read failed") {
		return fmt.Errorf("the Sunday read fails the act's checks:\n%s", logs.String())
	}
	if agent.turns == 0 {
		return fmt.Errorf("the Sunday read was never asked: no Story was given it")
	}
	_, found, err := store.SundayRead(ctx, sunday.Format(time.DateOnly))
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no Sunday read written:\n%s", logs.String())
	}
	return nil
}

// fakeAct stands in for the model of a daily act downstream: it answers
// from the request alone, as the act's input shows it, with an answer the
// act's checks take, and keeps the Items (or, for the Briefing, the days) it
// was asked about.
type fakeAct struct {
	act   string
	items []string
}

var (
	itemLine    = regexp.MustCompile(`(?m)^Item: (.*)$`)
	factLine    = regexp.MustCompile(`(?m)^(F\d+) \(as at [^)]*\): [^\n]*\n(?:  Entities: ([^\n]*)\n)?`)
	storyLine   = regexp.MustCompile(`(?m)^(S\d+): |^  (D\d+) \(as at `)
	briefingFor = regexp.MustCompile(`^The Briefing for (.*)\.`)
)

func (f *fakeAct) Complete(_ context.Context, req editor.Request) (editor.Response, error) {
	if req.Act == "briefing" {
		f.items = append(f.items, briefingFor.FindString(req.Input))
	} else if m := itemLine.FindStringSubmatch(req.Input); m != nil {
		f.items = append(f.items, m[1])
	}
	var out any
	switch req.Act {
	case "placing":
		// Every Fact starts a Story of its own.
		type newStory struct {
			Ref   string `json:"ref"`
			Title string `json:"title"`
		}
		type placed struct {
			Fact           string   `json:"fact"`
			DevelopmentsIn []string `json:"developments_in"`
		}
		o := struct {
			NewStories []newStory `json:"new_stories"`
			Facts      []placed   `json:"facts"`
		}{NewStories: []newStory{}, Facts: []placed{}}
		for _, m := range factLine.FindAllStringSubmatch(req.Input, -1) {
			ref := "N" + strings.TrimPrefix(m[1], "F")
			o.NewStories = append(o.NewStories, newStory{ref, "The Story " + m[1] + " started"})
			o.Facts = append(o.Facts, placed{m[1], []string{ref}})
		}
		out = o
	case "connecting":
		// Every Entity is an actor; no Fact causes another.
		type role struct {
			Entity string `json:"entity"`
			Role   string `json:"role"`
		}
		type connected struct {
			Fact  string `json:"fact"`
			Roles []role `json:"roles"`
		}
		o := struct {
			Facts  []connected `json:"facts"`
			Causes []struct{}  `json:"causes"`
		}{Facts: []connected{}, Causes: []struct{}{}}
		for _, m := range factLine.FindAllStringSubmatch(req.Input, -1) {
			c := connected{Fact: m[1], Roles: []role{}}
			if m[2] != "" {
				for _, e := range strings.Split(m[2], "; ") {
					c.Roles = append(c.Roles, role{e, "actor"})
				}
			}
			o.Facts = append(o.Facts, c)
		}
		out = o
	case "briefing":
		// Every Story is told in one statement citing all its Developments.
		type statement struct {
			Text         string   `json:"text"`
			Developments []string `json:"developments"`
		}
		type told struct {
			Story      string      `json:"story"`
			Statements []statement `json:"statements"`
		}
		var stories []told
		for _, m := range storyLine.FindAllStringSubmatch(req.Input, -1) {
			if m[1] != "" {
				stories = append(stories, told{m[1], []statement{{Text: "What moved " + m[1] + " on."}}})
				continue
			}
			if len(stories) == 0 {
				return editor.Response{}, fmt.Errorf("a Development before any Story:\n%s", req.Input)
			}
			last := &stories[len(stories)-1].Statements[0]
			last.Developments = append(last.Developments, m[2])
		}
		out = struct {
			Stories []told `json:"stories"`
		}{stories}
	default:
		return editor.Response{}, fmt.Errorf("no fake for the act %q", req.Act)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return editor.Response{}, err
	}
	return editor.Response{Output: b, Model: "fake-" + f.act}, nil
}

// fakeSundayAgent stands in for the Sunday read's agent: it submits a read
// of one statement citing the first Development it is shown.
type fakeSundayAgent struct{ turns int }

var developmentRef = regexp.MustCompile(`S\d+:F\d+`)

func (a *fakeSundayAgent) Turn(_ context.Context, req editor.ConversationRequest) (editor.Turn, error) {
	a.turns++
	if a.turns > 1 || len(req.Messages) == 0 {
		// Submitted once; a read refused stays refused.
		return editor.Turn{Model: "fake-agent"}, nil
	}
	ref := developmentRef.FindString(req.Messages[0].User)
	if ref == "" {
		return editor.Turn{Model: "fake-agent"}, nil
	}
	read, err := json.Marshal(map[string]any{"statements": []map[string]any{{"text": "What moved on this week.", "cites": []string{ref}}}})
	if err != nil {
		return editor.Turn{}, err
	}
	return editor.Turn{Model: "fake-agent", Calls: []editor.ToolCall{{ID: "call-1", Name: "submit_read", Input: read}}}, nil
}
