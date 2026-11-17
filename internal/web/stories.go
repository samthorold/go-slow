package web

import (
	"context"
	"net/http"
	"strconv"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
)

// Stories answers the Stories Page and a Story's Page.
type Stories interface {
	// StoriesMade are the Stories the Editor made, newest first.
	StoriesMade(ctx context.Context) ([]editor.Story, error)
	// StoryMade is a Story the Editor made, by its event, reporting whether
	// the Record holds one.
	StoryMade(ctx context.Context, id int64) (editor.Story, bool, error)
	// StoriesTouching are the Stories the Editor made that touch the Topic,
	// newest first.
	StoriesTouching(ctx context.Context, topic interest.Topic) ([]editor.Story, error)
}

// storiesTmpl is the Stories Page, and storyTmpl a Story's Page.
var (
	storiesTmpl = page("/stories", "stories.html")
	storyTmpl   = page(noIndex, "story.html")
)

// storyPath is a Story's Page: its event's id.
func storyPath(id int64) string { return "/stories/" + strconv.FormatInt(id, 10) }

// storyRow is a Story as the Stories Page lists it: by its opening words,
// until it has a Name, linking its page, with when it was made.
type storyRow struct {
	Opening, Href, When string
}

// storiesPage is the view model of the Stories Page.
type storiesPage struct {
	Stories []storyRow
}

// stories is the Stories Page: the Stories the Editor made, newest first.
// Opening it changes nothing.
func (a *app) stories(w http.ResponseWriter, r *http.Request) {
	made, err := a.Stories.StoriesMade(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, storiesTmpl, storiesPage{Stories: a.storyRows(made)})
}

// storyRows are Stories as a list of them shows them, in the order given.
func (a *app) storyRows(stories []editor.Story) []storyRow {
	var rows []storyRow
	for _, s := range stories {
		row := storyRow{Href: storyPath(s.ID), When: when(s.At, a.Now().Location())}
		if len(s.Spans) > 0 {
			row.Opening = shorten(s.Spans[0].Body)
		}
		rows = append(rows, row)
	}
	return rows
}

// storyPage is the view model of a Story's Page: the Editor's Spans it
// wrote, in the order written, each with the Spans it cites, and how the
// Judgement that made it was made.
type storyPage struct {
	Title string
	When  string
	Spans []editorSpanRow
	// Made is the call that made it, and Judge its Judge's act, version and
	// model.
	Made  made
	Judge string
}

// editorSpanRow is an Editor's Span as its Story's Page shows it: its words,
// then each Span it cites, its words linking its page, with its Item.
type editorSpanRow struct {
	Body  string
	Cites []spanLink
}

// story is a Story's Page. Opening it changes nothing.
func (a *app) story(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s, found, err := a.Stories.StoryMade(r.Context(), id)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	p := storyPage{
		When:  when(s.At, a.Now().Location()),
		Made:  made{Act: "woven", Call: s.Call},
		Judge: s.Judge.Act + " v" + strconv.Itoa(s.Judge.Version) + " · " + s.Judge.Model,
	}
	if len(s.Spans) > 0 {
		p.Title = shorten(s.Spans[0].Body)
	}
	for _, es := range s.Spans {
		row := editorSpanRow{Body: es.Body}
		for _, c := range es.Cites {
			row.Cites = append(row.Cites, spanLink{Words: c.Words, Href: spanPath(c.Address),
				Item: a.cite(c.Address.SourceID, c.Title, itemPath(c.Address.SourceID, c.Address.EntryID))})
		}
		p.Spans = append(p.Spans, row)
	}
	a.render(w, r, storyTmpl, p)
}
