package web

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/feedback"
)

// Briefings answers the queries the Briefing pages need.
type Briefings interface {
	// Briefing is the day's Briefing (YYYY-MM-DD) over Spans, reporting
	// whether one was written.
	Briefing(ctx context.Context, day string) (editor.Briefing, bool, error)
	// StoryBriefing is the day's Briefing in its earlier shape, over
	// Stories, reporting whether one was written so: the Record keeps those
	// it wrote, and their pages still show them.
	StoryBriefing(ctx context.Context, day string) (editor.StoryBriefing, bool, error)
	// BriefingFailure is the latest failure to write the day's Briefing.
	BriefingFailure(ctx context.Context, day string) (editor.BriefingFailure, bool, error)
	// BriefingDays are the days a Briefing was written for, and the first
	// day one was written or tried, from which a Briefing is owed every day
	// (empty if none ever was).
	BriefingDays(ctx context.Context) (written []string, first string, err error)
}

// briefingsPage is the view model for the list of Briefings: every day
// since the first, newest first.
type briefingsPage struct {
	Days []briefingDayRow
}

type briefingDayRow struct {
	Date string
	// Path is the day's page, for a day with a Briefing.
	Path string
	// Missed is the time the Briefing was due by, for a day with none by
	// then.
	Missed string
}

func briefingPath(day string) string { return "/briefings/" + day }

func (a *app) briefings(w http.ResponseWriter, r *http.Request) {
	written, first, err := a.Briefings.BriefingDays(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	now := a.Now()
	var p briefingsPage
	if first != "" {
		start, err := time.ParseInLocation(time.DateOnly, first, now.Location())
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		for day := dayOf(now); !day.Before(start); day = day.AddDate(0, 0, -1) {
			key := day.Format(time.DateOnly)
			row := briefingDayRow{Date: day.Format("Monday 2 January 2006")}
			if slices.Contains(written, key) {
				row.Path = briefingPath(key)
			} else if due := editor.DueAt(day, a.BriefingAt); !now.Before(due) {
				row.Missed = due.Format("15:04")
			}
			p.Days = append(p.Days, row)
		}
	}
	a.render(w, r, briefingsTmpl, p)
}

// pastBriefing is a day's Briefing page, by date: any day up to today.
func (a *app) pastBriefing(w http.ResponseWriter, r *http.Request) {
	now := a.Now()
	day, err := time.ParseInLocation(time.DateOnly, r.PathValue("day"), now.Location())
	if err != nil || day.After(now) {
		http.NotFound(w, r)
		return
	}
	a.briefing(w, r, day)
}

// dayOf is the start of the day that holds t, in t's location.
func dayOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// briefingPage is the view model for a day's Briefing page.
type briefingPage struct {
	Date   string
	Exists bool
	// Told is whether the Briefing tells Spans, in Statements, each with
	// the Spans it cites in its margin; one written before it did tells
	// Stories.
	Told       bool
	Statements []toldRow
	Stories    []briefedStoryRow
	// Status is the day's Briefing status, for a day with no Briefing.
	Status briefingStatus
	// SundayReadPath is the Sunday read's page, on a Sunday.
	SundayReadPath string
	// Missed is the time the Briefing was due by, for a day with none by
	// then; empty otherwise.
	Missed string
	// Failure is the latest failure to write the day's Briefing.
	Failure *failureRow
	// TrailPath is the page of how a written Briefing was written, and
	// WithoutCall whether it was written empty, with no model call.
	// That page links the call that wrote it, if one was kept.
	TrailPath   string
	WithoutCall bool
	// FailedAttempts are the kept calls of the day's failed attempts to
	// write it, for a day with no Briefing.
	FailedAttempts []attemptRow
	// FeedbackError is why Feedback last given on the Briefing was refused,
	// if it was.
	FeedbackError string
	// Emptied is whether Feedback took every statement out of the written
	// Briefing: it says so, rather than reading as a quiet day.
	Emptied bool
	// Feedback is the Feedback given on the Briefing, each to undo here.
	Feedback []pieceRow
}

// toldRow is one statement of a Briefing over Spans, with the Spans it
// cites: each its words, linking its page, whose words they are, and its
// Item; and the Feedback on it.
type toldRow struct {
	Text  string
	Cites []toldCite
	// Gives is the Feedback on the statement (wrong, new to me) and on each
	// Span it cites as told (unwanted).
	Gives gives
}

type toldCite struct {
	spanLink
	Speaker string
}

// toldRows are the statements of b as the page at page shows them, each
// with the Feedback it offers: those Feedback took out are left out
// (system design: A written Briefing re-renders without what Feedback took
// out).
func (a *app) toldRows(b editor.Briefing, page string, pieces []feedback.KeptPiece) []toldRow {
	path := page + "/feedback"
	var out []toldRow
	for _, st := range b.Statements {
		if len(st.TakenOutBy) > 0 {
			continue
		}
		row := toldRow{Text: st.Text}
		offer := []giveButton{wrongOn(path, st.ID, "this statement"), newToMeOn(path, st.ID, "this statement")}
		for _, c := range st.Cites {
			row.Cites = append(row.Cites, toldCite{spanLink: a.linkTo(c), Speaker: c.Speaker})
			offer = append(offer, unwantedOn(path, b.Day, c.At, c.Words))
		}
		row.Gives = givesOn(offer, pieces, page)
		out = append(out, row)
	}
	return out
}

type failureRow struct {
	At     string
	Reason string
	// Conversation is the page of the failed attempt's conversation, if it
	// had one.
	Conversation string
}

// briefedStoryRow is a Story of a Briefing written over Stories, as it was
// written: Feedback is no longer given on one.
type briefedStoryRow struct {
	ID         int64
	Title      string
	Path       string
	Statements []statementRow
}

// statementRow is one statement, with what it cites: each Development, on
// its Story's Timeline, and its Item, cited at the Fact on the Item's page.
type statementRow struct {
	Text  string
	Cites []citationRow
}

type citationRow struct {
	AsAt            string
	Statement       string
	DevelopmentPath string
	// Item is the Item the Development's Fact was drawn from, linking the
	// Fact on its page.
	Item itemCitation
}

// labelLength is the most of a Span's words, in runes, that labels the
// Feedback on it: enough to tell a statement's Spans apart.
const labelLength = 60

// shorten is s, cut at the last word that fits within labelLength and marked
// with an ellipsis if it is longer.
func shorten(s string) string {
	r := []rune(s)
	if len(r) <= labelLength {
		return s
	}
	cut := string(r[:labelLength])
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:") + "…"
}

func developmentAnchor(factID int64) string { return "development-" + strconv.FormatInt(factID, 10) }

func factAnchor(factID int64) string { return "fact-" + strconv.FormatInt(factID, 10) }

func (a *app) todaysBriefing(w http.ResponseWriter, r *http.Request) {
	a.briefing(w, r, a.Now())
}

// briefing renders the Briefing page for the day that holds day.
func (a *app) briefing(w http.ResponseWriter, r *http.Request, day time.Time) {
	a.briefingWith(w, r, day, http.StatusOK, nil)
}

// briefingWith renders the Briefing page, with why Feedback given on it was
// refused, if it was.
func (a *app) briefingWith(w http.ResponseWriter, r *http.Request, day time.Time, status int, errs feedback.Errors) {
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
	b.Day = key
	if (isTold || exists) && r.Method == http.MethodGet {
		// Opening a Briefing is kept for the register, as a side record.
		a.opened(r, a.Register.OpenedBriefing, key)
	}
	p := briefingPage{
		Date: day.Format("Monday 2 January 2006"), Exists: exists || isTold, Told: isTold,
		FeedbackError: errs[""],
	}
	page := briefingPath(key)
	if isTold {
		pieces, err := a.Given.KeptPieces(r.Context())
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		p.Statements = a.toldRows(told, page, pieces)
		p.Emptied = len(p.Statements) == 0 && len(told.Statements) > 0
		p.Feedback = a.piecesOn(pieces, page, func(k feedback.KeptPiece) bool {
			return k.Day == key && (k.Kind == feedback.Unwanted || k.Judged == feedback.Statement)
		})
		b.Model = told.Judge.Model
	}
	if day.Weekday() == time.Sunday {
		// The Sunday read is written alongside Sunday's Briefing.
		p.SundayReadPath = sundayReadPath(day.Format(time.DateOnly))
	}
	if !p.Exists {
		p.Status = a.statusOf(day, false)
		// The outputs appear: a day with no Briefing by the set time is
		// flagged, with the latest failure to write it, if there was one.
		due := editor.DueAt(day, a.BriefingAt)
		if !a.Now().Before(due) {
			p.Missed = due.Format("15:04")
		}
		f, failed, err := a.Briefings.BriefingFailure(r.Context(), day.Format(time.DateOnly))
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		if failed {
			p.Failure = &failureRow{At: when(f.At, day.Location()), Reason: f.Reason}
		}
	}
	if err := a.briefingCalls(r, &p, b.Day, b.Model, p.Exists, day.Location()); err != nil {
		a.serverError(w, r, err)
		return
	}
	// A Briefing written over Stories is shown as it was written.
	for _, bs := range b.Stories {
		row := briefedStoryRow{ID: bs.Story.ID, Title: bs.Story.Title, Path: storyPath(bs.Story.ID)}
		for _, st := range bs.Statements {
			sr := statementRow{Text: st.Text}
			for _, d := range st.Developments {
				item := itemPath(d.SourceID, d.EntryID)
				sr.Cites = append(sr.Cites, citationRow{
					AsAt: factDate(d.Fact), Statement: d.Statement,
					DevelopmentPath: storyPath(bs.Story.ID) + "#" + developmentAnchor(d.ID),
					Item:            a.cite(d.SourceID, d.ItemTitle, item+"#"+factAnchor(d.ID)),
				})
			}
			row.Statements = append(row.Statements, sr)
		}
		p.Stories = append(p.Stories, row)
	}
	t := briefingTmpl
	if dayOf(day).Equal(dayOf(a.Now())) {
		t = todaysBriefingTmpl
	}
	a.renderStatus(w, r, status, t, p)
}

// briefingStatus is the view model for the day's Briefing status: written,
// being written (polled), or not yet due or not written on a past day.
type briefingStatus struct {
	// Path is the status fragment's own URL, which it polls.
	Path         string
	BriefingPath string
	Written      bool
	// Polled is whether the Briefing is being written: today, past the set
	// time, with none written. The schedule writes it as soon as the work
	// collected before it is done, or retries a failure, all day.
	Polled bool
}

// statusOf is the status of the Briefing for the day that holds day.
func (a *app) statusOf(day time.Time, written bool) briefingStatus {
	key := day.Format(time.DateOnly)
	now := a.Now()
	return briefingStatus{
		Path: briefingPath(key) + "/status", BriefingPath: briefingPath(key), Written: written,
		Polled: !written && key == now.Format(time.DateOnly) && !now.Before(editor.DueAt(day, a.BriefingAt)),
	}
}

// briefingStatus is the day's Briefing status fragment, which the day's
// page polls while the Briefing is being written.
func (a *app) briefingStatus(w http.ResponseWriter, r *http.Request) {
	now := a.Now()
	day, err := time.ParseInLocation(time.DateOnly, r.PathValue("day"), now.Location())
	if err != nil || day.After(now) {
		http.NotFound(w, r)
		return
	}
	written, err := a.written(r.Context(), r.PathValue("day"))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.fragment(w, r, briefingStatusTmpl, "briefingStatus", a.statusOf(day, written))
}

// written is whether the day's Briefing was written, in either shape.
func (a *app) written(ctx context.Context, day string) (bool, error) {
	_, told, err := a.Briefings.Briefing(ctx, day)
	if err != nil || told {
		return told, err
	}
	_, written, err := a.Briefings.StoryBriefing(ctx, day)
	return written, err
}

// briefingCalls links the day's Briefing page to how it was written: a
// written Briefing to its trail, saying if it was written with no call; a
// day with no Briefing to each failed attempt to write it. model is what
// wrote it, empty for none.
func (a *app) briefingCalls(r *http.Request, p *briefingPage, day, model string, exists bool, loc *time.Location) error {
	calls, err := a.Calls.CallsOn(r.Context(), briefingAct, day)
	if err != nil {
		return err
	}
	var writtenBy int64
	for _, c := range calls {
		switch {
		case !exists:
			p.FailedAttempts = append(p.FailedAttempts, attemptRow{
				Page: callPath(c.ID), At: when(c.At, loc), Outcome: outcomeOf(c.Outcome), Why: c.Why,
			})
		case c.Outcome == editor.Applied:
			writtenBy = c.ID
		}
	}
	// A Briefing written empty is the one with no model to name.
	p.WithoutCall = exists && writtenBy == 0 && model == ""
	if exists {
		p.TrailPath = briefingTrailPath(day)
	}
	return nil
}

// briefingDay is the day of the Briefing the path names, and the page an
// answer lands back on (today's is "/"), reporting whether it is a day up
// to today.
func (a *app) briefingDay(r *http.Request) (time.Time, string, bool) {
	now := a.Now()
	day, err := time.ParseInLocation(time.DateOnly, r.PathValue("day"), now.Location())
	if err != nil || day.After(now) {
		return day, "", false
	}
	to := briefingPath(r.PathValue("day"))
	if r.PathValue("day") == now.Format(time.DateOnly) {
		to = "/"
	}
	return day, to, true
}

// briefingFeedback is Feedback given on a day's Briefing: on its statements,
// or the Spans they cite as told that day. It lands back on the Briefing,
// which re-renders without what Feedback took out.
func (a *app) briefingFeedback(w http.ResponseWriter, r *http.Request) {
	day, to, ok := a.briefingDay(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad form.", http.StatusBadRequest)
		return
	}
	a.givePiece(w, r, askFrom(r, "", "", day.Format(time.DateOnly)), to, func(errs feedback.Errors) {
		a.briefingWith(w, r, day, http.StatusUnprocessableEntity, errs)
	})
}

// undoOnBriefing is an undo from a day's Briefing, which it lands back on.
func (a *app) undoOnBriefing(w http.ResponseWriter, r *http.Request) {
	_, to, ok := a.briefingDay(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	a.undoPiece(w, r, to)
}
