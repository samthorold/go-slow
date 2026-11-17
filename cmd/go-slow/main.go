// Command go-slow serves the Stories and runs Collection on its timetable.
//
// main is the composition root: it reads the config from the environment,
// builds the adapters and passes them to the app's constructor. Nothing else
// reads the environment.
//
// The same binary runs the one-off admin commands (admin.go: retry,
// backfill; rejudge.go: rejudge; weave.go: weave), with the app's own
// config, tested end to end in admin_test.go, backfill_test.go,
// rejudge_test.go and weave_test.go.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
	_ "time/tzdata" // the TZ variable works in an image with no zoneinfo

	"github.com/samthorold/go-slow/internal/claude"
	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/config"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/polite"
	"github.com/samthorold/go-slow/internal/server"
	"github.com/samthorold/go-slow/internal/sqlite"
	"github.com/samthorold/go-slow/internal/web"
	"github.com/samthorold/go-slow/internal/wikidata"
)

// shutdownGrace is how long in-flight requests get to finish after SIGTERM;
// it is shorter than docker's 10s before it kills the process.
const shutdownGrace = 5 * time.Second

// fetchTimeout bounds one request to a Source.
const fetchTimeout = 30 * time.Second

func main() {
	// Logs are an event stream: JSON lines, unbuffered, to stdout.
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if len(os.Args) > 1 {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
		err := admin(ctx, log, os.Args[1:], adminDeps{
			Lookup: os.LookupEnv, Stdout: os.Stdout,
			Web: &http.Client{Timeout: fetchTimeout},
			Models: func(key config.Secret, model string) editor.Model {
				return claude.New(claude.Options{APIKey: string(key), Model: model})
			},
			Batchers: func(key config.Secret, act, model string) editor.Batcher {
				return claude.New(claude.Options{APIKey: string(key), Model: model, ActModels: map[string]string{act: model}})
			},
			Now: time.Now,
		})
		stop()
		if err != nil {
			log.Error("go-slow admin command failed", "command", os.Args[1], "error", err)
			os.Exit(1)
		}
		return
	}
	if err := run(log); err != nil {
		log.Error("go-slow stopped", "error", err)
		os.Exit(1)
	}
}

// batchActs are the acts run in batches, in the order their work is
// submitted; weaving from the time of day weavingAt, and naming after it, so
// the Stories a run's result makes are named in the batch submitted on the
// same turn.
func batchActs(ed *editor.Editor, weavingAt time.Duration) []editor.BatchAct {
	return []editor.BatchAct{ed.Reading(), ed.Weaving(weavingAt), ed.Naming()}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		return err
	}

	store, err := sqlite.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	sources := collect.Sources()
	if err := store.SyncSources(ctx, sources, time.Now()); err != nil {
		return err
	}
	// The Topics and the lazy-reading check: plain code, no model adapter.
	topics := interest.New(store)
	// The Editor, each act on the model the config names for it. One
	// adapter serves every act, sending each act's requests to its model.
	models := claude.New(claude.Options{
		APIKey: string(cfg.AnthropicAPIKey),
		Model:  cfg.ReadingModel,
		ActModels: map[string]string{
			"reading": cfg.ReadingModel, "weaving": cfg.WeavingModel, "naming": cfg.NamingModel,
		},
	})
	// What the readings name is looked up in Wikidata, politely, as go-slow.
	wd := wikidata.New(wikidata.Options{
		HTTP:      &http.Client{Timeout: fetchTimeout},
		Endpoint:  wikidata.Endpoint,
		UserAgent: polite.UserAgent(cfg.CollectionContact),
		Gap:       wikidata.Gap,
		Now:       time.Now,
		Sleep:     polite.Sleep,
	})
	ed := editor.New(editor.Deps{
		Store: store, Model: models, Weavings: store, Namings: store, ModelFor: models.ModelFor, Log: log,
	})
	// The acts that call a model run in batches: submitted after a Collection run, and
	// their results applied on later runs as they arrive. One batch carries
	// every act's requests.
	batches := editor.NewBatches(editor.BatchDeps{
		Store:    store,
		Batcher:  models,
		Acts:     batchActs(ed, cfg.WeavingAt),
		ModelFor: models.ModelFor,
		Log:      log,
	})
	collector := collect.New(collect.Deps{
		Sources: sources,
		Fetcher: polite.New(polite.Options{
			HTTP:      &http.Client{Timeout: fetchTimeout},
			UserAgent: polite.UserAgent(cfg.CollectionContact),
			Now:       time.Now,
			Sleep:     polite.Sleep,
		}),
		Store: store,
		// Cutting is plain code: the start's run cuts the Items already in
		// the Record, and every run those it collects.
		Cut:  cut.New(store),
		Read: batches,
		// What the readings name is looked up in Wikidata after each run,
		// in plain code, once per name.
		LookUp: editor.NewLookups(editor.LookupDeps{Store: store, Wikidata: wd, Log: log}),
		Log:    log,
	})
	// The timetable runs inside the app process, stopping with it.
	ticker := time.NewTicker(cfg.CollectionInterval)
	defer ticker.Stop()
	collectCtx, stopCollecting := context.WithCancel(ctx)
	collected := make(chan struct{})
	go func() {
		defer close(collected)
		collector.Run(collectCtx, time.Now())
		collector.RunEvery(collectCtx, ticker.C, time.Now)
	}()
	// Before the store closes: stop Collection and wait for its run to end.
	defer func() { stopCollecting(); <-collected }()

	app := web.New(web.Deps{
		Items:    store,
		Readings: store,
		Cuts:     store,
		Topics:   topics,
		Calls:    store,
		Stories:  store,
		Sources:  sources,
		Now:      time.Now,
		Log:      log,
	})

	ln, err := new(net.ListenConfig).Listen(ctx, "tcp", ":"+strconv.Itoa(cfg.Port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	log.Info("go-slow listening", "addr", ln.Addr().String(), "database", cfg.DatabasePath)

	if err := server.Run(ctx, ln, app, shutdownGrace); err != nil {
		return err
	}
	log.Info("go-slow shut down")
	return nil
}
