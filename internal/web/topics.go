package web

import (
	"cmp"
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
	SetDepth(ctx context.Context, id int64, depth string) (interest.Errors, error)
	Remove(ctx context.Context, id int64) error
}

// topicsPage is the view model for the Topics page: the Topics, and the
// form that takes up a new one.
type topicsPage struct {
	Topics  []topicRow
	Depths  []string
	Sources []sourceOption
	Form    topicForm
}

type topicRow struct {
	ID      int64
	Name    string
	Depth   string
	Sources []string
	Terms   []string
	// DepthError is why a change to its Depth failed.
	DepthError string
}

type sourceOption struct {
	ID      string
	Name    string
	Checked bool
}

// topicForm is the add form: what was entered, and why it failed.
type topicForm struct {
	Name    string
	Depth   string
	Terms   string
	Sources map[string]bool
	Errors  interest.Errors
}

func (a *app) topics(w http.ResponseWriter, r *http.Request) {
	a.renderTopics(w, r, http.StatusOK, topicForm{Depth: interest.Following.String()})
}

func (a *app) addTopic(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad form.", http.StatusBadRequest)
		return
	}
	d := interest.Draft{
		Name:    r.PostForm.Get("name"),
		Depth:   r.PostForm.Get("depth"),
		Sources: r.PostForm["source"],
		Terms:   r.PostForm.Get("terms"),
	}
	_, errs, err := a.Topics.Add(r.Context(), d)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if len(errs) > 0 {
		f := topicForm{Name: d.Name, Depth: d.Depth, Terms: d.Terms, Sources: map[string]bool{}, Errors: errs}
		for _, id := range d.Sources {
			f.Sources[id] = true
		}
		a.renderTopics(w, r, http.StatusUnprocessableEntity, f)
		return
	}
	http.Redirect(w, r, "/topics", http.StatusSeeOther)
}

func (a *app) setDepth(w http.ResponseWriter, r *http.Request) {
	id, ok := topicID(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad form.", http.StatusBadRequest)
		return
	}
	errs, err := a.Topics.SetDepth(r.Context(), id, r.PostForm.Get("depth"))
	switch {
	case errors.Is(err, interest.ErrNoTopic):
		http.NotFound(w, r)
	case err != nil:
		a.serverError(w, r, err)
	case len(errs) > 0:
		a.renderTopicsWith(w, r, http.StatusUnprocessableEntity,
			topicForm{Depth: interest.Following.String()}, map[int64]string{id: errs["depth"]})
	default:
		http.Redirect(w, r, "/topics", http.StatusSeeOther)
	}
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

func (a *app) renderTopics(w http.ResponseWriter, r *http.Request, status int, f topicForm) {
	a.renderTopicsWith(w, r, status, f, nil)
}

// renderTopicsWith renders the Topics page with the add form f and, by
// Topic, why a change to its Depth failed.
func (a *app) renderTopicsWith(w http.ResponseWriter, r *http.Request, status int, f topicForm, depthErrors map[int64]string) {
	topics, err := a.Topics.Topics(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	statuses, err := a.Items.SourceStatuses(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	names := map[string]string{}
	p := topicsPage{Form: f}
	for _, s := range statuses {
		names[s.ID] = s.Name
		p.Sources = append(p.Sources, sourceOption{ID: s.ID, Name: s.Name, Checked: f.Sources[s.ID]})
	}
	for _, d := range interest.Depths() {
		p.Depths = append(p.Depths, d.String())
	}
	for _, t := range topics {
		row := topicRow{ID: t.ID, Name: t.Name, Depth: t.Depth.String(), Terms: t.Terms, DepthError: depthErrors[t.ID]}
		for _, id := range t.Sources {
			row.Sources = append(row.Sources, cmp.Or(names[id], id))
		}
		p.Topics = append(p.Topics, row)
	}
	a.renderStatus(w, r, status, topicsTmpl, p)
}
