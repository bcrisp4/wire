// Package-private implementation — Phase 1 Unit 6 (Feeds REST) replaces the
// Unit 0 stub of this file with a real SQLite-backed FeedRepo.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/bcrisp4/wire/internal/model"
)

type feedRepo struct {
	db *sql.DB
}

const feedColumns = `id, user_id, category_id, icon_id, title, feed_url, site_url,
	description, etag, last_modified, last_polled_at, next_poll_at, poll_interval,
	error_count, last_error, weekly_entry_count, crawler, scraper_rules, disabled,
	ignore_entry_updates, created_at, updated_at`

// scanFeed reads one row's columns (in feedColumns order) into a model.Feed,
// translating sql.NullX into the *string / *int64 nullable fields.
func scanFeed(scanner interface{ Scan(...any) error }) (*model.Feed, error) {
	var f model.Feed
	var (
		categoryID    sql.NullInt64
		iconID        sql.NullInt64
		siteURL       sql.NullString
		description   sql.NullString
		etag          sql.NullString
		lastModified  sql.NullString
		lastPolledAt  sql.NullInt64
		nextPollAt    sql.NullInt64
		lastError     sql.NullString
		scraperRules  sql.NullString
	)
	if err := scanner.Scan(
		&f.ID, &f.UserID, &categoryID, &iconID, &f.Title, &f.FeedURL, &siteURL,
		&description, &etag, &lastModified, &lastPolledAt, &nextPollAt, &f.PollInterval,
		&f.ErrorCount, &lastError, &f.WeeklyEntryCount, &f.Crawler, &scraperRules, &f.Disabled,
		&f.IgnoreEntryUpdates, &f.CreatedAt, &f.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if categoryID.Valid {
		f.CategoryID = &categoryID.Int64
	}
	if iconID.Valid {
		f.IconID = &iconID.Int64
	}
	if siteURL.Valid {
		f.SiteURL = &siteURL.String
	}
	if description.Valid {
		f.Description = &description.String
	}
	if etag.Valid {
		f.ETag = &etag.String
	}
	if lastModified.Valid {
		f.LastModified = &lastModified.String
	}
	if lastPolledAt.Valid {
		f.LastPolledAt = &lastPolledAt.Int64
	}
	if nextPollAt.Valid {
		f.NextPollAt = &nextPollAt.Int64
	}
	if lastError.Valid {
		f.LastError = &lastError.String
	}
	if scraperRules.Valid {
		f.ScraperRules = &scraperRules.String
	}
	return &f, nil
}

// nullable returns nil for nil pointers (so database/sql writes NULL) and the
// pointed-to value otherwise. Avoids a forest of sql.NullX conversions on insert.
func nullable[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func (r *feedRepo) List(ctx context.Context, userID int64) ([]model.Feed, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+feedColumns+` FROM feeds WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, fmt.Errorf("feeds.List: %w", err)
	}
	defer rows.Close()
	var out []model.Feed
	for rows.Next() {
		f, err := scanFeed(rows)
		if err != nil {
			return nil, fmt.Errorf("feeds.List: %w", err)
		}
		out = append(out, *f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("feeds.List: %w", err)
	}
	return out, nil
}

func (r *feedRepo) Get(ctx context.Context, id int64) (*model.Feed, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+feedColumns+` FROM feeds WHERE id = ?`, id)
	f, err := scanFeed(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: feed id=%d", ErrNotFound, id)
		}
		return nil, fmt.Errorf("feeds.Get: %w", err)
	}
	return f, nil
}

// Create inserts the row and populates f.ID, f.CreatedAt, and f.UpdatedAt from
// the SQLite-side defaults via RETURNING (single round-trip).
func (r *feedRepo) Create(ctx context.Context, f *model.Feed) error {
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO feeds (
			user_id, category_id, icon_id, title, feed_url, site_url, description,
			etag, last_modified, last_polled_at, next_poll_at, poll_interval,
			error_count, last_error, weekly_entry_count, crawler, scraper_rules,
			disabled, ignore_entry_updates
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 RETURNING id, created_at, updated_at`,
		f.UserID, nullable(f.CategoryID), nullable(f.IconID), f.Title, f.FeedURL,
		nullable(f.SiteURL), nullable(f.Description),
		nullable(f.ETag), nullable(f.LastModified),
		nullable(f.LastPolledAt), nullable(f.NextPollAt), f.PollInterval,
		f.ErrorCount, nullable(f.LastError), f.WeeklyEntryCount, f.Crawler,
		nullable(f.ScraperRules), f.Disabled, f.IgnoreEntryUpdates,
	).Scan(&f.ID, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return fmt.Errorf("feeds.Create: %w", err)
	}
	return nil
}

// Update writes mutable columns and bumps updated_at to unixepoch(). user_id,
// feed_url, and created_at are immutable and untouched even if the caller
// modified them on the struct. RETURNING gives us back the new updated_at and
// also acts as the row-existence check (sql.ErrNoRows on a missing id).
func (r *feedRepo) Update(ctx context.Context, f *model.Feed) error {
	err := r.db.QueryRowContext(ctx,
		`UPDATE feeds SET
			category_id = ?, icon_id = ?, title = ?, site_url = ?, description = ?,
			etag = ?, last_modified = ?, last_polled_at = ?, next_poll_at = ?,
			poll_interval = ?, error_count = ?, last_error = ?, weekly_entry_count = ?,
			crawler = ?, scraper_rules = ?, disabled = ?, ignore_entry_updates = ?,
			updated_at = unixepoch()
		 WHERE id = ?
		 RETURNING updated_at`,
		nullable(f.CategoryID), nullable(f.IconID), f.Title,
		nullable(f.SiteURL), nullable(f.Description),
		nullable(f.ETag), nullable(f.LastModified),
		nullable(f.LastPolledAt), nullable(f.NextPollAt),
		f.PollInterval, f.ErrorCount, nullable(f.LastError), f.WeeklyEntryCount,
		f.Crawler, nullable(f.ScraperRules), f.Disabled, f.IgnoreEntryUpdates,
		f.ID,
	).Scan(&f.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: feed id=%d", ErrNotFound, f.ID)
		}
		return fmt.Errorf("feeds.Update: %w", err)
	}
	return nil
}

func (r *feedRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM feeds WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("feeds.Delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("feeds.Delete: rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: feed id=%d", ErrNotFound, id)
	}
	return nil
}

// DueForPolling returns feeds whose next_poll_at has passed, excluding
// disabled feeds and those past the error-count circuit breaker.
// Ordering matches the partial index idx_feeds_next_poll for index-only scans.
func (r *feedRepo) DueForPolling(ctx context.Context, now int64, limit int) ([]model.Feed, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+feedColumns+` FROM feeds
		  WHERE next_poll_at <= ? AND disabled = 0 AND error_count < 10
		  ORDER BY next_poll_at
		  LIMIT ?`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("feeds.DueForPolling: %w", err)
	}
	defer rows.Close()
	var out []model.Feed
	for rows.Next() {
		f, err := scanFeed(rows)
		if err != nil {
			return nil, fmt.Errorf("feeds.DueForPolling: %w", err)
		}
		out = append(out, *f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("feeds.DueForPolling: %w", err)
	}
	return out, nil
}
