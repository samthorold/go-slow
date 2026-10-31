package web_test

import (
	"bytes"
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/register"
	"github.com/samthorold/go-slow/internal/sqlite"
	"github.com/samthorold/go-slow/internal/web"
)

func TestOpeningABriefingIsRecordedWithItsDateOncePerDay(t *testing.T) {
	store := openStore(t)
	thursday := saturday.AddDate(0, 0, -2)
	writeStoryBriefing(t, store, thursday, `{}`)
	writeStoryBriefing(t, store, saturday, `{}`)
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
	start := strings.LastIndex(body[:i], "<tr")
	if start < 0 {
		start = i
	}
	r := body[start:]
	if end := strings.Index(r, "</tr>"); end >= 0 {
		r = r[:end]
	}
	return r
}

// cells are a row's cells, in order.
func cells(r string) []string {
	parts := strings.Split(r, "<td")
	if len(parts) < 2 {
		return nil
	}
	return parts[1:]
}

func TestTheRegisterLeadsEachRowWithItsStatusInItsHue(t *testing.T) {
	store := openStore(t)
	thursday := saturday.AddDate(0, 0, -2)
	speechStory(t, store, thursday)
	writeStoryBriefing(t, store, thursday, spoke)
	writeStoryBriefing(t, store, saturday, `{}`)
	app, _ := newAppWithLog(t, saturday, store)

	body := get(t, app, "/register").body

	for name, status := range map[string]string{
		"The outputs appear.":              "refuted",
		"Everything traces.":               "supported",
		"Coverage: the Editor misses less": "due",
	} {
		c := cells(row(t, body, name))
		if len(c) == 0 {
			t.Fatalf("%q has no cells", name)
		}
		if !strings.Contains(c[0], `class="status status-`+status+`"`) {
			t.Errorf("%q: first cell is not its status, %s, in its hue:\n%s", name, status, c[0])
		}
	}
	if !strings.Contains(body, "<th>Status</th><th>Hypothesis</th><th>Part</th>") {
		t.Errorf("the table's columns do not lead with Status, then the hypothesis and its part:\n%s", body)
	}
}

func TestTheRegisterReadsTheNecessaryConditionsFromTheRecord(t *testing.T) {
	store := openStore(t)
	thursday := saturday.AddDate(0, 0, -2)
	speechStory(t, store, thursday)
	writeStoryBriefing(t, store, thursday, spoke)
	// Friday's was never written.
	writeStoryBriefing(t, store, saturday, `{}`)
	if err := store.SyncSources(t.Context(), collect.Sources(), saturday.AddDate(0, 0, -30)); err != nil {
		t.Fatal(err)
	}
	app, _ := newAppWithLog(t, saturday, store)

	res := get(t, app, "/register")

	if r := row(t, res.body, "The outputs appear."); !strings.Contains(r, "refuted") || !strings.Contains(r, "No Briefing on 2026-10-23") {
		t.Errorf("a missed Friday does not refute the outputs appear:\n%s", r)
	}
	if r := row(t, res.body, "Everything traces."); !strings.Contains(r, `class="status status-supported">supported<`) {
		t.Errorf("everything traces is not supported:\n%s", r)
	}
	// Every Source has been collected for 30 days and yielded nothing but
	// the speech's.
	if r := row(t, res.body, "No Source goes quiet."); !strings.Contains(r, "refuted") || !strings.Contains(r, "Quiet past its usual gap") {
		t.Errorf("Sources silent for 30 days do not refute no Source goes quiet:\n%s", r)
	}
}

func TestTheRegistersReasonAndLastReadingAreQuiet(t *testing.T) {
	store := openStore(t)
	writeStoryBriefing(t, store, saturday, `{}`)
	app, _ := newAppWithLog(t, saturday, store)

	body := get(t, app, "/register").body

	// One row with a reason (Due), one with a last reading.
	for _, name := range []string{"Coverage: the Editor misses less", "Everything traces."} {
		c := cells(row(t, body, name))
		if len(c) != 4 {
			t.Fatalf("%q: %d cells, want 4", name, len(c))
		}
		if !strings.HasPrefix(c[3], ` class="quiet">`) {
			t.Errorf("%q: the reading is not quiet:\n%s", name, c[3])
		}
	}
}

// untracedRecord is the store's Record with the given untraced records added,
// which a real Record would need a broken Briefing for each to hold.
type untracedRecord struct {
	*sqlite.Store
	untraced []string
}

func (u untracedRecord) RegisterRecord(ctx context.Context, loc *time.Location) (register.Record, error) {
	r, err := u.Store.RegisterRecord(ctx, loc)
	r.Untraced = append(r.Untraced, u.untraced...)
	return r, err
}

func TestTheRegisterCollapsesALongListOfWhatWasFound(t *testing.T) {
	store := openStore(t)
	writeStoryBriefing(t, store, saturday, `{}`)
	if err := store.SyncSources(t.Context(), collect.Sources(), saturday.AddDate(0, 0, -30)); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	deps := appDeps(saturday, store, &logs)
	untraced := []string{"Development 1", "Development 2", "Development 3", "Development 4", "Fact 5"}
	deps.Register = untracedRecord{store, untraced}
	app := web.New(deps)

	body := get(t, app, "/register").body

	r := row(t, body, "Everything traces.")
	summary := regexp.MustCompile(`<details><summary>5 found</summary><ul>(?:<li>[^<]*</li>){5}</ul></details>`)
	if !summary.MatchString(r) {
		t.Errorf("five things found are not collapsed behind their count:\n%s", r)
	}
	// A short list stays open.
	if r := row(t, body, "No Source goes quiet."); strings.Contains(r, "<details>") || !strings.Contains(r, "<ul><li>") {
		t.Errorf("one thing found is collapsed:\n%s", r)
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
		if r := row(t, res.body, name); !strings.Contains(r, `class="status status-due">Due<`) {
			t.Errorf("%q is not Due on an empty Record:\n%s", name, r)
		}
	}
}
