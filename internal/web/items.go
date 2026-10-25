package web

import (
	"net/http"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
)

// itemsPage is the view model for the Items page: what has been collected,
// whether each Source has gone quiet, and the acts' work on Items that has
// failed.
type itemsPage struct {
	Sources  []sourceRow
	Failed   []failedRow
	Upcoming []itemRow
	Items    []itemRow
}

// failedRow is an act's work on an Item that failed every attempt.
type failedRow struct {
	Act      string
	Title    string
	Page     string
	Attempts int
	When     string
	Why      string
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
	// Scheduled is set for an upcoming scheduled event, shown with when it
	// is due rather than how it stands against the Topics.
	Scheduled bool
	Checked   bool
	// Topics are those the Item awaits reading for.
	Topics []string
	// Page is the Item's own page, with what reading drew from it.
	Page   string
	Read   bool
	Facts  int
	Claims int
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

	failed, err := a.Items.FailedWork(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	var p itemsPage
	for _, f := range failed {
		p.Failed = append(p.Failed, failedRow{
			Act: f.Act, Title: f.Title, Page: itemPath(f.SourceID, f.EntryID),
			Attempts: f.Attempts, When: when(f.LastFailedAt, now.Location()), Why: f.Why,
		})
	}
	for _, s := range statuses {
		p.Sources = append(p.Sources, sourceRow{
			Name:   s.Name,
			Newest: when(s.Newest, now.Location()),
			Quiet:  s.Quiet(now),
		})
	}
	for _, it := range upcoming {
		r := row(it, it.Due, now.Location())
		r.Scheduled = true
		p.Upcoming = append(p.Upcoming, r)
	}
	for _, it := range items {
		r := row(it, it.Published, now.Location())
		r.Checked, r.Topics = it.Checked, it.Topics
		r.Read, r.Facts, r.Claims = it.Read, it.Facts, it.Claims
		p.Items = append(p.Items, r)
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
		Page:        itemPath(it.SourceID, it.EntryID),
	}
}

// when shows t in the clock's time zone, or nothing for the zero time.
func when(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	return t.In(loc).Format(shownTime)
}
