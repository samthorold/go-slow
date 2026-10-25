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

// ToConnect is an Item's Facts awaiting connecting. They are connected
// together, in one call, so a cause between two Facts of one Item is seen.
type ToConnect struct {
	Item  Item
	Facts []Fact
}

// Connecting is the result of connecting an Item's Facts.
type Connecting struct {
	SourceID string
	EntryID  string
	// Facts are the ids of every Fact connected, including those given no
	// cause.
	Facts []int64
	// Roles are the roles each Fact's Entities have in it.
	Roles []Role
	// Causes are the cause Connections made, each between two Facts.
	Causes []Cause
	Model  string
	Usage  Usage
}

// Role is the role an Entity has in a Fact: a Connection from the Fact to
// the Entity.
type Role struct {
	FactID int64
	Entity string
	Role   string
}

// Cause is a cause Connection: one Fact caused another.
type Cause struct {
	CauseID  int64
	EffectID int64
}

// factsShown is how many other Facts an Item's Facts are weighed against:
// enough for the causes near them in time, without the input growing with
// the Record.
const factsShown = 30

// The roles an Entity can have in a Fact.
var roles = []string{"actor", "affected", "place", "occasion"}

// connectingInstructions are the connecting act's own instructions.
const connectingInstructions = `You are the Editor of a private slow-news briefing. You are connecting the Facts drawn from one Item.

For each Fact to connect (F1, F2 and so on), give every Entity it involves its role in the Fact:
- actor: who did it: who decided, announced, said, made, met, visited or acted.
- affected: to whom or what it was done, or who or what it is about: who was appointed, met, funded, investigated or affected.
- place: where it happened.
- occasion: the named occasion it happened at or belongs to (a summit, a Budget, an election).
Give each Entity of a Fact exactly one role, the one that fits best.

Then give the cause Connections: where one Fact caused another, or was what led to it (the investigation caused the resignations; the offer led to the agreement). Each cause is a pair of refs, cause first. At least one of the pair must be a Fact to connect; the other may be another Fact to connect or one of the other Facts (C1, C2 and so on). Make a cause only where the Facts make it plain that one led to the other, not where two Facts merely share a subject, an Entity or a day. Most Facts have no cause among those shown; give none for them.`

// connectingSchema is the connecting act's output schema for an Item's
// Facts. Its refs are enumerated (the Facts to connect, the other Facts
// they may be connected to, the Entities and the roles), so a model can only
// answer in refs the checks accept.
func connectingSchema(tc ToConnect, others []Fact) map[string]any {
	var facts, all, names []string
	for i, f := range tc.Facts {
		facts = append(facts, "F"+strconv.Itoa(i+1))
		for _, n := range f.Entities {
			if !slices.Contains(names, n) {
				names = append(names, n)
			}
		}
	}
	all = append(all, facts...)
	for i := range others {
		all = append(all, "C"+strconv.Itoa(i+1))
	}
	enum := func(values []string) map[string]any { return map[string]any{"type": "string", "enum": values} }
	entity := enum(names)
	if len(names) == 0 {
		// An enum may not be empty; a Fact with no Entities gives no roles.
		entity = str()
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"facts": array(object(map[string]any{
				"fact": enum(facts),
				"roles": array(object(map[string]any{
					"entity": entity,
					"role":   enum(roles),
				})),
			})),
			"causes": array(object(map[string]any{
				"cause":  enum(all),
				"effect": enum(all),
			})),
		},
		"required":             []string{"facts", "causes"},
		"additionalProperties": false,
	}
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

// Connect is the act of connecting an Item's Facts: each Entity a Fact
// involves is given its role in it, and a Fact is connected as cause or
// effect to another of the Item's Facts or to another Fact in the Record.
// A result that fails the act's checks is ErrFailedChecks, and none of it is
// returned.
func Connect(ctx context.Context, m Model, tc ToConnect, others []Fact) (Connecting, error) {
	res, err := m.Complete(ctx, connectingRequest(tc, others))
	if err != nil {
		return Connecting{}, fmt.Errorf("connecting %s: %w", tc.Item.EntryID, err)
	}
	return checkConnecting(tc, others, res)
}

// connectingRequest is the connecting act's call on a model for an Item's
// Facts.
func connectingRequest(tc ToConnect, others []Fact) Request {
	return Request{
		Act:          "connecting",
		Instructions: connectingInstructions,
		Input:        connectingInput(tc, others),
		Schema:       connectingSchema(tc, others),
	}
}

// checkConnecting decodes the model's answer on an Item's Facts and runs the
// act's checks over it. A result that fails them is ErrFailedChecks.
func checkConnecting(tc ToConnect, others []Fact, res Response) (Connecting, error) {
	it := tc.Item
	var out connectingOutput
	dec := json.NewDecoder(bytes.NewReader(res.Output))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return Connecting{}, failedConnecting(it, "output does not match the schema: %v", err)
	}
	c := Connecting{SourceID: it.SourceID, EntryID: it.EntryID, Model: res.Model, Usage: res.Usage}
	connected := make([]bool, len(tc.Facts))
	for _, f := range out.Facts {
		i := factIndex(f.Fact)
		if i < 0 || i >= len(tc.Facts) {
			return Connecting{}, failedConnecting(it, "a Fact not given to connect: %q", f.Fact)
		}
		if connected[i] {
			return Connecting{}, failedConnecting(it, "%s connected twice", f.Fact)
		}
		connected[i] = true
		fact := tc.Facts[i]
		given := map[string]bool{}
		for _, r := range f.Roles {
			if !slices.Contains(fact.Entities, r.Entity) {
				return Connecting{}, failedConnecting(it, "%s gives a role to %q, which it does not involve", f.Fact, r.Entity)
			}
			if !slices.Contains(roles, r.Role) {
				return Connecting{}, failedConnecting(it, "%s gives %q the role %q, which is no role", f.Fact, r.Entity, r.Role)
			}
			if given[r.Entity] {
				return Connecting{}, failedConnecting(it, "%s gives %q two roles", f.Fact, r.Entity)
			}
			given[r.Entity] = true
			c.Roles = append(c.Roles, Role{FactID: fact.ID, Entity: r.Entity, Role: r.Role})
		}
		for _, e := range fact.Entities {
			if !given[e] {
				return Connecting{}, failedConnecting(it, "%s gives %q no role", f.Fact, e)
			}
		}
	}
	for i, ok := range connected {
		if !ok {
			return Connecting{}, failedConnecting(it, "F%d left unconnected", i+1)
		}
		c.Facts = append(c.Facts, tc.Facts[i].ID)
	}
	// ref is the Fact the model calls r, and whether it is one of the Item's.
	ref := func(r string) (Fact, bool, bool) {
		if i := factIndex(r); i >= 0 && i < len(tc.Facts) {
			return tc.Facts[i], true, true
		}
		n, err := strconv.Atoi(strings.TrimPrefix(r, "C"))
		if err != nil || !strings.HasPrefix(r, "C") || n < 1 || n > len(others) {
			return Fact{}, false, false
		}
		return others[n-1], false, true
	}
	for _, p := range out.Causes {
		cause, mine, ok := ref(p.Cause)
		effect, mineToo, okToo := ref(p.Effect)
		if !ok || !okToo {
			return Connecting{}, failedConnecting(it, "a cause between Facts not shown: %s and %s", p.Cause, p.Effect)
		}
		if !mine && !mineToo {
			return Connecting{}, failedConnecting(it, "a cause between two other Facts: %s and %s", p.Cause, p.Effect)
		}
		if cause.ID == effect.ID {
			return Connecting{}, failedConnecting(it, "%s the cause of itself", p.Cause)
		}
		if cs := (Cause{CauseID: cause.ID, EffectID: effect.ID}); !slices.Contains(c.Causes, cs) {
			c.Causes = append(c.Causes, cs)
		}
	}
	return c, nil
}

// Connecting is the connecting act as it runs in batches: the Items whose
// Facts are placed and await connecting, each connected into the Record when
// its result passes the checks. Items in one batch need not wait for each
// other: each is weighed against the Facts in the Record, which already
// holds every Item's Facts once read, and a cause made from both sides is
// kept once.
func (e *Editor) Connecting() BatchAct {
	return BatchAct{
		Name: "connecting",
		Waiting: func(ctx context.Context) ([]Job, error) {
			waiting, err := e.Store.FactsToConnect(ctx)
			if err != nil {
				return nil, err
			}
			jobs := make([]Job, 0, len(waiting))
			for _, tc := range waiting {
				others, err := e.Store.FactsNear(ctx, tc.Facts, factsShown)
				if err != nil {
					return nil, err
				}
				jobs = append(jobs, Job{
					Key:     itemKey(tc.Item),
					Request: connectingRequest(tc, others),
					Apply: func(ctx context.Context, res Response, now time.Time) error {
						c, err := checkConnecting(tc, others, res)
						if err != nil {
							return err
						}
						return e.Store.SaveConnecting(ctx, c, now)
					},
				})
			}
			return jobs, nil
		},
	}
}

func failedConnecting(it Item, format string, args ...any) error {
	return fmt.Errorf("connecting %s: %w: %s", it.EntryID, ErrFailedChecks, fmt.Sprintf(format, args...))
}

// connectingInput is the Item's Facts and the other Facts they may be
// connected to, as the model is shown them.
func connectingInput(tc ToConnect, others []Fact) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Item: %s\n", tc.Item.Title)
	if !tc.Item.Published.IsZero() {
		fmt.Fprintf(&b, "Published: %s\n", tc.Item.Published.UTC().Format(time.DateOnly))
	}
	b.WriteString("\nFacts to connect:\n")
	for i, f := range tc.Facts {
		writeFact(&b, "F"+strconv.Itoa(i+1), f)
	}
	if len(others) == 0 {
		b.WriteString("\nOther Facts: none\n")
		return b.String()
	}
	b.WriteString("\nOther Facts, which share an Entity or a Story with them:\n")
	for i, f := range others {
		writeFact(&b, "C"+strconv.Itoa(i+1), f)
	}
	return b.String()
}

func writeFact(b *strings.Builder, ref string, f Fact) {
	fmt.Fprintf(b, "\n%s (as at %s): %s\n", ref, f.AsAt.Format(time.DateOnly), f.Statement)
	if len(f.Entities) > 0 {
		fmt.Fprintf(b, "  Entities: %s\n", strings.Join(f.Entities, "; "))
	}
}
