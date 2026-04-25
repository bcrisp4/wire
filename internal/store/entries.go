// Package-private stub — Phase 1 Unit (Entries REST) replaces this file.
package store

import (
	"context"
	"database/sql"

	"github.com/bcrisp4/wire/internal/model"
)

type entryRepo struct {
	db *sql.DB
}

func (r *entryRepo) List(ctx context.Context, q EntryQuery) ([]model.Entry, error) {
	panic("store: EntryRepo.List not yet implemented")
}

func (r *entryRepo) Get(ctx context.Context, id int64) (*model.Entry, error) {
	panic("store: EntryRepo.Get not yet implemented")
}

func (r *entryRepo) Insert(ctx context.Context, e *model.Entry) error {
	panic("store: EntryRepo.Insert not yet implemented")
}

func (r *entryRepo) UpdateState(ctx context.Context, id int64, read, saved *bool) error {
	panic("store: EntryRepo.UpdateState not yet implemented")
}

func (r *entryRepo) BulkMarkRead(ctx context.Context, scope BulkReadScope) error {
	panic("store: EntryRepo.BulkMarkRead not yet implemented")
}

func (r *entryRepo) Search(ctx context.Context, userID int64, query string, limit, offset int) ([]model.Entry, error) {
	panic("store: EntryRepo.Search not yet implemented")
}
