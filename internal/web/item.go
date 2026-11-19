package web

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// Readings answers the queries an Item's page needs.
type Readings interface {
	// KeptItem is one Item as collected, reporting whether it is in the
	// Record.
	KeptItem(ctx context.Context, sourceID, entryID string) (editor.KeptItem, bool, error)
	// NewestReading is the Item's newest reading of its Spans, reporting
	// whether it has one.
	NewestReading(ctx context.Context, sourceID, entryID string) (editor.Reading, bool, error)
	// EntitiesNamed is the Entity each key names, as the Record's look-ups
	// have it now; a key not yet looked up is left out.
	EntitiesNamed(ctx context.Context, keys []editor.LookupKey) (map[editor.LookupKey]editor.NamedEntity, error)
}

// Cuts answers the Spans an Item's page shows.
type Cuts interface {
	// NewestCut is the Item's newest Cut, with the Item's text as kept,
	// reporting whether it has one.
	NewestCut(ctx context.Context, sourceID, entryID string) (cut.Cut, string, bool, error)
}

// spanRow is one Span of the Item's newest Cut, as its newest reading
// judged it: its number in the Cut, its hydrated reading (its words, each
// stretch that refers to another read as what it refers to), whether it is
// Rejected, whose words it is and what it names.
type spanRow struct {
	N        int
	Pieces   []editor.Piece
	Rejected bool
	// Speakers are whose words it is.
	Speakers []judged
	// RefersTo are what each stretch in it refers to: a margin line, since
	// the body is collected text, inert (hx-disable).
	RefersTo []judged
	// Names are the Entities it mentions.
	Names []pill
	// Href is the Span's own page, where the Stories holding it are; empty
	// where it is shown on that page.
	Href string
}

// itemPage is the view model for an Item's page: the Item as collected, and
// its text as the Spans of its newest Cut, read as its newest reading judged
// them.
type itemPage struct {
	// Spans are the Spans of the Item's newest Cut, in order; none if it
	// has none yet. CutBy is what made the Cut, as a machine label.
	Spans []spanRow
	CutBy string
	// Story is the Page of the Story its newest Cut made, the one way to it.
	Story string
	// ReadBy is the call of the Item's newest reading, if it has one.
	ReadBy      *made
	Title       string
	Link        string
	Summary     string
	Source      string
	Published   string
	Attribution string
}

// itemPath is an Item's page: its Source and its entry id, each one path
// segment.
func itemPath(sourceID, entryID string) string {
	return "/items/" + url.PathEscape(sourceID) + "/" + url.PathEscape(entryID)
}

// spanJudgements is what an Item's page and a Span's need to show its
// Spans as read: the Item's newest reading and the Entities its names are.
type spanJudgements struct {
	reading editor.Reading
	read    bool
	is      map[editor.LookupKey]editor.NamedEntity
}

// judgementsOf is what the pages need to show the Spans of the Item.
func (a *app) judgementsOf(ctx context.Context, sourceID, entryID string, published time.Time) (spanJudgements, error) {
	var j spanJudgements
	var err error
	if j.reading, j.read, err = a.Readings.NewestReading(ctx, sourceID, entryID); err != nil {
		return j, err
	}
	keys := j.reading.Keys(published)
	j.is, err = a.Readings.EntitiesNamed(ctx, keys)
	return j, err
}

// item renders the Item's page the path names.
func (a *app) item(w http.ResponseWriter, r *http.Request) {
	sourceID, entryID := r.PathValue("source"), r.PathValue("entry")
	it, found, err := a.Readings.KeptItem(r.Context(), sourceID, entryID)
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
	}
	c, text, cutYet, err := a.Cuts.NewestCut(r.Context(), sourceID, entryID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	j, err := a.judgementsOf(r.Context(), sourceID, entryID, it.Published)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if j.read {
		p.ReadBy = &made{Act: "read", Call: j.reading.Call}
	}
	if cutYet {
		p.CutBy = fmt.Sprintf("cut · %s v%d", c.By.Cutter, c.By.Version)
		p.Story = storyPath(c.ID)
		for i, s := range c.Spans {
			at := editor.Address{SourceID: sourceID, EntryID: entryID, Span: s}
			row := spanOf(i+1, text, at, it.Published, j)
			row.Href = spanPath(at)
			p.Spans = append(p.Spans, row)
		}
	}
	a.render(w, r, itemTmpl, p)
}

// spanOf is the Span at, numbered n in its Cut, of text, as j's reading
// judged it, in an Item published on published: what the reading judged of
// a stretch within it is shown with it, and the Entities it mentions as j
// has them. A Span no reading judged reads as its words.
func spanOf(n int, text string, at editor.Address, published time.Time, j spanJudgements) spanRow {
	s, r := at.Span, j.reading
	in := func(a editor.Address) bool { return a.Span.Start >= s.Start && a.Span.End <= s.End }
	row := spanRow{N: n, Pieces: inPlace(r.Hydrated(text, s))}
	for _, a := range r.Rejected {
		if a.Span == s {
			row.Rejected = true
		}
	}
	for _, ref := range r.RefersTo {
		if in(ref.From) {
			from, to := cut.Words(text, ref.From.Span), r.Words(text, ref.To)
			row.RefersTo = append(row.RefersTo, judged{Words: from, To: to})
		}
	}
	for _, sp := range r.Speakers {
		if in(sp.Of) {
			who := r.Words(text, sp.By)
			if !slices.ContainsFunc(row.Speakers, func(j judged) bool { return j.Words == who }) {
				row.Speakers = append(row.Speakers, judged{Words: who})
			}
		}
	}
	// What it mentions: each Entity its names are, or, for a name not yet
	// looked up, the name as read, a person by their office.
	var shown []editor.NamedEntity
	for _, nm := range r.Mentions(s) {
		k := editor.KeyOf(nm, published)
		e, ok := j.is[k]
		switch {
		case !ok:
			row.Names = append(row.Names, pill{Name: nm.Name, Tag: cmp.Or(nm.Office, nm.Kind)})
		case !slices.Contains(shown, e):
			shown = append(shown, e)
			row.Names = append(row.Names, pillOf(e))
		}
	}
	return row
}
