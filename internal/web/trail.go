package web

import (
	"net/http"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

// trailTmpl is a Briefing's trail: how it was written.
var trailTmpl = page(noIndex, "trail.html")

func briefingTrailPath(day string) string { return briefingPath(day) + "/trail" }

// trailPage is the view model for a Briefing's trail: a table whose first
// row is the call that wrote it, then a row for each Development it cites,
// with the placing call that made it and the reading call that drew its
// Fact, each a machine label with its tokens; the tokens are summed over the
// distinct calls on the page.
type trailPage struct {
	Date         string
	BriefingPath string
	// Writing is the call that wrote the Briefing, nil if none was kept.
	Writing *made
	// WithoutCall is whether the Briefing was written empty, with no model
	// call.
	WithoutCall bool
	Citations   []trailCitation
	Tokens      tokens
	// Calls is how many distinct calls Tokens sums.
	Calls int
}

type trailCitation struct {
	Story           string
	StoryPath       string
	Statement       string
	DevelopmentPath string
	// Missed is whether the Owner added the Development, so no placing call
	// made it.
	Missed bool
	// Placing and Reading are the calls behind the Development, nil where
	// none was kept.
	Placing *made
	Reading *made
}

// briefingTrail is a written Briefing's trail. Checking how something was
// made is not reading it, so opening it records no open.
func (a *app) briefingTrail(w http.ResponseWriter, r *http.Request) {
	now := a.Now()
	day, err := time.ParseInLocation(time.DateOnly, r.PathValue("day"), now.Location())
	if err != nil || day.After(now) {
		http.NotFound(w, r)
		return
	}
	key := day.Format(time.DateOnly)
	b, exists, err := a.Briefings.Briefing(r.Context(), key)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !exists {
		http.NotFound(w, r)
		return
	}
	p := trailPage{Date: day.Format("Monday 2 January 2006"), BriefingPath: briefingPath(key)}
	seen := map[int64]tokens{}
	// call is the kept call with id as the trail labels it, the act worded
	// act, counted once in the page's tokens however often it is shown; nil
	// for none.
	call := func(act string, id int64) (*made, error) {
		if id == 0 {
			return nil, nil
		}
		t, ok := seen[id]
		if !ok {
			c, found, err := a.Calls.Call(r.Context(), id)
			if err != nil || !found {
				return nil, err
			}
			if c.Response != nil {
				t = tokensOf(c.Response.Usage)
			}
			seen[id] = t
			p.Tokens, p.Calls = p.Tokens.add(t), p.Calls+1
		}
		return &made{Act: act, Call: id, Tokens: t.Total, Counted: true}, nil
	}
	calls, err := a.Calls.CallsOn(r.Context(), briefingAct, key)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	for _, c := range calls {
		if c.Outcome == editor.Applied {
			if p.Writing, err = call("written", c.ID); err != nil {
				a.serverError(w, r, err)
				return
			}
		}
	}
	p.WithoutCall = p.Writing == nil && b.Model == ""
	cites, err := a.Calls.BriefingCitations(r.Context(), key)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	for _, c := range cites {
		d := c.Development
		row := trailCitation{
			Story: c.StoryTitle, StoryPath: storyPath(c.StoryID), Statement: d.Statement,
			DevelopmentPath: storyPath(c.StoryID) + "#" + developmentAnchor(d.ID), Missed: d.Missed,
		}
		if row.Placing, err = call("placed", d.PlacingCall); err != nil {
			a.serverError(w, r, err)
			return
		}
		if row.Reading, err = call("read", c.ReadingCall); err != nil {
			a.serverError(w, r, err)
			return
		}
		p.Citations = append(p.Citations, row)
	}
	a.render(w, r, trailTmpl, p)
}
