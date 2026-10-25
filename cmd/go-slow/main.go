// Command go-slow serves the Briefing and runs Collection on its timetable.
//
// main is the composition root: it reads the config from the environment,
// builds the adapters and passes them to the app's constructor. Nothing else
// reads the environment.
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
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/polite"
	"github.com/samthorold/go-slow/internal/server"
	"github.com/samthorold/go-slow/internal/sqlite"
	"github.com/samthorold/go-slow/internal/web"
)

// shutdownGrace is how long in-flight requests get to finish after SIGTERM;
// it is shorter than docker's 10s before it kills the process.
const shutdownGrace = 5 * time.Second

// fetchTimeout bounds one request to a Source.
const fetchTimeout = 30 * time.Second

func main() {
	// Logs are an event stream: JSON lines, unbuffered, to stdout.
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("go-slow stopped", "error", err)
		os.Exit(1)
	}
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
	// The Editor, each act on the model the config names for it.
	// One adapter serves every act, sending each act's requests to its model.
	models := claude.New(claude.Options{
		APIKey:    string(cfg.AnthropicAPIKey),
		Model:     cfg.ReadingModel,
		ActModels: map[string]string{"reading": cfg.ReadingModel, "placing": cfg.PlacingModel},
	})
	ed := editor.New(editor.Deps{Store: store, Model: models, PlacingModel: models, Log: log})
	// The daily acts run in batches: submitted after a Collection run, and
	// their results applied on later runs as they arrive. One batch carries
	// every act's requests.
	batches := editor.NewBatches(editor.BatchDeps{
		Store:   store,
		Batcher: models,
		Acts:    []editor.BatchAct{ed.Reading(), ed.Placing()},
		Log:     log,
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
		Check: topics,
		Read:  batches,
		Log:   log,
	})
	// The timetable runs inside the app process, stopping with it.
	ticker := time.NewTicker(cfg.CollectionInterval)
	defer ticker.Stop()
	collectCtx, stopCollecting := context.WithCancel(ctx)
	collected := make(chan struct{})
	go func() {
		defer close(collected)
		collector.RunOn(collectCtx, ticker.C, time.Now)
	}()
	// Before the store closes: stop Collection and wait for its run to end.
	defer func() { stopCollecting(); <-collected }()

	app := web.New(web.Deps{
		Briefings: store,
		Items:     store,
		Readings:  store,
		Topics:    topics,
		Stories:   store,
		Now:       time.Now,
		Log:       log,
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
