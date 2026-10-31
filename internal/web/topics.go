package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/samthorold/go-slow/internal/interest"
)

// Topics is the Owner's commands and queries on Topics.
type Topics interface {
	Topics(ctx context.Context) ([]interest.Topic, error)
	Add(ctx context.Context, d interest.Draft) (interest.Topic, interest.Errors, error)
	Remove(ctx context.Context, id int64) error
}

// topicsPage is the view model for the Topics page: the Topics, and the
// form that takes up a new one.
type topicsPage struct {
	Topics []topicRow
	Form   topicForm
}

type topicRow struct {
	ID    int64
	Name  string
	Terms []string
}

// topicForm is the add form: what was entered, and why it failed.
type topicForm struct {
	Name   string
	Terms  string
	Errors interest.Errors
}

func (a *app) topics(w http.ResponseWriter, r *http.Request) {
	a.renderTopics(w, r, http.StatusOK, topicForm{})
}

func (a *app) addTopic(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad form.", http.StatusBadRequest)
		return
	}
	d := interest.Draft{Name: r.PostForm.Get("name"), Terms: r.PostForm.Get("terms")}
	_, errs, err := a.Topics.Add(r.Context(), d)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if len(errs) > 0 {
		a.renderTopics(w, r, http.StatusUnprocessableEntity, topicForm{Name: d.Name, Terms: d.Terms, Errors: errs})
		return
	}
	http.Redirect(w, r, "/topics", http.StatusSeeOther)
}

// removeTopic stops following a Topic. The page removes its row: 200 with
// nothing to swap in.
func (a *app) removeTopic(w http.ResponseWriter, r *http.Request) {
	id, ok := topicID(w, r)
	if !ok {
		return
	}
	err := a.Topics.Remove(r.Context(), id)
	switch {
	case errors.Is(err, interest.ErrNoTopic):
		http.NotFound(w, r)
	case err != nil:
		a.serverError(w, r, err)
	default:
		w.WriteHeader(http.StatusOK)
	}
}

// topicID reads the Topic's id from the path, answering 404 if it can't.
func topicID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}

// renderTopics renders the Topics page with the add form f.
func (a *app) renderTopics(w http.ResponseWriter, r *http.Request, status int, f topicForm) {
	topics, err := a.Topics.Topics(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p := topicsPage{Form: f}
	for _, t := range topics {
		p.Topics = append(p.Topics, topicRow{ID: t.ID, Name: t.Name, Terms: t.Terms})
	}
	a.renderStatus(w, r, status, topicsTmpl, p)
}
