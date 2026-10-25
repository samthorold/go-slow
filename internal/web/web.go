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
	"github.com/samthorold/go-slow/internal/editor"
)

// Items answers the queries the Items page needs.
type Items interface {
	// ListItems lists the Items in the Record, newest first.
	ListItems(ctx context.Context) ([]collect.ListedItem, error)
	// Upcoming lists the scheduled events not yet due at now, soonest
	// first.
	Upcoming(ctx context.Context, now time.Time) ([]collect.ListedItem, error)
	// SourceStatuses reports how each Source stands for going quiet.
	SourceStatuses(ctx context.Context) ([]collect.SourceStatus, error)
	// FailedWork is the acts' work marked failed, most recently failed
	// first.
	FailedWork(ctx context.Context) ([]editor.FailedWork, error)
}

// Deps are the app's dependencies. The app reads nothing from the
// environment: main builds these and passes them in.
type Deps struct {
	Briefings Briefings
	Items     Items
	Readings  Readings
	Topics    Topics
	Stories   Stories
	Feedback  Feedback
	Kept      Kept
	Register  Register
	// SundayReads answers the Sunday read's page.
	SundayReads SundayReads
	// BriefingAt is the time of day the Briefing is due (7h is 07:00): a
	// day with none by then is flagged.
	BriefingAt time.Duration
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

// briefingsTmpl is the list of Briefings, by day.
var briefingsTmpl = page("briefings.html")

// itemsTmpl is the Items page.
var itemsTmpl = page("items.html")

// itemTmpl is an Item's own page.
var itemTmpl = page("item.html")

// topicsTmpl is the Topics page.
var topicsTmpl = page("topics.html")

// storiesTmpl is the Stories page, and storyTmpl a Story's own page.
var (
	storiesTmpl = page("stories.html")
	storyTmpl   = page("story.html")
)

// feedbackTmpl is the Feedback page: every piece, as given.
var feedbackTmpl = page("feedback_list.html")

// sundayReadTmpl is a Sunday read's page.
var sundayReadTmpl = page("sunday_read.html")

// page parses a page template together with the layout it fills.
func page(name string) *template.Template {
	return template.Must(template.ParseFS(templateFS, "templates/layout.html", "templates/feedback.html", "templates/"+name))
}

// New builds the app from its dependencies.
func New(d Deps) http.Handler {
	a := &app{Deps: d}
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.FileServerFS(staticFS))
	mux.HandleFunc("GET /{$}", a.todaysBriefing)
	mux.HandleFunc("GET /briefings", a.briefings)
	mux.HandleFunc("GET /briefings/{day}", a.pastBriefing)
	mux.HandleFunc("POST /briefings/{day}/feedback", a.briefingFeedback)
	mux.HandleFunc("GET /sunday-reads/{day}", a.sundayRead)
	mux.HandleFunc("POST /sunday-reads/{day}/feedback", a.sundayReadFeedback)
	mux.HandleFunc("GET /assessments/{id}", a.assessment)
	mux.HandleFunc("GET /feedback", a.feedback)
	mux.HandleFunc("GET /items", a.items)
	mux.HandleFunc("GET /items/{source}/{entry}", a.item)
	mux.HandleFunc("GET /register", a.register)
	mux.HandleFunc("GET /stories", a.stories)
	mux.HandleFunc("GET /stories/{id}", a.story)
	mux.HandleFunc("POST /stories/{id}/feedback", a.storyFeedback)
	mux.HandleFunc("GET /topics", a.topics)
	mux.HandleFunc("POST /topics", a.addTopic)
	mux.HandleFunc("PUT /topics/{id}", a.setDepth)
	mux.HandleFunc("DELETE /topics/{id}", a.removeTopic)
	return mux
}

type app struct {
	Deps
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
