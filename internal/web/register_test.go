package web_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
)

func TestOpeningABriefingIsRecordedWithItsDateOncePerDay(t *testing.T) {
	store := openStore(t)
	thursday := saturday.AddDate(0, 0, -2)
	writeBriefing(t, store, thursday, `{}`)
	writeBriefing(t, store, saturday, `{}`)
	app, _ := newAppWithLog(t, saturday, store)

	first := get(t, app, "/")
	again := get(t, app, "/")
	get(t, app, "/briefings/2026-10-22")
	// Friday has no Briefing to open.
	get(t, app, "/briefings/2026-10-23")

	if first.body != again.body {
		t.Errorf("recording the open changed the page:\n%s\n---\n%s", first.body, again.body)
	}
	rec, err := store.RegisterRecord(t.Context(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"2026-10-24"}; !slices.Equal(rec.BriefingOpens, want) {
		t.Errorf("Briefing opens %v, want %v: one day, however many Briefings were opened on it", rec.BriefingOpens, want)
	}
	if rec.First != "2026-10-22" {
		t.Errorf("first day %q, want the first Briefing's, 2026-10-22", rec.First)
	}
}

// row is the register's row for the named hypothesis.
func row(t *testing.T, body, name string) string {
	t.Helper()
	i := strings.Index(body, name)
	if i < 0 {
		t.Fatalf("the register does not show %q:\n%s", name, body)
	}
	r := body[i:]
	if end := strings.Index(r, "</tr>"); end >= 0 {
		r = r[:end]
	}
	return r
}

func TestTheRegisterReadsTheNecessaryConditionsFromTheRecord(t *testing.T) {
	store := openStore(t)
	thursday := saturday.AddDate(0, 0, -2)
	speechStory(t, store, thursday)
	writeBriefing(t, store, thursday, spoke)
	// Friday's was never written.
	writeBriefing(t, store, saturday, `{}`)
	if err := store.SyncSources(t.Context(), collect.Sources(), saturday.AddDate(0, 0, -30)); err != nil {
		t.Fatal(err)
	}
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/register")

	if r := row(t, res.body, "The outputs appear."); !strings.Contains(r, "refuted") || !strings.Contains(r, "No Briefing on 2026-10-23") {
		t.Errorf("a missed Friday does not refute the outputs appear:\n%s", r)
	}
	if r := row(t, res.body, "Everything traces."); !strings.Contains(r, "<strong>supported</strong>") {
		t.Errorf("everything traces is not supported:\n%s", r)
	}
	// Every Source has been collected for 30 days and yielded nothing but
	// the speech's.
	if r := row(t, res.body, "No Source goes quiet."); !strings.Contains(r, "refuted") || !strings.Contains(r, "Quiet past its usual gap") {
		t.Errorf("Sources silent for 30 days do not refute no Source goes quiet:\n%s", r)
	}
}

func TestTheRegisterShowsEveryHypothesisDueOnAnEmptyRecord(t *testing.T) {
	app := newApp(t, saturday)

	res := get(t, app, "/register")

	if res.status != 200 || !strings.Contains(res.body, "<!doctype html>") {
		t.Fatalf("status %d, not a full page:\n%s", res.status, res.body)
	}
	for _, name := range []string{
		"The Owner keeps reading the Briefing and the Sunday read.",
		"The outputs appear.", "Everything traces.", "No Source goes quiet.",
		"Coverage: the Editor misses less", "Brevity: the Editor includes less that isn&#39;t wanted",
		"Accuracy: the Editor gets less wrong", "Foresight: the Editor shows the Owner what they hadn&#39;t seen",
	} {
		i := strings.Index(res.body, name)
		if i < 0 {
			t.Errorf("the register does not show %q:\n%s", name, res.body)
			continue
		}
		// The status follows the hypothesis, in its row.
		row := res.body[i:]
		if end := strings.Index(row, "</tr>"); end >= 0 {
			row = row[:end]
		}
		if !strings.Contains(row, "Due") {
			t.Errorf("%q is not Due on an empty Record:\n%s", name, row)
		}
	}
}
