package web

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/feedback"
)

// SundayReads answers the queries the Sunday read's page needs.
type SundayReads interface {
	// SundayRead is the day's Sunday read (YYYY-MM-DD) over Spans, reporting
	// whether one was written.
	SundayRead(ctx context.Context, day string) (editor.SundayRead, bool, error)
	// HasSundayRead reports whether the day's Sunday read was written: over
	// Spans, or before the Record kept only events, when only its day is
	// kept.
	HasSundayRead(ctx context.Context, day string) (bool, error)
	// SundayReadFailures are the failures to write the day's read.
	SundayReadFailures(ctx context.Context, day string) ([]editor.SundayReadFailure, error)
	// Assessment is one Assessment, reporting whether it is in the Record.
	Assessment(ctx context.Context, id int64) (editor.Assessment, bool, error)
}

func sundayReadPath(day string) string { return "/sunday-reads/" + day }

// assessmentPath is an Assessment's stable address, which leads to it in the
// read that made it.
func assessmentPath(id int64) string { return "/assessments/" + strconv.FormatInt(id, 10) }

// assessment redirects to the Assessment in the read that made it.
func (a *app) assessment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	as, found, err := a.SundayReads.Assessment(r.Context(), id)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, sundayReadPath(as.Day)+"#"+assessmentAnchor(id), http.StatusSeeOther)
}

func assessmentAnchor(id int64) string { return "assessment-" + strconv.FormatInt(id, 10) }

func statementAnchor(id int64) string { return "statement-" + strconv.FormatInt(id, 10) }

// sundayReadPage is the view model for a Sunday read's page.
type sundayReadPage struct {
	Date         string
	BriefingPath string
	Exists       bool
	// Earlier is whether the read was written before the Record kept only
	// events: of it, only its day is kept.
	Earlier bool
	// Gives is the Feedback on the read as a whole.
	Gives       gives
	Statements  []readStatementRow
	Assessments []assessmentRow
	// Failure is the latest failure to write the read, for a Sunday with
	// none.
	Failure *failureRow
	// Feedback is the Feedback on the read and its Assessments, each to
	// undo here.
	Feedback []pieceRow
	// Conversation is the page of the conversation that wrote the read; empty
	// for one written with no model call.
	Conversation string
}

type readStatementRow struct {
	Anchor string
	Text   string
	Cites  []readCitationRow
}

// readCitationRow is one thing a statement or Assessment cites, with where
// it can be read: a Span on its own page, an Assessment on this page or an
// earlier read's, or the Wikidata revision a lookup saw.
type readCitationRow struct {
	Kind string
	Text string
	Path string
	AsAt string
	// Speaker is whose words a cited Span is, if its reading judged one.
	Speaker string
	// Item is the Item a cited Span is of, cited; nil for anything else.
	Item *itemCitation
}

type assessmentRow struct {
	Anchor     string
	Gives      gives
	Withdrawn  bool
	Statement  string
	AsAt       string
	Supersedes string
	Rests      []readCitationRow
	// Reached is the turn of the conversation that reached it.
	Reached reached
}

// sundayRead is a Sunday read's page, by date: any Sunday up to today.
func (a *app) sundayRead(w http.ResponseWriter, r *http.Request) {
	now := a.Now()
	day, err := time.ParseInLocation(time.DateOnly, r.PathValue("day"), now.Location())
	if err != nil || day.After(now) || day.Weekday() != time.Sunday {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	key := day.Format(time.DateOnly)
	read, exists, err := a.SundayReads.SundayRead(ctx, key)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p := sundayReadPage{Date: day.Format("Sunday 2 January 2006"), BriefingPath: briefingPath(key), Exists: exists}
	if !exists {
		earlier, err := a.SundayReads.HasSundayRead(ctx, key)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		if earlier {
			a.opened(r, a.Register.OpenedSundayRead, key)
			p.Exists, p.Earlier = true, true
			a.render(w, r, sundayReadTmpl, p)
			return
		}
		fs, err := a.SundayReads.SundayReadFailures(ctx, key)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		if len(fs) > 0 {
			f := fs[len(fs)-1]
			p.Failure = &failureRow{At: when(f.At, day.Location()), Reason: f.Reason,
				Conversation: conversationPath(f.ConversationID, 0)}
		}
		a.render(w, r, sundayReadTmpl, p)
		return
	}
	a.opened(r, a.Register.OpenedSundayRead, key)
	pieces, err := a.Given.KeptPieces(ctx)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	page := sundayReadPath(key)
	path := page + "/feedback"
	p.Gives = givesOn([]giveButton{{Path: path, Kind: string(feedback.NewToMe), Judgement: read.ID, Label: "New to me"}}, pieces, page)
	p.Conversation = conversationPath(read.ConversationID, 0)
	for _, st := range read.Statements {
		if len(st.TakenOutBy) > 0 {
			continue // cites an Assessment Feedback withdrew
		}
		row := readStatementRow{Anchor: statementAnchor(st.ID), Text: st.Text}
		for _, c := range st.Cites {
			row.Cites = append(row.Cites, a.citation(c, key))
		}
		p.Statements = append(p.Statements, row)
	}
	ids := map[int64]bool{read.ID: true}
	for _, as := range read.Assessments {
		ids[as.ID] = true
		row := assessmentRow{Anchor: assessmentAnchor(as.ID), Statement: as.Statement, AsAt: as.AsAt.Format("2 January 2006"),
			Withdrawn: as.Withdrawn, Reached: reachedBy(read)}
		if !as.Withdrawn {
			row.Gives = givesOn([]giveButton{
				{Path: path, Kind: string(feedback.Wrong), Judgement: as.ID, Label: "Wrong",
					Confirm: "Withdraw this Assessment, and the statements that cite it?"},
				{Path: path, Kind: string(feedback.NewToMe), Judgement: as.ID, Label: "New to me"},
			}, pieces, page)
		}
		if as.Supersedes != 0 {
			row.Supersedes = assessmentPath(as.Supersedes)
		}
		for _, o := range as.Spans {
			row.Rests = append(row.Rests, a.citation(editor.Citation{Kind: editor.CitesSpan, Span: o}, key))
		}
		for _, e := range as.Earlier {
			row.Rests = append(row.Rests, a.citation(editor.Citation{Kind: editor.CitesAssessment, Assessment: e}, key))
		}
		p.Assessments = append(p.Assessments, row)
	}
	p.Feedback = a.piecesOn(pieces, page, func(k feedback.KeptPiece) bool { return ids[k.Judgement] })
	a.render(w, r, sundayReadTmpl, p)
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
	ask := askFrom(r, "", "", "")
	// Every piece given here is a button the page offers.
	a.givePiece(w, r, ask, sundayReadPath(day.Format(time.DateOnly)), func(feedback.Errors) {
		http.Error(w, "That Feedback can't be given here.", http.StatusUnprocessableEntity)
	})
}

// undoOnSundayRead is an undo from a Sunday read, which it lands back on.
func (a *app) undoOnSundayRead(w http.ResponseWriter, r *http.Request) {
	now := a.Now()
	day, err := time.ParseInLocation(time.DateOnly, r.PathValue("day"), now.Location())
	if err != nil || day.After(now) || day.Weekday() != time.Sunday {
		http.NotFound(w, r)
		return
	}
	a.undoPiece(w, r, sundayReadPath(day.Format(time.DateOnly)))
}

// citation is what a statement or Assessment in the read for day cites, as
// the page shows it.
func (a *app) citation(c editor.Citation, day string) readCitationRow {
	switch c.Kind {
	case editor.CitesSpan:
		o := c.Span
		path := spanPath(o.At)
		row := readCitationRow{Kind: "Span", Text: o.Words, Path: path, Speaker: o.Speaker,
			Item: new(a.cite(o.Item.SourceID, o.Item.Title, itemPath(o.Item.SourceID, o.Item.EntryID)))}
		if !o.Item.Published.IsZero() {
			row.AsAt = o.Item.Published.Format(time.DateOnly)
		}
		return row
	case editor.CitesAssessment:
		path := "#" + assessmentAnchor(c.Assessment.ID)
		if c.Assessment.Day != day {
			path = sundayReadPath(c.Assessment.Day) + path
		}
		return readCitationRow{Kind: "Assessment", Text: c.Assessment.Statement, Path: path,
			AsAt: c.Assessment.AsAt.Format(time.DateOnly)}
	case editor.CitesLookup:
		return readCitationRow{Kind: "Wikidata", Text: c.Lookup.ItemID + ", revision " + strconv.FormatInt(c.Lookup.Revision, 10),
			Path: c.Lookup.URL(), AsAt: c.Lookup.At.Format(time.DateOnly)}
	}
	return readCitationRow{Kind: string(c.Kind)}
}
