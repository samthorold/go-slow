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

// storyPage is the view model for a Story's page: its Timeline.
type storyPage struct {
	Title    string
	Timeline []developmentRow
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
	p := storyPage{Title: s.Title}
	for _, d := range s.Timeline {
		p.Timeline = append(p.Timeline, developmentRow{
			Anchor: developmentAnchor(d.ID),
			AsAt:   factDate(d.Fact), Statement: d.Statement, Quote: d.Quote,
			ItemTitle: d.ItemTitle, ItemPath: itemPath(d.SourceID, d.EntryID),
		})
	}
	a.render(w, r, storyTmpl, p)
}
