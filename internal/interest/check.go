package interest

import (
	"context"
	"html"
	"slices"
	"strings"
	"unicode"
)

// Item is what the check tying Items to Topics looks at of a collected Item: where it
// came from and its text.
type Item struct {
	SourceID string
	EntryID  string
	Title    string
	Summary  string
	Content  string
}

// Ties is the check for one Topic, which the Sunday read selects Stories
// by until it tells Spans: whether the Item comes from
// one of the Topic's Sources or names one of its terms. A term matches as
// whole words of the text a reader sees (markup stripped, entities decoded),
// ignoring case and punctuation, so "Farage" ties "Mr Farage's
// statement" but not "Farageist". It is plain code with no model in it, so it
// can run over every Item collected.
func (t Topic) Ties(it Item) bool {
	if slices.Contains(t.Sources, it.SourceID) {
		return true
	}
	return t.matches(words(visible(it.Title) + " " + visible(it.Summary) + " " + visible(it.Content)))
}

// Touches is whether a Span, given as its Item's text holds it (which may be
// the publisher's HTML), touches the Topic: whether it names one of the
// Topic's terms, matched as Ties matches them, as whole words of the text a
// reader sees. The Topic's Sources play no part: they say where an Item
// came from, not what a Span is about. Touching counts and selects nothing
// (system design: Every Item is read, and Topics select nothing).
func (t Topic) Touches(span string) bool {
	return t.matches(words(visible(span)))
}

// matches is whether text, as words gives it, holds one of the Topic's
// terms as whole words.
func (t Topic) matches(text string) bool {
	for _, term := range t.Terms {
		if w := words(term); w != "  " && strings.Contains(text, w) {
			return true
		}
	}
	return false
}

// SourceCounts is how one Source stands over a window, for the Owner to
// steer by (system design: Every Item is read, and Topics select nothing):
// the Stories gathered in it that it contributes to (those holding a Span
// of one of its Items), as many as touch a Topic and as many as don't, and
// the Spans of its Items collected in it, as their newest readings read
// them, and how many of those are Rejected. It counts and selects nothing.
type SourceCounts struct {
	SourceID            string
	Touching, Untouched int
	Spans, Rejected     int
}

// Checked is the check's result for one Item: the Topics it ties the Item
// to, none if it touches no Topic.
type Checked struct {
	SourceID string
	EntryID  string
	TopicIDs []int64
}

// CheckNew runs the check over the Items not yet checked,
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

// recheck runs the check for one Topic, just taken up, over the
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

// visible is the text a reader sees of s, which may be the publisher's HTML:
// each tag, comment or declaration becomes a space (so its names, attributes
// and link URLs are not words of the text), and entities are decoded. A '<'
// starts a tag only where HTML's tokenizer would take it as one (before a
// letter, '/', '!' or '?'), so plain text such as "inflation < 2%" keeps it.
// An unclosed tag runs to the end of s, as it would in a browser.
func visible(s string) string {
	var b strings.Builder
	for {
		i := tagStart(s)
		if i < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		b.WriteByte(' ')
		end := tagEnd(s[i:])
		if end < 0 {
			break
		}
		s = s[i+end:]
	}
	return html.UnescapeString(b.String())
}

// tagStart is the index of the first '<' in s that opens markup, or -1.
func tagStart(s string) int {
	for i := 0; i < len(s)-1; i++ {
		if s[i] != '<' {
			continue
		}
		c := s[i+1]
		if c == '/' || c == '!' || c == '?' || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') {
			return i
		}
	}
	return -1
}

// tagEnd is the length of the markup at the start of s, through its closing
// '>', skipping any '>' inside a quoted attribute value; -1 if it never
// closes.
func tagEnd(s string) int {
	var quote byte
	for i := 1; i < len(s); i++ {
		switch c := s[i]; {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return i + 1
		}
	}
	return -1
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
