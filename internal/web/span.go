package web

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// spanLink is a Span linking its page, with its Item.
type spanLink struct {
	Words string
	Href  string
	Item  itemCitation
}

// spanPage is the view model for a Span's page: the Span as its Item's
// newest reading reads it, its Item, and the Stories holding it.
type spanPage struct {
	Span spanRow
	Item itemCitation
	// Published is when its Item was published.
	Published string
	// ReadBy is the call of its Item's newest reading, if it has one.
	ReadBy *made
	// Stories are the Stories holding it, and Citing the Editor's Spans
	// citing it (system design: The Owner reads Stories).
	Stories []storyLink
	Citing  []citingRow
}

// spanPath is the page of the Span at a: its Item's page, then its start and
// end in the Item's text as kept.
func spanPath(a editor.Address) string {
	return itemPath(a.SourceID, a.EntryID) + "/spans/" + strconv.Itoa(a.Span.Start) + "-" + strconv.Itoa(a.Span.End)
}

// spanOfPath is the Span a path's {span} names, "start-end", if it is one.
func spanOfPath(s string) (cut.Span, bool) {
	start, end, ok := strings.Cut(s, "-")
	if !ok {
		return cut.Span{}, false
	}
	a, err1 := strconv.Atoi(start)
	b, err2 := strconv.Atoi(end)
	if err1 != nil || err2 != nil || a < 0 || a >= b {
		return cut.Span{}, false
	}
	return cut.Span{Start: a, End: b}, true
}

// span is a Span's page: a real, bookmarkable URL for a Span of an Item,
// which needs no Cut to list it, only to lie within the Item's text.
func (a *app) span(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sourceID, entryID := r.PathValue("source"), r.PathValue("entry")
	sp, ok := spanOfPath(r.PathValue("span"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	it, found, err := a.Readings.KeptItem(ctx, sourceID, entryID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !found || sp.End > len(it.Content) {
		http.NotFound(w, r)
		return
	}
	at := editor.Address{SourceID: sourceID, EntryID: entryID, Span: sp}
	j, err := a.judgementsOf(ctx, sourceID, entryID, it.Published)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	c, _, _, err := a.Cuts.NewestCut(ctx, sourceID, entryID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p := spanPage{
		Span:      spanOf(slices.Index(c.Spans, sp)+1, it.Content, at, it.Published, j),
		Item:      a.cite(sourceID, it.Title, itemPath(sourceID, entryID)),
		Published: when(it.Published, a.Now().Location()),
	}
	if j.read {
		p.ReadBy = &made{Act: "read", Call: j.reading.Call}
	}
	if p.Stories, p.Citing, err = a.holding(ctx, at); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, spanTmpl, p)
}

// Title is how the page titles the Span: its number in its Item's
// newest Cut, if a Cut lists it.
func (p spanPage) Title() string {
	if p.Span.N > 0 {
		return fmt.Sprintf("S%d", p.Span.N)
	}
	return "Span"
}
