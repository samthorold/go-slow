package editor_test

import (
	"context"

	"github.com/samthorold/go-slow/internal/editor"
)

// fakeWikidata stands in for Wikidata behind the port: it answers from what
// it is given, and finds nothing else.
type fakeWikidata struct {
	search map[string][]string
	items  map[string]editor.WikidataItem
	labels map[string]string
}

func (f *fakeWikidata) Search(_ context.Context, text string) ([]string, error) {
	return f.search[text], nil
}

func (f *fakeWikidata) Items(_ context.Context, ids []string) ([]editor.WikidataItem, error) {
	var out []editor.WikidataItem
	for _, id := range ids {
		if it, ok := f.items[id]; ok {
			out = append(out, it)
		}
	}
	return out, nil
}

func (f *fakeWikidata) Labels(_ context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		if l, ok := f.labels[id]; ok {
			out[id] = l
		}
	}
	return out, nil
}
