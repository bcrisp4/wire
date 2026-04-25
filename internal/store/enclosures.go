// Package-private stub — Phase 1 Unit (Enclosures REST) replaces this file.
package store

import (
	"context"
	"database/sql"

	"github.com/bcrisp4/wire/internal/model"
)

type enclosureRepo struct {
	db *sql.DB
}

func (r *enclosureRepo) List(ctx context.Context, entryID int64) ([]model.Enclosure, error) {
	panic("store: EnclosureRepo.List not yet implemented")
}

func (r *enclosureRepo) Insert(ctx context.Context, e *model.Enclosure) error {
	panic("store: EnclosureRepo.Insert not yet implemented")
}
