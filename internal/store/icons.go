// Package-private stub — Phase 1 Unit (Icons REST) replaces this file.
package store

import (
	"context"
	"database/sql"

	"github.com/bcrisp4/wire/internal/model"
)

type iconRepo struct {
	db *sql.DB
}

func (r *iconRepo) GetByHash(ctx context.Context, hash string) (*model.Icon, error) {
	panic("store: IconRepo.GetByHash not yet implemented")
}

func (r *iconRepo) Insert(ctx context.Context, i *model.Icon) (int64, error) {
	panic("store: IconRepo.Insert not yet implemented")
}
