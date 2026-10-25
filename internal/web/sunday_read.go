package web

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
)

// SundayReads answers the queries the Sunday read's page needs.
type SundayReads interface {
	// SundayRead is the day's Sunday read (YYYY-MM-DD), reporting whether
	// one was written.
	SundayRead(ctx context.Context, day string) (editor.SundayRead, bool, error)
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
	// Gives is the Feedback on the read as a whole.
	Gives        []giveButton
	Statements   []readStatementRow
	Assessments  []assessmentRow
	Stories      []startedStoryRow
	// Failure is the latest failure to write the read, for a Sunday with
	// none.
	Failure *failureRow
}

type readStatementRow struct {
	Anchor string
	Text   string
	Cites  []readCitationRow
}

// readCitationRow is one thing a statement cites, with where it can be
// read: a Development on its Story's Timeline, a Fact on its Item's page, an
// Assessment on this page or an earlier read's, or the Wikidata revision a
// lookup saw.
type readCitationRow struct {
	Kind      string
	Text      string
	Path      string
	AsAt      string
	ItemTitle string
	ItemPath  string
}

type assessmentRow struct {
	Anchor     string
	Gives      []giveButton
	Withdrawn  bool
	Statement  string
	AsAt       string
	Supersedes string
	Facts      []readCitationRow
}

type startedStoryRow struct {
	Title string
	Path  string
}

// sundayRead is a Sunday read's page, by date: any Sunday up to today.
func (a *app) sundayRead(w http.ResponseWriter, r *http.Request) {
	now := a.Now()
	day, err := time.ParseInLocation(time.DateOnly, r.PathValue("day"), now.Location())
	if err != nil || day.After(now) || day.Weekday() != time.Sunday {
		http.NotFound(w, r)
		return
	}
	key := day.Format(time.DateOnly)
	read, exists, err := a.SundayReads.SundayRead(r.Context(), key)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p := sundayReadPage{Date: day.Format("Sunday 2 January 2006"), BriefingPath: briefingPath(key), Exists: exists}
	feedbackPath := sundayReadPath(key) + "/feedback"
	if exists {
		p.Gives = readGives(feedbackPath, read.ID)
	}
	if !exists {
		fs, err := a.SundayReads.SundayReadFailures(r.Context(), key)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		if len(fs) > 0 {
			f := fs[len(fs)-1]
			p.Failure = &failureRow{At: when(f.At, day.Location()), Reason: f.Reason}
		}
	}
	for _, st := range read.Statements {
		row := readStatementRow{Anchor: statementAnchor(st.ID), Text: st.Text}
		for _, c := range st.Cites {
			row.Cites = append(row.Cites, citation(c, key))
		}
		p.Statements = append(p.Statements, row)
	}
	for _, as := range read.Assessments {
		row := assessmentRow{Anchor: assessmentAnchor(as.ID), Statement: as.Statement, AsAt: as.AsAt.Format("2 January 2006"),
			Withdrawn: as.Withdrawn}
		if !as.Withdrawn {
			row.Gives = assessmentGives(feedbackPath, as.ID)
		}
		if as.Supersedes != 0 {
			row.Supersedes = assessmentPath(as.Supersedes)
		}
		for _, f := range as.Facts {
			row.Facts = append(row.Facts, citation(editor.Citation{Kind: editor.CitesFact, Fact: f}, key))
		}
		p.Assessments = append(p.Assessments, row)
	}
	for _, s := range read.Stories {
		p.Stories = append(p.Stories, startedStoryRow{Title: s.Title, Path: storyPath(s.ID)})
	}
	a.render(w, r, sundayReadTmpl, p)
}

// citation is what a statement in the read for day cites, as the page
// shows it.
func citation(c editor.Citation, day string) readCitationRow {
	switch c.Kind {
	case editor.CitesDevelopment, editor.CitesFact:
		item := itemPath(c.Fact.SourceID, c.Fact.EntryID)
		row := readCitationRow{Kind: "Fact", Text: c.Fact.Statement, Path: item + "#" + factAnchor(c.Fact.ID),
			AsAt: factDate(c.Fact.Fact), ItemTitle: c.Fact.ItemTitle, ItemPath: item}
		if c.Kind == editor.CitesDevelopment {
			row.Kind, row.Path = "Development", storyPath(c.Story.ID)+"#"+developmentAnchor(c.Fact.ID)
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
