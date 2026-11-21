package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/claude"
	"github.com/samthorold/go-slow/internal/config"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// askingBatcher is a batch interface whose batches end at once, each request
// answered by a turn that asks the Record for words, 1200 tokens in and 340
// out.
type askingBatcher struct {
	submitted *[][]editor.BatchRequest
}

func (b askingBatcher) Submit(_ context.Context, rs []editor.BatchRequest) (string, error) {
	*b.submitted = append(*b.submitted, rs)
	return "batch-asking", nil
}

func (b askingBatcher) Poll(context.Context, string) (editor.BatchPoll, error) {
	poll := editor.BatchPoll{Ended: true}
	for _, r := range (*b.submitted)[len(*b.submitted)-1] {
		poll.Results = append(poll.Results, editor.BatchResult{ID: r.ID, Response: editor.Response{
			Model: "claude-opus-5-5", StopReason: "tool_use", Raw: json.RawMessage(`{}`),
			Calls: []editor.ToolCall{{ID: "call-1", Name: "find", Input: json.RawMessage(`{"words": "Scotland"}`)}},
			Usage: editor.Usage{InputTokens: 1200, OutputTokens: 340},
		}})
	}
	return poll, nil
}

// overCapRecord is cutRecord with saturday's weaving run, capped at $0.02,
// given up over its cap on its first turn, which asked: it spent $0.0058 at
// claude-opus-5-5's batch rates, and its next turn is its first again.
func overCapRecord(t *testing.T, db string) {
	t.Helper()
	cutRecord(t, db)
	ctx := t.Context()
	store, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.KeepWeavingCap(ctx, "2026-10-24", 0.02); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	modelFor := func(string) string { return "claude-opus-5-5" }
	ed := editor.New(editor.Deps{Store: store, Weavings: store, ModelFor: modelFor, PriceOf: claude.PriceOf, Log: log})
	var submitted [][]editor.BatchRequest
	b := editor.NewBatches(editor.BatchDeps{Store: store, Batcher: askingBatcher{submitted: &submitted},
		Acts: []editor.BatchAct{ed.Weaving(6 * time.Hour)}, ModelFor: modelFor, Log: log})
	for _, at := range []time.Time{saturday, saturday.Add(time.Hour)} {
		if err := b.RunWaiting(ctx, at); err != nil {
			t.Fatal(err)
		}
	}
	failed, err := store.FailedWork(ctx)
	if err != nil || len(failed) != 1 || !strings.Contains(failed[0].Why, "over its cap") {
		t.Fatalf("failed work %+v (%v), want the run given up over its cap", failed, err)
	}
}

// retrying runs the retry command with args over the Record at db, at now.
func retrying(t *testing.T, db string, now time.Time, args ...string) (weaveRun, error) {
	t.Helper()
	var out bytes.Buffer
	var r weaveRun
	err := admin(t.Context(), slog.New(slog.DiscardHandler), append([]string{"retry"}, args...), adminDeps{
		Lookup: env(db), Stdout: &out,
		Batchers: func(_ config.Secret, act, model string) editor.Batcher {
			r.act, r.model = act, model
			return countingBatcher{submitted: &r.submitted}
		},
		Now: func() time.Time { return now },
	})
	r.out = out.String()
	return r, err
}

// stillGivenUp reports whether saturday's weaving run is still marked failed
// in the Record at db, and how many batches are pending.
func stillGivenUp(t *testing.T, db string) (bool, int) {
	t.Helper()
	failed, _ := inRecord(t, db)
	_, pending := recordState(t, db)
	return len(failed) == 1 && failed[0].Act == "weaving", len(pending)
}

func TestARetryWithACapPricesTheRunGivenUpAndWritesNothing(t *testing.T) {
	db := filepath.Join(t.TempDir(), "go-slow.db")
	overCapRecord(t, db)

	r, err := retrying(t, db, saturday.Add(2*time.Hour), "weaving", "--max-usd", "1")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"2026-10-24", "spent $0.0058", "cap of $0.02", "batch rates", "dry run", "--confirm"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("the dry run printed no %q:\n%s", want, r.out)
		}
	}
	if !regexp.MustCompile(`estimate: \$\d+\.\d{4}`).MatchString(r.out) {
		t.Errorf("the dry run printed no estimate of the next turn:\n%s", r.out)
	}
	if failed, pending := stillGivenUp(t, db); !failed || pending != 0 || len(r.submitted) != 0 {
		t.Errorf("after the dry run given up %v, %d pending, %d submitted; want it as it was", failed, pending, len(r.submitted))
	}
}

func TestARetryWithAHigherCapResumesTheRunGivenUp(t *testing.T) {
	db := filepath.Join(t.TempDir(), "go-slow.db")
	overCapRecord(t, db)

	r, err := retrying(t, db, saturday.Add(2*time.Hour), "weaving", "--max-usd", "1", "--confirm")
	if err != nil {
		t.Fatal(err)
	}

	if len(r.submitted) != 1 || len(r.submitted[0]) != 1 || r.act != "weaving" || r.model != "claude-opus-5-5" {
		t.Fatalf("submitted %v for %s on %s, want the run's next turn on WEAVING_MODEL", r.submitted, r.act, r.model)
	}
	failed, pending := stillGivenUp(t, db)
	if failed || pending != 1 {
		t.Errorf("given up %v with %d batches pending, want it resumed, its next turn pending", failed, pending)
	}
	_, batches := recordState(t, db)
	if batches[0].Requests[0].Key != "2026-10-24" || !strings.Contains(r.out, "batch-weave") {
		t.Errorf("pending %+v, printed:\n%s\nwant the run's own turn, its batch printed", batches, r.out)
	}
	store, err := sqlite.Open(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if usd, capped, err := store.WeavingCap(t.Context(), "2026-10-24"); err != nil || !capped || usd != 1 {
		t.Errorf("the run's cap is $%v (%v, %v), want the new $1 kept", usd, capped, err)
	}
}

func TestARetryWithACapIsRefusedWritingNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		why  string
	}{
		"a cap the run is already over": {[]string{"weaving", "--max-usd", "0.03", "--confirm"},
			"over the most the run may spend"},
		"a confirm with no cap":  {[]string{"weaving", "--confirm"}, "--max-usd"},
		"a cap on another act":   {[]string{"hydration", "--max-usd", "1"}, "weaving"},
		"a cap on every act":     {[]string{"--max-usd", "1"}, "weaving"},
		"a cap of nothing":       {[]string{"weaving", "--max-usd", "0", "--confirm"}, "more than nothing"},
		"a malformed cap":        {[]string{"weaving", "--max-usd", "a dollar"}, "max-usd"},
		"no run given up at all": {nil, "no weaving run is marked failed"},
	} {
		t.Run(name, func(t *testing.T) {
			db := filepath.Join(t.TempDir(), "go-slow.db")
			args := tc.args
			if args == nil {
				cutRecord(t, db)
				args = []string{"weaving", "--max-usd", "1", "--confirm"}
			} else {
				overCapRecord(t, db)
			}
			before, _ := stillGivenUp(t, db)

			r, err := retrying(t, db, saturday.Add(2*time.Hour), args...)

			if err == nil || !strings.Contains(err.Error(), tc.why) {
				t.Errorf("err = %v, want it refused saying %q", err, tc.why)
			}
			if failed, pending := stillGivenUp(t, db); failed != before || pending != 0 || len(r.submitted) != 0 {
				t.Errorf("given up %v, %d pending, %d submitted; want it as it was", failed, pending, len(r.submitted))
			}
		})
	}
}

func TestARetryWithACapIsRefusedWhileAnotherRunIsUnderWay(t *testing.T) {
	db := filepath.Join(t.TempDir(), "go-slow.db")
	overCapRecord(t, db)
	if _, err := weaving(t, db, saturday.AddDate(0, 0, 1), "--confirm", "--max-usd", "1"); err != nil {
		t.Fatal(err)
	}

	r, err := retrying(t, db, saturday.AddDate(0, 0, 1), "weaving", "--max-usd", "1", "--confirm")

	if err == nil || !strings.Contains(err.Error(), "under way") {
		t.Errorf("err = %v, want it refused while 2026-10-25's run is under way", err)
	}
	if failed, pending := stillGivenUp(t, db); !failed || pending != 1 || len(r.submitted) != 0 {
		t.Errorf("given up %v, %d pending, %d submitted; want it as it was", failed, pending, len(r.submitted))
	}
}

// Without a new cap, a run given up over its cap stays given up, said so
// with its price, while the rest of the failed work is taken up again.
func TestARetryWithoutACapLeavesARunOverItsCapGivenUp(t *testing.T) {
	for _, args := range [][]string{{"weaving"}, nil} {
		t.Run(strings.Join(append([]string{"retry"}, args...), " "), func(t *testing.T) {
			db := filepath.Join(t.TempDir(), "go-slow.db")
			overCapRecord(t, db)

			r, err := retrying(t, db, saturday.Add(2*time.Hour), args...)
			if err != nil {
				t.Fatal(err)
			}

			if failed, pending := stillGivenUp(t, db); !failed || pending != 0 || len(r.submitted) != 0 {
				t.Errorf("given up %v, %d pending, %d submitted; want it left given up", failed, pending, len(r.submitted))
			}
			for _, want := range []string{"2026-10-24 stays given up", "spent $0.0058", "estimate: $", "--max-usd"} {
				if !strings.Contains(r.out, want) {
					t.Errorf("printed no %q:\n%s", want, r.out)
				}
			}
		})
	}
}
