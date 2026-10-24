package web

import (
	"net/http"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
)

// itemsPage is the view model for the Items page: what has been collected,
// and whether each Source has gone quiet.
type itemsPage struct {
	Sources  []sourceRow
	Upcoming []itemRow
	Items    []itemRow
}

type sourceRow struct {
	Name   string
	Newest string
	Quiet  bool
}

type itemRow struct {
	Title       string
	Link        string
	Summary     string
	Source      string
	When        string
	Attribution string
}

const shownTime = "Mon 2 Jan 2006, 15:04"

func (a *app) items(w http.ResponseWriter, r *http.Request) {
	now := a.Now()
	statuses, err := a.Items.SourceStatuses(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	items, err := a.Items.ListItems(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	upcoming, err := a.Items.Upcoming(r.Context(), now)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	var p itemsPage
	for _, s := range statuses {
		p.Sources = append(p.Sources, sourceRow{
			Name:   s.Name,
			Newest: when(s.Newest, now.Location()),
			Quiet:  s.Quiet(now),
		})
	}
	for _, it := range upcoming {
		p.Upcoming = append(p.Upcoming, row(it, it.Due, now.Location()))
	}
	for _, it := range items {
		p.Items = append(p.Items, row(it, it.Published, now.Location()))
	}
	a.render(w, r, itemsTmpl, p)
}

// row shows it, dated at: its publication, or for a scheduled event when it
// is due.
func row(it collect.ListedItem, at time.Time, loc *time.Location) itemRow {
	return itemRow{
		Title:       it.Title,
		Link:        it.Link,
		Summary:     it.Summary,
		Source:      it.SourceName,
		When:        when(at, loc),
		Attribution: it.Attribution,
	}
}

// when shows t in the clock's time zone, or nothing for the zero time.
func when(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	return t.In(loc).Format(shownTime)
}
