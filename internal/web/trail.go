package web

import (
	"net/http"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

// trailTmpl is a Briefing's trail: how it was written.
var trailTmpl = page("trail.html")

func briefingTrailPath(day string) string { return briefingPath(day) + "/trail" }

// trailPage is the view model for a Briefing's trail: the call that wrote
// it, then, for each Development it cites, the placing call that made it and
// the reading call that drew its Fact, with tokens per call and summed over
// the distinct calls on the page.
type trailPage struct {
	Date         string
	BriefingPath string
	Writing      *trailCall
	// WithoutCall is whether the Briefing was written empty, with no model
	// call.
	WithoutCall bool
	Citations   []trailCitation
	Tokens      tokens
	// Calls is how many distinct calls Tokens sums.
	Calls int
}

type trailCall struct {
	Page   string
	Act    string
	At     string
	Model  string
	Tokens tokens
}

type trailCitation struct {
	Story           string
	StoryPath       string
	Statement       string
	DevelopmentPath string
	// Missed is whether the Owner added the Development, so no placing call
	// made it.
	Missed  bool
	Placing *trailCall
	Reading *trailCall
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
	seen := map[int64]*trailCall{}
	// call is the kept call with id as the trail shows it, counted once in
	// the page's tokens however often it is shown; nil for none.
	call := func(id int64) (*trailCall, error) {
		if id == 0 {
			return nil, nil
		}
		if c, ok := seen[id]; ok {
			return c, nil
		}
		c, found, err := a.Calls.Call(r.Context(), id)
		if err != nil || !found {
			return nil, err
		}
		row := &trailCall{Page: callPath(c.ID), Act: c.Act, At: when(c.At, now.Location()), Model: c.Model}
		if c.Response != nil {
			row.Tokens = tokensOf(c.Response.Usage)
		}
		seen[id] = row
		p.Tokens, p.Calls = p.Tokens.add(row.Tokens), p.Calls+1
		return row, nil
	}
	calls, err := a.Calls.CallsOn(r.Context(), briefingAct, key)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	for _, c := range calls {
		if c.Outcome == editor.Applied {
			if p.Writing, err = call(c.ID); err != nil {
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
		if row.Placing, err = call(d.PlacingCall); err != nil {
			a.serverError(w, r, err)
			return
		}
		if row.Reading, err = call(c.ReadingCall); err != nil {
			a.serverError(w, r, err)
			return
		}
		p.Citations = append(p.Citations, row)
	}
	a.render(w, r, trailTmpl, p)
}
