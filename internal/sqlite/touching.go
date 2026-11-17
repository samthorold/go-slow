package sqlite

import (
	"context"
	"slices"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
)

// StoriesTouching are the Stories the Editor made that touch the Topic,
// newest first, worked out when asked and never stored (system design: The
// Owner reads Stories).
func (s *Store) StoriesTouching(ctx context.Context, topic interest.Topic) ([]editor.Story, error) {
	made, err := s.StoriesMade(ctx)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(made, func(st editor.Story) bool { return !storyTouches(st, []interest.Topic{topic}) }), nil
}

// storyTouches is whether a Story the Editor made touches one of the Topics:
// whether one of the Spans it holds does: its Editor's Spans, by their own
// words, and the Spans they cite.
func storyTouches(st editor.Story, topics []interest.Topic) bool {
	touches := func(text string) bool {
		return slices.ContainsFunc(topics, func(t interest.Topic) bool { return t.Touches(text) })
	}
	for _, es := range st.Spans {
		if touches(es.Body) || slices.ContainsFunc(es.Cites, func(c editor.Cited) bool { return touches(c.Words) }) {
			return true
		}
	}
	return false
}
