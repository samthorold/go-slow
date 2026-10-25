package editor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ToPlace is an Item's Facts awaiting placing. They are placed together, in
// one call, so Facts from one Item can start one new Story between them.
type ToPlace struct {
	Item  Item
	Facts []Fact
}

// Story is an ongoing subject, made of its Developments. Entities are the
// names of every Entity its Developments involve, which decide the Facts it
// is a candidate for.
type Story struct {
	ID       int64
	Title    string
	Entities []string
	// Timeline is the Story's Developments in order.
	Timeline []Development
}

// Development is a Fact that moves a Story on, with the title of the Item it
// was drawn from.
type Development struct {
	Fact
	ItemTitle string
}

// Placing is the result of placing an Item's Facts.
type Placing struct {
	SourceID string
	EntryID  string
	// Facts are the ids of every Fact placed, including those that became a
	// Development in no Story.
	Facts []int64
	// Developments are the Facts placed in existing Stories.
	Developments []Placement
	// NewStories are the Stories placing started.
	NewStories []NewStory
	Model      string
	Usage      Usage
}

// Placement makes a Fact a Development in an existing Story.
type Placement struct {
	FactID  int64
	StoryID int64
}

// NewStory is a Story placing started, with the Facts that are its first
// Developments.
type NewStory struct {
	Title   string
	FactIDs []int64
}

// placingInstructions are the placing act's own instructions.
const placingInstructions = `You are the Editor of a private slow-news briefing. You are placing the Facts drawn from one Item.

A Story is an ongoing subject followed across days: a negotiation, a policy's passage, a crisis, a campaign, an investigation. Its Timeline is its Developments in order. A Development is a Fact that moves a Story on: something was decided, announced, agreed, done or changed that someone following the Story would need to know.

Each Fact lists its candidate Stories: the Stories that share an Entity with it, shown with their latest Developments. Decide which candidates each Fact moves on. You may place a Fact only in its own candidates, or in a new Story you start. A Fact can be a Development in several Stories, in one, or in none.

Not every Fact is a Development. Place a Fact in no Story, and start none for it, when it is:
- a reaction: someone welcoming, praising, criticising, condemning or commenting on something, or making a statement about it. What is reacted to may be a Development; the reaction is not.
- background: biography, history, or what was already so before the Item (a person's past posts, a programme already running), or a restatement of a Development already on the Story's Timeline.
- incidental: who was present, who told whom, with nothing decided or changed.

Start a new Story only for a Fact that is a Development but moves on none of its candidates, because its subject is one no candidate follows. Prefer a candidate wherever the subject is the same. Several Facts from this Item can share one new Story. Give a new Story a ref (N1, N2 and so on) and a title naming the ongoing subject rather than the event: "UK military support for Ukraine", not "UK sends missiles to Ukraine".

Answer with every Fact exactly once, by its ref (F1, F2 and so on), giving in developments_in the refs of the Stories it is a Development in (S refs for candidates, N refs for new Stories), or an empty list.`

// placingSchema is the placing act's output schema for an Item's Facts. Its
// refs are enumerated, the Facts to place and the Stories they may be placed
// in (their candidates, and up to one new Story per Fact), so a model can
// only answer in refs the checks accept.
func placingSchema(tp ToPlace, stories []Story) map[string]any {
	var facts, newRefs, storyRefs []string
	for i, f := range tp.Facts {
		facts = append(facts, "F"+strconv.Itoa(i+1))
		newRefs = append(newRefs, "N"+strconv.Itoa(i+1))
		for _, s := range candidates(f, stories) {
			if ref := "S" + strconv.FormatInt(s.ID, 10); !slices.Contains(storyRefs, ref) {
				storyRefs = append(storyRefs, ref)
			}
		}
	}
	enum := func(values []string) map[string]any { return map[string]any{"type": "string", "enum": values} }
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"new_stories": array(object(map[string]any{
				"ref":   enum(newRefs),
				"title": str(),
			})),
			"facts": array(object(map[string]any{
				"fact":            enum(facts),
				"developments_in": array(enum(append(storyRefs, newRefs...))),
			})),
		},
		"required":             []string{"new_stories", "facts"},
		"additionalProperties": false,
	}
}

type placingOutput struct {
	NewStories []struct {
		Ref   string `json:"ref"`
		Title string `json:"title"`
	} `json:"new_stories"`
	Facts []struct {
		Fact           string   `json:"fact"`
		DevelopmentsIn []string `json:"developments_in"`
	} `json:"facts"`
}

// Place is the act of placing an Item's Facts: each is weighed against the
// Stories that share its Entities, and becomes a Development in each it
// moves on, or in none; a Fact that fits no candidate can start a new Story.
// stories are the candidates of any of the Facts. A result that fails the
// act's checks is ErrFailedChecks, and none of it is returned.
func Place(ctx context.Context, m Model, tp ToPlace, stories []Story) (Placing, error) {
	res, err := m.Complete(ctx, placingRequest(tp, stories))
	if err != nil {
		return Placing{}, fmt.Errorf("placing %s: %w", tp.Item.EntryID, err)
	}
	return checkPlacing(tp, stories, res)
}

// placingRequest is the placing act's call on a model for an Item's Facts.
func placingRequest(tp ToPlace, stories []Story) Request {
	return Request{
		Act:          "placing",
		Instructions: placingInstructions,
		Input:        placingInput(tp, stories),
		Schema:       placingSchema(tp, stories),
	}
}

// checkPlacing decodes the model's answer on an Item's Facts and runs the
// act's checks over it. A result that fails them is ErrFailedChecks.
func checkPlacing(tp ToPlace, stories []Story, res Response) (Placing, error) {
	it := tp.Item
	var out placingOutput
	dec := json.NewDecoder(bytes.NewReader(res.Output))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return Placing{}, failedPlacing(it, "output does not match the schema: %v", err)
	}
	p := Placing{SourceID: it.SourceID, EntryID: it.EntryID, Model: res.Model, Usage: res.Usage}
	newRefs := map[string]int{}
	for _, s := range out.NewStories {
		title := strings.TrimSpace(s.Title)
		if title == "" {
			return Placing{}, failedPlacing(it, "a new Story with no title")
		}
		if _, dup := newRefs[s.Ref]; dup {
			return Placing{}, failedPlacing(it, "two new Stories called %s", s.Ref)
		}
		newRefs[s.Ref] = len(p.NewStories)
		p.NewStories = append(p.NewStories, NewStory{Title: title})
	}
	// started is whether each new Story has a Fact that fits no candidate:
	// placing starts a Story only when none fits.
	started := make([]bool, len(p.NewStories))
	placed := make([]bool, len(tp.Facts))
	for _, f := range out.Facts {
		i := factIndex(f.Fact)
		if i < 0 || i >= len(tp.Facts) {
			return Placing{}, failedPlacing(it, "a Fact not given to place: %q", f.Fact)
		}
		if placed[i] {
			return Placing{}, failedPlacing(it, "%s placed twice", f.Fact)
		}
		placed[i] = true
		fact := tp.Facts[i]
		cands := candidates(fact, stories)
		p.Facts = append(p.Facts, fact.ID)
		var inNew []int
		inExisting := false
		for _, ref := range f.DevelopmentsIn {
			if n, ok := newRefs[ref]; ok {
				if !slices.Contains(inNew, n) {
					inNew = append(inNew, n)
					p.NewStories[n].FactIDs = append(p.NewStories[n].FactIDs, fact.ID)
				}
				continue
			}
			s, ok := storyRef(ref, cands)
			if !ok {
				return Placing{}, failedPlacing(it, "%s placed in %s, which is not one of its candidates", f.Fact, ref)
			}
			pl := Placement{FactID: fact.ID, StoryID: s.ID}
			if !slices.Contains(p.Developments, pl) {
				p.Developments = append(p.Developments, pl)
			}
			inExisting = true
		}
		if !inExisting {
			for _, n := range inNew {
				started[n] = true
			}
		}
	}
	for i, ok := range placed {
		if !ok {
			return Placing{}, failedPlacing(it, "F%d left unplaced", i+1)
		}
	}
	for n, ok := range started {
		if !ok {
			return Placing{}, failedPlacing(it, "new Story %q is started by no Fact that fits no candidate", p.NewStories[n].Title)
		}
	}
	return p, nil
}

// Placing is the placing act as it runs in batches: the Items whose Facts
// await placing, each placed into the Record when its result passes the
// checks. An Item's Facts are placed only once every earlier Item awaiting
// placing that shares an Entity with them has been placed, since placing
// that Item may start or move on a Story they would be weighed against.
// Until then they wait for a later batch, so a batch gives the same Stories
// as placing one Item at a time in the order they were published; Items
// that share no Entity go in one batch together.
func (e *Editor) Placing() BatchAct {
	return BatchAct{
		Name: "placing",
		Waiting: func(ctx context.Context) ([]Job, error) {
			waiting, err := e.Store.FactsToPlace(ctx)
			if err != nil {
				return nil, err
			}
			var jobs []Job
			earlier := map[string]bool{}
			for _, tp := range waiting {
				names := entityNames(tp)
				blocked := slices.ContainsFunc(names, func(n string) bool { return earlier[n] })
				for _, n := range names {
					earlier[n] = true
				}
				if blocked {
					continue
				}
				stories, err := e.Store.StoriesSharing(ctx, names)
				if err != nil {
					return nil, err
				}
				jobs = append(jobs, Job{
					Key:     tp.Item.SourceID + "\n" + tp.Item.EntryID,
					Request: placingRequest(tp, stories),
					Apply: func(ctx context.Context, res Response, now time.Time) error {
						p, err := checkPlacing(tp, stories, res)
						if err != nil {
							return err
						}
						return e.Store.SavePlacing(ctx, p, now)
					},
				})
			}
			return jobs, nil
		},
	}
}

// entityNames are the names of every Entity an Item's Facts involve.
func entityNames(tp ToPlace) []string {
	var out []string
	for _, f := range tp.Facts {
		for _, n := range f.Entities {
			if !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
	}
	return out
}

// factIndex is the index of the Fact the model calls ref (F1 is the first),
// or -1.
func factIndex(ref string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(ref, "F"))
	if err != nil || !strings.HasPrefix(ref, "F") {
		return -1
	}
	return n - 1
}

// storyRef is the Story among stories the model calls ref (S3 is Story 3).
func storyRef(ref string, stories []Story) (Story, bool) {
	for _, s := range stories {
		if ref == "S"+strconv.FormatInt(s.ID, 10) {
			return s, true
		}
	}
	return Story{}, false
}

// candidates are the Stories a Fact is weighed against: those whose
// Developments involve any of its Entities (system design: Placing weighs a
// Fact against the Stories that share its Entities).
func candidates(f Fact, stories []Story) []Story {
	var out []Story
	for _, s := range stories {
		for _, e := range f.Entities {
			if slices.Contains(s.Entities, e) {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

func failedPlacing(it Item, format string, args ...any) error {
	return fmt.Errorf("placing %s: %w: %s", it.EntryID, ErrFailedChecks, fmt.Sprintf(format, args...))
}

// placingInput is the Item's Facts and their candidate Stories as the model
// is shown them.
func placingInput(tp ToPlace, stories []Story) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Item: %s\n", tp.Item.Title)
	if !tp.Item.Published.IsZero() {
		fmt.Fprintf(&b, "Published: %s\n", tp.Item.Published.UTC().Format(time.DateOnly))
	}
	b.WriteString("\nFacts to place:\n")
	var shown []Story
	for i, f := range tp.Facts {
		fmt.Fprintf(&b, "\nF%d (as at %s): %s\n", i+1, f.AsAt.Format(time.DateOnly), f.Statement)
		if f.Quote != "" {
			fmt.Fprintf(&b, "  Quote: %q\n", f.Quote)
		}
		if len(f.Entities) > 0 {
			fmt.Fprintf(&b, "  Entities: %s\n", strings.Join(f.Entities, "; "))
		}
		var refs []string
		for _, s := range candidates(f, stories) {
			refs = append(refs, "S"+strconv.FormatInt(s.ID, 10))
			if !slices.ContainsFunc(shown, func(x Story) bool { return x.ID == s.ID }) {
				shown = append(shown, s)
			}
		}
		if len(refs) == 0 {
			b.WriteString("  Candidate Stories: none\n")
		} else {
			fmt.Fprintf(&b, "  Candidate Stories: %s\n", strings.Join(refs, ", "))
		}
	}
	if len(shown) > 0 {
		b.WriteString("\nCandidate Stories:\n")
	}
	for _, s := range shown {
		fmt.Fprintf(&b, "\nS%d: %s\n", s.ID, s.Title)
		tl := s.Timeline
		if len(tl) > timelineShown {
			fmt.Fprintf(&b, "  (%d earlier Developments not shown)\n", len(tl)-timelineShown)
			tl = tl[len(tl)-timelineShown:]
		}
		for _, d := range tl {
			fmt.Fprintf(&b, "  - %s: %s\n", d.AsAt.Format(time.DateOnly), d.Statement)
		}
	}
	return b.String()
}

// timelineShown is how many of a candidate Story's latest Developments the
// model is shown: enough to say what the Story is following now, without the
// input growing with the Story's age.
const timelineShown = 8
