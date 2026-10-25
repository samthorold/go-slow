package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/samthorold/go-slow/internal/config"
	"github.com/samthorold/go-slow/internal/register"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// admin runs a one-off admin command, from the same binary and config as
// the app.
//
//	commits: keeps the commits to go-slow, read from git log on stdin, for
//	the register's quiet window and its changes to the Editor. The app
//	can't see the repository, so the Owner pipes the log in:
//
//	  git log --format='commit %H %cI' --name-only | docker compose run --rm -T app commits
//
//	Commits already kept are skipped, so the whole history can be piped in
//	each time.
func admin(log *slog.Logger, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	switch args[0] {
	case "commits":
		cfg, err := config.Load(os.LookupEnv)
		if err != nil {
			return err
		}
		commits, err := register.ParseGitLog(os.Stdin)
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
	default:
		return fmt.Errorf("unknown command %q (known: commits)", args[0])
	}
}
