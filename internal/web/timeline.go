package web

import "github.com/samthorold/go-slow/internal/editor"

// timelineEntry is one entry on a Story's Timeline: a run of consecutive
// Developments drawn from one Item, its head giving the date and the Item
// once.
type timelineEntry struct {
	// AsAt is the date of the run's first Development.
	AsAt string
	Item itemCitation
	// Developments are the run's, each with its own date only where its
	// Fact's differs from AsAt.
	Developments []developmentRow
}

// timelineOf is a Timeline, its Developments ds in order, as entries: each
// run of consecutive Developments from one Item is one entry, so a later
// Item falling between two runs of the same Item splits them and the order
// never misleads. cite is how each entry cites its Item, and row how each
// Development is shown.
func timelineOf(ds []editor.Development, cite func(editor.Development) itemCitation, row func(editor.Development) developmentRow) []timelineEntry {
	var out []timelineEntry
	for i, d := range ds {
		if i == 0 || d.SourceID != ds[i-1].SourceID || d.EntryID != ds[i-1].EntryID {
			out = append(out, timelineEntry{
				AsAt: factDate(d.Fact),
				Item: cite(d),
			})
		}
		e := &out[len(out)-1]
		r := row(d)
		if r.AsAt == e.AsAt {
			r.AsAt = ""
		}
		e.Developments = append(e.Developments, r)
	}
	return out
}
