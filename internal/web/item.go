package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/samthorold/go-slow/internal/editor"
)

// Readings answers the queries an Item's page needs.
type Readings interface {
	// ReadItem is one Item with what reading drew from it, reporting
	// whether the Item is in the Record.
	ReadItem(ctx context.Context, sourceID, entryID string) (editor.ReadItem, bool, error)
}

// itemPage is the view model for an Item's page: the Item as collected, and
// the Facts and Claims reading drew from it.
type itemPage struct {
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
	Facts       []factRow
	Claims      []claimRow
}

type factRow struct {
	// Anchor is the Fact's id on the page, which the Briefing's statements
	// link to.
	Anchor    string
	Statement string
	AsAt      string
	Quote     string
	Entities  string
	// Call is the page of the reading call that drew it, if one was kept.
	Call string
}

type claimRow struct {
	Statement string
	MadeBy    string
	Quote     string
	Entities  string
}

// itemPath is an Item's page: its Source and its entry id, each one path
// segment.
func itemPath(sourceID, entryID string) string {
	return "/items/" + url.PathEscape(sourceID) + "/" + url.PathEscape(entryID)
}

func (a *app) item(w http.ResponseWriter, r *http.Request) {
	it, found, err := a.Readings.ReadItem(r.Context(), r.PathValue("source"), r.PathValue("entry"))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
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
	}
	for _, f := range it.Facts {
		p.Facts = append(p.Facts, factRow{
			Anchor:    factAnchor(f.ID),
			Statement: f.Statement,
			// A Fact's date is a calendar date, in no time zone.
			AsAt:     f.AsAt.Format("2 January 2006"),
			Quote:    f.Quote,
			Entities: strings.Join(f.Entities, ", "),
			Call:     callPath(it.ReadingCall),
		})
	}
	for _, c := range it.Claims {
		p.Claims = append(p.Claims, claimRow{
			Statement: c.Statement, MadeBy: c.MadeBy, Quote: c.Quote, Entities: strings.Join(c.Entities, ", "),
		})
	}
	a.render(w, r, itemTmpl, p)
}
