package main

import (
	"context"
	"fmt"
	"os"

	"github.com/bcrisp4/wire/internal/config"
	"github.com/bcrisp4/wire/internal/jobs"
	"github.com/bcrisp4/wire/internal/logger"
	"github.com/bcrisp4/wire/internal/store"
)

func migrate(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log, err := logger.New(os.Stderr, cfg.LogFormat, cfg.LogLevel)
	if err != nil {
		return err
	}
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
	log.Info("migrations applied", "db", cfg.DBPath)
	return nil
}
