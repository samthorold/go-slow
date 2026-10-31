package web

import (
	"context"
	"net/http"
	"strconv"

	"github.com/samthorold/go-slow/internal/editor"
)

// Stories answers the queries the Stories pages need.
type Stories interface {
	// ListStories lists the Stories, the one moved on most recently first.
	ListStories(ctx context.Context) ([]editor.StoryListing, error)
	// Story is one Story with its Timeline, reporting whether it is in the
	// Record.
	Story(ctx context.Context, id int64) (editor.Story, bool, error)
	// ConnectedStories are the Stories connected to the Story id, derived
	// from what they share.
	ConnectedStories(ctx context.Context, id int64) ([]editor.StoryConnection, error)
}

// storiesPage is the view model for the Stories page.
type storiesPage struct {
	Stories []storyRow
}

type storyRow struct {
	Path         string
	Title        string
	Developments int
	Latest       string
}

// storyPage is the view model for a Story's page: its Timeline, the cause
// Connections to and from its Developments, and the Stories connected to it.
// A Story of Facts is kept as the Record holds it, before the Span was its
// unit: Feedback is no longer given on one, and what earlier Feedback
// marked shows as marked.
type storyPage struct {
	Title string
	// Unwanted and Missed are what Feedback has said of the Story.
	Unwanted, Missed bool
	// Timeline is the Story's Developments in order, a run of them from
	// one Item to an entry.
	Timeline  []timelineEntry
	Causes    []causeRow
	Connected []connectedRow
}

// developmentRow is one Development, as a block: its statement in the body,
// its notes in the margin. The Item its Fact was drawn from is its
// timeline entry's.
type developmentRow struct {
	// Anchor is the Development's id on the page, which the Briefing's
	// statements link to.
	Anchor string
	// AsAt is its Fact's date; on a Timeline, only where it differs from
	// its entry's.
	AsAt      string
	Statement string
	Quote     string
	// Pills are the Entities its Fact involves, each with its Role.
	Pills []pill
	// Withdrawn and Unwanted are what Feedback has marked it.
	Withdrawn, Unwanted, Missed bool
	// Made is the placing call that made it a Development, if one was
	// kept; set only on its own Story's Timeline.
	Made made
}

// Struck reports whether it is shown struck through: Feedback took it out
// of the Briefing, and the Record keeps it, marked.
func (d developmentRow) Struck() bool { return d.Withdrawn || d.Unwanted }

// causeRow is a cause Connection, each Fact tracing to its Item.
type causeRow struct {
	Cause, Effect developmentRow
	// CauseItem and EffectItem are the Items its Facts were drawn from.
	CauseItem, EffectItem itemCitation
	Withdrawn             bool
	// Made is the connecting call that made it, if one was kept.
	Made made
}

// connectedRow is a Story connected to this one, with what the two share,
// the reasons.
type connectedRow struct {
	Path  string
	Title string
	// Shared are the Developments both have.
	Shared []developmentRow
	// Items are the Items both have Developments from: announced together.
	Items []itemCitation
	// Entities are the Entities both Stories' Facts involve in one Role,
	// the rarest over Stories first.
	Entities []pill
}

func developmentView(d editor.Development) developmentRow {
	return developmentRow{
		Anchor: developmentAnchor(d.ID),
		AsAt:   factDate(d.Fact), Statement: d.Statement, Quote: d.Quote,
		Pills:     pillsInvolved(d.Fact),
		Withdrawn: d.Withdrawn, Unwanted: d.Unwanted, Missed: d.Missed,
	}
}

// citeItem is the Item d's Fact was drawn from, cited.
func (a *app) citeItem(d editor.Development) itemCitation {
	return a.cite(d.SourceID, d.ItemTitle, itemPath(d.SourceID, d.EntryID))
}

func storyPath(id int64) string { return "/stories/" + strconv.FormatInt(id, 10) }

// factDate is a Fact's date: a calendar date, in no time zone.
func factDate(f editor.Fact) string { return f.AsAt.Format("2 January 2006") }

func (a *app) stories(w http.ResponseWriter, r *http.Request) {
	list, err := a.Stories.ListStories(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var p storiesPage
	for _, s := range list {
		row := storyRow{Path: storyPath(s.ID), Title: s.Title, Developments: s.Developments}
		if !s.Latest.IsZero() {
			row.Latest = s.Latest.Format("2 January 2006")
		}
		p.Stories = append(p.Stories, row)
	}
	a.render(w, r, storiesTmpl, p)
}

func (a *app) story(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s, found, err := a.Stories.Story(r.Context(), id)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	connected, err := a.Stories.ConnectedStories(r.Context(), id)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p := storyPage{Title: s.Title, Unwanted: s.Unwanted, Missed: s.Missed}
	p.Timeline = timelineOf(s.Timeline, a.citeItem, func(d editor.Development) developmentRow {
		row := developmentView(d)
		row.Made = made{Act: "placed", Call: d.PlacingCall}
		return row
	})
	for _, c := range s.Causes {
		p.Causes = append(p.Causes, causeRow{
			Cause: developmentView(c.Cause), Effect: developmentView(c.Effect),
			CauseItem: a.citeItem(c.Cause), EffectItem: a.citeItem(c.Effect),
			Withdrawn: c.Withdrawn, Made: made{Act: "connected", Call: c.Call},
		})
	}
	for _, c := range connected {
		row := connectedRow{Path: storyPath(c.ID), Title: c.Title}
		for _, d := range c.Shared {
			row.Shared = append(row.Shared, developmentView(d))
		}
		for _, it := range c.Items {
			row.Items = append(row.Items, a.cite(it.SourceID, it.Title, itemPath(it.SourceID, it.EntryID)))
		}
		for _, e := range c.Entities {
			row.Entities = append(row.Entities, pill{Name: e.Entity, Tag: e.Role})
		}
		p.Connected = append(p.Connected, row)
	}
	a.render(w, r, storyTmpl, p)
}
