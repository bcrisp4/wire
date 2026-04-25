// Package-private stub — Phase 1 Unit (Tombstones REST) replaces this file.
package store

import (
	"context"
	"database/sql"
)

type tombstoneRepo struct {
	db *sql.DB
}

func (r *tombstoneRepo) Has(ctx context.Context, feedID int64, hash string) (bool, error) {
	panic("store: TombstoneRepo.Has not yet implemented")
}

func (r *tombstoneRepo) Insert(ctx context.Context, feedID int64, hash string) error {
	panic("store: TombstoneRepo.Insert not yet implemented")
}
