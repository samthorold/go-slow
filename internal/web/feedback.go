package web

import (
	"cmp"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/feedback"
)

// feedbackPage is the view model for the Feedback page: every piece, as
// given, newest first.
type feedbackPage struct {
	Feedback []pieceRow
}

// undonePath is where the undo of the piece id goes from the page at page
// ("" for the Feedback page).
func undonePath(page string, id int64) string {
	return page + "/feedback/" + strconv.FormatInt(id, 10) + "/undone"
}

// judgedLabels name what each kind of judgement is, as the Feedback page
// says what a piece is about.
var judgedLabels = map[feedback.Judged]string{
	feedback.RefersTo: "Refers to", feedback.Lookup: "Entity", feedback.Speaker: "Speaker",
	feedback.Relation: "Relation", feedback.Gathering: "Story", feedback.Statement: "Statement",
	feedback.Assessment: "Assessment", feedback.SundayRead: "Sunday read",
}

func (a *app) feedback(w http.ResponseWriter, r *http.Request) {
	pieces, err := a.Given.KeptPieces(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var p feedbackPage
	for _, k := range pieces {
		row := a.pieceRowOf(k, "")
		row.Level = cmp.Or(judgedLabels[k.Judged], "Span")
		if k.Day != "" {
			if d, err := time.Parse(time.DateOnly, k.Day); err == nil {
				row.Briefing, row.BriefPath = d.Format("2 January 2006"), briefingPath(k.Day)
			}
		}
		p.Feedback = append(p.Feedback, row)
	}
	a.render(w, r, feedbackTmpl, p)
}

// undoOnFeedbackPage is an undo from the Feedback page.
func (a *app) undoOnFeedbackPage(w http.ResponseWriter, r *http.Request) {
	a.undoPiece(w, r, "/feedback")
}

// giveButton is a piece of Feedback given with one button.
type giveButton struct {
	Path, Kind string
	// Judgement is the judgement it lands on; Source, Entry and Span the
	// Span ("start-end"), for unwanted and missed.
	Judgement           int64
	Source, Entry, Span string
	Label, Confirm      string
	// UndoPath is set where Feedback of its kind already stands on its
	// target: the button shows as given, beside the undo of that piece from
	// the page, and is not offered again.
	UndoPath string
	// at and day are the Span and the Briefing day it lands on, to match
	// it with the Feedback standing.
	at  editor.Address
	day string
}

// kindIcons are the icon of each kind of Feedback, the same on every page:
// plain Unicode, so no icon font or build step.
var kindIcons = map[string]string{
	string(feedback.Wrong): "✗", string(feedback.NewToMe): "✦",
	string(feedback.Unwanted): "⊘", string(feedback.Missed): "⊕",
}

// Icon is the icon of its kind.
func (g giveButton) Icon() string { return kindIcons[g.Kind] }

// KindClass is its kind as a class name.
func (g giveButton) KindClass() string { return kindClass(g.Kind) }

// gives are the pieces of Feedback offered on one target, each a fresh
// button or, where it stands already, shown as given with its undo.
type gives []giveButton

// kindClass is a kind as a class name: "new to me" is "new-to-me", so its
// label takes the hue .kind-new-to-me.
func kindClass(kind string) string { return strings.ReplaceAll(kind, " ", "-") }
