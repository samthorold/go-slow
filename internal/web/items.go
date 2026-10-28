package web

import (
	"net/http"
	"strconv"
	"strings"
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
	Checked     bool
	// Topics are those the Item awaits reading for.
	Topics []string
	// Page is the Item's own page, with what reading drew from it.
	Page   string
	Read   bool
	Facts  int
	Claims int
}

// Status is how a collected Item stands, as a machine label: "read · 3
// Facts, 1 Claim", "awaiting reading for <Topic>", "touches no Topic" or
// "not yet checked".
func (r itemRow) Status() string {
	switch {
	case r.Read:
		return "read · " + count(r.Facts, "Fact") + ", " + count(r.Claims, "Claim")
	case len(r.Topics) > 0:
		return "awaiting reading for " + strings.Join(r.Topics, ", ")
	case r.Checked:
		return "touches no Topic"
	default:
		return "not yet checked"
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
