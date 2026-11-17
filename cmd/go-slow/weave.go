package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/samthorold/go-slow/internal/claude"
	"github.com/samthorold/go-slow/internal/config"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// weave submits today's weaving run on the Owner's say, rather than waiting
// for the first Collection run after WEAVING_TIME (system design: Weaving
// runs at most once a day):
//
//	weave [--since YYYY-MM-DD] [--confirm --max-usd N]
//
// Without --confirm it is a dry run: it prints what the run would be shown
// (the Stories the Editor made, and each Story cut since the last run, by its
// Item's title and date) and its estimated price at batch rates, and calls
// no model and writes nothing. With --confirm it submits the run as a batch,
// keyed by today in the time zone TZ, whatever the time of day; the app
// applies its result after a Collection run once the batch ends, and names
// its Stories as usual. It is refused, writing nothing, when its estimate is
// over --max-usd, when today's run has been written or marked failed, when a
// weaving run's batch is pending, or when nothing awaits weaving.
//
// --since moves the switch to Stories back to the day, in TZ, for a Record
// not yet woven: it keeps a new switch through the newest Cut of an Item
// collected before the day, so the Stories cut since then await weaving
// (system design: The switch to Stories). It is refused on a Record already
// woven, which carries on from its last run, so moving its switch would
// change nothing.
func weave(ctx context.Context, log *slog.Logger, args []string, d adminDeps) error {
	cfg, err := config.Load(d.Lookup)
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("weave", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	since := flags.String("since", "", "move the switch to Stories back to this day, YYYY-MM-DD")
	confirm := flags.Bool("confirm", false, "submit it, rather than only pricing it")
	maxUSD := flags.Float64("max-usd", 0, "the most the run may spend, in dollars at batch rates")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("weave: %w", err)
	}
	var day time.Time
	if *since != "" {
		if day, err = time.ParseInLocation(dateLayout, *since, time.Local); err != nil {
			return fmt.Errorf("weave: --since: %w", err)
		}
	}
	if *confirm && *maxUSD <= 0 {
		return errors.New("weave: a run needs --max-usd, the most it may spend")
	}
	model := cfg.WeavingModel
	price, ok := claude.PriceOf(model)
	if !ok {
		return fmt.Errorf("weave: no price known for model %q", model)
	}
	modelFor := func(string) string { return model }
	return withStore(ctx, cfg, func(store *sqlite.Store) error {
		now := d.Now()
		// weavings is the Record as the run reads it: with the switch moved
		// back, for --since, before it is kept.
		var weavings editor.WeavingStore = store
		var through int64
		if *since != "" {
			woven, err := store.Woven(ctx)
			if err != nil {
				return err
			}
			if woven {
				return errors.New("weave: the Record is woven, and carries on from its last run: moving its switch to Stories would change nothing")
			}
			var found bool
			if through, found, err = store.CutBefore(ctx, day); err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("weave: no Item collected before %s is cut: give a later day", *since)
			}
			weavings = switchedAt{Store: store, through: through}
		}
		ed := editor.New(editor.Deps{Weavings: weavings, ModelFor: modelFor, Log: log})
		run, err := ed.WeaveToday(ctx, now)
		if err != nil {
			return fmt.Errorf("weave: %w", err)
		}
		p := &printer{w: d.Stdout}
		p.f("weave today's run, %s, on %s: shown %d Stories made and %d Stories cut\n",
			run.Key, model, len(run.Made), len(run.Stories))
		for _, s := range run.Stories {
			date := "undated"
			if !s.Item.Published.IsZero() {
				date = s.Item.Published.In(time.Local).Format(dateLayout)
			}
			p.f("  %s  %s\n", date, s.Item.Title)
		}
		past, err := store.PastUsage(ctx, "weaving")
		if err != nil {
			return err
		}
		est, err := editor.EstimateOf("weaving", model, []editor.Request{run.Job.Request}, past,
			price.AtBatchRates(), editor.BatchRates)
		if err != nil {
			return err
		}
		printEstimate(p, est)
		if !*confirm {
			p.f("dry run: no model was called and nothing written. Run again with --confirm --max-usd N to submit it.\n")
			return p.err
		}
		if est.USD > *maxUSD {
			return fmt.Errorf("weave: %w: $%.2f over $%.2f", editor.ErrOverCap, est.USD, *maxUSD)
		}
		if *since != "" {
			// The switch is kept first, so the app's batch runner, reading
			// the Record, finds the run it submits still awaiting weaving.
			if err := store.KeepStorySwitch(ctx, through, now); err != nil {
				return err
			}
			p.f("switch to Stories moved back to %s\n", *since)
		}
		batches := editor.NewBatches(editor.BatchDeps{
			Store: store, Batcher: d.Batchers(cfg.AnthropicAPIKey, "weaving", model), ModelFor: modelFor, Log: log,
		})
		id, err := batches.Submit(ctx, run.Job, now)
		if err != nil {
			return err
		}
		p.f("submitted as batch %s: the app applies its result after a Collection run once the batch ends, and names its Stories\n", id)
		return p.err
	})
}

// switchedAt is the Record as it would be with its switch to Stories through
// the Cut through: the Record not yet woven, so the Stories awaiting weaving
// are those cut after it.
type switchedAt struct {
	*sqlite.Store
	through int64
}

func (s switchedAt) StoriesToWeave(ctx context.Context) ([]editor.CutStory, error) {
	return s.StoriesCutAfter(ctx, s.through)
}
