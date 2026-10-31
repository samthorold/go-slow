// Package interest is the Owner's Topics, and how a Span touches one, by
// its terms, with no model call (system design: Every Item is read, and
// Topics select nothing). Touching counts and selects nothing: the Owner
// steers by the counts per Source. The check that ties whole Items to
// Topics remains only for the Sunday read, which still selects Stories by
// Topic until it tells Spans.
package interest

import (
	"context"
	"errors"
	"slices"
	"strings"
)

// Depth is how closely a Topic was once followed, on an ordered scale.
// Topics no longer carry one: what is read and told is the Editor's
// judgement. It remains only where the Record still holds it: kept with
// Feedback given before, and shown to the Sunday read with the Stories its
// Topics select, until each is replaced.
type Depth int

// The Depths, from the least close to the closest.
const (
	// Passing: only what moves the Topic on in a big way.
	Passing Depth = iota + 1
	// Following: the Topic's Developments as they happen.
	Following
	// Close: the detail as well, reactions and all.
	Close
)

// String is the Depth's name.
func (d Depth) String() string {
	switch d {
	case Passing:
		return "passing"
	case Following:
		return "following"
	case Close:
		return "close"
	}
	return ""
}

// Topic is a standing area of interest the Owner follows. A Span touches it
// when the Span names one of its terms (keywords, or the names of the
// Entities it is about). Its Sources tie whole Items to it for the Sunday
// read alone.
type Topic struct {
	ID      int64
	Name    string
	Sources []string // Source IDs
	Terms   []string
}

// Draft is a Topic as the Owner typed it, not yet validated.
type Draft struct {
	Name    string
	Sources []string
	// Terms are one per line.
	Terms string
}

// Errors are a Draft's validation failures, by field; "" is for the form as
// a whole.
type Errors map[string]string

// Store is the part of the Record that holds the Topics.
type Store interface {
	Topics(ctx context.Context) ([]Topic, error)
	AddTopic(ctx context.Context, t Topic) (Topic, error)
	// RemoveTopic removes the Topic and its ties, reporting whether it was
	// there.
	RemoveTopic(ctx context.Context, id int64) (bool, error)

	// UncheckedItems are the Items the check has not yet looked at.
	UncheckedItems(ctx context.Context) ([]Item, error)
	// RecordChecks marks each Item checked, tied to its Topics.
	RecordChecks(ctx context.Context, checks []Checked) error
	// WaitingItems are the Items not yet read, tied to a Topic or not.
	WaitingItems(ctx context.Context) ([]Item, error)
	// Tie adds ties to Topics, leaving the Items' other ties, and whether
	// they have been checked, as they were.
	Tie(ctx context.Context, ties []Checked) error
}

// Service is the Owner's commands and queries on Topics.
type Service struct {
	store Store
}

// New builds the Service over its store. It takes no model: nothing here
// calls one.
func New(store Store) *Service { return &Service{store: store} }

// Topics lists the Topics.
func (s *Service) Topics(ctx context.Context) ([]Topic, error) { return s.store.Topics(ctx) }

// Add takes up a new Topic. A Draft that fails validation adds nothing and
// returns its Errors. Taking one up changes only what is counted, never
// what is read or told.
func (s *Service) Add(ctx context.Context, d Draft) (Topic, Errors, error) {
	t, errs := validate(d)
	existing, err := s.store.Topics(ctx)
	if err != nil {
		return Topic{}, nil, err
	}
	for _, e := range existing {
		if strings.EqualFold(e.Name, t.Name) {
			errs["name"] = "There is already a Topic with this name."
		}
	}
	if len(errs) > 0 {
		return Topic{}, errs, nil
	}
	t, err = s.store.AddTopic(ctx, t)
	if err != nil {
		return t, nil, err
	}
	return t, nil, s.recheck(ctx, t)
}

// ErrNoTopic is the error for a Topic that does not exist.
var ErrNoTopic = errors.New("no such Topic")

// Remove stops following a Topic. Like taking one up, it changes only what
// is counted.
func (s *Service) Remove(ctx context.Context, id int64) error {
	found, err := s.store.RemoveTopic(ctx, id)
	if err == nil && !found {
		return ErrNoTopic
	}
	return err
}

func validate(d Draft) (Topic, Errors) {
	errs := Errors{}
	t := Topic{Name: strings.TrimSpace(d.Name)}
	for _, id := range d.Sources {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(t.Sources, id) {
			t.Sources = append(t.Sources, id)
		}
	}
	if t.Name == "" {
		errs["name"] = "Give the Topic a name."
	}
	for line := range strings.Lines(d.Terms) {
		if term := strings.TrimSpace(line); term != "" {
			t.Terms = append(t.Terms, term)
		}
	}
	if len(t.Terms) == 0 {
		// A Span touches a Topic by its terms alone: with none, nothing
		// would.
		errs["terms"] = "Give at least one term, so Spans can touch the Topic."
	}
	return t, errs
}
