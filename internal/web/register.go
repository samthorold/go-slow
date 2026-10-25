package web

import (
	"context"
	"net/http"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/register"
)

// Register answers the query the register page needs.
type Register interface {
	// RegisterRecord is what the register reads from the Record, with days
	// taken in loc.
	RegisterRecord(ctx context.Context, loc *time.Location) (register.Record, error)
	// OpenedBriefing keeps that the day's Briefing was opened on the day
	// on, once per day.
	OpenedBriefing(ctx context.Context, day, on string) error
	// OpenedSundayRead keeps that the day's Sunday read was opened on the
	// day on, once per day.
	OpenedSundayRead(ctx context.Context, day, on string) error
}

// opened keeps that an output was opened now, as a side record: a failure
// to keep it is logged and the page is shown as it would be anyway.
func (a *app) opened(r *http.Request, keep func(ctx context.Context, day, on string) error, day string) {
	if err := keep(r.Context(), day, a.Now().Format(time.DateOnly)); err != nil {
		a.Log.ErrorContext(r.Context(), "keeping an open failed", "path", r.URL.Path, "error", err)
	}
}

// registerPage is the view model for the register: every hypothesis, its
// status and its last reading.
type registerPage struct {
	Hypotheses []hypothesisRow
}

type hypothesisRow struct {
	Name, Part, Status, Why string
	// Window is the last reading's, in words; empty for a reading of the
	// Record as it stands.
	Window  string
	Last    string
	Found   []string
	HasLast bool
}

var registerTmpl = page("register.html")

func (a *app) register(w http.ResponseWriter, r *http.Request) {
	now := a.Now()
	rec, err := a.Register.RegisterRecord(r.Context(), now.Location())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	rec.Today, rec.Location = now.Format(time.DateOnly), now.Location()
	rec.TodayDue = !now.Before(editor.DueAt(now, a.BriefingAt))
	sources, err := a.Items.SourceStatuses(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	for _, s := range sources {
		rec.Sources = append(rec.Sources, s.Name)
		if s.Quiet(now) {
			rec.QuietSources = append(rec.QuietSources, s.Name)
		}
	}
	var p registerPage
	for _, h := range register.Read(rec) {
		row := hypothesisRow{Name: h.Name, Part: string(h.Part), Status: string(h.Status), Why: h.Why}
		if h.Last != nil {
			row.HasLast, row.Last, row.Found = true, string(h.Last.Status), h.Last.Found
			if h.Last.From != "" {
				row.Window = h.Last.From + " to " + h.Last.To
			}
		}
		p.Hypotheses = append(p.Hypotheses, row)
	}
	a.render(w, r, registerTmpl, p)
}
