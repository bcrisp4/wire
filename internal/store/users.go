package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/bcrisp4/wire/internal/model"
)

// userRepo implements UserRepo against SQLite.
//
// Missing rows surface as ErrNotFound (wrapped via fmt.Errorf %w) so callers
// can errors.Is-test it without importing database/sql.
type userRepo struct {
	db *sql.DB
}

func (r *userRepo) Get(ctx context.Context, id int64) (*model.User, error) {
	var u model.User
	err := r.db.QueryRowContext(ctx,
		`SELECT id, username, theme, font, entries_per_page, default_sort, default_order, created_at
		   FROM users WHERE id = ?`, id,
	).Scan(
		&u.ID, &u.Username, &u.Theme, &u.Font,
		&u.EntriesPerPage, &u.DefaultSort, &u.DefaultOrder, &u.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: user id=%d", ErrNotFound, id)
		}
		return nil, fmt.Errorf("users.Get: %w", err)
	}
	return &u, nil
}

// Update writes mutable preference columns. id, username, and created_at are
// immutable and untouched even if the caller modified them on the struct.
func (r *userRepo) Update(ctx context.Context, u *model.User) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE users
		    SET theme = ?, font = ?, entries_per_page = ?, default_sort = ?, default_order = ?
		  WHERE id = ?`,
		u.Theme, u.Font, u.EntriesPerPage, u.DefaultSort, u.DefaultOrder, u.ID,
	)
	if err != nil {
		return fmt.Errorf("users.Update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("users.Update: rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: user id=%d", ErrNotFound, u.ID)
	}
	return nil
}
