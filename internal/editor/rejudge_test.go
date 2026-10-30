package editor_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// october is a range holding the speech, published on 16 October.
var october = editor.Range{
	From: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
	To:   time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC),
}

// testPrice is a model's price for the tests: a dollar a million input
// tokens and five a million output.
func testPrice(model string) (editor.Price, bool) {
	if model != "claude-test" {
		return editor.Price{}, false
	}
	return editor.Price{InputPerMTok: 1, OutputPerMTok: 5}, true
}

func rejudger(store *sqlite.Store, model editor.Model) *editor.Rejudge {
	return editor.NewRejudge(editor.RejudgeDeps{
		Store: store, Model: model, ModelName: "claude-test", PriceOf: testPrice,
		Now: func() time.Time { return saturday.Add(time.Hour) }, Log: slog.New(slog.DiscardHandler),
	})
}

// costing is a model answering as m does, reporting usage and model as its
// own.
type costing struct {
	m     editor.Model
	usage editor.Usage
	model string
}

func (c costing) Complete(ctx context.Context, req editor.Request) (editor.Response, error) {
	res, err := c.m.Complete(ctx, req)
	res.Usage, res.Model = c.usage, c.model
	return res, err
}

func TestADryRunEstimatesTokensFromTheActsPastCallsInTheRecord(t *testing.T) {
	store := record(t)
	past := costing{m: &fakeModel{output: goodReading}, usage: editor.Usage{InputTokens: 3000, OutputTokens: 1500}}
	ed, _ := newEditor(store, past, &fakeWikidata{})
	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}

	est, err := rejudger(store, &fakeModel{}).Price(t.Context(), editor.Rereading(store), october)
	if err != nil {
		t.Fatal(err)
	}

	// The speech is asked again as it was: the same request, so the same
	// tokens in, and the past call's tokens out.
	if est.FromPast != 1 || est.InputTokens != 3000 || est.OutputTokens != 1500 {
		t.Errorf("estimate = %+v, want 3000 in and 1500 out, from the one past call", est)
	}
}

func TestADryRunPricesTheRequestsItWouldSendAndCallsNoModel(t *testing.T) {
	store := record(t)
	model := &fakeModel{output: goodReading}

	est, err := rejudger(store, model).Price(t.Context(), editor.Rereading(store), october)
	if err != nil {
		t.Fatal(err)
	}

	if len(model.sent) != 0 {
		t.Errorf("the dry run called the model %d times", len(model.sent))
	}
	if est.Act != "reading" || est.Model != "claude-test" || est.Requests != 1 {
		t.Errorf("estimate = %+v, want one reading request on claude-test: only the speech is cut", est)
	}
	if est.InputTokens <= 0 || est.OutputTokens <= 0 {
		t.Errorf("estimate = %+v, want tokens in and out", est)
	}
	want := float64(est.InputTokens)/1e6*1 + float64(est.OutputTokens)/1e6*5
	if est.USD != want || est.Rates != "real-time" {
		t.Errorf("estimate %v USD at %s rates, want %v at real-time rates", est.USD, est.Rates, want)
	}
}

// readLive reads the speech as the live judge on a model reporting itself
// as model.
func readLive(t *testing.T, store *sqlite.Store, model string) {
	t.Helper()
	ed, _ := newEditor(store, costing{m: &fakeModel{output: goodReading}, model: model}, &fakeWikidata{})
	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
}

// viewedModel is the model of the reading of the speech a view reads.
func viewedModel(t *testing.T, store *sqlite.Store) string {
	t.Helper()
	r, found, err := store.NewestReading(t.Context(), speech.SourceID, speech.EntryID)
	if err != nil || !found {
		t.Fatalf("NewestReading: %v %v", found, err)
	}
	return r.Judge.Model
}

func TestARunAsTheLiveJudgeIsWhatViewsReadFromThenOn(t *testing.T) {
	store := record(t)
	readLive(t, store, "claude-old")
	model := costing{m: &fakeModel{output: goodReading}, model: "claude-test",
		usage: editor.Usage{InputTokens: 1000, OutputTokens: 200}}

	out, err := rejudger(store, model).Run(t.Context(), editor.Rereading(store), october, editor.RunOptions{MaxUSD: 1})
	if err != nil {
		t.Fatal(err)
	}

	if out.Judged != 1 || out.Candidate != 0 || len(out.Failed) != 0 {
		t.Errorf("outcome = %+v, want the speech judged as the live judge", out)
	}
	if want := 1000.0/1e6 + 200*5.0/1e6; out.USD != want {
		t.Errorf("spent %v, want %v by the usage reported", out.USD, want)
	}
	if got := viewedModel(t, store); got != "claude-test" {
		t.Errorf("a view reads %q's reading, want the run's", got)
	}
}

func TestARunAsACandidateIsKeptAndReadByNoView(t *testing.T) {
	store := record(t)
	readLive(t, store, "claude-old")
	model := costing{m: &fakeModel{output: goodReading}, model: "claude-test"}

	out, err := rejudger(store, model).Run(t.Context(), editor.Rereading(store), october,
		editor.RunOptions{Candidate: true, MaxUSD: 1})
	if err != nil {
		t.Fatal(err)
	}

	if out.Judged != 1 || out.Candidate == 0 {
		t.Errorf("outcome = %+v, want the speech judged as a candidate", out)
	}
	if got := viewedModel(t, store); got != "claude-old" {
		t.Errorf("a view reads %q's reading, want still the live judge's", got)
	}
	cands, err := store.Candidates(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].ID != out.Candidate || cands[0].Model != "claude-test" ||
		cands[0].Version != editor.ReadingVersion || cands[0].Judgements != 1 || !cands[0].Range.From.Equal(october.From) {
		t.Errorf("candidates = %+v, want the run's, with its one reading", cands)
	}
}

func TestARunWhoseEstimateIsOverItsCapCallsNoModel(t *testing.T) {
	store := record(t)
	model := &fakeModel{output: goodReading}

	_, err := rejudger(store, model).Run(t.Context(), editor.Rereading(store), october, editor.RunOptions{MaxUSD: 0.000001})

	if !errors.Is(err, editor.ErrOverCap) {
		t.Errorf("err = %v, want over its cap", err)
	}
	if len(model.sent) != 0 {
		t.Errorf("the model was called %d times", len(model.sent))
	}
}

func TestARunStopsBeforeACallThatCouldTakeItOverItsCap(t *testing.T) {
	store := record(t)
	if _, err := store.AddItems(t.Context(), []collect.Item{{
		SourceID: release.SourceID, EntryID: release.EntryID, Title: release.Title, Content: release.Content,
		Published: release.Published, Attribution: collect.OGL, CollectedAt: saturday,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(store).CutNew(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	// Each call reports a dollar's worth of input: more than the estimate.
	inner := &fakeModel{output: goodReading}
	model := costing{m: inner, model: "claude-test", usage: editor.Usage{InputTokens: 1_000_000}}

	out, err := rejudger(store, model).Run(t.Context(), editor.Rereading(store), october, editor.RunOptions{MaxUSD: 0.5})
	if err != nil {
		t.Fatal(err)
	}

	if len(inner.sent) != 1 || !out.Stopped || out.USD != 1 {
		t.Errorf("%d calls, stopped %v, spent $%v: want the run stopped after its first call took it over $0.50", len(inner.sent), out.Stopped, out.USD)
	}
}

func TestAPieceThatFailsItsChecksIsTriedAsInUseThenLeft(t *testing.T) {
	store := record(t)
	model := &fakeModel{output: `{"facts": []}`}

	out, err := rejudger(store, model).Run(t.Context(), editor.Rereading(store), october, editor.RunOptions{MaxUSD: 1})
	if err != nil {
		t.Fatal(err)
	}

	if len(model.sent) != editor.MaxAttempts || out.Judged != 0 || len(out.Failed) != 1 {
		t.Errorf("%d calls, outcome %+v: want %d attempts, then the speech left failed", len(model.sent), out, editor.MaxAttempts)
	}
	calls, err := store.CallsOn(t.Context(), "reading", speech.SourceID+"\n"+speech.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != editor.MaxAttempts {
		t.Errorf("%d calls kept, want every attempt", len(calls))
	}
}
