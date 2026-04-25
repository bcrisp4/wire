package main

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/bcrisp4/wire/internal/api"
	"github.com/bcrisp4/wire/internal/config"
	"github.com/bcrisp4/wire/internal/extract"
	"github.com/bcrisp4/wire/internal/feedpoll"
	"github.com/bcrisp4/wire/internal/jobs"
	"github.com/bcrisp4/wire/internal/logger"
	"github.com/bcrisp4/wire/internal/store"
	"github.com/bcrisp4/wire/internal/web"
)

func serve(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log, err := logger.New(os.Stderr, cfg.LogFormat, cfg.LogLevel)
	if err != nil {
		return err
	}

	// Honker owns the SQLite connection. Application SQL goes through hb.RawDB().
	hb, err := jobs.NewHonker(jobs.HonkerOptions{
		DBPath:        cfg.DBPath,
		ExtensionPath: cfg.HonkerExtensionPath,
	})
	if err != nil {
		return fmt.Errorf("open honker: %w", err)
	}
	defer hb.Close()

	if err := store.Migrate(ctx, hb.RawDB()); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// Unit 4: feed.poll cron replaces the Phase 0 wire.heartbeat canary.
	// The cron tick uses feedpoll.TickPayload so the worker (wired below
	// once sibling units land) can distinguish dispatcher ticks from
	// per-feed poll jobs and call EnqueueDue to fan out.
	if err := hb.Scheduler().Schedule(jobs.ScheduledTask{
		Name:    jobs.QueueFeedPoll,
		Cron:    "* * * * *",
		Queue:   jobs.QueueFeedPoll,
		Payload: feedpoll.TickPayload(),
	}); err != nil {
		return fmt.Errorf("schedule feed.poll: %w", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := hb.Scheduler().Run(ctx); err != nil && ctx.Err() == nil {
			log.Error("scheduler exited", "err", err)
		}
	}()

	// TODO(unit-0/1/2): wire feedpoll.RunWorker once store.New, feedparse, and
	// feedfetch land. feedpoll.Deps takes locally-defined interfaces so the
	// store/parser/fetcher impls satisfy them structurally. The cron above
	// fires a job on QueueFeedPoll; the worker calls feedpoll.EnqueueDue to
	// fan it out into per-feed jobs.

	// Unit 5: entry.extract worker.
	// No cron is needed — extract jobs are enqueued by the poll worker (Unit 4)
	// when it inserts entries on feeds with crawler=1. We just register and
	// drain the queue.
	extractDeps := extract.Deps{
		Queue:        hb.Queue(),
		Logger:       log,
		EntryFetcher: extract.NewSQLEntryFetcher(hb.RawDB()),
		EntryUpdater: extract.NewSQLEntryUpdater(hb.RawDB()),
	}
	wg.Add(1)
	go func() { defer wg.Done(); extract.RunWorker(ctx, extractDeps, "wire-extractor-1") }()
	// Unit 5: end.

	spaFS, err := web.FS()
	if err != nil {
		return err
	}

	// Unit 0: storage bootstrap. Store wraps Honker's *sql.DB; Honker still owns
	// the connection lifecycle, so Store.Close is a no-op.
	srv, err := api.NewServer(api.Options{
		Listen: cfg.Listen,
		Logger: log,
		SPA:    api.SPAHandler(spaFS),
		Store:  store.New(hb.RawDB()),
		Queue:  hb.Queue(),
	})
	if err != nil {
		return err
	}
	log.Info("starting", "listen", cfg.Listen, "db", cfg.DBPath, "extension", cfg.HonkerExtensionPath)
	runErr := srv.Run(ctx)
	wg.Wait() // ensure the scheduler goroutine returns before we close hb
	return runErr
}
