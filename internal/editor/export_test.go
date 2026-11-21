package editor

import (
	"context"
	"slices"
	"strings"
	"time"
)

// KeepAsWeavingVersion3 keeps res, a recording of weaving at version 3, as
// the run keyed key at at, over the Record as weavings holds it: for an act
// downstream whose frozen copies of weaving's recordings were made before a
// run was a conversation (naming's), until a refresh brings it onto
// weaving's current ones (system design: Each Command is recorded over its
// own frozen inputs; until a refresh, the Commands downstream are tested on
// what the earlier Commands used to write). Version 3 showed a run every
// Story the Editor had made, oldest first, each Editor's Span after the
// Spans it cites not yet shown, then the Stories cut since the last run, all
// numbered S1, S2… across them; its answer's citations are read by that
// numbering and checked by weaving's checks as they stand.
func KeepAsWeavingVersion3(ctx context.Context, e *Editor, key string, at time.Time, res Response) error {
	stories, err := e.Weavings.StoriesToWeave(ctx)
	if err != nil {
		return err
	}
	made, err := e.Weavings.StoriesMade(ctx)
	if err != nil {
		return err
	}
	slices.Reverse(made)
	h, err := e.hydratedItems(ctx, made, stories)
	if err != nil {
		return err
	}
	sh := &woven{numbers: map[citedAt]int{}}
	for _, st := range made {
		for i, es := range st.Spans {
			for _, c := range es.Cites {
				if _, ok := sh.numbers[citedAt{c.Address, c.Of}]; !ok {
					sh.number(c)
				}
			}
			sh.number(Cited{Of: EditorSpanAt{Story: st.ID, N: i + 1}, Words: es.Body})
		}
	}
	var b strings.Builder
	for _, s := range stories {
		sh.cutStory(&b, s, h)
	}
	w, err := checkWeaving(sh, Request{Act: weavingAct}, "", res)
	if err != nil {
		return err
	}
	w.Key, w.At = key, at
	return e.Weavings.KeepWeaving(ctx, w)
}
