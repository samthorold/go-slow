// Package interest is the Owner's Topics, each at a Depth, and the
// lazy-reading check that ties collected Items to them with no model call
// (system design: Reading is lazy).
package interest

import (
	"context"
	"errors"
	"slices"
	"strings"
)

// Depth is how closely the Owner follows a Topic, on an ordered scale: a
// greater Depth follows it more closely.
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

// Depths lists every Depth, from the least close to the closest.
func Depths() []Depth { return []Depth{Passing, Following, Close} }

// String is the Depth's name, as the Topics page and its forms use it.
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

// ParseDepth reads a Depth from its name.
func ParseDepth(s string) (Depth, bool) {
	for _, d := range Depths() {
		if d.String() == s {
			return d, true
		}
	}
	return 0, false
}

// Topic is a standing area of interest the Owner follows, at a Depth. The
// check ties an Item to it when the Item comes from one of its Sources, or
// names one of its terms (keywords, or the names of the Entities it is about).
type Topic struct {
	ID      int64
	Name    string
	Depth   Depth
	Sources []string // Source IDs
	Terms   []string
}

// Draft is a Topic as the Owner typed it, not yet validated.
type Draft struct {
	Name    string
	Depth   string
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
	// SetDepth reports whether the Topic was there to change.
	SetDepth(ctx context.Context, id int64, d Depth) (bool, error)
	// RemoveTopic removes the Topic and its ties, reporting whether it was
	// there.
	RemoveTopic(ctx context.Context, id int64) (bool, error)

	// UncheckedItems are the Items the check has not yet looked at.
	UncheckedItems(ctx context.Context) ([]Item, error)
	// RecordChecks marks each Item checked, tied to its Topics.
	RecordChecks(ctx context.Context, checks []Checked) error
	// WaitingItems are the Items not yet read. Nothing reads Items yet, so
	// that is every Item.
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
// returns its Errors.
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

// SetDepth changes how closely the Owner follows a Topic. A Depth that is
// not on the scale changes nothing and returns its Errors.
func (s *Service) SetDepth(ctx context.Context, id int64, depth string) (Errors, error) {
	d, ok := ParseDepth(depth)
	if !ok {
		return Errors{"depth": "Choose a Depth."}, nil
	}
	found, err := s.store.SetDepth(ctx, id, d)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNoTopic
	}
	return nil, nil
}

// Remove stops following a Topic. The Items it tied wait unread again
// unless another Topic ties them.
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
	depth, ok := ParseDepth(d.Depth)
	if !ok {
		errs["depth"] = "Choose a Depth."
	}
	t.Depth = depth
	for line := range strings.Lines(d.Terms) {
		if term := strings.TrimSpace(line); term != "" {
			t.Terms = append(t.Terms, term)
		}
	}
	if len(t.Terms) == 0 && len(t.Sources) == 0 {
		// With neither, the check could tie nothing to it.
		errs["terms"] = "Give at least one term or Source, so Items can be tied to the Topic."
	}
	return t, errs
}
