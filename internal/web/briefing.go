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
	// SundayReadPath is the Sunday read's page, on a Sunday.
	SundayReadPath string
	// Missed is the time the Briefing was due by, for a day with none by
	// then; empty otherwise.
	Missed string
	// Failure is the latest failure to write the day's Briefing.
	Failure *failureRow
	// FeedbackPath is where Feedback given on the Briefing goes.
	FeedbackPath string
	// MissedStory is the form for a missed Story: what was entered, and why
	// it failed.
	MissedStory missedStoryForm
}

type failureRow struct {
	At     string
	Reason string
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
	}
	a.renderStatus(w, r, status, briefingTmpl, p)
}
