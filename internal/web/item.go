package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/feedback"
)

// Readings answers the queries an Item's page needs.
type Readings interface {
	// ReadItem is one Item with what reading drew from it, reporting
	// whether the Item is in the Record.
	ReadItem(ctx context.Context, sourceID, entryID string) (editor.ReadItem, bool, error)
}

// Cuts answers the Spans an Item's page shows.
type Cuts interface {
	// NewestCut is the Item's newest Cut, with the Item's text as kept,
	// reporting whether it has one.
	NewestCut(ctx context.Context, sourceID, entryID string) (cut.Cut, string, bool, error)
}

// spanRow is one Span of the Item's newest Cut: its number in the Cut, and
// its words.
type spanRow struct {
	N     int
	Words string
}

// itemPage is the view model for an Item's page: the Item as collected, its
// text as the Spans of its newest Cut, and the Facts and Claims reading drew
// from it.
type itemPage struct {
	// Spans are the Spans of the Item's newest Cut, in order; none if it
	// has none yet. CutBy is the Cut's Judge, as a machine label.
	Spans       []spanRow
	CutBy       string
	Title       string
	Link        string
	Summary     string
	Source      string
	Published   string
	Attribution string
	Read        bool
	ReadAt      string
	Model       string
	Topics      []string
	Mentions    []pill
	Facts       []factRow
	Claims      []claimRow
	// FeedbackError is why Feedback last given on the page failed, if it
	// did.
	FeedbackError string
}

type factRow struct {
	// Anchor is the Fact's id on the page, which the Briefing's statements
	// link to.
	Anchor    string
	Statement string
	// AsAt is the Fact's date, or "" where it is the Item's.
	AsAt  string
	Quote string
	// Entities are the Entities it involves, each with its Role; none its
	// Item only mentions.
	Entities []pill
	// Made is the reading call that drew it.
	Made made
	// Gives is the Feedback the Owner can give on it here: wrong.
	Gives gives
}

type claimRow struct {
	Statement string
	MadeBy    string
	Quote     string
	// Made is the reading call that drew it.
	Made made
}

// itemPath is an Item's page: its Source and its entry id, each one path
// segment.
func itemPath(sourceID, entryID string) string {
	return "/items/" + url.PathEscape(sourceID) + "/" + url.PathEscape(entryID)
}

func (a *app) item(w http.ResponseWriter, r *http.Request) {
	a.itemWith(w, r, http.StatusOK, nil)
}

// itemWith renders the Item's page the path names, with status, and why
// Feedback given on it failed, if it did.
func (a *app) itemWith(w http.ResponseWriter, r *http.Request, status int, errs feedback.Errors) {
	sourceID, entryID := r.PathValue("source"), r.PathValue("entry")
	it, found, err := a.Readings.ReadItem(r.Context(), sourceID, entryID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	kept, err := a.Kept.KeptFeedback(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	page := itemPath(sourceID, entryID)
	loc := a.Now().Location()
	p := itemPage{
		Title:       it.Title,
		Link:        it.Link,
		Summary:     it.Summary,
		Source:      it.SourceName,
		Published:   when(it.Published, loc),
		Attribution: it.Attribution,
		Read:        it.Read,
		ReadAt:      when(it.ReadAt, loc),
		Model:       it.Model,
		Topics:      it.Topics,
		// The page's one form is a button naming its Fact: a failure is
		// the form's, or that Fact's.
		FeedbackError: errs[""],
	}
	if p.FeedbackError == "" {
		p.FeedbackError = errs["fact"]
	}
	c, text, cutYet, err := a.Cuts.NewestCut(r.Context(), sourceID, entryID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if cutYet {
		p.CutBy = fmt.Sprintf("cut · %s v%d", c.Judge.Cutter, c.Judge.Version)
		for i, s := range c.Spans {
			p.Spans = append(p.Spans, spanRow{N: i + 1, Words: cut.Words(text, s)})
		}
	}
	for _, e := range it.Mentions {
		p.Mentions = append(p.Mentions, pillMentioned(e))
	}
	// A Fact's date is a calendar date, in no time zone; the Item's is the
	// day it was published where the Owner is.
	const day = "2 January 2006"
	published := ""
	if !it.Published.IsZero() {
		published = it.Published.In(loc).Format(day)
	}
	for _, f := range it.Facts {
		asAt := f.AsAt.Format(day)
		if asAt == published {
			asAt = ""
		}
		p.Facts = append(p.Facts, factRow{
			Anchor:    factAnchor(f.ID),
			Statement: f.Statement,
			AsAt:      asAt,
			Quote:     f.Quote,
			Entities:  pillsInvolved(f),
			Made:      made{Act: "read", Call: it.ReadingCall},
			Gives:     givenIn(factGives(page+"/feedback", f.ID), kept, page),
		})
	}
	// A Claim shows no Entities: a reading gives a Claim none, and who made
	// it is its attribution.
	for _, c := range it.Claims {
		p.Claims = append(p.Claims, claimRow{Statement: c.Statement, MadeBy: c.MadeBy, Quote: c.Quote, Made: made{Act: "read", Call: it.ReadingCall}})
	}
	a.renderStatus(w, r, status, itemTmpl, p)
}

// itemFeedback is Feedback given on an Item's page, about one of its Facts.
// It lands back on the page.
func (a *app) itemFeedback(w http.ResponseWriter, r *http.Request) {
	sourceID, entryID := r.PathValue("source"), r.PathValue("entry")
	if _, found, err := a.Readings.ReadItem(r.Context(), sourceID, entryID); err != nil || !found {
		if err != nil {
			a.serverError(w, r, err)
		} else {
			http.NotFound(w, r)
		}
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad form.", http.StatusBadRequest)
		return
	}
	_, errs, err := a.Feedback.Give(r.Context(), draftFrom(r), a.Now())
	switch {
	case errors.Is(err, feedback.ErrNotInRecord):
		http.NotFound(w, r)
	case err != nil:
		a.serverError(w, r, err)
	case len(errs) > 0:
		a.itemWith(w, r, http.StatusUnprocessableEntity, errs)
	default:
		http.Redirect(w, r, itemPath(sourceID, entryID), http.StatusSeeOther)
	}
}

// undoOnItem is an undo from an Item's page, which it lands back on.
func (a *app) undoOnItem(w http.ResponseWriter, r *http.Request) {
	a.undo(w, r, itemPath(r.PathValue("source"), r.PathValue("entry")))
}
