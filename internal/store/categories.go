package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/bcrisp4/wire/internal/model"
	"github.com/mattn/go-sqlite3"
)

// categoryRepo implements CategoryRepo against SQLite.
//
// UNIQUE(user_id, name) violations surface as ErrConflict so handlers can
// translate them to HTTP 409. Missing rows on Rename/Delete surface as
// ErrNotFound. Both are wrapped via fmt.Errorf("%w", ...) for errors.Is.
type categoryRepo struct {
	db *sql.DB
}

func (r *categoryRepo) List(ctx context.Context, userID int64) ([]model.Category, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, name FROM categories WHERE user_id = ? ORDER BY name`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("categories: list: %w", err)
	}
	defer rows.Close()

	var out []model.Category
	for rows.Next() {
		var c model.Category
		if err := rows.Scan(&c.ID, &c.UserID, &c.Name); err != nil {
			return nil, fmt.Errorf("categories: scan: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("categories: list rows: %w", err)
	}
	return out, nil
}

// ListWithUnreadCounts returns all categories for userID with the count of
// unread entries across feeds in each category. Categories with no feeds (or
// no unread entries) report UnreadCount = 0 thanks to LEFT JOIN + COALESCE.
// One round trip; no N+1.
func (r *categoryRepo) ListWithUnreadCounts(ctx context.Context, userID int64) ([]CategoryWithUnreadCount, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT categories.id, categories.user_id, categories.name,
		        COALESCE(SUM(CASE WHEN entries.read = 0 THEN 1 ELSE 0 END), 0) AS unread_count
		   FROM categories
		   LEFT JOIN feeds   ON feeds.category_id = categories.id
		   LEFT JOIN entries ON entries.feed_id   = feeds.id
		  WHERE categories.user_id = ?
		  GROUP BY categories.id
		  ORDER BY categories.name`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("categories: list with unread: %w", err)
	}
	defer rows.Close()

	var out []CategoryWithUnreadCount
	for rows.Next() {
		var c CategoryWithUnreadCount
		if err := rows.Scan(&c.ID, &c.UserID, &c.Name, &c.UnreadCount); err != nil {
			return nil, fmt.Errorf("categories: scan: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("categories: list with unread rows: %w", err)
	}
	return out, nil
}

func (r *categoryRepo) Create(ctx context.Context, c *model.Category) error {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO categories(user_id, name) VALUES (?, ?)`,
		c.UserID, c.Name,
	)
	if err != nil {
		return fmt.Errorf("categories: create: %w", mapSQLiteErr(err))
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("categories: create: last insert id: %w", err)
	}
	c.ID = id
	return nil
}

func (r *categoryRepo) Rename(ctx context.Context, id int64, name string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE categories SET name = ? WHERE id = ?`,
		name, id,
	)
	if err != nil {
		return fmt.Errorf("categories: rename: %w", mapSQLiteErr(err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("categories: rename: rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("categories: %w: id=%d", ErrNotFound, id)
	}
	return nil
}

func (r *categoryRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM categories WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("categories: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("categories: delete: rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("categories: %w: id=%d", ErrNotFound, id)
	}
	return nil
}

// mapSQLiteErr converts mattn/go-sqlite3 constraint failures to store-level
// sentinels: UNIQUE violations -> ErrConflict, FOREIGN KEY violations ->
// ErrInvalid. Other errors pass through unchanged.
func mapSQLiteErr(err error) error {
	var serr sqlite3.Error
	if errors.As(err, &serr) {
		switch serr.ExtendedCode {
		case sqlite3.ErrConstraintUnique:
			return fmt.Errorf("%w: %s", ErrConflict, serr.Error())
		case sqlite3.ErrConstraintForeignKey:
			return fmt.Errorf("%w: %s", ErrInvalid, serr.Error())
		}
	}
	return err
}
