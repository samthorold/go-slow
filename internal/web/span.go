package web

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// Relations answers what a Span's page shows of its relations.
type Relations interface {
	// RelationsOf are the relations judged of the Span at a, either way,
	// each with the Span at its other end and the call that made it.
	RelationsOf(ctx context.Context, a editor.Address) ([]editor.SpanRelation, error)
	// LatestGathering is the latest Story gathered from the Span at a,
	// reporting whether one was.
	LatestGathering(ctx context.Context, a editor.Address) (editor.GatheredStory, bool, error)
}

// gatheringRow is a Span's latest gathering, as its page shows it: the
// Briefing that gathered it, linked, and the call that wrote it, and the
// Spans it holds, each linking its page with its Item.
type gatheringRow struct {
	BriefingPath, BriefingDate string
	Made                       made
	Spans                      []spanLink
}

// spanLink is a Span linking its page, with its Item.
type spanLink struct {
	Words string
	Href  string
	Item  itemCitation
}

// spanPage is the view model for a Span's page: the Span as its Item's
// newest reading reads it, its Item, and its relations both ways.
type spanPage struct {
	Span spanRow
	Item itemCitation
	// Published is when its Item was published.
	Published string
	// ReadBy is the call of its Item's newest reading, if it has one.
	ReadBy *made
	// Gathered is its latest gathering, if a Briefing told it; Relations
	// are then the relations judged since, else every relation judged of
	// it, none of them yet gathered (system design: A Story is gathered by
	// the pass that tells it).
	Gathered  *gatheringRow
	Relations []relationRow
}

// relationRow is one relation of a Span, as its page shows it: how it
// relates (worded from the Span's side: "follows", "followed by", "same
// as", "contradicts"), the Span at its other end, linking its page, with
// its Item, and the call that made it.
type relationRow struct {
	How   string
	Words string
	Href  string
	Item  itemCitation
	Made  made
}

// linkTo is the Span o, linking its page, with its Item.
func (a *app) linkTo(o editor.OtherSpan) spanLink {
	return spanLink{Words: o.Words, Href: spanPath(o.At),
		Item: a.cite(o.Item.SourceID, o.Item.Title, itemPath(o.Item.SourceID, o.Item.EntryID))}
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

// howRelated is how rel relates the Span at a, worded from its side.
func howRelated(rel editor.Relation, a editor.Address) string {
	switch {
	case rel.Kind == editor.Follows && rel.From == a:
		return "follows"
	case rel.Kind == editor.Follows:
		return "followed by"
	case rel.Kind == editor.SameAs:
		return "same as"
	}
	return "contradicts"
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
	it, found, err := a.Readings.ReadItem(ctx, sourceID, entryID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !found || sp.End > len(it.Content) {
		http.NotFound(w, r)
		return
	}
	at := editor.Address{SourceID: sourceID, EntryID: entryID, Span: sp}
	reading, read, err := a.Readings.NewestReading(ctx, sourceID, entryID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	is, err := a.Readings.EntitiesNamed(ctx, reading.Keys(it.Published))
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
		Span:      spanOf(slices.Index(c.Spans, sp)+1, it.Content, sp, reading, it.Published, is),
		Item:      a.cite(sourceID, it.Title, itemPath(sourceID, entryID)),
		Published: when(it.Published, a.Now().Location()),
	}
	if read {
		p.ReadBy = &made{Act: "read", Call: reading.Call}
	}
	rels, err := a.Relations.RelationsOf(ctx, at)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	g, gathered, err := a.Relations.LatestGathering(ctx, at)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if gathered {
		day, err := time.ParseInLocation(time.DateOnly, g.Day, a.Now().Location())
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		p.Gathered = &gatheringRow{BriefingPath: briefingPath(g.Day), BriefingDate: day.Format("Monday 2 January 2006"),
			Made: made{Act: "gathered", Call: g.Call}}
		for _, o := range g.Spans {
			p.Gathered.Spans = append(p.Gathered.Spans, a.linkTo(o))
		}
	}
	for _, rel := range rels {
		if gathered && rel.ID < g.ID {
			continue // judged before the latest gathering, which weighed it
		}
		o := rel.Other
		p.Relations = append(p.Relations, relationRow{
			How:   howRelated(rel.Relation, at),
			Words: o.Words,
			Href:  spanPath(o.At),
			Item:  a.cite(o.Item.SourceID, o.Item.Title, itemPath(o.Item.SourceID, o.Item.EntryID)),
			Made:  made{Act: "related", Call: rel.Call},
		})
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
