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
	"github.com/samthorold/go-slow/internal/feedback"
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
	// LookupsNamed is the look-up each key's Entity is read from, as
	// EntitiesNamed reads it.
	LookupsNamed(ctx context.Context, keys []editor.LookupKey) (map[editor.LookupKey]int64, error)
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
	Speakers []string
	Names    []pill
	// Href is the Span's own page, where its relations are; empty where it
	// is shown on that page.
	Href string
	// Gives is the Feedback the Owner can give on it here: missed on the
	// Span, and wrong on each judgement shown with it (what a stretch in it
	// refers to, whose words it is, which Entity a name in it is).
	Gives gives
}

// itemPage is the view model for an Item's page: the Item as collected, and
// its text as the Spans of its newest Cut, read as its newest reading judged
// them.
type itemPage struct {
	// Spans are the Spans of the Item's newest Cut, in order; none if it
	// has none yet. CutBy is the Cut's Judge, as a machine label.
	Spans []spanRow
	CutBy string
	// ReadBy is the call of the Item's newest reading, if it has one.
	ReadBy      *made
	Title       string
	Link        string
	Summary     string
	Source      string
	Published   string
	Attribution string
	// FeedbackError is why Feedback last given on the page was refused, if
	// it was.
	FeedbackError string
	// Feedback is the Feedback given on its Spans and the judgements made
	// of them, each to undo here: what wrong took out shows nowhere else.
	Feedback []pieceRow
}

// itemPath is an Item's page: its Source and its entry id, each one path
// segment.
func itemPath(sourceID, entryID string) string {
	return "/items/" + url.PathEscape(sourceID) + "/" + url.PathEscape(entryID)
}

func (a *app) item(w http.ResponseWriter, r *http.Request) {
	a.itemWith(w, r, http.StatusOK, nil)
}

// spanJudgements is what an Item's page and a Span's need to show its
// Spans as read: the Item's newest reading, the Entities its names are and
// the look-ups they are read from, and the Feedback kept as events.
type spanJudgements struct {
	reading editor.Reading
	read    bool
	is      map[editor.LookupKey]editor.NamedEntity
	lookups map[editor.LookupKey]int64
	pieces  []feedback.KeptPiece
}

// judgementsOf is what the pages need to show the Spans of the Item.
func (a *app) judgementsOf(ctx context.Context, sourceID, entryID string, published time.Time) (spanJudgements, error) {
	var j spanJudgements
	var err error
	if j.reading, j.read, err = a.Readings.NewestReading(ctx, sourceID, entryID); err != nil {
		return j, err
	}
	keys := j.reading.Keys(published)
	if j.is, err = a.Readings.EntitiesNamed(ctx, keys); err != nil {
		return j, err
	}
	if j.lookups, err = a.Readings.LookupsNamed(ctx, keys); err != nil {
		return j, err
	}
	j.pieces, err = a.Given.KeptPieces(ctx)
	return j, err
}

// itemWith renders the Item's page the path names, with status, and why
// Feedback given on it was refused, if it was.
func (a *app) itemWith(w http.ResponseWriter, r *http.Request, status int, errs feedback.Errors) {
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
	page := itemPath(sourceID, entryID)
	loc := a.Now().Location()
	p := itemPage{
		Title:         it.Title,
		Link:          it.Link,
		Summary:       it.Summary,
		Source:        it.SourceName,
		Published:     when(it.Published, loc),
		Attribution:   it.Attribution,
		FeedbackError: errs[""],
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
		p.CutBy = fmt.Sprintf("cut · %s v%d", c.Judge.Cutter, c.Judge.Version)
		for i, s := range c.Spans {
			at := editor.Address{SourceID: sourceID, EntryID: entryID, Span: s}
			row := spanOf(i+1, text, at, it.Published, j, page)
			row.Href = spanPath(at)
			p.Spans = append(p.Spans, row)
		}
	}
	keys := j.reading.Keys(it.Published)
	p.Feedback = a.piecesOn(j.pieces, page, func(k feedback.KeptPiece) bool {
		inItem := func(s feedback.Span) bool { return s.SourceID == sourceID && s.EntryID == entryID }
		key := editor.LookupKey{Name: k.Name, Kind: k.NameKind, Office: k.Office, On: k.On}
		return inItem(k.At) || inItem(k.To) || (k.Judged == feedback.Lookup && slices.Contains(keys, key))
	})
	a.renderStatus(w, r, status, itemTmpl, p)
}

// spanOf is the Span at, numbered n in its Cut, of text, as j's reading
// judged it, in an Item published on published: what the reading judged of
// a stretch within it is shown with it, and the Entities it mentions as j
// has them, each with the Feedback the page at page offers on it. A Span no
// reading judged reads as its words.
func spanOf(n int, text string, at editor.Address, published time.Time, j spanJudgements, page string) spanRow {
	s, r := at.Span, j.reading
	in := func(a editor.Address) bool { return a.Span.Start >= s.Start && a.Span.End <= s.End }
	path := page + "/feedback"
	row := spanRow{N: n, Pieces: inPlace(r.Hydrated(text, s))}
	offer := []giveButton{missedOn(path, at)}
	for _, a := range r.Rejected {
		if a.Span == s {
			row.Rejected = true
		}
	}
	for _, ref := range r.RefersTo {
		if in(ref.From) {
			offer = append(offer, wrongOn(path, ref.ID,
				"“"+cut.Words(text, ref.From.Span)+"” refers to “"+r.Words(text, ref.To)+"”"))
		}
	}
	for _, sp := range r.Speakers {
		if in(sp.Of) {
			who := r.Words(text, sp.By)
			if !slices.Contains(row.Speakers, who) {
				row.Speakers = append(row.Speakers, who)
			}
			offer = append(offer, wrongOn(path, sp.ID, "spoken by “"+who+"”"))
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
			offer = append(offer, wrongOn(path, j.lookups[k], "“"+nm.Name+"” is "+e.Name))
		}
	}
	row.Gives = givesOn(offer, j.pieces, page)
	return row
}

// itemFeedback is Feedback given on an Item's page, about one of its Spans
// or the judgements shown with them. It lands back on the page.
func (a *app) itemFeedback(w http.ResponseWriter, r *http.Request) {
	sourceID, entryID := r.PathValue("source"), r.PathValue("entry")
	if _, found, err := a.Readings.KeptItem(r.Context(), sourceID, entryID); err != nil || !found {
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
	a.givePiece(w, r, askFrom(r, sourceID, entryID, ""), itemPath(sourceID, entryID), func(errs feedback.Errors) {
		a.itemWith(w, r, http.StatusUnprocessableEntity, errs)
	})
}

// undoOnItem is an undo from an Item's page, which it lands back on.
func (a *app) undoOnItem(w http.ResponseWriter, r *http.Request) {
	a.undoPiece(w, r, itemPath(r.PathValue("source"), r.PathValue("entry")))
}
