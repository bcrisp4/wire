package main

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/bcrisp4/wire/internal/api"
	"github.com/bcrisp4/wire/internal/config"
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

	// Heartbeat keeps the scheduler exercised so Phase 1 workers can register against
	// a known-working scheduler without bootstrapping it themselves.
	if err := hb.Scheduler().Schedule(jobs.ScheduledTask{
		Name:  jobs.QueueHeartbeat,
		Cron:  "* * * * *",
		Queue: jobs.QueueHeartbeat,
	}); err != nil {
		return fmt.Errorf("schedule heartbeat: %w", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := hb.Scheduler().Run(ctx); err != nil && ctx.Err() == nil {
			log.Error("scheduler exited", "err", err)
		}
	}()

	spaFS, err := web.FS()
	if err != nil {
		return err
	}

	srv, err := api.NewServer(api.Options{
		Listen: cfg.Listen,
		Logger: log,
		SPA:    api.SPAHandler(spaFS),
	})
	if err != nil {
		return err
	}
	log.Info("starting", "listen", cfg.Listen, "db", cfg.DBPath, "extension", cfg.HonkerExtensionPath)
	runErr := srv.Run(ctx)
	wg.Wait() // ensure the scheduler goroutine returns before we close hb
	return runErr
}
