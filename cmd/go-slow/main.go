// Command go-slow serves the Briefing.
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
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
	_ "time/tzdata" // the TZ variable works in an image with no zoneinfo

	"github.com/samthorold/go-slow/internal/config"
	"github.com/samthorold/go-slow/internal/server"
	"github.com/samthorold/go-slow/internal/sqlite"
	"github.com/samthorold/go-slow/internal/web"
)

// shutdownGrace is how long in-flight requests get to finish after SIGTERM;
// it is shorter than docker's 10s before it kills the process.
const shutdownGrace = 5 * time.Second

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

	app := web.New(web.Deps{
		Briefings: store,
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
