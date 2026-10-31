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
// row is the call that wrote it, then a row for each Span it cites, with the
// reading call behind the reading it was shown and the relating calls on its
// Item before it (or, for a Briefing over Stories, each Development it
// cites, with its placing and reading calls), each a machine label with its
// tokens; the tokens are summed over the distinct calls on the page.
type trailPage struct {
	Date         string
	BriefingPath string
	// Writing is the call that wrote the Briefing, nil if none was kept.
	Writing *made
	// WithoutCall is whether the Briefing was written empty, with no model
	// call.
	WithoutCall bool
	// Told is whether the Briefing tells Spans, each cited in Spans; one
	// over Stories cites its Developments in Citations.
	Told      bool
	Spans     []trailSpan
	Citations []trailCitation
	Tokens    tokens
	// Calls is how many distinct calls Tokens sums.
	Calls int
}

// trailSpan is a Span the Briefing cites, linking its page, with the calls
// behind it: Reading nil for a Span of an Item only cut (Unread) or with no
// call kept, and Relating each relating call on its Item before the
// Briefing.
type trailSpan struct {
	spanLink
	Unread   bool
	Reading  *made
	Relating []made
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
	told, isTold, err := a.Briefings.Briefing(r.Context(), key)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	b, exists, err := a.Briefings.StoryBriefing(r.Context(), key)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !exists && !isTold {
		http.NotFound(w, r)
		return
	}
	p := trailPage{Date: day.Format("Monday 2 January 2006"), BriefingPath: briefingPath(key), Told: isTold}
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
	model := b.Model
	if isTold {
		model = told.Judge.Model
		err = a.spansTrail(r, &p, key, call)
	} else {
		err = a.storiesTrail(r, &p, key, call)
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p.WithoutCall = p.Writing == nil && model == ""
	a.render(w, r, trailTmpl, p)
}

// spansTrail adds to p each Span the day's Briefing over Spans cites, with
// the calls behind it, each labelled by call.
func (a *app) spansTrail(r *http.Request, p *trailPage, day string, call func(string, int64) (*made, error)) error {
	cites, err := a.Calls.BriefingTrail(r.Context(), day)
	if err != nil {
		return err
	}
	for _, c := range cites {
		row := trailSpan{spanLink: a.linkTo(c.Span), Unread: c.Span.ReadingID == 0}
		if row.Reading, err = call("read", c.ReadingCall); err != nil {
			return err
		}
		for _, id := range c.RelatingCalls {
			m, err := call("related", id)
			if err != nil {
				return err
			}
			if m != nil {
				row.Relating = append(row.Relating, *m)
			}
		}
		p.Spans = append(p.Spans, row)
	}
	return nil
}

// storiesTrail adds to p each Development the day's Briefing over Stories
// cites, with the calls behind it, each labelled by call.
func (a *app) storiesTrail(r *http.Request, p *trailPage, day string, call func(string, int64) (*made, error)) error {
	cites, err := a.Calls.StoryBriefingCitations(r.Context(), day)
	if err != nil {
		return err
	}
	for _, c := range cites {
		d := c.Development
		row := trailCitation{
			Story: c.StoryTitle, StoryPath: storyPath(c.StoryID), Statement: d.Statement,
			DevelopmentPath: storyPath(c.StoryID) + "#" + developmentAnchor(d.ID), Missed: d.Missed,
		}
		if row.Placing, err = call("placed", d.PlacingCall); err != nil {
			return err
		}
		if row.Reading, err = call("read", c.ReadingCall); err != nil {
			return err
		}
		p.Citations = append(p.Citations, row)
	}
	return nil
}
