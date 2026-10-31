// Package interest is the Owner's Topics, and how a Span touches one, by
// its terms, with no model call (system design: Every Item is read, and
// Topics select nothing). Touching counts and selects nothing: the Owner
// steers by the counts per Source.
package interest

import (
	"context"
	"errors"
	"strings"
)

// Topic is a standing area of interest the Owner follows. A Span touches it
// when the Span names one of its terms (keywords, or the names of the
// Entities it is about).
type Topic struct {
	ID    int64
	Name  string
	Terms []string
}

// Draft is a Topic as the Owner typed it, not yet validated.
type Draft struct {
	Name string
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
	// RemoveTopic removes the Topic, reporting whether it was there.
	RemoveTopic(ctx context.Context, id int64) (bool, error)
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
	return t, nil, err
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
