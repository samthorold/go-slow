package interest

import (
	"context"
	"slices"
	"strings"
	"unicode"
)

// Item is what the lazy-reading check looks at of a collected Item: where it
// came from and its text.
type Item struct {
	SourceID string
	EntryID  string
	Title    string
	Summary  string
	Content  string
}

// Ties is the lazy-reading check for one Topic: whether the Item comes from
// one of the Topic's Sources or names one of its terms. A term matches as
// whole words, ignoring case and punctuation, so "Farage" ties "Mr Farage's
// statement" but not "Farageist". It is plain code with no model in it, so it
// can run over every Item collected.
func (t Topic) Ties(it Item) bool {
	if slices.Contains(t.Sources, it.SourceID) {
		return true
	}
	if len(t.Terms) == 0 {
		return false
	}
	text := words(it.Title + " " + it.Summary + " " + it.Content)
	for _, term := range t.Terms {
		if w := words(term); w != "  " && strings.Contains(text, w) {
			return true
		}
	}
	return false
}

// Checked is the check's result for one Item: the Topics it ties the Item
// to, none if it touches no Topic.
type Checked struct {
	SourceID string
	EntryID  string
	TopicIDs []int64
}

// CheckNew runs the lazy-reading check over the Items not yet checked,
// marking each as tied to the Topics it matches, or to none.
func (s *Service) CheckNew(ctx context.Context) error {
	items, err := s.store.UncheckedItems(ctx)
	if err != nil || len(items) == 0 {
		return err
	}
	topics, err := s.store.Topics(ctx)
	if err != nil {
		return err
	}
	results := make([]Checked, 0, len(items))
	for _, it := range items {
		c := Checked{SourceID: it.SourceID, EntryID: it.EntryID}
		for _, t := range topics {
			if t.Ties(it) {
				c.TopicIDs = append(c.TopicIDs, t.ID)
			}
		}
		results = append(results, c)
	}
	return s.store.RecordChecks(ctx, results)
}

// recheck runs the check for one Topic, just taken up or deepened, over the
// Items waiting unread, and ties those it matches to it. Keeping every Item
// is what makes this possible: interest that changes reaches back over what
// was collected before it.
func (s *Service) recheck(ctx context.Context, t Topic) error {
	items, err := s.store.WaitingItems(ctx)
	if err != nil {
		return err
	}
	var tied []Checked
	for _, it := range items {
		if t.Ties(it) {
			tied = append(tied, Checked{SourceID: it.SourceID, EntryID: it.EntryID, TopicIDs: []int64{t.ID}})
		}
	}
	return s.store.Tie(ctx, tied)
}

// words lowercases s and turns every run of characters that are not letters
// or digits into one space, with a space at each end, so a whole-word match
// is a substring match.
func words(s string) string {
	f := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return " " + strings.Join(f, " ") + " "
}
