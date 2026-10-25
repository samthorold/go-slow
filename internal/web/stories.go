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
type storyPage struct {
	Title     string
	Timeline  []developmentRow
	Causes    []causeRow
	Connected []connectedRow
}

// developmentRow is one Development on a Timeline, tracing to its Fact and
// the Item the Fact was drawn from.
type developmentRow struct {
	// Anchor is the Development's id on the page, which the Briefing's
	// statements link to.
	Anchor    string
	AsAt      string
	Statement string
	Quote     string
	ItemTitle string
	ItemPath  string
}

// causeRow is a cause Connection, each Fact tracing to its Item.
type causeRow struct {
	Cause, Effect developmentRow
}

// connectedRow is a Story connected to this one, with the shared Facts that
// are the reason.
type connectedRow struct {
	Path     string
	Title    string
	Shared   []developmentRow
	Entities []sharedEntityRow
}

// sharedEntityRow is an Entity both Stories' Facts involve in one role.
type sharedEntityRow struct {
	Entity, Role string
	Here, There  developmentRow
}

func developmentView(d editor.Development) developmentRow {
	return developmentRow{
		Anchor: developmentAnchor(d.ID),
		AsAt:   factDate(d.Fact), Statement: d.Statement, Quote: d.Quote,
		ItemTitle: d.ItemTitle, ItemPath: itemPath(d.SourceID, d.EntryID),
	}
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
	p := storyPage{Title: s.Title}
	for _, d := range s.Timeline {
		p.Timeline = append(p.Timeline, developmentView(d))
	}
	for _, c := range s.Causes {
		p.Causes = append(p.Causes, causeRow{Cause: developmentView(c.Cause), Effect: developmentView(c.Effect)})
	}
	for _, c := range connected {
		row := connectedRow{Path: storyPath(c.ID), Title: c.Title}
		for _, d := range c.Shared {
			row.Shared = append(row.Shared, developmentView(d))
		}
		for _, e := range c.Entities {
			row.Entities = append(row.Entities, sharedEntityRow{
				Entity: e.Entity, Role: e.Role, Here: developmentView(e.Here), There: developmentView(e.There),
			})
		}
		p.Connected = append(p.Connected, row)
	}
	a.render(w, r, storyTmpl, p)
}
