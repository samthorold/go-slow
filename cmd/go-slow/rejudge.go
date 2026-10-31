package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/samthorold/go-slow/internal/claude"
	"github.com/samthorold/go-slow/internal/config"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// rejudgeAct is an act the rejudge command can run: how it judges a range
// again, and the model its live judge runs on, as configured.
type rejudgeAct struct {
	act   func(store *sqlite.Store) editor.RejudgeAct
	model func(cfg config.Config) string
	// variable names the configuration variable of its model.
	variable string
	// cutsFirst is whether the act reads Cuts, so the Items in the range
	// not yet cut are cut before it is priced.
	cutsFirst bool
}

// rejudgeActs are the acts that judge Spans by a model call and keep each
// judgement with its judge. Only reading does today; relating and the
// Briefing are added here as they come, each with its RejudgeAct.
var rejudgeActs = map[string]rejudgeAct{
	"reading": {
		act:       func(store *sqlite.Store) editor.RejudgeAct { return editor.Rereading(store) },
		model:     func(cfg config.Config) string { return cfg.ReadingModel },
		variable:  "READING_MODEL",
		cutsFirst: true,
	},
}

// dateLayout is how a range's days are given.
const dateLayout = time.DateOnly

// rejudge judges a range of the Record again with a chosen judge (system
// design: Every judgement carries its judge, and a changed judge judges
// forward):
//
//	rejudge <act> --from YYYY-MM-DD --to YYYY-MM-DD [--candidate [--model M]] [--confirm --max-usd N]
//
// judges again the Items published on the days from --from to --to, both
// included, in the time zone TZ. Without --confirm it is a dry run: it prints
// the requests it would send, the tokens and their price in dollars, and
// calls no model and writes no judgement. Either way it first cuts the
// Items in the range with text and no Cut, as Collection does, since
// reading reads only Items cut: the Cuts are its only write, plain code
// with no model call (a Record from before Cuts holds none until its first
// Collection run). With --confirm it runs, in real time,
// stopping before a call that could take it over --max-usd. It judges as the
// live judge, on the act's configured model, so views read its judgements as
// the newest; or, with --candidate, as a candidate on --model (by default the
// configured one), kept beside the live judgements and read by no view:
//
//	rejudge candidates
//
// lists the candidates,
//
//	rejudge compare <candidate>
//
// compares a candidate's judgements with the live ones, Item by Item, and
//
//	rejudge choose <candidate>
//
// makes it the live judge from then on, as an event, rewriting none of its
// judgements. A candidate is chosen only once the act's model variable names
// its model, so the act goes on judging as the chosen judge.
func rejudge(ctx context.Context, log *slog.Logger, args []string, d adminDeps) error {
	if len(args) == 0 {
		return errors.New("rejudge: name an act, or candidates, compare or choose")
	}
	cfg, err := config.Load(d.Lookup)
	if err != nil {
		return err
	}
	switch args[0] {
	case "candidates":
		return withStore(ctx, cfg, func(store *sqlite.Store) error { return listCandidates(ctx, store, d.Stdout) })
	case "compare":
		id, err := candidateArg(args)
		if err != nil {
			return err
		}
		return withStore(ctx, cfg, func(store *sqlite.Store) error { return compareCandidate(ctx, store, d.Stdout, id) })
	case "choose":
		id, err := candidateArg(args)
		if err != nil {
			return err
		}
		return withStore(ctx, cfg, func(store *sqlite.Store) error {
			return chooseCandidate(ctx, store, cfg, d, id)
		})
	}
	a, ok := rejudgeActs[args[0]]
	if !ok {
		return fmt.Errorf("rejudge: unknown act %q (known: reading)", args[0])
	}
	flags := flag.NewFlagSet("rejudge "+args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	from := flags.String("from", "", "the first day, YYYY-MM-DD")
	to := flags.String("to", "", "the last day, YYYY-MM-DD, included")
	asCandidate := flags.Bool("candidate", false, "judge as a candidate, read by no view until chosen")
	model := flags.String("model", "", "the candidate's model (default: the act's configured model)")
	confirm := flags.Bool("confirm", false, "run it, rather than only pricing it")
	maxUSD := flags.Float64("max-usd", 0, "the most the run may spend, in dollars")
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("rejudge %s: %w", args[0], err)
	}
	rng, err := daysRange(*from, *to)
	if err != nil {
		return fmt.Errorf("rejudge %s: %w", args[0], err)
	}
	judgeModel := a.model(cfg)
	if *model != "" {
		if !*asCandidate {
			return fmt.Errorf("rejudge %s: --model is for a candidate; the live judge runs on %s (%s)", args[0], judgeModel, a.variable)
		}
		judgeModel = *model
	}
	if *confirm && *maxUSD <= 0 {
		return fmt.Errorf("rejudge %s: a run needs --max-usd, the most it may spend", args[0])
	}
	return withStore(ctx, cfg, func(store *sqlite.Store) error {
		act := a.act(store)
		run := editor.NewRejudge(editor.RejudgeDeps{
			Store: store, Model: d.Models(cfg.AnthropicAPIKey, judgeModel), ModelName: judgeModel,
			PriceOf: claude.PriceOf, Now: d.Now, Log: log,
		})
		as := "the live judge"
		if *asCandidate {
			as = "a candidate"
		}
		p := &printer{w: d.Stdout}
		p.f("rejudge %s as %s: version %d on %s, Items published %s to %s (%s)\n",
			act.Name, as, act.Version, judgeModel, *from, *to, time.Local)
		if a.cutsFirst {
			n, err := cut.New(store).CutPublished(ctx, rng.From, rng.To, d.Now())
			if err != nil {
				return err
			}
			p.f("cut: %d Items in the range not yet cut (no model call)\n", n)
		}
		if !*confirm {
			est, err := run.Price(ctx, act, rng)
			if err != nil {
				return err
			}
			printEstimate(p, est)
			p.f("dry run: no model was called and no judgement written. Run again with --confirm --max-usd N to run it.\n")
			return p.err
		}
		out, err := run.Run(ctx, act, rng, editor.RunOptions{Candidate: *asCandidate, MaxUSD: *maxUSD})
		printEstimate(p, out.Estimate)
		if err != nil {
			return err
		}
		if out.Candidate != 0 {
			p.f("kept as candidate %d: compare it with rejudge compare %d, choose it with rejudge choose %d\n",
				out.Candidate, out.Candidate, out.Candidate)
		}
		p.f("judged: %d\nfailed: %d\nspent: $%.4f (%d input tokens, %d output tokens)\n",
			out.Judged, len(out.Failed), out.USD, out.Usage.InputTokens+out.Usage.CacheReadTokens, out.Usage.OutputTokens)
		for _, k := range out.Failed {
			p.f("  failed: %s\n", strings.ReplaceAll(k, "\n", " "))
		}
		if out.Stopped {
			p.f("stopped at the cap of $%.2f before the end of the range\n", *maxUSD)
		}
		return p.err
	})
}

// printer prints to w, keeping the first error, after which it prints
// nothing.
type printer struct {
	w   io.Writer
	err error
}

func (p *printer) f(format string, args ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, args...)
	}
}

// printEstimate prints what a run would send and cost.
func printEstimate(p *printer, est editor.Estimate) {
	basis := fmt.Sprintf("from %d past calls of the act", est.FromPast)
	if est.FromPast == 0 {
		basis = "no past calls of the act to estimate from: 3 bytes a token in, 4000 tokens out a call"
	}
	p.f("requests: %d\ninput tokens: ~%d\noutput tokens: ~%d (estimated %s)\n",
		est.Requests, est.InputTokens, est.OutputTokens, basis)
	p.f("estimate: $%.4f at %s rates on %s (prices as of %s, from %s)\n",
		est.USD, est.Rates, est.Model, claude.PricesAsOf, claude.PricesSource)
}

// daysRange is the range of Items published on the days from to to, both
// included, in the local time zone.
func daysRange(from, to string) (editor.Range, error) {
	if from == "" || to == "" {
		return editor.Range{}, errors.New("give the range as --from YYYY-MM-DD --to YYYY-MM-DD")
	}
	f, err := time.ParseInLocation(dateLayout, from, time.Local)
	if err != nil {
		return editor.Range{}, fmt.Errorf("--from: %w", err)
	}
	t, err := time.ParseInLocation(dateLayout, to, time.Local)
	if err != nil {
		return editor.Range{}, fmt.Errorf("--to: %w", err)
	}
	if t.Before(f) {
		return editor.Range{}, errors.New("--to is before --from")
	}
	return editor.Range{From: f, To: t.AddDate(0, 0, 1)}, nil
}

// candidateArg is the candidate a compare or choose names.
func candidateArg(args []string) (int64, error) {
	if len(args) != 2 {
		return 0, fmt.Errorf("rejudge %s <candidate>: name one candidate", args[0])
	}
	id, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("rejudge %s: candidate %q: %w", args[0], args[1], err)
	}
	return id, nil
}

// withStore runs f over the Record.
func withStore(ctx context.Context, cfg config.Config, f func(*sqlite.Store) error) error {
	store, err := sqlite.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	return f(store)
}

// describe is a candidate in a line.
func describe(c editor.Candidate) string {
	state := "not chosen"
	if !c.ChosenAt.IsZero() {
		state = "chosen " + c.ChosenAt.Local().Format(time.DateTime)
	}
	return fmt.Sprintf("candidate %d: %s v%d on %s, Items published %s to %s, run %s, %d judgements, %s",
		c.ID, c.Act, c.Version, c.Model, c.Range.From.Local().Format(dateLayout),
		c.Range.To.Local().AddDate(0, 0, -1).Format(dateLayout), c.At.Local().Format(time.DateTime), c.Judgements, state)
}

func listCandidates(ctx context.Context, store *sqlite.Store, w io.Writer) error {
	p := &printer{w: w}
	cands, err := store.Candidates(ctx)
	if err != nil {
		return err
	}
	if len(cands) == 0 {
		p.f("no candidates\n")
	}
	for _, c := range cands {
		p.f("%s\n", describe(c))
	}
	return p.err
}

// findCandidate is the candidate with the id.
func findCandidate(ctx context.Context, store *sqlite.Store, id int64) (editor.Candidate, error) {
	cands, err := store.Candidates(ctx)
	if err != nil {
		return editor.Candidate{}, err
	}
	for _, c := range cands {
		if c.ID == id {
			return c, nil
		}
	}
	return editor.Candidate{}, fmt.Errorf("no candidate %d", id)
}

// compareCandidate prints, Item by Item, each kind of judgement the live
// reading a view reads made, the candidate's made, and both made, then the
// totals.
func compareCandidate(ctx context.Context, store *sqlite.Store, w io.Writer, id int64) error {
	p := &printer{w: w}
	c, err := findCandidate(ctx, store, id)
	if err != nil {
		return err
	}
	if c.Act != "reading" {
		return fmt.Errorf("candidate %d is of %s, which can't be compared yet", id, c.Act)
	}
	p.f("%s\n", describe(c))
	readings, err := store.CandidateReadings(ctx, id)
	if err != nil {
		return err
	}
	totals := map[string]*editor.KindCompared{}
	var order []string
	for _, cr := range readings {
		live, found, err := store.NewestReading(ctx, cr.SourceID, cr.EntryID)
		if err != nil {
			return err
		}
		p.f("\n%s %s\n", cr.SourceID, cr.EntryID)
		switch {
		case !found:
			p.f("  live: none\n")
		case live.ID == cr.ID:
			p.f("  live: this candidate's, since it was chosen\n")
		default:
			p.f("  live: %s v%d on %s\n", live.Judge.Act, live.Judge.Version, live.Judge.Model)
		}
		for _, k := range editor.CompareReadings(live, cr) {
			p.f("  %-10s live %d  candidate %d  both %d\n", k.Kind, k.Live, k.Candidate, k.Both)
			t, ok := totals[k.Kind]
			if !ok {
				t = &editor.KindCompared{Kind: k.Kind}
				totals[k.Kind] = t
				order = append(order, k.Kind)
			}
			t.Live += k.Live
			t.Candidate += k.Candidate
			t.Both += k.Both
		}
	}
	p.f("\ntotals over %d Items\n", len(readings))
	for _, kind := range order {
		t := totals[kind]
		p.f("  %-10s live %d  candidate %d  both %d\n", t.Kind, t.Live, t.Candidate, t.Both)
	}
	return p.err
}

// chooseCandidate makes a candidate the live judge from now on, once the
// act's configured model and this build's version are the candidate's, so
// the act goes on judging as the judge chosen.
func chooseCandidate(ctx context.Context, store *sqlite.Store, cfg config.Config, d adminDeps, id int64) error {
	c, err := findCandidate(ctx, store, id)
	if err != nil {
		return err
	}
	a, ok := rejudgeActs[c.Act]
	if !ok {
		return fmt.Errorf("candidate %d is of %s, which this build doesn't run", id, c.Act)
	}
	if m := a.model(cfg); m != c.Model {
		return fmt.Errorf("candidate %d runs on %s but %s is %s: set %s=%s and choose it again, so %s goes on judging as the judge chosen",
			id, c.Model, a.variable, m, a.variable, c.Model, c.Act)
	}
	if v := a.act(store).Version; v != c.Version {
		return fmt.Errorf("candidate %d is %s version %d but this build's is %d: choose it from a build of that version", id, c.Act, c.Version, v)
	}
	if err := store.ChooseCandidate(ctx, id, d.Now()); err != nil {
		return err
	}
	p := &printer{w: d.Stdout}
	p.f("chose candidate %d: from now on it is the live judge of %s\n", id, c.Act)
	return p.err
}
