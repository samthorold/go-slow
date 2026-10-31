package web

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/feedback"
)

// Feedback is the Owner's Feedback commands on a Sunday read and its
// Assessments, kept as the Feedback before the Record became events, until
// the read's Feedback lands on its judgements too.
type Feedback interface {
	// Give keeps Feedback as given, at now, and fixes the Record by it.
	Give(ctx context.Context, d feedback.Draft, now time.Time) (feedback.Feedback, feedback.Errors, error)
	// Undo undoes the piece of Feedback with id, at now, lifting the marks
	// it made; undoing it again changes nothing.
	Undo(ctx context.Context, id int64, now time.Time) (feedback.Feedback, error)
}

// Kept answers the Feedback kept before the Record became events.
type Kept interface {
	// KeptFeedback is every piece of that Feedback, as given, newest first.
	KeptFeedback(ctx context.Context) ([]feedback.Kept, error)
}

// feedbackPage is the view model for the Feedback page: every piece, as
// given, newest first.
type feedbackPage struct {
	Feedback []pieceRow
}

// keptRow is a piece of the Sunday read's Feedback as a page lists it.
type keptRow struct {
	Kind, Level string
	// About names what it is about; AboutPath is its page.
	About, AboutPath string
	GivenAt          string
	// Undone is when the Owner undid it; UndoPath, for a piece standing, is
	// where its undo goes from this page.
	Undone, UndoPath string
	// Behind is the kept conversation behind it.
	Behind []behindLink
}

// behindLink is a link to a kept conversation behind what a piece of
// Feedback is about.
type behindLink struct{ Path, Label string }

// onRead reports whether k is Feedback on a Sunday read or its
// Assessments: the Feedback kept before the Record became events that is
// still given.
func onRead(k feedback.Kept) bool {
	return k.Level == feedback.SundayRead || k.Level == feedback.Assessment
}

// undonePath is where the undo of the piece id goes from the page at page
// ("" for the Feedback page).
func undonePath(page string, id int64) string {
	return page + "/feedback/" + strconv.FormatInt(id, 10) + "/undone"
}

// readUndonePath is where the undo of the Sunday read's piece id goes from
// the Feedback page: its own route, since the read's Feedback is numbered
// apart from the Feedback kept as events.
func readUndonePath(id int64) string {
	return "/feedback/sunday-read/" + strconv.FormatInt(id, 10) + "/undone"
}

// judgedLabels name what each kind of judgement is, as the Feedback page
// says what a piece is about.
var judgedLabels = map[feedback.Judged]string{
	feedback.RefersTo: "Refers to", feedback.Lookup: "Entity", feedback.Speaker: "Speaker",
	feedback.Relation: "Relation", feedback.Gathering: "Story", feedback.Statement: "Statement",
}

func (a *app) feedback(w http.ResponseWriter, r *http.Request) {
	pieces, err := a.Given.KeptPieces(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	kept, err := a.Kept.KeptFeedback(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	type dated struct {
		at  time.Time
		row pieceRow
	}
	var rows []dated
	for _, k := range pieces {
		row := a.pieceRowOf(k, "")
		row.Level = cmp.Or(judgedLabels[k.Judged], "Span")
		if k.Day != "" {
			if d, err := time.Parse(time.DateOnly, k.Day); err == nil {
				row.Briefing, row.BriefPath = d.Format("2 January 2006"), briefingPath(k.Day)
			}
		}
		rows = append(rows, dated{k.GivenAt, row})
	}
	loc := a.Now().Location()
	for _, k := range kept {
		if !onRead(k) {
			continue
		}
		kr := a.keptRow(k, "")
		row := pieceRow{Kind: kr.Kind, Level: levelLabels[k.Level], What: kr.About, AboutPath: kr.AboutPath,
			GivenAt: kr.GivenAt, Undone: kr.Undone, Written: k.Conversation}
		if !k.Undone() {
			row.UndoPath = readUndonePath(k.ID)
		}
		rows = append(rows, dated{k.GivenAt.In(loc), row})
	}
	slices.SortStableFunc(rows, func(x, y dated) int { return y.at.Compare(x.at) })
	var p feedbackPage
	for _, d := range rows {
		p.Feedback = append(p.Feedback, d.row)
	}
	a.render(w, r, feedbackTmpl, p)
}

// levelLabels name the Sunday read's levels as a page shows them.
var levelLabels = map[feedback.Level]string{feedback.Assessment: "Assessment", feedback.SundayRead: "Sunday read"}

// keptRow is a piece of the Sunday read's Feedback as a page lists it,
// undone from the page at page.
func (a *app) keptRow(k feedback.Kept, page string) keptRow {
	loc := a.Now().Location()
	row := keptRow{Kind: string(k.Kind), Level: string(k.Level), GivenAt: when(k.GivenAt, loc)}
	if k.Undone() {
		row.Undone = when(k.UndoneAt, loc)
	} else {
		row.UndoPath = undonePath(page, k.ID)
	}
	if k.Conversation != 0 {
		row.Behind = []behindLink{{Path: conversationPath(k.Conversation, 0), Label: "the read's conversation"}}
	}
	switch k.Level {
	case feedback.Assessment:
		row.About, row.AboutPath = k.Assessment, assessmentPath(k.AssessmentID)
	case feedback.SundayRead:
		row.About, row.AboutPath = "The Sunday read", sundayReadPath(k.ReadDay)
	}
	return row
}

// undo undoes the piece of the Sunday read's Feedback the path names,
// landing back on the page to: the page re-renders with the Record as the
// undo left it.
func (a *app) undo(w http.ResponseWriter, r *http.Request, to string) {
	id, err := strconv.ParseInt(r.PathValue("feedback"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, err = a.Feedback.Undo(r.Context(), id, a.Now())
	switch {
	case errors.Is(err, feedback.ErrNotInRecord):
		http.NotFound(w, r)
	case err != nil:
		a.serverError(w, r, err)
	default:
		http.Redirect(w, r, to, http.StatusSeeOther)
	}
}

// undoOnFeedbackPage is an undo from the Feedback page.
func (a *app) undoOnFeedbackPage(w http.ResponseWriter, r *http.Request) {
	a.undoPiece(w, r, "/feedback")
}

// undoReadOnFeedbackPage is an undo of the Sunday read's Feedback from the
// Feedback page.
func (a *app) undoReadOnFeedbackPage(w http.ResponseWriter, r *http.Request) {
	a.undo(w, r, "/feedback")
}

// giveButton is a piece of Feedback given with one button.
type giveButton struct {
	Path, Kind, Level string
	Assessment, Read  int64
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

// gives are the pieces of Feedback offered on one target.
type gives []giveButton

// Given are those already standing on the target, each with its undo.
func (gs gives) Given() gives {
	return slices.DeleteFunc(slices.Clone(gs), func(g giveButton) bool { return g.UndoPath == "" })
}

// Offered are those not yet given, each a fresh button.
func (gs gives) Offered() gives {
	return slices.DeleteFunc(slices.Clone(gs), func(g giveButton) bool { return g.UndoPath != "" })
}

// about reports whether f is the Sunday read's Feedback of g's kind on g's
// target.
func (g giveButton) about(f feedback.Feedback) bool {
	if string(f.Kind) != g.Kind || string(f.Level) != g.Level {
		return false
	}
	switch f.Level {
	case feedback.Assessment:
		return f.AssessmentID == g.Assessment
	case feedback.SundayRead:
		return f.ReadID == g.Read
	}
	return false
}

// givenIn marks each of gives that the Sunday read's Feedback standing in
// kept already gives, with the undo of that piece from the page at page.
// Undone Feedback stands nowhere.
func givenIn(gives []giveButton, kept []feedback.Kept, page string) []giveButton {
	for i, g := range gives {
		for _, k := range kept {
			if !k.Undone() && g.about(k.Feedback) {
				gives[i].UndoPath = undonePath(page, k.ID)
				break
			}
		}
	}
	return gives
}

// assessmentGives is the Feedback on an Assessment: wrong, or new to the
// Owner.
func assessmentGives(path string, id int64) []giveButton {
	return []giveButton{
		{Path: path, Kind: string(feedback.Wrong), Level: string(feedback.Assessment), Assessment: id, Label: "Wrong",
			Confirm: "Withdraw this Assessment, and the statements that cite it?"},
		{Path: path, Kind: string(feedback.NewToMe), Level: string(feedback.Assessment), Assessment: id, Label: "New to me"},
	}
}

// readGives is the Feedback on a Sunday read as a whole: new to the Owner.
func readGives(path string, id int64) []giveButton {
	return []giveButton{{Path: path, Kind: string(feedback.NewToMe), Level: string(feedback.SundayRead), Read: id, Label: "New to me"}}
}

// draftFrom reads the Sunday read's Feedback from a form.
func draftFrom(r *http.Request) feedback.Draft {
	return feedback.Draft{
		Kind:       r.PostForm.Get("kind"),
		Level:      r.PostForm.Get("level"),
		Assessment: r.PostForm.Get("assessment"),
		Read:       r.PostForm.Get("read"),
	}
}

// sundayReadFeedback is Feedback given on a Sunday read, about the read or
// its Assessments. It lands back on the read, which re-renders with the
// Record as Feedback left it.
func (a *app) sundayReadFeedback(w http.ResponseWriter, r *http.Request) {
	now := a.Now()
	day, err := time.ParseInLocation(time.DateOnly, r.PathValue("day"), now.Location())
	if err != nil || day.After(now) || day.Weekday() != time.Sunday {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad form.", http.StatusBadRequest)
		return
	}
	d := draftFrom(r)
	d.Day = day.Format(time.DateOnly)
	_, errs, err := a.Feedback.Give(r.Context(), d, now)
	switch {
	case errors.Is(err, feedback.ErrNotInRecord):
		http.NotFound(w, r)
	case err != nil:
		a.serverError(w, r, err)
	case len(errs) > 0:
		// Every piece given here is a button the page offers.
		http.Error(w, "That Feedback can't be given here.", http.StatusUnprocessableEntity)
	default:
		http.Redirect(w, r, sundayReadPath(d.Day), http.StatusSeeOther)
	}
}

// kindClass is a kind as a class name: "new to me" is "new-to-me", so its
// label takes the hue .kind-new-to-me.
func kindClass(kind string) string { return strings.ReplaceAll(kind, " ", "-") }
