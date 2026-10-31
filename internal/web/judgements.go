package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"time"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/feedback"
)

// Pieces is the Owner's Feedback commands on judgements and Spans (system
// design: Feedback lands on a judgement, so the judge to fix is known).
type Pieces interface {
	// Give keeps Feedback as given, at now; Feedback the design doesn't
	// give where it was asked for keeps nothing and returns its Errors.
	Give(ctx context.Context, a feedback.Ask, now time.Time) (feedback.Piece, feedback.Errors, error)
	// Undo keeps the undoing of the piece id, at now; undoing it again
	// changes nothing.
	Undo(ctx context.Context, id int64, now time.Time) (feedback.Piece, error)
}

// Given answers the Feedback kept as events.
type Given interface {
	// KeptPieces are every piece of Feedback kept as an event, newest
	// first, the undone with the rest, each with what it is about.
	KeptPieces(ctx context.Context) ([]feedback.KeptPiece, error)
}

// pieceRow is a piece of Feedback as a page lists it: its kind, what it is
// about, linking its page, when it was given, the calls behind what it is
// about, and its undo from the page, or when it was undone.
type pieceRow struct {
	Kind, What, AboutPath, GivenAt string
	// Level says what kind of thing it is about ("Refers to", "Span"), as
	// the Feedback page prefixes it.
	Level string
	// Briefing and BriefPath are the Briefing it is about, if any, as the
	// Feedback page links it.
	Briefing, BriefPath string
	Made                []made
	// Written is the kept conversation behind a Sunday read or an
	// Assessment, or 0.
	Written int64
	// Undone is when it was undone; UndoPath, for a piece standing, is
	// where its undo goes from the page.
	Undone, UndoPath string
}

// KindClass is the row's kind as a class name.
func (r pieceRow) KindClass() string { return kindClass(r.Kind) }

// WrittenHref is the page of the conversation behind it.
func (r pieceRow) WrittenHref() string { return conversationPath(r.Written, 0) }

// pieceRowOf is k as the page at page lists it.
func (a *app) pieceRowOf(k feedback.KeptPiece, page string) pieceRow {
	loc := a.Now().Location()
	row := pieceRow{Kind: string(k.Kind), What: k.What, GivenAt: when(k.GivenAt, loc), AboutPath: aboutPath(k)}
	if k.Undone() {
		row.Undone = when(k.UndoneAt, loc)
	} else {
		row.UndoPath = undonePath(page, k.ID)
	}
	for _, c := range k.Calls {
		row.Made = append(row.Made, made{Act: c.Act, Call: c.ID})
	}
	row.Written = k.Conversation
	return row
}

// aboutPath is the page of what k is about: the Span it lands on, the
// Briefing a statement is in, or none for a look-up.
func aboutPath(k feedback.KeptPiece) string {
	switch {
	case k.Judged == feedback.Statement:
		return briefingPath(k.Day)
	case k.Judged == feedback.Assessment:
		return assessmentPath(k.Judgement)
	case k.Judged == feedback.SundayRead:
		return sundayReadPath(k.Read)
	case k.At.SourceID != "":
		return spanPath(addressOf(k.At))
	}
	return ""
}

// addressOf is the Span s as the editor addresses it.
func addressOf(s feedback.Span) editor.Address {
	return editor.Address{SourceID: s.SourceID, EntryID: s.EntryID, Span: cut.Span{Start: s.Start, End: s.End}}
}

// spanOfAddress is the Span at a as Feedback names it.
func spanOfAddress(a editor.Address) feedback.Span {
	return feedback.Span{SourceID: a.SourceID, EntryID: a.EntryID, Start: a.Span.Start, End: a.Span.End}
}

// within reports whether the Span s lies within the Span at a.
func within(s feedback.Span, a editor.Address) bool {
	return s.SourceID == a.SourceID && s.EntryID == a.EntryID && s.Start >= a.Span.Start && s.End <= a.Span.End
}

// piecesOn are the rows of the pieces in kept that about selects, as the
// page at page lists them.
func (a *app) piecesOn(kept []feedback.KeptPiece, page string, about func(feedback.KeptPiece) bool) []pieceRow {
	var out []pieceRow
	for _, k := range kept {
		if about(k) {
			out = append(out, a.pieceRowOf(k, page))
		}
	}
	return out
}

// wrongOn is the button giving wrong on the judgement id, labelled for what
// it judged.
func wrongOn(path string, id int64, label string) giveButton {
	return giveButton{Path: path, Kind: string(feedback.Wrong), Judgement: id, Label: "Wrong: " + label}
}

// newToMeOn is the button giving new to me on the judgement id.
func newToMeOn(path string, id int64, label string) giveButton {
	return giveButton{Path: path, Kind: string(feedback.NewToMe), Judgement: id, Label: "New to me: " + label}
}

// spanField is a Span as a form names it: "start-end".
func spanField(a editor.Address) string {
	return strconv.Itoa(a.Span.Start) + "-" + strconv.Itoa(a.Span.End)
}

// missedOn is the button giving missed on the Span at a, from its Item's
// page or its own.
func missedOn(path string, a editor.Address) giveButton {
	return giveButton{Path: path, Kind: string(feedback.Missed), Span: spanField(a), Label: "Missed",
		at: a}
}

// unwantedOn is the button giving unwanted on the Span at a, as told in the
// Briefing for day, labelled for its words.
func unwantedOn(path, day string, a editor.Address, words string) giveButton {
	return giveButton{Path: path, Kind: string(feedback.Unwanted), Source: a.SourceID, Entry: a.EntryID,
		Span: spanField(a), Label: "Unwanted: " + shorten(words), at: a, day: day}
}

// givesOn marks each of gives that Feedback standing in pieces already
// gives, with the undo of that piece from the page at page. Undone Feedback
// stands nowhere.
func givesOn(gives []giveButton, pieces []feedback.KeptPiece, page string) gives {
	for i, g := range gives {
		for _, p := range pieces {
			if !p.Undone() && g.lands(p) {
				gives[i].UndoPath = undonePath(page, p.ID)
				break
			}
		}
	}
	return gives
}

// lands reports whether p is Feedback of g's kind on g's target.
func (g giveButton) lands(p feedback.KeptPiece) bool {
	if string(p.Kind) != g.Kind {
		return false
	}
	if g.Judgement != 0 {
		return p.Judgement == g.Judgement
	}
	return p.Span == spanOfAddress(g.at) && p.Day == g.day
}

// askFrom reads Feedback from a form, about the Item at source and entry
// where the page is one Item's, and on the Briefing for day where the page
// is a Briefing.
func askFrom(r *http.Request, source, entry, day string) feedback.Ask {
	a := feedback.Ask{
		Kind:      r.PostForm.Get("kind"),
		Judgement: r.PostForm.Get("judgement"),
		SourceID:  r.PostForm.Get("source"),
		EntryID:   r.PostForm.Get("entry"),
		Span:      r.PostForm.Get("span"),
		Day:       day,
	}
	if source != "" {
		a.SourceID, a.EntryID = source, entry
	}
	return a
}

// givePiece gives the Feedback a asks for, landing back on the page to with
// 303, or re-rendering the page by again with 422 and why it was refused.
func (a *app) givePiece(w http.ResponseWriter, r *http.Request, ask feedback.Ask, to string,
	again func(errs feedback.Errors)) {
	_, errs, err := a.Pieces.Give(r.Context(), ask, a.Now())
	switch {
	case errors.Is(err, feedback.ErrNotInRecord):
		http.NotFound(w, r)
	case err != nil:
		a.serverError(w, r, err)
	case len(errs) > 0:
		again(errs)
	default:
		http.Redirect(w, r, to, http.StatusSeeOther)
	}
}

// undoPiece undoes the piece the path names, landing back on the page to.
func (a *app) undoPiece(w http.ResponseWriter, r *http.Request, to string) {
	id, err := strconv.ParseInt(r.PathValue("feedback"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, err = a.Pieces.Undo(r.Context(), id, a.Now())
	switch {
	case errors.Is(err, feedback.ErrNotInRecord):
		http.NotFound(w, r)
	case err != nil:
		a.serverError(w, r, err)
	default:
		http.Redirect(w, r, to, http.StatusSeeOther)
	}
}
