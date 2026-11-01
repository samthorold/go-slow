package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/config"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// speechReading is a reading of the speech, cut into its one Span, S1 "It
// is good to be in Scotland."
const speechReading = `{"names": [{"at": {"span": 1, "words": "Scotland"}, "kind": "place", "name": "Scotland", "office": ""}],
	"refers_to": [], "rejected": [], "speakers": []}`

// reader is a model answering every request with a reading of the speech,
// reporting itself as the model it was asked for, and counting its calls.
type reader struct {
	model string
	calls *int
}

func (r reader) Complete(context.Context, editor.Request) (editor.Response, error) {
	*r.calls++
	return editor.Response{Output: json.RawMessage(speechReading), Model: r.model,
		Usage: editor.Usage{InputTokens: 400, OutputTokens: 100}}, nil
}

// readers is a model adapter for the admin commands, each model it is
// asked for a reader, its calls counted in calls.
func readers(calls *int) func(config.Secret, string) editor.Model {
	return func(_ config.Secret, model string) editor.Model { return reader{model: model, calls: calls} }
}

// rejudging runs the admin command args over the Record at db, its models
// counting their calls in calls, and returns what it printed.
func rejudging(t *testing.T, db string, calls *int, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := admin(t.Context(), slog.New(slog.DiscardHandler), args, adminDeps{
		Lookup: env(db), Stdout: &out, Models: readers(calls),
		Now: func() time.Time { return saturday.Add(2 * time.Hour) },
	})
	return out.String(), err
}

// viewed is the model of the reading of the speech a view reads, or "" for
// none.
func viewed(t *testing.T, db string) string {
	t.Helper()
	store, err := sqlite.Open(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	r, found, err := store.NewestReading(t.Context(), "number-10", "speech")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		return ""
	}
	return r.Judge.Model
}

// The speech is published on 24 October.
var range24 = []string{"--from", "2026-10-24", "--to", "2026-10-24"}

func TestARejudgingDryRunPricesItsRequestsAndCallsNoModel(t *testing.T) {
	db := filepath.Join(t.TempDir(), "go-slow.db")
	failedRecord(t, db)
	var calls int

	out, err := rejudging(t, db, &calls, append([]string{"rejudge", "reading"}, range24...)...)
	if err != nil {
		t.Fatal(err)
	}

	if calls != 0 {
		t.Errorf("the dry run called a model %d times", calls)
	}
	for _, want := range []string{"requests: 1", "claude-haiku-5-5", "input tokens:", "output tokens:", "real-time rates",
		"prices as of 2026-10-06", "dry run", "--confirm"} {
		if !strings.Contains(out, want) {
			t.Errorf("the dry run printed no %q:\n%s", want, out)
		}
	}
	if !regexp.MustCompile(`\$\d+\.\d{4}`).MatchString(out) {
		t.Errorf("the dry run printed no estimate in dollars:\n%s", out)
	}
	if got := viewed(t, db); got != "" {
		t.Errorf("the dry run kept a reading by %s", got)
	}
}

// A Record whose Items were never cut (an app built before Cuts) has
// nothing to read again until they are: the dry run cuts those in its
// range, as Collection would, and prices them.
func TestARejudgingDryRunCutsTheItemsInItsRangeNotYetCutAndPricesThem(t *testing.T) {
	ctx := t.Context()
	db := filepath.Join(t.TempDir(), "go-slow.db")
	store, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	src := collect.Source{ID: "number-10", Name: "Number 10", FeedURL: "https://example.org/feed",
		Attribution: collect.OGL, UsualGap: time.Hour}
	if err := store.SyncSources(ctx, []collect.Source{src}, saturday); err != nil {
		t.Fatal(err)
	}
	item := func(entry, content string, published time.Time) collect.Item {
		return collect.Item{SourceID: src.ID, EntryID: entry, Title: entry, Link: "https://example.org/" + entry,
			Content: content, Published: published, Attribution: collect.OGL, CollectedAt: published}
	}
	if _, err := store.AddItems(ctx, []collect.Item{
		item("speech", "<p>It is good to be in Scotland.</p>", saturday),
		item("untold", "", saturday),
		item("later", "<p>It is good to be in Wales.</p>", saturday.AddDate(0, 0, 2)),
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	var calls int

	out, err := rejudging(t, db, &calls, append([]string{"rejudge", "reading"}, range24...)...)
	if err != nil {
		t.Fatal(err)
	}

	if calls != 0 {
		t.Errorf("the dry run called a model %d times", calls)
	}
	for _, want := range []string{"cut: 1", "requests: 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("the dry run printed no %q:\n%s", want, out)
		}
	}
	store, err = sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	for entry, want := range map[string]bool{"speech": true, "untold": false, "later": false} {
		if _, _, found, err := store.NewestCut(ctx, src.ID, entry); err != nil || found != want {
			t.Errorf("%s cut: %v (%v), want %v", entry, found, err, want)
		}
	}
	if got := viewed(t, db); got != "" {
		t.Errorf("the dry run kept a reading by %s", got)
	}
}

func TestARejudgingRunNeedsACap(t *testing.T) {
	db := filepath.Join(t.TempDir(), "go-slow.db")
	failedRecord(t, db)
	var calls int

	_, err := rejudging(t, db, &calls, append([]string{"rejudge", "reading", "--confirm"}, range24...)...)

	if err == nil || !strings.Contains(err.Error(), "--max-usd") {
		t.Errorf("err = %v, want a run with no cap refused", err)
	}
	if calls != 0 {
		t.Errorf("a model was called %d times", calls)
	}
}

func TestARejudgingRunAsTheLiveJudgeIsWhatViewsRead(t *testing.T) {
	db := filepath.Join(t.TempDir(), "go-slow.db")
	failedRecord(t, db)
	var calls int

	out, err := rejudging(t, db, &calls,
		append([]string{"rejudge", "reading", "--confirm", "--max-usd", "1"}, range24...)...)
	if err != nil {
		t.Fatal(err)
	}

	if calls != 1 {
		t.Errorf("the run called a model %d times, want once for the speech", calls)
	}
	if !strings.Contains(out, "judged: 1") {
		t.Errorf("the run printed no outcome:\n%s", out)
	}
	if got := viewed(t, db); got != "claude-haiku-5-5" {
		t.Errorf("a view reads %q's reading, want the run's", got)
	}
}

func TestACandidateIsComparedWithTheLiveJudgeAndChosenToBeLive(t *testing.T) {
	db := filepath.Join(t.TempDir(), "go-slow.db")
	failedRecord(t, db)
	var calls int
	if _, err := rejudging(t, db, &calls,
		append([]string{"rejudge", "reading", "--confirm", "--max-usd", "1"}, range24...)...); err != nil {
		t.Fatal(err)
	}

	out, err := rejudging(t, db, &calls,
		append([]string{"rejudge", "reading", "--candidate", "--model", "claude-opus-5-5", "--confirm", "--max-usd", "1"}, range24...)...)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`candidate (\d+)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("the run named no candidate:\n%s", out)
	}
	if got := viewed(t, db); got != "claude-haiku-5-5" {
		t.Errorf("a view reads %q's reading, want still the live judge's", got)
	}

	listed, err := rejudging(t, db, &calls, "rejudge", "candidates")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed, "candidate "+m[1]) || !strings.Contains(listed, "claude-opus-5-5") {
		t.Errorf("the candidates listed are:\n%s", listed)
	}
	compared, err := rejudging(t, db, &calls, "rejudge", "compare", m[1])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"number-10 speech", "live: reading v" + strconv.Itoa(editor.ReadingVersion) + " on claude-haiku-5-5", "names", "live 1", "candidate 1", "both 1"} {
		if !strings.Contains(compared, want) {
			t.Errorf("the comparison printed no %q:\n%s", want, compared)
		}
	}

	// The live judge is chosen by its act's configured model, so a candidate
	// on another model is refused until READING_MODEL names it.
	if _, err := rejudging(t, db, &calls, "rejudge", "choose", m[1]); err == nil ||
		!strings.Contains(err.Error(), "READING_MODEL") {
		t.Errorf("err = %v, want the choice refused while READING_MODEL names another model", err)
	}
	if got := viewed(t, db); got != "claude-haiku-5-5" {
		t.Errorf("a view reads %q's reading after a refused choice", got)
	}
	if _, err := choosingOn(t, db, "claude-opus-5-5", m[1]); err != nil {
		t.Fatal(err)
	}
	if got := viewed(t, db); got != "claude-opus-5-5" {
		t.Errorf("a view reads %q's reading, want the chosen candidate's", got)
	}
}

// choosingOn runs rejudge choose with READING_MODEL set to model.
func choosingOn(t *testing.T, db, model, id string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	lookup := func(k string) (string, bool) {
		if k == "READING_MODEL" {
			return model, true
		}
		return env(db)(k)
	}
	var calls int
	err := admin(t.Context(), slog.New(slog.DiscardHandler), []string{"rejudge", "choose", id}, adminDeps{
		Lookup: lookup, Stdout: &out, Models: readers(&calls),
		Now: func() time.Time { return saturday.Add(3 * time.Hour) },
	})
	return out.String(), err
}
