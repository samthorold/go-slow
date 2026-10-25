package web

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/feedback"
)

// Feedback is the Owner's Feedback commands.
type Feedback interface {
	// Give keeps Feedback as given, at now, and fixes the Record by it.
	Give(ctx context.Context, d feedback.Draft, now time.Time) (feedback.Feedback, feedback.Errors, error)
	// Undo undoes the piece of Feedback with id, at now, lifting the marks
	// it made; undoing it again changes nothing.
	Undo(ctx context.Context, id int64, now time.Time) (feedback.Feedback, error)
}

// Kept answers the query the Feedback page needs.
type Kept interface {
	// KeptFeedback is every piece of Feedback, as given, newest first.
	KeptFeedback(ctx context.Context) ([]feedback.Kept, error)
}

// feedbackPage is the view model for the Feedback page: every piece, as
// given.
type feedbackPage struct {
	Feedback []keptRow
}

type keptRow struct {
	Kind, Level string
	// About names what it is about; AboutPath is its page, if it has one.
	About, AboutPath string
	// Typed is a missed Story's title, as the Owner typed it.
	Typed     string
	Depth     string
	Briefing  string
	BriefPath string
	GivenAt   string
	// Undone is when the Owner undid it; UndoPath, for a piece standing, is
	// where its undo goes from this page.
	Undone, UndoPath string
	// Behind is the kept calls behind what it is about, and ByOwner
	// whether the Owner made it, so no call is behind it.
	Behind  []behindLink
	ByOwner bool
}

// behindLink is a link to a kept call or conversation behind what a piece
// of Feedback is about.
type behindLink struct{ Path, Label string }

// behind is the links to the kept calls behind what k is about.
func behind(k feedback.Kept) []behindLink {
	label := map[feedback.Level]string{
		feedback.Fact: "reading call", feedback.Development: "placing call",
		feedback.Story: "placing call", feedback.Connection: "connecting call",
	}[k.Level]
	var out []behindLink
	for _, id := range k.Calls {
		if id != 0 {
			out = append(out, behindLink{Path: callPath(id), Label: "the " + label})
		}
	}
	if k.Conversation != 0 {
		out = append(out, behindLink{Path: conversationPath(k.Conversation, 0), Label: "the read's conversation"})
	}
	return out
}

// undonePath is where the undo of the piece id goes from the page at page
// ("" for the Feedback page).
func undonePath(page string, id int64) string {
	return page + "/feedback/" + strconv.FormatInt(id, 10) + "/undone"
}

func (a *app) feedback(w http.ResponseWriter, r *http.Request) {
	kept, err := a.Kept.KeptFeedback(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var p feedbackPage
	for _, k := range kept {
		p.Feedback = append(p.Feedback, a.keptRow(k, ""))
	}
	a.render(w, r, feedbackTmpl, p)
}

// keptRow is a piece of Feedback as a page lists it, undone from the page
// at page.
func (a *app) keptRow(k feedback.Kept, page string) keptRow {
	loc := a.Now().Location()
	row := keptRow{Kind: string(k.Kind), Level: string(k.Level), Typed: k.Title,
		Depth: k.Depth.String(), GivenAt: when(k.GivenAt, loc)}
	if k.Undone() {
		row.Undone = when(k.UndoneAt, loc)
	} else {
		row.UndoPath = undonePath(page, k.ID)
	}
	row.Behind, row.ByOwner = behind(k), k.ByOwner
	switch k.Level {
	case feedback.Story:
		row.About = k.Story
	case feedback.Development:
		row.About = k.Fact + " in " + k.Story
	case feedback.Fact:
		row.About = k.Fact
	case feedback.Connection:
		row.About = k.Fact + " led to " + k.ToFact
	case feedback.Assessment:
		row.About, row.AboutPath = k.Assessment, assessmentPath(k.AssessmentID)
	case feedback.SundayRead:
		row.About, row.AboutPath = "The Sunday read", sundayReadPath(k.ReadDay)
	}
	if k.StoryID != 0 {
		row.AboutPath = storyPath(k.StoryID)
	}
	if day, ok := briefingGivenOn(k.Feedback); ok {
		if d, err := time.Parse(time.DateOnly, day); err == nil {
			row.Briefing, row.BriefPath = d.Format("2 January 2006"), briefingPath(day)
		}
	}
	return row
}

// briefingGivenOn is the day of the Briefing f was given on, if it was.
// Feedback on a Sunday read was given on the read, not a Briefing.
func briefingGivenOn(f feedback.Feedback) (string, bool) {
	onRead := f.Level == feedback.Assessment || f.Level == feedback.SundayRead
	return f.Day, f.Day != "" && !onRead
}

// undo undoes the piece of Feedback the path names, landing back on the
// page to: the page re-renders with the Record as the undo left it.
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
	a.undo(w, r, "/feedback")
}

// giveButton is a piece of Feedback given with one button.
type giveButton struct {
	Path, Kind, Level   string
	Story, Fact, ToFact int64
	Assessment, Read    int64
	Label, Confirm      string
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

const confirmWrongFact = "Withdraw this Fact, and the Developments and Connections built on it?"

// storyGives is the Feedback on a Story: not wanted in the Briefing, or new
// to the Owner.
func storyGives(path string, story int64) []giveButton {
	return []giveButton{
		{Path: path, Kind: string(feedback.Unwanted), Level: string(feedback.Story), Story: story, Label: "Unwanted"},
		{Path: path, Kind: string(feedback.NewToMe), Level: string(feedback.Story), Story: story, Label: "New to me"},
	}
}

// developmentGives is the Feedback on a Development: not wanted, its Fact
// wrong, or new to the Owner.
func developmentGives(path string, story, fact int64) []giveButton {
	return []giveButton{
		{Path: path, Kind: string(feedback.Unwanted), Level: string(feedback.Development), Story: story, Fact: fact, Label: "Unwanted"},
		{Path: path, Kind: string(feedback.Wrong), Level: string(feedback.Fact), Fact: fact, Label: "Wrong", Confirm: confirmWrongFact},
		{Path: path, Kind: string(feedback.NewToMe), Level: string(feedback.Development), Story: story, Fact: fact, Label: "New to me"},
	}
}

// connectionGives is the Feedback on a Connection: wrong, or new to the
// Owner.
func connectionGives(path string, from, to int64) []giveButton {
	return []giveButton{
		{Path: path, Kind: string(feedback.Wrong), Level: string(feedback.Connection), Fact: from, ToFact: to, Label: "Wrong",
			Confirm: "Withdraw this Connection?"},
		{Path: path, Kind: string(feedback.NewToMe), Level: string(feedback.Connection), Fact: from, ToFact: to, Label: "New to me"},
	}
}

// missedStoryForm is the form for a Story the Owner says was missed: its
// title, and which of the Facts on the page it gathers as its Developments.
type missedStoryForm struct {
	Path   string
	Title  string
	Facts  []factOption
	Errors feedback.Errors
}

type factOption struct {
	ID        int64
	Statement string
	Checked   bool
}

// missedStory is the missed-Story form offering facts, with what d entered
// and its errs if d was a missed Story.
func missedStory(path string, facts []editor.Development, d feedback.Draft, errs feedback.Errors) missedStoryForm {
	f := missedStoryForm{Path: path}
	mine := d.Kind == string(feedback.Missed) && d.Level == string(feedback.Story)
	if mine {
		f.Title, f.Errors = d.Title, errs
	}
	for _, fact := range facts {
		if fact.Withdrawn || slices.ContainsFunc(f.Facts, func(o factOption) bool { return o.ID == fact.ID }) {
			continue
		}
		id := strconv.FormatInt(fact.ID, 10)
		f.Facts = append(f.Facts, factOption{ID: fact.ID, Statement: fact.Statement, Checked: mine && slices.Contains(d.Facts, id)})
	}
	return f
}

// missedDevelopmentForm is the form for a Development the Owner says a
// Story missed: one of the Facts that share an Entity with it.
type missedDevelopmentForm struct {
	Path  string
	Story int64
	Facts []factOption
	Error string
}

// missedDevelopment is the missed-Development form for the Story, offering
// facts, with d's error if d was a missed Development in it.
func (a *app) missedDevelopment(r *http.Request, path string, story int64, d feedback.Draft, errs feedback.Errors) (missedDevelopmentForm, error) {
	facts, err := a.Stories.FactsToAdd(r.Context(), story)
	if err != nil {
		return missedDevelopmentForm{}, err
	}
	f := missedDevelopmentForm{Path: path, Story: story}
	if d.Kind == string(feedback.Missed) && d.Level == string(feedback.Development) && d.Story == strconv.FormatInt(story, 10) {
		f.Error = errs["fact"]
		if f.Error == "" {
			f.Error = errs[""]
		}
	}
	for _, fact := range facts {
		f.Facts = append(f.Facts, factOption{ID: fact.ID, Statement: factDate(fact) + ": " + fact.Statement})
	}
	return f, nil
}

// draftFrom reads Feedback from a form.
func draftFrom(r *http.Request) feedback.Draft {
	return feedback.Draft{
		Kind:       r.PostForm.Get("kind"),
		Level:      r.PostForm.Get("level"),
		Story:      r.PostForm.Get("story"),
		Fact:       r.PostForm.Get("fact"),
		ToFact:     r.PostForm.Get("to_fact"),
		Assessment: r.PostForm.Get("assessment"),
		Read:       r.PostForm.Get("read"),
		Title:      r.PostForm.Get("title"),
		Facts:      r.PostForm["facts"],
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

// storyFeedback is Feedback given on a Story's page, about the Story, its
// Developments, their Facts or its Connections. It lands back on the page.
func (a *app) storyFeedback(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, found, err := a.Stories.Story(r.Context(), id); err != nil || !found {
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
	d := draftFrom(r)
	_, errs, err := a.Feedback.Give(r.Context(), d, a.Now())
	switch {
	case errors.Is(err, feedback.ErrNotInRecord):
		http.NotFound(w, r)
	case err != nil:
		a.serverError(w, r, err)
	case len(errs) > 0:
		a.storyWith(w, r, id, http.StatusUnprocessableEntity, d, errs)
	default:
		http.Redirect(w, r, storyPath(id), http.StatusSeeOther)
	}
}

// briefingFeedback is Feedback given on a day's Briefing. It lands back on
// the Briefing, which re-renders with the Record as Feedback left it.
func (a *app) briefingFeedback(w http.ResponseWriter, r *http.Request) {
	now := a.Now()
	day, err := time.ParseInLocation(time.DateOnly, r.PathValue("day"), now.Location())
	if err != nil || day.After(now) {
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
		a.briefingWith(w, r, day, http.StatusUnprocessableEntity, d, errs)
	default:
		to := briefingPath(d.Day)
		if d.Day == now.Format(time.DateOnly) {
			to = "/"
		}
		http.Redirect(w, r, to, http.StatusSeeOther)
	}
}
