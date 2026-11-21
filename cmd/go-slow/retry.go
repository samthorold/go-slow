package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	"github.com/samthorold/go-slow/internal/claude"
	"github.com/samthorold/go-slow/internal/config"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// retry takes failed work up again (system design: Work that keeps failing
// is given up after three attempts, and taken up again by hand):
//
//	retry [act]
//	retry weaving --max-usd N [--confirm]
//
// Without --max-usd it clears the failed marks on the work of one act run in
// batches (hydration, weaving or naming), or of every act, resetting its
// attempts, so the next run takes that work up again. A weaving run given up
// over its cap (the weave command's --max-usd) stays given up: resumed under
// that cap, its next turn would give it up again, at the price of the turn.
// The command prints what such a run has spent and its next turn's estimate.
//
// With --max-usd it resumes the weaving run marked failed most recently
// under a new cap, N dollars over all its turns at batch rates, the turns it
// has taken included. Without --confirm it is a dry run: it prints what the
// run has spent, its cap, and its next turn (its last kept turn's next, or
// its first again if it kept none) with that turn's estimate, and calls no
// model and writes nothing. With --confirm it keeps the cap, clears the
// run's failed mark and submits its next turn as a batch, which the app
// applies as it applies any turn, holding the run's later turns to the new
// cap. It is refused, writing nothing, when what the run has spent and its
// next turn's estimate are over N, or when a weaving run is under way.
func retry(ctx context.Context, log *slog.Logger, args []string, d adminDeps) error {
	var act string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		act, args = args[0], args[1:]
	}
	var known []string
	for _, a := range batchActs(editor.New(editor.Deps{}), 0) {
		known = append(known, a.Name)
	}
	if act != "" && !slices.Contains(known, act) {
		return fmt.Errorf("unknown act %q (known: %s)", act, strings.Join(known, ", "))
	}
	flags := flag.NewFlagSet("retry", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	confirm := flags.Bool("confirm", false, "resume the run, rather than only pricing it")
	maxUSD := flags.Float64("max-usd", 0, "the most the weaving run may spend over all its turns, in dollars at batch rates")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("retry: %w", err)
	}
	capped := false
	flags.Visit(func(f *flag.Flag) { capped = capped || f.Name == "max-usd" })
	switch {
	case (capped || *confirm) && act != "weaving":
		return errors.New("retry: --max-usd and --confirm resume a weaving run: give retry weaving --max-usd N")
	case *confirm && !capped:
		return errors.New("retry: resuming a weaving run needs --max-usd, the most it may spend")
	case capped && *maxUSD <= 0:
		return errors.New("retry: --max-usd must be more than nothing")
	}
	cfg, err := config.Load(d.Lookup)
	if err != nil {
		return err
	}
	return withStore(ctx, cfg, func(store *sqlite.Store) error {
		model := cfg.WeavingModel
		modelFor := func(string) string { return model }
		ed := editor.New(editor.Deps{Weavings: store, ModelFor: modelFor, PriceOf: claude.PriceOf, Log: log})
		p := &printer{w: d.Stdout}
		givenUp, err := weavingsGivenUp(ctx, store, ed, act)
		if err != nil {
			return err
		}
		if capped {
			if len(givenUp) == 0 {
				return errors.New("retry: no weaving run is marked failed")
			}
			return resume(ctx, log, p, store, ed, givenUp[0], *maxUSD, *confirm, d, cfg)
		}
		var kept []sqlite.Work
		for _, g := range givenUp {
			if !g.OverCap() {
				continue
			}
			kept = append(kept, sqlite.Work{Act: "weaving", Key: g.Run.Key})
			p.f("weaving run %s stays given up: resumed under its cap of $%.2f, its next turn would give it up again\n",
				g.Run.Key, g.MaxUSD)
			printGivenUp(p, g)
			p.f("resume it with a higher cap: retry weaving --max-usd N, then --confirm\n")
		}
		n, err := store.RetryFailed(ctx, act, kept...)
		if err != nil {
			return err
		}
		log.Info("failed work cleared for retry", "act", act, "cleared", n, "kept", len(kept))
		return p.err
	})
}

// weavingsGivenUp are the weaving runs marked failed, as each would resume,
// most recently failed first, when act is weaving or every act; a run with
// nothing to resume (no turn kept, and nothing awaiting weaving) is left out.
func weavingsGivenUp(ctx context.Context, store *sqlite.Store, ed *editor.Editor, act string) ([]editor.GivenUp, error) {
	if act != "" && act != "weaving" {
		return nil, nil
	}
	failed, err := store.FailedWork(ctx)
	if err != nil {
		return nil, err
	}
	var out []editor.GivenUp
	for _, f := range failed {
		if f.Act != "weaving" {
			continue
		}
		// A weaving run's key, its day, is its Title (editor.FailedWork).
		g, err := ed.GivenUpWeaving(ctx, f.Title)
		if errors.Is(err, editor.ErrNothingToWeave) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("retry: weaving run %s: %w", f.Title, err)
		}
		out = append(out, g)
	}
	return out, nil
}

// printGivenUp prints what a run given up has spent, its cap and its next
// turn's estimate.
func printGivenUp(p *printer, g editor.GivenUp) {
	limit := "no cap"
	if g.Capped {
		limit = fmt.Sprintf("its cap of $%.2f", g.MaxUSD)
	}
	turn := "its first, again: it kept no turn"
	if n := len(g.Run.Job.Request.Messages); n > 0 {
		turn = fmt.Sprintf("its turn %d, after the last it kept", n/2+1)
	}
	p.f("spent $%.4f at batch rates over its turns, against %s\nits next turn is %s:\n", g.SpentUSD, limit, turn)
	printEstimate(p, g.Next)
}

// resume prices the weaving run given up g under the cap maxUSD and, with
// confirm, keeps the cap, clears its failed mark and submits its next turn.
func resume(ctx context.Context, log *slog.Logger, p *printer, store *sqlite.Store, ed *editor.Editor,
	g editor.GivenUp, maxUSD float64, confirm bool, d adminDeps, cfg config.Config) error {
	key := g.Run.Key
	p.f("resume weaving run %s, given up, under a cap of $%.2f over all its turns\n", key, maxUSD)
	printGivenUp(p, g)
	p.f("spent and the next turn: ~$%.4f; later turns depend on what the model asks, and --max-usd caps the whole run\n",
		g.SpentUSD+g.Next.USD)
	if !confirm {
		p.f("dry run: no model was called and nothing written. Run again with --confirm --max-usd N to resume it.\n")
		return p.err
	}
	if g.SpentUSD+g.Next.USD > maxUSD {
		return fmt.Errorf("retry: %w: $%.4f spent and the next turn ~$%.4f, over $%.2f",
			editor.ErrOverCap, g.SpentUSD, g.Next.USD, maxUSD)
	}
	if under, ok, err := ed.WeavingUnderWay(ctx); err != nil || ok {
		if err != nil {
			return err
		}
		return fmt.Errorf("retry: %w: %s", editor.ErrWeavingPending, under)
	}
	// The cap is kept before the turn is submitted, so the app's batch runner
	// holds every later turn to it.
	if err := store.KeepWeavingCap(ctx, key, maxUSD); err != nil {
		return err
	}
	model := cfg.WeavingModel
	batches := editor.NewBatches(editor.BatchDeps{
		Store: store, Batcher: d.Batchers(cfg.AnthropicAPIKey, "weaving", model),
		ModelFor: func(string) string { return model }, Log: log,
	})
	id, err := batches.Submit(ctx, g.Run.Job, d.Now())
	if err != nil {
		return err
	}
	// The mark is cleared once the turn is pending, so the app's runner never
	// finds the run neither marked nor pending, and submits no turn of its
	// own; it applies the pending turn to the run's kept conversation,
	// marked or not.
	if _, err := store.ClearFailed(ctx, sqlite.Work{Act: "weaving", Key: key}); err != nil {
		return err
	}
	p.f("resumed: its next turn submitted as batch %s, which the app applies after a Collection run once it ends\n", id)
	return p.err
}
