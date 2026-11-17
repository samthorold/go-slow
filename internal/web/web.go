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
	"github.com/samthorold/go-slow/internal/interest"
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
	// SourceCounts are, per Source, what the Owner steers by over the
	// window (from, to].
	SourceCounts(ctx context.Context, from, to time.Time) ([]interest.SourceCounts, error)
}

// Deps are the app's dependencies. The app reads nothing from the
// environment: main builds these and passes them in.
type Deps struct {
	Items    Items
	Readings Readings
	// Cuts answers an Item's Spans, from its newest Cut.
	Cuts   Cuts
	Topics Topics
	// Calls answers the kept calls' pages, the retired acts' included.
	Calls Calls
	// Stories answers the Stories Page and each Story's.
	Stories Stories
	// Sources is the Sources list, from which a citation takes its
	// Source's short name, by ID. A Source not in it is cited by the
	// Item's title alone.
	Sources []collect.Source
	// Now is the clock. The day it returns, in its location, is "today".
	Now func() time.Time
	// Log takes one line for each request the app answers. Required: tests
	// pass one they can read, or one that discards.
	Log *slog.Logger
}

var (
	//go:embed templates/*.html
	templateFS embed.FS
	//go:embed static
	staticFS embed.FS
)

// itemsTmpl is the Items page.
var itemsTmpl = page("/items", "items.html")

// itemTmpl is an Item's own page, and spanTmpl a Span's.
var (
	itemTmpl = page(noIndex, "item.html")
	spanTmpl = page(noIndex, "span.html")
)

// topicsTmpl is the Topics page.
var topicsTmpl = page("/topics", "topics.html")

// navLink is one link of the layout's navigation, to a page with an index of
// its own.
type navLink struct {
	Href, Name string
	// Current is whether it links to the page shown.
	Current bool
}

// indexes are the pages the navigation reaches, in its order: flat, one row.
var indexes = []navLink{
	{Href: "/stories", Name: "Stories"},
	{Href: "/items", Name: "Items"},
	{Href: "/topics", Name: "Topics"},
}

// noIndex is the index of a page that is none of the navigation's indexes,
// such as a Story's page: no link is marked current on it.
const noIndex = ""

// page parses a page template, and the components it renders, together
// with the layout it fills. index is the navigation's link to the page, or
// noIndex: what the page is is fixed by its template, not by the request.
func page(index, name string, components ...string) *template.Template {
	nav := make([]navLink, len(indexes))
	for i, l := range indexes {
		l.Current = l.Href == index
		nav[i] = l
	}
	files := []string{"templates/layout.html", "templates/margin.html", "templates/" + name}
	for _, c := range components {
		files = append(files, "templates/"+c)
	}
	t := template.New("layout.html").Funcs(template.FuncMap{"nav": func() []navLink { return nav }, "grouped": grouped})
	return template.Must(t.ParseFS(templateFS, files...))
}

// New builds the app from its dependencies.
func New(d Deps) http.Handler {
	a := &app{Deps: d, shortNames: map[string]string{}}
	for _, s := range d.Sources {
		a.shortNames[s.ID] = s.ShortName
	}
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.FileServerFS(staticFS))
	// The Owner reads Stories: the root lands on them.
	mux.Handle("GET /{$}", http.RedirectHandler("/stories", http.StatusSeeOther))
	mux.HandleFunc("GET /calls/{id}", a.call)
	mux.HandleFunc("GET /conversations/{id}", a.conversation)
	mux.HandleFunc("GET /items", a.items)
	mux.HandleFunc("GET /items/{source}/{entry}", a.item)
	mux.HandleFunc("GET /items/{source}/{entry}/spans/{span}", a.span)
	mux.HandleFunc("GET /stories", a.stories)
	mux.HandleFunc("GET /stories/{id}", a.story)
	mux.HandleFunc("GET /stories/{id}/spans/{n}", a.editorSpan)
	mux.HandleFunc("GET /topics", a.topics)
	mux.HandleFunc("POST /topics", a.addTopic)
	mux.HandleFunc("GET /topics/{id}", a.topic)
	mux.HandleFunc("DELETE /topics/{id}", a.removeTopic)
	return a.logRequests(mux)
}

type app struct {
	Deps
	// shortNames are the Sources' short names, by Source ID.
	shortNames map[string]string
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

// serverError answers 500 and notes err for the request's log line.
func (a *app) serverError(w http.ResponseWriter, r *http.Request, err error) {
	failed(r, err)
	http.Error(w, "Something went wrong.", http.StatusInternalServerError)
}
