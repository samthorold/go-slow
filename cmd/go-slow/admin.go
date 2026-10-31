package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/config"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/polite"
	"github.com/samthorold/go-slow/internal/register"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// adminDeps are what the admin commands are given by main.
type adminDeps struct {
	// Lookup reads the config's environment.
	Lookup func(string) (string, bool)
	// Stdin is what is piped in, and Stdout where a command prints what
	// the Owner reads (logs go to the log).
	Stdin  io.Reader
	Stdout io.Writer
	// Web fetches from Sources.
	Web *http.Client
	// Models is the model adapter on the model named, with the key.
	Models func(key config.Secret, model string) editor.Model
	Now    func() time.Time
}

// admin runs a one-off admin command, from the same binary and config as
// the app, the config read through d.Lookup.
//
//	commits: keeps the commits to go-slow, read from git log on stdin, for
//	the register's quiet window and its changes to the Editor. The app
//	can't see the repository, so the Owner pipes the log in:
//
//	  git log --format='commit %H %cI' --name-only | docker compose run --rm -T app commits
//
//	Commits already kept are skipped, so the whole history can be piped in
//	each time.
//
//	retry [act]: clears the failed marks on the work of one act run in
//	batches (reading or relating), or of every act, resetting its
//	attempts, so the next run takes that work up again:
//
//	  docker compose run --rm app retry relating
//
//	backfill: gives their text to the Items of the Sources whose text is
//	fetched that were collected with only a summary and are still uncut,
//	fetching it politely through web; the next Collection run cuts them.
//	Items already cut are left as they are:
//
//	  docker compose run --rm app backfill
//
//	rejudge: judges a range of the Record again with a chosen judge, priced
//	first; see rejudge.
func admin(ctx context.Context, log *slog.Logger, args []string, d adminDeps) error {
	switch args[0] {
	case "backfill":
		cfg, err := config.Load(d.Lookup)
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
		n, err := collect.Backfill(ctx, collect.BackfillDeps{
			Sources: sources,
			Fetcher: polite.New(polite.Options{
				HTTP:      d.Web,
				UserAgent: polite.UserAgent(cfg.CollectionContact),
				Now:       time.Now,
				Sleep:     polite.Sleep,
			}),
			Store: store,
			Log:   log,
		})
		if err != nil {
			return err
		}
		log.Info("items given their text", "given", n)
		return nil
	case "commits":
		cfg, err := config.Load(d.Lookup)
		if err != nil {
			return err
		}
		commits, err := register.ParseGitLog(d.Stdin)
		if err != nil {
			return fmt.Errorf("read git log (want --format='%s' --name-only): %w", register.GitLogFormat, err)
		}
		store, err := sqlite.Open(ctx, cfg.DatabasePath)
		if err != nil {
			return err
		}
		defer func() { _ = store.Close() }()
		n, err := store.KeepCommits(ctx, commits)
		if err != nil {
			return err
		}
		log.Info("commits kept", "read", len(commits), "new", n)
		return nil
	case "retry":
		var act string
		if len(args) > 1 {
			act = args[1]
		}
		var known []string
		for _, a := range batchActs(editor.New(editor.Deps{})) {
			known = append(known, a.Name)
		}
		if act != "" && !slices.Contains(known, act) {
			return fmt.Errorf("unknown act %q (known: %s)", act, strings.Join(known, ", "))
		}
		cfg, err := config.Load(d.Lookup)
		if err != nil {
			return err
		}
		store, err := sqlite.Open(ctx, cfg.DatabasePath)
		if err != nil {
			return err
		}
		defer func() { _ = store.Close() }()
		n, err := store.RetryFailed(ctx, act)
		if err != nil {
			return err
		}
		log.Info("failed work cleared for retry", "act", act, "cleared", n)
		return nil
	case "rejudge":
		return rejudge(ctx, log, args[1:], d)
	default:
		return fmt.Errorf("unknown command %q (known: backfill, commits, rejudge, retry)", args[0])
	}
}
