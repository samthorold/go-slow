package web

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/samthorold/go-slow/internal/feedback"
)

// Feedback is the Owner's Feedback commands.
type Feedback interface {
	// Give keeps Feedback as given, at now, and fixes the Record by it.
	Give(ctx context.Context, d feedback.Draft, now time.Time) (feedback.Feedback, feedback.Errors, error)
}

// draftFrom reads Feedback from a form.
func draftFrom(r *http.Request) feedback.Draft {
	return feedback.Draft{
		Kind:   r.PostForm.Get("kind"),
		Level:  r.PostForm.Get("level"),
		Story:  r.PostForm.Get("story"),
		Fact:   r.PostForm.Get("fact"),
		ToFact: r.PostForm.Get("to_fact"),
		Title:  r.PostForm.Get("title"),
		Facts:  r.PostForm["facts"],
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
