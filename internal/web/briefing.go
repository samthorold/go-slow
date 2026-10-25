package web

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/feedback"
)

// Briefings answers the queries the Briefing pages need.
type Briefings interface {
	// Briefing is the day's Briefing (YYYY-MM-DD), reporting whether one
	// was written.
	Briefing(ctx context.Context, day string) (editor.Briefing, bool, error)
	// BriefingFailure is the latest failure to write the day's Briefing.
	BriefingFailure(ctx context.Context, day string) (editor.BriefingFailure, bool, error)
	// BriefingDays are the days a Briefing was written for, and the first
	// day one was written or tried, from which a Briefing is owed every day
	// (empty if none ever was).
	BriefingDays(ctx context.Context) (written []string, first string, err error)
	// TakenOutBy are the ids of the Feedback that takes something of the
	// day's written Briefing out of it.
	TakenOutBy(ctx context.Context, day string) ([]int64, error)
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
	Date    string
	Exists  bool
	Stories []briefedStoryRow
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
	// FeedbackPath is where Feedback given on the Briefing goes.
	FeedbackPath string
	// MissedStory is the form for a missed Story: what was entered, and why
	// it failed.
	MissedStory missedStoryForm
	// Emptied is whether Feedback took every Story out of the written
	// Briefing, and TakenOut the Feedback that did.
	Emptied  bool
	TakenOut []keptRow
	// Feedback is the rest of the Feedback given on the Briefing.
	Feedback []keptRow
}

type failureRow struct {
	At     string
	Reason string
	// Conversation is the page of the failed attempt's conversation, if it
	// had one.
	Conversation string
}

type briefedStoryRow struct {
	ID    int64
	Gives []giveButton
	// MissedDevelopment is the form for a Development the Story missed.
	MissedDevelopment missedDevelopmentForm
	Title             string
	Path              string
	Statements        []statementRow
}

// statementRow is one statement, with what it cites: each Development, on
// its Story's Timeline, and its Fact and Item, on the Item's page.
type statementRow struct {
	Text  string
	Cites []citationRow
}

type citationRow struct {
	Gives           []giveButton
	AsAt            string
	Statement       string
	DevelopmentPath string
	FactPath        string
	ItemTitle       string
	ItemPath        string
}

func developmentAnchor(factID int64) string { return "development-" + strconv.FormatInt(factID, 10) }

func factAnchor(factID int64) string { return "fact-" + strconv.FormatInt(factID, 10) }

func (a *app) todaysBriefing(w http.ResponseWriter, r *http.Request) {
	a.briefing(w, r, a.Now())
}

// briefing renders the Briefing page for the day that holds day.
func (a *app) briefing(w http.ResponseWriter, r *http.Request, day time.Time) {
	a.briefingWith(w, r, day, http.StatusOK, feedback.Draft{}, nil)
}

// briefingWith renders the Briefing page with the Feedback last entered on
// it and why it failed, if it did.
func (a *app) briefingWith(w http.ResponseWriter, r *http.Request, day time.Time, status int, d feedback.Draft, errs feedback.Errors) {
	b, exists, err := a.Briefings.Briefing(r.Context(), day.Format(time.DateOnly))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if exists && r.Method == http.MethodGet {
		// Opening a Briefing is kept for the register, as a side record.
		a.opened(r, a.Register.OpenedBriefing, b.Day)
	}
	p := briefingPage{
		Date: day.Format("Monday 2 January 2006"), Exists: exists,
		FeedbackPath: briefingPath(day.Format(time.DateOnly)) + "/feedback",
	}
	if day.Weekday() == time.Sunday {
		// The Sunday read is written alongside Sunday's Briefing.
		p.SundayReadPath = sundayReadPath(day.Format(time.DateOnly))
	}
	if !exists {
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
	if err := a.briefingCalls(r, &p, b, exists, day.Location()); err != nil {
		a.serverError(w, r, err)
		return
	}
	for _, bs := range b.Stories {
		row := briefedStoryRow{
			ID: bs.Story.ID, Title: bs.Story.Title, Path: storyPath(bs.Story.ID),
			Gives: storyGives(p.FeedbackPath, bs.Story.ID),
		}
		if row.MissedDevelopment, err = a.missedDevelopment(r, p.FeedbackPath, bs.Story.ID, d, errs); err != nil {
			a.serverError(w, r, err)
			return
		}
		for _, st := range bs.Statements {
			sr := statementRow{Text: st.Text}
			for _, d := range st.Developments {
				item := itemPath(d.SourceID, d.EntryID)
				sr.Cites = append(sr.Cites, citationRow{
					Gives: developmentGives(p.FeedbackPath, bs.Story.ID, d.ID),
					AsAt:  factDate(d.Fact), Statement: d.Statement,
					DevelopmentPath: storyPath(bs.Story.ID) + "#" + developmentAnchor(d.ID),
					FactPath:        item + "#" + factAnchor(d.ID),
					ItemTitle:       d.ItemTitle, ItemPath: item,
				})
			}
			row.Statements = append(row.Statements, sr)
		}
		p.Stories = append(p.Stories, row)
	}
	var facts []editor.Development
	for _, bs := range b.Stories {
		for _, st := range bs.Statements {
			facts = append(facts, st.Developments...)
		}
	}
	if exists {
		p.MissedStory = missedStory(p.FeedbackPath, facts, d, errs)
		if err := a.briefingFeedbackRows(r, &p, b); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	a.renderStatus(w, r, status, briefingTmpl, p)
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
	_, written, err := a.Briefings.Briefing(r.Context(), r.PathValue("day"))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.fragment(w, r, briefingStatusTmpl, "briefingStatus", a.statusOf(day, written))
}

// briefingCalls links the day's Briefing page to how it was written: a
// written Briefing to its trail, saying if it was written with no call; a
// day with no Briefing to each failed attempt to write it.
func (a *app) briefingCalls(r *http.Request, p *briefingPage, b editor.Briefing, exists bool, loc *time.Location) error {
	calls, err := a.Calls.CallsOn(r.Context(), briefingAct, b.Day)
	if err != nil {
		return err
	}
	var writtenBy int64
	for _, c := range calls {
		switch {
		case !exists:
			p.FailedAttempts = append(p.FailedAttempts, attemptRow{
				Page: callPath(c.ID), At: when(c.At, loc), Outcome: string(c.Outcome), Why: c.Why,
			})
		case c.Outcome == editor.Applied:
			writtenBy = c.ID
		}
	}
	// A Briefing written empty is the one with no model to name.
	p.WithoutCall = exists && writtenBy == 0 && b.Model == ""
	if exists {
		p.TrailPath = briefingTrailPath(b.Day)
	}
	return nil
}

// briefingFeedbackRows lists, on the written Briefing, the Feedback given on
// it, each to undo there; and, when Feedback took every Story out, says so,
// listing the Feedback that did.
func (a *app) briefingFeedbackRows(r *http.Request, p *briefingPage, b editor.Briefing) error {
	takenOut, err := a.Briefings.TakenOutBy(r.Context(), b.Day)
	if err != nil {
		return err
	}
	kept, err := a.Kept.KeptFeedback(r.Context())
	if err != nil {
		return err
	}
	p.Emptied = len(b.Stories) == 0 && len(takenOut) > 0
	page := briefingPath(b.Day)
	for _, k := range kept {
		switch day, onBriefing := briefingGivenOn(k.Feedback); {
		case p.Emptied && slices.Contains(takenOut, k.ID):
			p.TakenOut = append(p.TakenOut, a.keptRow(k, page))
		case onBriefing && day == b.Day:
			p.Feedback = append(p.Feedback, a.keptRow(k, page))
		}
	}
	return nil
}

// undoOnBriefing is an undo from a day's Briefing, which it lands back on.
func (a *app) undoOnBriefing(w http.ResponseWriter, r *http.Request) {
	now := a.Now()
	day, err := time.ParseInLocation(time.DateOnly, r.PathValue("day"), now.Location())
	if err != nil || day.After(now) {
		http.NotFound(w, r)
		return
	}
	to := briefingPath(r.PathValue("day"))
	if r.PathValue("day") == now.Format(time.DateOnly) {
		to = "/"
	}
	a.undo(w, r, to)
}
