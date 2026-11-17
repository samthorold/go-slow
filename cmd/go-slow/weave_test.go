package main

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/config"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// countingBatcher is a batch interface keeping what is submitted to it,
// whose batches never end.
type countingBatcher struct {
	submitted *[][]editor.BatchRequest
}

func (b countingBatcher) Submit(_ context.Context, rs []editor.BatchRequest) (string, error) {
	*b.submitted = append(*b.submitted, rs)
	return "batch-weave", nil
}

func (countingBatcher) Poll(context.Context, string) (editor.BatchPoll, error) {
	return editor.BatchPoll{}, nil
}

// weaveRun is what a weave command did: what it printed and what it
// submitted, on the act and model its batch interface was built for.
type weaveRun struct {
	out        string
	submitted  [][]editor.BatchRequest
	act, model string
}

// weaving runs the weave command with args over the Record at db, at now.
func weaving(t *testing.T, db string, now time.Time, args ...string) (weaveRun, error) {
	t.Helper()
	var out bytes.Buffer
	var r weaveRun
	var calls int
	err := admin(t.Context(), slog.New(slog.DiscardHandler), append([]string{"weave"}, args...), adminDeps{
		Lookup: env(db), Stdout: &out, Models: readers(&calls),
		Batchers: func(_ config.Secret, act, model string) editor.Batcher {
			r.act, r.model = act, model
			return countingBatcher{submitted: &r.submitted}
		},
		Now: func() time.Time { return now },
	})
	if calls != 0 {
		t.Errorf("the weave command called a model in real time %d times", calls)
	}
	r.out = out.String()
	return r, err
}

// collectedOn are the days, in October 2026, the Items of switchedRecord
// were collected and published.
var collectedOn = []int{20, 22, 24}

// october is a day in October 2026, at noon UTC, so the day holds it in
// whichever time zone the tests run in, within twelve hours of UTC.
func october(day int) time.Time { return time.Date(2026, time.October, day, 12, 0, 0, 0, time.UTC) }

// switchedRecord is a Record at db holding three Items, collected on the
// days of collectedOn, each cut, and switched to Stories through the last
// Cut, as a Record not yet woven is by its first start after the switch.
func switchedRecord(t *testing.T, db string) {
	t.Helper()
	ctx := t.Context()
	store, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	src := collect.Source{ID: "number-10", Name: "Number 10", FeedURL: "https://example.org/feed",
		Attribution: collect.OGL, UsualGap: time.Hour}
	if err := store.SyncSources(ctx, []collect.Source{src}, october(20)); err != nil {
		t.Fatal(err)
	}
	for _, day := range collectedOn {
		at := october(day)
		entry := at.Format(time.DateOnly)
		if _, err := store.AddItems(ctx, []collect.Item{{SourceID: src.ID, EntryID: entry, Title: "Speech of " + entry,
			Link: "https://example.org/" + entry, Content: "<p>It is good to be in Scotland.</p>", Published: at,
			Attribution: collect.OGL, CollectedAt: at}}); err != nil {
			t.Fatal(err)
		}
		if err := cut.New(store).CutNew(ctx, at); err != nil {
			t.Fatal(err)
		}
	}
	waiting, err := store.StoriesToWeave(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.KeepStorySwitch(ctx, waiting[len(waiting)-1].CutID, october(24).Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
}

// cutRecord is a Record at db holding the speech, published and collected
// on saturday and cut, awaiting weaving.
func cutRecord(t *testing.T, db string) {
	t.Helper()
	ctx := t.Context()
	store, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	src := collect.Source{ID: "number-10", Name: "Number 10", FeedURL: "https://example.org/feed",
		Attribution: collect.OGL, UsualGap: time.Hour}
	if err := store.SyncSources(ctx, []collect.Source{src}, saturday); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddItems(ctx, []collect.Item{{SourceID: src.ID, EntryID: "speech", Title: "PM speech in Scotland",
		Link: "https://example.org/speech", Content: "<p>It is good to be in Scotland.</p>", Published: saturday,
		Attribution: collect.OGL, CollectedAt: saturday}}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(store).CutNew(ctx, saturday); err != nil {
		t.Fatal(err)
	}
}

// recordState is what of the Record at db the weave command may write: the
// Items awaiting weaving and the pending batches.
func recordState(t *testing.T, db string) (awaiting []string, pending []editor.PendingBatch) {
	t.Helper()
	store, err := sqlite.Open(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	waiting, err := store.StoriesToWeave(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range waiting {
		awaiting = append(awaiting, c.Item.EntryID)
	}
	if pending, err = store.PendingBatches(t.Context()); err != nil {
		t.Fatal(err)
	}
	return awaiting, pending
}

// early is saturday before WEAVING_TIME, 06:00.
var early = saturday.Add(-2 * time.Hour)

func TestAWeaveDryRunPrintsWhatTheRunIsShownAndItsPriceAndWritesNothing(t *testing.T) {
	db := filepath.Join(t.TempDir(), "go-slow.db")
	cutRecord(t, db)

	r, err := weaving(t, db, early)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"2026-10-24", "claude-opus-5-5", "0 Stories made", "1 Stories cut",
		"PM speech in Scotland", "batch rates", "prices as of 2026-10-06", "dry run", "--confirm"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("the dry run printed no %q:\n%s", want, r.out)
		}
	}
	if !regexp.MustCompile(`\$\d+\.\d{4}`).MatchString(r.out) {
		t.Errorf("the dry run printed no estimate in dollars:\n%s", r.out)
	}
	if len(r.submitted) != 0 {
		t.Errorf("the dry run submitted %d batches", len(r.submitted))
	}
	if awaiting, pending := recordState(t, db); !slices.Equal(awaiting, []string{"speech"}) || len(pending) != 0 {
		t.Errorf("after the dry run %v await weaving and %d batches are pending", awaiting, len(pending))
	}
}

func TestAWeaveRunIsSubmittedAsAPendingBatchKeyedByTodayBeforeItsTime(t *testing.T) {
	db := filepath.Join(t.TempDir(), "go-slow.db")
	cutRecord(t, db)

	r, err := weaving(t, db, early, "--confirm", "--max-usd", "1")
	if err != nil {
		t.Fatal(err)
	}

	if len(r.submitted) != 1 || len(r.submitted[0]) != 1 || r.submitted[0][0].Request.Act != "weaving" {
		t.Fatalf("submitted %v, want one weaving request", r.submitted)
	}
	if r.act != "weaving" || r.model != "claude-opus-5-5" {
		t.Errorf("submitted to the batch interface for %s on %s, want weaving on WEAVING_MODEL", r.act, r.model)
	}
	_, pending := recordState(t, db)
	if len(pending) != 1 || pending[0].ID != "batch-weave" || len(pending[0].Requests) != 1 ||
		pending[0].Requests[0].Key != "2026-10-24" || pending[0].Requests[0].Model != "claude-opus-5-5" {
		t.Errorf("pending %+v, want today's run in the batch submitted", pending)
	}
	if !strings.Contains(r.out, "batch-weave") {
		t.Errorf("the run printed no batch:\n%s", r.out)
	}
}

func TestWeaveSinceMovesTheSwitchBackToThatDay(t *testing.T) {
	db := filepath.Join(t.TempDir(), "go-slow.db")
	switchedRecord(t, db)
	if awaiting, _ := recordState(t, db); len(awaiting) != 0 {
		t.Fatalf("awaiting weaving %v before the switch is moved", awaiting)
	}

	dry, err := weaving(t, db, october(25), "--since", "2026-10-22")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dry.out, "2 Stories cut") || !strings.Contains(dry.out, "Speech of 2026-10-22") ||
		strings.Contains(dry.out, "Speech of 2026-10-20") {
		t.Errorf("the dry run printed:\n%s\nwant the Stories cut of the Items collected on and after the day", dry.out)
	}
	if awaiting, _ := recordState(t, db); len(awaiting) != 0 {
		t.Errorf("after the dry run %v await weaving, want the switch where it was", awaiting)
	}

	r, err := weaving(t, db, october(25), "--since", "2026-10-22", "--confirm", "--max-usd", "1")
	if err != nil {
		t.Fatal(err)
	}

	awaiting, pending := recordState(t, db)
	if !slices.Equal(awaiting, []string{"2026-10-22", "2026-10-24"}) {
		t.Errorf("awaiting weaving %v, want the Items collected on and after the day", awaiting)
	}
	if len(r.submitted) != 1 || len(pending) != 1 || pending[0].Requests[0].Key != "2026-10-25" {
		t.Errorf("submitted %d batches, pending %+v, want today's run", len(r.submitted), pending)
	}
	if in := r.submitted[0][0].Request.Input; !strings.Contains(in, "Speech of 2026-10-22") ||
		strings.Contains(in, "Speech of 2026-10-20") {
		t.Errorf("the run is shown:\n%s", in)
	}
}

func TestWeaveRefusesWritingNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		record func(t *testing.T, db string)
		now    time.Time
		args   []string
		why    string
	}{
		"a run with no cap": {cutRecord, early, []string{"--confirm"}, "--max-usd"},
		"an estimate over the cap": {cutRecord, early, []string{"--confirm", "--max-usd", "0.0001"},
			"over the most the run may spend"},
		"today's run failed": {failedRecord, saturday, []string{"--confirm", "--max-usd", "1"}, "already"},
		"nothing to weave":   {switchedRecord, october(25), []string{"--confirm", "--max-usd", "1"}, "nothing"},
		"a day before every Item": {switchedRecord, october(25),
			[]string{"--since", "2026-10-01", "--confirm", "--max-usd", "1"}, "no Item collected before"},
		"a malformed day": {switchedRecord, october(25), []string{"--since", "22 October"}, "--since"},
		"a woven Record": {wovenRecord, october(26), []string{"--since", "2026-10-22", "--confirm", "--max-usd", "1"},
			"woven"},
		"a run pending": {pendingRecord, october(26), []string{"--confirm", "--max-usd", "1"}, "pending"},
	} {
		t.Run(name, func(t *testing.T) {
			db := filepath.Join(t.TempDir(), "go-slow.db")
			tc.record(t, db)
			awaiting, pending := recordState(t, db)

			r, err := weaving(t, db, tc.now, tc.args...)

			if err == nil || !strings.Contains(err.Error(), tc.why) {
				t.Errorf("err = %v, want it refused saying %q", err, tc.why)
			}
			if len(r.submitted) != 0 {
				t.Errorf("submitted %d batches", len(r.submitted))
			}
			if a, p := recordState(t, db); !slices.Equal(a, awaiting) || len(p) != len(pending) {
				t.Errorf("awaiting %v and %d pending, were %v and %d", a, len(p), awaiting, len(pending))
			}
		})
	}
}

// wovenRecord is switchedRecord with a weaving run written on 25 October,
// through its last Cut, and the Stories cut since awaiting weaving.
func wovenRecord(t *testing.T, db string) {
	t.Helper()
	switchedRecord(t, db)
	ctx := t.Context()
	store, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	through, _, err := store.CutBefore(ctx, october(25))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.KeepWeaving(ctx, editor.Weaving{Key: "2026-10-25", ThroughCut: through, At: october(25),
		Judge: editor.Judge{Act: "weaving", Version: editor.WeavingVersion, Model: "m", RequestHash: "h"}}); err != nil {
		t.Fatal(err)
	}
}

// pendingRecord is switchedRecord with the switch moved back to 22 October
// and that run's batch pending.
func pendingRecord(t *testing.T, db string) {
	t.Helper()
	switchedRecord(t, db)
	if _, err := weaving(t, db, october(25), "--since", "2026-10-22", "--confirm", "--max-usd", "1"); err != nil {
		t.Fatal(err)
	}
}
