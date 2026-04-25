// Package-private stub — Phase 1 Unit (Feeds REST) replaces this file.
package store

import (
	"context"
	"database/sql"

	"github.com/bcrisp4/wire/internal/model"
)

type feedRepo struct {
	db *sql.DB
}

func (r *feedRepo) List(ctx context.Context, userID int64) ([]model.Feed, error) {
	panic("store: FeedRepo.List not yet implemented")
}

func (r *feedRepo) Get(ctx context.Context, id int64) (*model.Feed, error) {
	panic("store: FeedRepo.Get not yet implemented")
}

func (r *feedRepo) Create(ctx context.Context, f *model.Feed) error {
	panic("store: FeedRepo.Create not yet implemented")
}

func (r *feedRepo) Update(ctx context.Context, f *model.Feed) error {
	panic("store: FeedRepo.Update not yet implemented")
}

func (r *feedRepo) Delete(ctx context.Context, id int64) error {
	panic("store: FeedRepo.Delete not yet implemented")
}

func (r *feedRepo) DueForPolling(ctx context.Context, now int64, limit int) ([]model.Feed, error) {
	panic("store: FeedRepo.DueForPolling not yet implemented")
}
