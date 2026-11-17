package web

import (
	"context"
	"net/http"
	"slices"
	"strconv"

	"github.com/samthorold/go-slow/internal/cut"
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
	// CutStory is the Story a Cut made, by the Cut's event, reporting
	// whether the Record holds one.
	CutStory(ctx context.Context, id int64) (editor.CutStory, bool, error)
	// HoldingSpan is what holds the Span at a: the Cuts whose Stories list
	// it, and the Editor's Spans citing it.
	HoldingSpan(ctx context.Context, a editor.Address) (editor.SpanHeld, error)
	// CitingEditorSpan are the Editor's Spans citing the Editor's Span at a.
	CitingEditorSpan(ctx context.Context, a editor.EditorSpanAt) ([]editor.CitingSpan, error)
	// StoriesTouching are the Stories the Editor made that touch the Topic,
	// newest first.
	StoriesTouching(ctx context.Context, topic interest.Topic) ([]editor.Story, error)
}

// storiesTmpl is the Stories Page, and storyTmpl a Story's Page.
var (
	storiesTmpl = page("/stories", "stories.html")
	storyTmpl   = page(noIndex, "story.html")
	// editorSpanTmpl is an Editor's Span's Page.
	editorSpanTmpl = page(noIndex, "editor_span.html")
)

// storyPath is a Story's Page: its event's id.
func storyPath(id int64) string { return "/stories/" + strconv.FormatInt(id, 10) }

// editorSpanPath is an Editor's Span's Page: its Story's, then its number in
// it, from 1. That is its address, since the Judgement holding it never
// changes.
func editorSpanPath(story int64, n int) string {
	return storyPath(story) + "/spans/" + strconv.Itoa(n)
}

// storyLink is a Story linking its Page, by its Name or until it has one its
// opening words, or for a
// Story a Cut made, by saying so. Editor is whether its words are the
// Editor's.
type storyLink struct {
	Words, Href string
	Editor      bool
}

// citingRow is an Editor's Span citing a Span: its words linking its Page,
// its Story and when it was written.
type citingRow struct {
	Body, Href, When string
	Story            storyLink
}

// holding is what a Span's Page shows of what holds the Span at a: the
// Stories holding it, a Cut's first, then the Editor's in the order made,
// and the Editor's Spans citing it.
func (a *app) holding(ctx context.Context, at editor.Address) ([]storyLink, []citingRow, error) {
	h, err := a.Stories.HoldingSpan(ctx, at)
	if err != nil {
		return nil, nil, err
	}
	var stories []storyLink
	for _, id := range h.Cuts {
		stories = append(stories, storyLink{Words: "The Story its Cut made", Href: storyPath(id)})
	}
	citing := a.citingRows(h.Citing)
	for _, c := range citing {
		if !slices.Contains(stories, c.Story) {
			stories = append(stories, c.Story)
		}
	}
	return stories, citing, nil
}

// citingRows are the Editor's Spans cs as a Page lists them, citing what it
// shows.
func (a *app) citingRows(cs []editor.CitingSpan) []citingRow {
	var out []citingRow
	for _, c := range cs {
		words := c.Name
		if words == "" {
			words = shorten(c.Opening)
		}
		out = append(out, citingRow{Body: c.Body, Href: editorSpanPath(c.Story, c.N),
			When:  when(c.At, a.Now().Location()),
			Story: storyLink{Words: words, Href: storyPath(c.Story), Editor: true}})
	}
	return out
}

// storyRow is a Story as the Stories Page lists it: by its Name, or by its
// opening words until it has one, linking its page, with when it was made.
type storyRow struct {
	Title, Href, When string
}

// storyTitle is what a Story is known by: its Name, or until it has one, its
// opening words.
func storyTitle(s editor.Story) string {
	if s.Name.Text != "" {
		return s.Name.Text
	}
	if len(s.Spans) > 0 {
		return shorten(s.Spans[0].Body)
	}
	return ""
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
		rows = append(rows, storyRow{Title: storyTitle(s), Href: storyPath(s.ID), When: when(s.At, a.Now().Location())})
	}
	return rows
}

// storyPage is the view model of a Story's Page: for a Story the Editor
// made, the Editor's Spans it wrote, in the order written, each with the
// Spans it cites; then its Timeline; then, for the Editor's, how the
// Judgement that made it was made.
type storyPage struct {
	Title string
	When  string
	// Cut is the Item whose Cut made it, for a Story a Cut made; nil for a
	// Story the Editor made.
	Cut      *itemCitation
	Spans    []editorSpanRow
	Timeline []timelineRow
	// Made is the call that made it, and Judge its Judge's act, version and
	// model; Named and NameJudge the same of its Name, if it has one.
	Made      made
	Judge     string
	Named     made
	NameJudge string
}

// judgeLabel is a Judge as a page shows it: its act, version and model.
func judgeLabel(j editor.Judge) string {
	return j.Act + " v" + strconv.Itoa(j.Version) + " · " + j.Model
}

// editorSpanRow is an Editor's Span as its Story's Page shows it: its words,
// then each Span it cites, its words linking its page, with its Item. Href
// is its own Page.
type editorSpanRow struct {
	Body  string
	Href  string
	Cites []spanLink
}

// timelineRow is one Span of a Story's Timeline: its words linking its page,
// with its Item, or for an earlier Editor's Span (Editor) saying it is the
// Editor's, and when it was published.
type timelineRow struct {
	Words, Href string
	Item        itemCitation
	Editor      bool
	Published   string
}

// story is a Story's Page, the Editor's or a Cut's: both are known by the
// event that made them. Opening it changes nothing.
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
		a.cutStory(w, r, id)
		return
	}
	p := storyPage{
		Title: storyTitle(s),
		When:  when(s.At, a.Now().Location()),
		Made:  made{Act: "woven", Call: s.Call},
		Judge: judgeLabel(s.Judge),
	}
	if s.Name.Text != "" {
		p.Named, p.NameJudge = made{Act: "named", Call: s.Name.Call}, judgeLabel(s.Name.Judge)
	}
	for i, es := range s.Spans {
		row := editorSpanRow{Body: es.Body, Href: editorSpanPath(s.ID, i+1)}
		for _, c := range es.Cites {
			row.Cites = append(row.Cites, a.citedLink(c))
		}
		p.Spans = append(p.Spans, row)
	}
	for _, c := range s.Timeline() {
		row := timelineRow{Words: c.Words, Published: when(c.Published, a.Now().Location())}
		if c.Of.IsZero() {
			row.Href = spanPath(c.Address)
			row.Item = a.cite(c.Address.SourceID, c.Title, itemPath(c.Address.SourceID, c.Address.EntryID))
		} else {
			row.Href, row.Editor = editorSpanPath(c.Of.Story, c.Of.N), true
		}
		p.Timeline = append(p.Timeline, row)
	}
	a.render(w, r, storyTmpl, p)
}

// citedLink is a Span an Editor's Span cites, as its margin shows it: a
// Source's, its words linking its page, with its Item; an earlier Editor's,
// its words linking the Story that wrote them, cited as the Editor's.
func (a *app) citedLink(c editor.Cited) spanLink {
	if !c.Of.IsZero() {
		href := storyPath(c.Of.Story)
		return spanLink{Words: c.Words, Href: href,
			Item: itemCitation{Source: "the Editor", Title: "an earlier Story", Href: href}}
	}
	return spanLink{Words: c.Words, Href: spanPath(c.Address),
		Item: a.cite(c.Address.SourceID, c.Title, itemPath(c.Address.SourceID, c.Address.EntryID))}
}

// editorSpanPage is the view model of an Editor's Span's Page: its words,
// the Story whose Judgement wrote it and when, the Spans it cites, and the
// later Editor's Spans citing it: a wrong one's corrections.
type editorSpanPage struct {
	Title   string
	Body    string
	When    string
	Story   storyLink
	Cites   []spanLink
	CitedBy []citingRow
}

// editorSpan is an Editor's Span's Page, by its Story and its number in it.
// Opening it changes nothing.
func (a *app) editorSpan(w http.ResponseWriter, r *http.Request) {
	id, err1 := strconv.ParseInt(r.PathValue("id"), 10, 64)
	n, err2 := strconv.Atoi(r.PathValue("n"))
	if err1 != nil || err2 != nil || n < 1 {
		http.NotFound(w, r)
		return
	}
	s, found, err := a.Stories.StoryMade(r.Context(), id)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !found || n > len(s.Spans) {
		http.NotFound(w, r)
		return
	}
	es := s.Spans[n-1]
	p := editorSpanPage{
		Title: "The Editor's Span " + strconv.Itoa(n), Body: es.Body, When: when(s.At, a.Now().Location()),
		Story: storyLink{Words: storyTitle(s), Href: storyPath(id)},
	}
	for _, c := range es.Cites {
		p.Cites = append(p.Cites, a.citedLink(c))
	}
	citing, err := a.Stories.CitingEditorSpan(r.Context(), editor.EditorSpanAt{Story: id, N: n})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p.CitedBy = a.citingRows(citing)
	a.render(w, r, editorSpanTmpl, p)
}

// cutStory is the Page of the Story the Cut id made: its Spans, in the
// order the Cut lists them, all published with its Item.
func (a *app) cutStory(w http.ResponseWriter, r *http.Request, id int64) {
	c, found, err := a.Stories.CutStory(r.Context(), id)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	it := c.Item
	item := a.cite(it.SourceID, it.Title, itemPath(it.SourceID, it.EntryID))
	p := storyPage{Title: it.Title, When: when(it.Published, a.Now().Location()), Cut: &item}
	for _, sp := range c.Spans {
		at := editor.Address{SourceID: it.SourceID, EntryID: it.EntryID, Span: sp}
		p.Timeline = append(p.Timeline, timelineRow{Words: cut.Words(it.Content, sp), Href: spanPath(at),
			Item: item, Published: p.When})
	}
	a.render(w, r, storyTmpl, p)
}
