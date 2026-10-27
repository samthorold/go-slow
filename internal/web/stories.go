package web

import (
	"context"
	"net/http"
	"strconv"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/feedback"
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
	// FactsToAdd are the Facts the Owner may say the Story id missed, as
	// Developments.
	FactsToAdd(ctx context.Context, id int64) ([]editor.Fact, error)
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
	// FeedbackPath is where Feedback given on the page goes.
	FeedbackPath string
	Title        string
	// Unwanted and Missed are what Feedback has said of the Story.
	Unwanted, Missed bool
	Gives            []giveButton
	// MissedStory is the form for a missed Story gathering this one's
	// Developments.
	MissedStory       missedStoryForm
	MissedDevelopment missedDevelopmentForm
	Timeline          []developmentRow
	Causes            []causeRow
	Connected         []connectedRow
	// Feedback is the Feedback on the Story, its Developments, their Facts
	// and its Connections, each to undo here.
	Feedback []keptRow
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
	// Involves are the Entities its Fact involves, each in its role.
	Involves []involvedRow
	// Withdrawn and Unwanted are what Feedback has marked it.
	Withdrawn, Unwanted, Missed bool
	// Call is the page of the placing call that made it a Development, if
	// one was kept; set only on its own Story's Timeline.
	Call string
	// Gives is the Feedback the Owner can give on it here.
	Gives []giveButton
}

// causeRow is a cause Connection, each Fact tracing to its Item.
type causeRow struct {
	Cause, Effect developmentRow
	Withdrawn     bool
	// Call is the page of the connecting call that made it, if one was
	// kept.
	Call  string
	Gives []giveButton
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

// involvedRow is an Entity a Fact involves, in its role.
type involvedRow struct {
	Entity, Role string
}

// involvedView is the Entities f involves, in their roles.
func involvedView(f editor.Fact) []involvedRow {
	var out []involvedRow
	for _, r := range f.Involves {
		out = append(out, involvedRow{Entity: r.Entity, Role: r.Role})
	}
	return out
}

func developmentView(d editor.Development) developmentRow {
	return developmentRow{
		Anchor: developmentAnchor(d.ID),
		AsAt:   factDate(d.Fact), Statement: d.Statement, Quote: d.Quote,
		ItemTitle: d.ItemTitle, ItemPath: itemPath(d.SourceID, d.EntryID),
		Involves:  involvedView(d.Fact),
		Withdrawn: d.Withdrawn, Unwanted: d.Unwanted, Missed: d.Missed,
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
	a.storyWith(w, r, id, http.StatusOK, feedback.Draft{}, nil)
}

// storyWith renders a Story's page with the Feedback last entered on it and
// why it failed, if it did.
func (a *app) storyWith(w http.ResponseWriter, r *http.Request, id int64, status int, d feedback.Draft, errs feedback.Errors) {
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
	kept, err := a.Kept.KeptFeedback(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	page := storyPath(id)
	p := storyPage{Title: s.Title, FeedbackPath: page + "/feedback", Unwanted: s.Unwanted, Missed: s.Missed}
	p.Gives = notOfferedAgain(givenIn(storyGives(p.FeedbackPath, id), kept, page), s.Unwanted)
	for _, d := range s.Timeline {
		row := developmentView(d)
		row.Call = callPath(d.PlacingCall)
		if !d.Withdrawn {
			row.Gives = notOfferedAgain(givenIn(developmentGives(p.FeedbackPath, id, d.ID), kept, page), d.Unwanted)
		}
		p.Timeline = append(p.Timeline, row)
	}
	for _, c := range s.Causes {
		p.Causes = append(p.Causes, causeRow{
			Cause: developmentView(c.Cause), Effect: developmentView(c.Effect),
			Withdrawn: c.Withdrawn, Call: callPath(c.Call),
		})
		if !c.Withdrawn {
			p.Causes[len(p.Causes)-1].Gives = givenIn(connectionGives(p.FeedbackPath, c.Cause.ID, c.Effect.ID), kept, page)
		}
	}
	p.MissedStory = missedStory(p.FeedbackPath, s.Timeline, d, errs)
	if p.MissedDevelopment, err = a.missedDevelopment(r, p.FeedbackPath, id, d, errs); err != nil {
		a.serverError(w, r, err)
		return
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
	p.Feedback = a.storyFeedbackRows(s, kept)
	a.renderStatus(w, r, status, storyTmpl, p)
}

// notOfferedAgain is gives, less its first (Unwanted) when the target is
// marked unwanted and that button isn't already shown as given.
func notOfferedAgain(gives []giveButton, unwanted bool) []giveButton {
	if unwanted && gives[0].UndoPath == "" {
		return gives[1:]
	}
	return gives
}

// storyFeedbackRows is the Feedback in kept about s: on the Story or one of
// its Developments, or on a Fact or Connection its page shows.
func (a *app) storyFeedbackRows(s editor.Story, kept []feedback.Kept) []keptRow {
	facts := map[int64]bool{}
	for _, d := range s.Timeline {
		facts[d.ID] = true
	}
	for _, c := range s.Causes {
		facts[c.Cause.ID], facts[c.Effect.ID] = true, true
	}
	var out []keptRow
	for _, k := range kept {
		about := k.StoryID == s.ID ||
			(k.Level == feedback.Fact && facts[k.FactID]) ||
			(k.Level == feedback.Connection && facts[k.FactID] && facts[k.ToFactID])
		if about {
			out = append(out, a.keptRow(k, storyPath(s.ID)))
		}
	}
	return out
}

// undoOnStory is an undo from a Story's page, which it lands back on.
func (a *app) undoOnStory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a.undo(w, r, storyPath(id))
}
