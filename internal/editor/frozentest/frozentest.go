// Package frozentest writes the outputs of the retired placing and connecting
// acts, and of the Briefing over Stories they fed, into a Record, for tests:
// the Sunday read's replays rest on frozen copies of those acts' recordings
// (Stories, Developments, roles and causes), and tests of the pages that
// show them, a Briefing over Stories among them, build them the same way.
// Relating replaced both acts (system design: Relating a Span), and the
// Briefing tells Spans; nothing in the app places, connects or writes a
// Briefing over Stories, and this package is not part of it. It goes when
// the Sunday read and the pages no longer read Stories built of Facts.
package frozentest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

// Store is the part of the Record the retired acts' outputs are written to.
type Store interface {
	FactsToPlace(ctx context.Context) ([]editor.ToPlace, error)
	CandidateStories(ctx context.Context, it editor.Item, limit int) ([]editor.Story, error)
	SavePlacing(ctx context.Context, p editor.Placing, placedAt time.Time) error
	FactsToConnect(ctx context.Context) ([]editor.ToConnect, error)
	FactsNear(ctx context.Context, tc editor.ToConnect, limit int) ([]editor.Fact, error)
	SaveConnecting(ctx context.Context, c editor.Connecting, connectedAt time.Time) error
	editor.CallKeeper
}

// Deps are what an output is written with: the Record, the model answering
// with the outputs (a replay of a recording, or a fake), and the log, which
// takes "placing failed" or "connecting failed" for an output the act's
// checks refused.
type Deps struct {
	Store Store
	Model editor.Model
	Log   *slog.Logger
}

// The caps the retired acts read the Record with.
const (
	candidatesShown = 20
	factsShown      = 30
)

// PlaceWaiting places every Fact awaiting placing with the model's
// outputs, an Item at a time, as the retired placing act did, keeping each
// call. An output its checks refuse is logged and writes nothing.
func PlaceWaiting(ctx context.Context, d Deps, now time.Time) error {
	waiting, err := d.Store.FactsToPlace(ctx)
	if err != nil {
		return err
	}
	for _, tp := range waiting {
		stories, err := d.Store.CandidateStories(ctx, tp.Item, candidatesShown)
		if err != nil {
			return err
		}
		req := editor.Request{Act: "placing", Instructions: placingInstructions, Input: factsInput(tp.Item, "Facts to place", tp.Facts, "Candidate Stories", storyLines(stories))}
		var p editor.Placing
		call, err := callOn(ctx, d.Model, tp.Item, req, now, func(res editor.Response) (err error) {
			p, err = checkPlacing(tp, stories, res)
			return err
		})
		if err != nil {
			if err := d.Store.KeepCall(ctx, call); err != nil {
				return err
			}
			d.Log.ErrorContext(ctx, "placing failed", "entry", tp.Item.EntryID, "error", err)
			continue
		}
		if err := d.Store.SavePlacing(ctx, p, now); err != nil {
			return err
		}
		if err := d.Store.KeepCall(ctx, call); err != nil {
			return err
		}
	}
	return nil
}

// ConnectWaiting connects every Fact awaiting connecting with the model's
// outputs, an Item at a time, as the retired connecting act did, keeping
// each call. An output its checks refuse is logged and writes nothing.
func ConnectWaiting(ctx context.Context, d Deps, now time.Time) error {
	waiting, err := d.Store.FactsToConnect(ctx)
	if err != nil {
		return err
	}
	for _, tc := range waiting {
		others, err := d.Store.FactsNear(ctx, tc, factsShown)
		if err != nil {
			return err
		}
		req := editor.Request{Act: "connecting", Instructions: connectingInstructions, Input: factsInput(tc.Item, "Facts to connect", tc.Facts, "Other Facts", factLines(others))}
		var c editor.Connecting
		call, err := callOn(ctx, d.Model, tc.Item, req, now, func(res editor.Response) (err error) {
			c, err = checkConnecting(tc, others, res)
			return err
		})
		if err != nil {
			if err := d.Store.KeepCall(ctx, call); err != nil {
				return err
			}
			d.Log.ErrorContext(ctx, "connecting failed", "entry", tc.Item.EntryID, "error", err)
			continue
		}
		if err := d.Store.SaveConnecting(ctx, c, now); err != nil {
			return err
		}
		if err := d.Store.KeepCall(ctx, call); err != nil {
			return err
		}
	}
	return nil
}

// The retired acts' requests, as their kept calls show them: their opening
// lines, and their inputs in brief.
const (
	placingInstructions    = "You are placing the Facts drawn from one Item."
	connectingInstructions = "You are connecting the Facts drawn from one Item."
)

// factsInput is an Item's Facts, headed, and the lines of what they are
// weighed against, headed, as a retired act's input.
func factsInput(it editor.Item, heading string, facts []editor.Fact, others string, lines []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Item: %s\n\n%s:\n", it.Title, heading)
	for i, f := range facts {
		fmt.Fprintf(&b, "F%d (as at %s): %s\n", i+1, f.AsAt.Format(time.DateOnly), f.Statement)
	}
	fmt.Fprintf(&b, "\n%s:\n", others)
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	return b.String()
}

func storyLines(stories []editor.Story) []string {
	var out []string
	for _, s := range stories {
		out = append(out, fmt.Sprintf("S%d: %s", s.ID, s.Title))
	}
	return out
}

func factLines(facts []editor.Fact) []string {
	var out []string
	for i, f := range facts {
		out = append(out, fmt.Sprintf("C%d (as at %s): %s", i+1, f.AsAt.Format(time.DateOnly), f.Statement))
	}
	return out
}

// callOn asks m req on the Item it, at now, and checks the answer: the
// call, ended as it came out, and the error that ended it.
func callOn(ctx context.Context, m editor.Model, it editor.Item, req editor.Request, now time.Time,
	check func(editor.Response) error,
) (editor.Call, error) {
	call := editor.Call{Act: req.Act, Key: it.SourceID + "\n" + it.EntryID, At: now, Request: req,
		RequestHash: editor.RequestHash(req, "")}
	res, err := m.Complete(ctx, req)
	call.Response = &res
	if err != nil {
		call.Outcome, call.Why = editor.Failed, err.Error()
		return call, err
	}
	if err := check(res); err != nil {
		call.Outcome, call.Why = editor.Refused, err.Error()
		return call, err
	}
	call.Outcome = editor.Applied
	return call, nil
}

func refusedPlacing(it editor.Item, format string, args ...any) error {
	return fmt.Errorf("placing %s: %w: %s", it.EntryID, editor.ErrFailedChecks, fmt.Sprintf(format, args...))
}

func refusedConnecting(it editor.Item, format string, args ...any) error {
	return fmt.Errorf("connecting %s: %w: %s", it.EntryID, editor.ErrFailedChecks, fmt.Sprintf(format, args...))
}

// factIndex is the index of the Fact ref names (F1 is the first), or -1.
func factIndex(ref string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(ref, "F"))
	if err != nil || !strings.HasPrefix(ref, "F") {
		return -1
	}
	return n - 1
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

// checkPlacing is the retired placing act's checks on an output.
func checkPlacing(tp editor.ToPlace, stories []editor.Story, res editor.Response) (editor.Placing, error) {
	it := tp.Item
	var out placingOutput
	dec := json.NewDecoder(bytes.NewReader(res.Output))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return editor.Placing{}, refusedPlacing(it, "output does not match the schema: %v", err)
	}
	p := editor.Placing{SourceID: it.SourceID, EntryID: it.EntryID, Model: res.Model, Usage: res.Usage}
	newRefs := map[string]int{}
	for _, s := range out.NewStories {
		title := strings.TrimSpace(s.Title)
		if title == "" {
			return editor.Placing{}, refusedPlacing(it, "a new Story with no title")
		}
		if _, dup := newRefs[s.Ref]; dup {
			return editor.Placing{}, refusedPlacing(it, "two new Stories called %s", s.Ref)
		}
		newRefs[s.Ref] = len(p.NewStories)
		p.NewStories = append(p.NewStories, editor.NewStory{Title: title})
	}
	placed := make([]bool, len(tp.Facts))
	for _, f := range out.Facts {
		i := factIndex(f.Fact)
		if i < 0 || i >= len(tp.Facts) || placed[i] {
			return editor.Placing{}, refusedPlacing(it, "a Fact not given to place, or placed twice: %q", f.Fact)
		}
		placed[i] = true
		fact := tp.Facts[i]
		p.Facts = append(p.Facts, fact.ID)
		for _, ref := range f.DevelopmentsIn {
			if n, ok := newRefs[ref]; ok {
				if !slices.Contains(p.NewStories[n].FactIDs, fact.ID) {
					p.NewStories[n].FactIDs = append(p.NewStories[n].FactIDs, fact.ID)
				}
				continue
			}
			i := slices.IndexFunc(stories, func(s editor.Story) bool { return ref == "S"+strconv.FormatInt(s.ID, 10) })
			if i < 0 {
				return editor.Placing{}, refusedPlacing(it, "%s placed in %s, which is not one of the Item's candidates", f.Fact, ref)
			}
			if pl := (editor.Placement{FactID: fact.ID, StoryID: stories[i].ID}); !slices.Contains(p.Developments, pl) {
				p.Developments = append(p.Developments, pl)
			}
		}
	}
	for i, ok := range placed {
		if !ok {
			return editor.Placing{}, refusedPlacing(it, "F%d left unplaced", i+1)
		}
	}
	return p, nil
}

type connectingOutput struct {
	Facts []struct {
		Fact  string `json:"fact"`
		Roles []struct {
			Entity string `json:"entity"`
			Role   string `json:"role"`
		} `json:"roles"`
	} `json:"facts"`
	Causes []struct {
		Cause  string `json:"cause"`
		Effect string `json:"effect"`
	} `json:"causes"`
}

// roles are the roles an Entity can have in a Fact.
var roles = []string{"actor", "undergoer", "place", "occasion"}

// checkConnecting is the retired connecting act's checks on an output.
func checkConnecting(tc editor.ToConnect, others []editor.Fact, res editor.Response) (editor.Connecting, error) {
	it := tc.Item
	var out connectingOutput
	dec := json.NewDecoder(bytes.NewReader(res.Output))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return editor.Connecting{}, refusedConnecting(it, "output does not match the schema: %v", err)
	}
	c := editor.Connecting{SourceID: it.SourceID, EntryID: it.EntryID, Model: res.Model, Usage: res.Usage}
	connected := make([]bool, len(tc.Facts))
	for _, f := range out.Facts {
		i := factIndex(f.Fact)
		if i < 0 || i >= len(tc.Facts) || connected[i] {
			return editor.Connecting{}, refusedConnecting(it, "a Fact not given to connect, or connected twice: %q", f.Fact)
		}
		connected[i] = true
		given := map[string]bool{}
		for _, r := range f.Roles {
			if !slices.ContainsFunc(tc.Mentions, func(m editor.Mention) bool { return m.Name == r.Entity }) {
				return editor.Connecting{}, refusedConnecting(it, "%s gives a role to %q, which the Item's reading never gave", f.Fact, r.Entity)
			}
			if !slices.Contains(roles, r.Role) || given[r.Entity] {
				return editor.Connecting{}, refusedConnecting(it, "%s gives %q the role %q, no role or a second", f.Fact, r.Entity, r.Role)
			}
			given[r.Entity] = true
			c.Roles = append(c.Roles, editor.Role{FactID: tc.Facts[i].ID, Entity: r.Entity, Role: r.Role})
		}
	}
	for i, ok := range connected {
		if !ok {
			return editor.Connecting{}, refusedConnecting(it, "F%d left unconnected", i+1)
		}
		c.Facts = append(c.Facts, tc.Facts[i].ID)
	}
	// ref is the Fact r names, and whether it is one of the Item's.
	ref := func(r string) (editor.Fact, bool, bool) {
		if i := factIndex(r); i >= 0 && i < len(tc.Facts) {
			return tc.Facts[i], true, true
		}
		n, err := strconv.Atoi(strings.TrimPrefix(r, "C"))
		if err != nil || !strings.HasPrefix(r, "C") || n < 1 || n > len(others) {
			return editor.Fact{}, false, false
		}
		return others[n-1], false, true
	}
	for _, p := range out.Causes {
		cause, mine, ok := ref(p.Cause)
		effect, mineToo, okToo := ref(p.Effect)
		if !ok || !okToo || (!mine && !mineToo) || cause.ID == effect.ID {
			return editor.Connecting{}, refusedConnecting(it, "a cause between Facts not shown, two others' or one Fact: %s and %s", p.Cause, p.Effect)
		}
		if cs := (editor.Cause{CauseID: cause.ID, EffectID: effect.ID}); !slices.Contains(c.Causes, cs) {
			c.Causes = append(c.Causes, cs)
		}
	}
	return c, nil
}
