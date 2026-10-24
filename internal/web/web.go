// Package web is the web adapter: the hypermedia API, its handlers and
// templates.
package web

import (
	"bytes"
	"context"
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
)

// Briefings answers the queries the Briefing pages need.
type Briefings interface {
	// HasBriefing reports whether a Briefing has been written for the day
	// that contains t.
	HasBriefing(ctx context.Context, t time.Time) (bool, error)
}

// Items answers the queries the Items page needs.
type Items interface {
	// ListItems lists the Items in the Record, newest first.
	ListItems(ctx context.Context) ([]collect.ListedItem, error)
	// SourceStatuses reports how each Source stands for going quiet.
	SourceStatuses(ctx context.Context) ([]collect.SourceStatus, error)
}

// Deps are the app's dependencies. The app reads nothing from the
// environment: main builds these and passes them in.
type Deps struct {
	Briefings Briefings
	Items     Items
	Topics    Topics
	// Now is the clock. The day it returns, in its location, is "today".
	Now func() time.Time
	Log *slog.Logger
}

var (
	//go:embed templates/*.html
	templateFS embed.FS
	//go:embed static
	staticFS embed.FS
)

// briefingTmpl is the Briefing page: the layout around its content.
var briefingTmpl = page("briefing.html")

// itemsTmpl is the Items page.
var itemsTmpl = page("items.html")

// topicsTmpl is the Topics page.
var topicsTmpl = page("topics.html")

// page parses a page template together with the layout it fills.
func page(name string) *template.Template {
	return template.Must(template.ParseFS(templateFS, "templates/layout.html", "templates/"+name))
}

// New builds the app from its dependencies.
func New(d Deps) http.Handler {
	a := &app{Deps: d}
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.FileServerFS(staticFS))
	mux.HandleFunc("GET /{$}", a.todaysBriefing)
	mux.HandleFunc("GET /items", a.items)
	mux.HandleFunc("GET /topics", a.topics)
	mux.HandleFunc("POST /topics", a.addTopic)
	mux.HandleFunc("PUT /topics/{id}", a.setDepth)
	mux.HandleFunc("DELETE /topics/{id}", a.removeTopic)
	return mux
}

type app struct {
	Deps
}

// briefingPage is the view model for a day's Briefing page.
type briefingPage struct {
	Date   string
	Exists bool
}

func (a *app) todaysBriefing(w http.ResponseWriter, r *http.Request) {
	now := a.Now()
	exists, err := a.Briefings.HasBriefing(r.Context(), now)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, briefingTmpl, briefingPage{
		Date:   now.Format("Monday 2 January 2006"),
		Exists: exists,
	})
}

// render writes a full page, rendered first so a template error can still
// become a clean 500.
func (a *app) render(w http.ResponseWriter, r *http.Request, t *template.Template, data any) {
	a.renderStatus(w, r, http.StatusOK, t, data)
}

// renderStatus is render with a status other than 200, such as the 422 of a
// form that failed validation.
func (a *app) renderStatus(w http.ResponseWriter, r *http.Request, status int, t *template.Template, data any) {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", data); err != nil {
		a.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

func (a *app) serverError(w http.ResponseWriter, r *http.Request, err error) {
	a.Log.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	http.Error(w, "Something went wrong.", http.StatusInternalServerError)
}
