// Package-private stub — Phase 1 Unit (Categories REST) replaces this file.
package store

import (
	"context"
	"database/sql"

	"github.com/bcrisp4/wire/internal/model"
)

type categoryRepo struct {
	db *sql.DB
}

func (r *categoryRepo) List(ctx context.Context, userID int64) ([]model.Category, error) {
	panic("store: CategoryRepo.List not yet implemented")
}

func (r *categoryRepo) Create(ctx context.Context, c *model.Category) error {
	panic("store: CategoryRepo.Create not yet implemented")
}

func (r *categoryRepo) Rename(ctx context.Context, id int64, name string) error {
	panic("store: CategoryRepo.Rename not yet implemented")
}

func (r *categoryRepo) Delete(ctx context.Context, id int64) error {
	panic("store: CategoryRepo.Delete not yet implemented")
}
