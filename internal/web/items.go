package web

import (
	"net/http"
	"strconv"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
)

// itemsPage is the view model for the Items page: what has been collected,
// whether each Source has gone quiet, and the acts' work on Items that has
// failed.
type itemsPage struct {
	Sources []sourceRow
	// Quiet are the Sources gone quiet, shown first, as exceptions.
	Quiet    []sourceRow
	Failed   []failedRow
	Upcoming []itemRow
	Items    []itemRow
}

// Exceptions is whether anything needs the Owner's hand: a quiet Source or
// failed work.
func (p itemsPage) Exceptions() bool { return len(p.Quiet) > 0 || len(p.Failed) > 0 }

// failedRow is an act's work on an Item that failed every attempt.
type failedRow struct {
	Act      string
	Title    string
	Page     string
	Attempts int
	When     string
	Why      string
	// Attempts is the page of its last attempt's call, which lists every
	// attempt; empty if none was kept.
	AttemptsPage string
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
	// Page is the Item's own page, with what reading judged of it.
	Page string
	// HasText, Cut and Read are how far it has come; Names, Speakers and
	// Rejected what its newest reading judged.
	HasText, Cut, Read        bool
	Names, Speakers, Rejected int
}

// Status is how a collected Item stands with reading, as a machine label,
// whatever Topics it touches (Topics select nothing): "read · 3 names, 1
// speaker, 2 rejected", "awaiting reading", "awaiting cutting" or "no text
// yet".
func (r itemRow) Status() string {
	switch {
	case r.Read:
		return "read · " + count(r.Names, "name") + ", " + count(r.Speakers, "speaker") + ", " +
			strconv.Itoa(r.Rejected) + " rejected"
	case r.Cut:
		return "awaiting reading"
	case r.HasText:
		return "awaiting cutting"
	default:
		return "no text yet"
	}
}

// count is n things, named in the singular or plural.
func count(n int, thing string) string {
	if n != 1 {
		thing += "s"
	}
	return strconv.Itoa(n) + " " + thing
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
			AttemptsPage: callPath(f.LastCall),
		})
	}
	for _, s := range statuses {
		src := sourceRow{
			Name:   s.Name,
			Newest: when(s.Newest, now.Location()),
			Quiet:  s.Quiet(now),
		}
		p.Sources = append(p.Sources, src)
		if src.Quiet {
			p.Quiet = append(p.Quiet, src)
		}
	}
	for _, it := range upcoming {
		p.Upcoming = append(p.Upcoming, row(it, it.Due, now.Location()))
	}
	for _, it := range items {
		r := row(it, it.Published, now.Location())
		r.HasText, r.Cut, r.Read = it.HasText, it.Cut, it.Read
		r.Names, r.Speakers, r.Rejected = it.Names, it.Speakers, it.Rejected
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
