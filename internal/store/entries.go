package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/mattn/go-sqlite3"

	"github.com/bcrisp4/wire/internal/model"
)

// ErrDuplicateEntry is returned (wrapped) by Insert when the UNIQUE(feed_id, hash)
// constraint fails. Callers test with errors.Is(err, ErrDuplicateEntry).
var ErrDuplicateEntry = errors.New("duplicate hash")

const (
	defaultListLimit = 50
	maxListLimit     = 200

	statusUnread = "unread"
	statusRead   = "read"
	statusAll    = "all"
)

// EntriesAPI is the surface required by HTTP handlers: the EntryRepo interface
// plus a CountList helper used to populate pagination totals. Handlers depend
// on this narrower interface (defined in store to keep the implementation
// import-free).
type EntriesAPI interface {
	EntryRepo
	CountList(ctx context.Context, q EntryQuery) (int, error)
}

// NewEntryRepo returns a sqlite-backed EntriesAPI. The handler package depends
// on this constructor; tests also use it directly.
func NewEntryRepo(db *sql.DB) EntriesAPI { return &entryRepo{db: db} }

type entryRepo struct{ db *sql.DB }

// listColumnsQualified excludes content (the heaviest column) — list endpoints
// return summaries only. Get returns full content via getColumns.
const listColumnsQualified = `entries.id, entries.feed_id, entries.user_id, entries.hash, entries.title,
        entries.url, entries.comments_url, entries.author, entries.summary,
        entries.published_at, entries.reading_time, entries.read, entries.read_at,
        entries.saved, entries.saved_at, entries.created_at, entries.changed_at`

const getColumns = `id, feed_id, user_id, hash, title, url, comments_url, author, summary, content,
        published_at, reading_time, read, read_at, saved, saved_at, created_at, changed_at`

// buildEntryFilter returns the JOIN clause, WHERE conditions joined with AND,
// and bound args common to List and CountList.
func buildEntryFilter(q EntryQuery) (join, where string, args []any) {
	conds := []string{"entries.user_id = ?"}
	args = []any{q.UserID}

	switch q.Status {
	case statusRead:
		conds = append(conds, "entries.read = 1")
	case statusAll:
		// no read filter
	default:
		conds = append(conds, "entries.read = 0")
	}
	if q.Saved != nil {
		conds = append(conds, "entries.saved = ?")
		args = append(args, boolToInt(*q.Saved))
	}
	if q.FeedID != nil {
		conds = append(conds, "entries.feed_id = ?")
		args = append(args, *q.FeedID)
	}
	if q.CategoryID != nil {
		join = " JOIN feeds f ON entries.feed_id = f.id"
		conds = append(conds, "f.category_id = ?")
		args = append(args, *q.CategoryID)
	}
	return join, strings.Join(conds, " AND "), args
}

func (r *entryRepo) List(ctx context.Context, q EntryQuery) ([]model.Entry, error) {
	join, where, args := buildEntryFilter(q)

	sortCol := "published_at"
	if q.Sort == "created_at" {
		sortCol = "created_at"
	}
	order := "DESC"
	if strings.EqualFold(q.Order, "asc") {
		order = "ASC"
	}

	limit, offset := boundLimitOffset(q.Limit, q.Offset)

	// Secondary sort by id keeps order deterministic when published_at is NULL
	// or duplicated.
	query := fmt.Sprintf(
		"SELECT %s FROM entries%s WHERE %s ORDER BY entries.%s %s, entries.id %s LIMIT ? OFFSET ?",
		listColumnsQualified, join, where, sortCol, order, order,
	)
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("entries: %w", err)
	}
	defer rows.Close()

	var out []model.Entry
	for rows.Next() {
		e, err := scanEntryList(rows)
		if err != nil {
			return nil, fmt.Errorf("entries: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("entries: %w", err)
	}
	return out, nil
}

// CountList returns the total row count for the same filter as List, ignoring
// limit/offset. Exposed so handlers can supply pagination totals.
func (r *entryRepo) CountList(ctx context.Context, q EntryQuery) (int, error) {
	join, where, args := buildEntryFilter(q)
	var n int
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries"+join+" WHERE "+where, args...).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("entries: %w", err)
	}
	return n, nil
}

func (r *entryRepo) Get(ctx context.Context, id int64) (*model.Entry, error) {
	row := r.db.QueryRowContext(ctx, "SELECT "+getColumns+" FROM entries WHERE id = ?", id)
	e, err := scanEntryFull(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("entries: %w", err)
	}
	return &e, nil
}

func (r *entryRepo) Insert(ctx context.Context, e *model.Entry) error {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO entries
            (feed_id, user_id, hash, title, url, comments_url, author, summary, content,
             published_at, reading_time, read, saved)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.FeedID, e.UserID, e.Hash, e.Title, e.URL, e.CommentsURL, e.Author, e.Summary, e.Content,
		e.PublishedAt, e.ReadingTime, boolToInt(e.Read), boolToInt(e.Saved),
	)
	if err != nil {
		var sqlErr sqlite3.Error
		if errors.As(err, &sqlErr) && sqlErr.ExtendedCode == sqlite3.ErrConstraintUnique {
			return fmt.Errorf("entries: %w: %v", ErrDuplicateEntry, err)
		}
		return fmt.Errorf("entries: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("entries: %w", err)
	}
	e.ID = id
	return nil
}

func (r *entryRepo) UpdateState(ctx context.Context, id int64, read, saved *bool) error {
	if read == nil && saved == nil {
		return nil
	}
	var sets []string
	var args []any
	if read != nil {
		sets = append(sets, "read = ?")
		args = append(args, boolToInt(*read))
		if *read {
			sets = append(sets, "read_at = strftime('%s','now')")
		} else {
			sets = append(sets, "read_at = NULL")
		}
	}
	if saved != nil {
		sets = append(sets, "saved = ?")
		args = append(args, boolToInt(*saved))
		if *saved {
			sets = append(sets, "saved_at = strftime('%s','now')")
		} else {
			sets = append(sets, "saved_at = NULL")
		}
	}
	sets = append(sets, "changed_at = strftime('%s','now')")
	args = append(args, id)
	query := "UPDATE entries SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	if _, err := r.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("entries: %w", err)
	}
	return nil
}

func (r *entryRepo) BulkMarkRead(ctx context.Context, scope BulkReadScope) error {
	conds := []string{"user_id = ?", "read = 0"}
	args := []any{scope.UserID}
	if scope.FeedID != nil {
		conds = append(conds, "feed_id = ?")
		args = append(args, *scope.FeedID)
	}
	if scope.CategoryID != nil {
		conds = append(conds, "feed_id IN (SELECT id FROM feeds WHERE category_id = ? AND user_id = ?)")
		args = append(args, *scope.CategoryID, scope.UserID)
	}
	query := "UPDATE entries SET read = 1, read_at = strftime('%s','now'), changed_at = strftime('%s','now') WHERE " + strings.Join(conds, " AND ")
	if _, err := r.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("entries: %w", err)
	}
	return nil
}

func (r *entryRepo) Search(ctx context.Context, userID int64, query string, limit, offset int) ([]model.Entry, error) {
	limit, offset = boundLimitOffset(limit, offset)
	q := `SELECT ` + listColumnsQualified + `
        FROM entries_fts
        JOIN entries ON entries_fts.rowid = entries.id
        WHERE entries_fts MATCH ? AND entries.user_id = ?
        ORDER BY rank
        LIMIT ? OFFSET ?`
	rows, err := r.db.QueryContext(ctx, q, query, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("entries: %w", err)
	}
	defer rows.Close()
	var out []model.Entry
	for rows.Next() {
		e, err := scanEntryList(rows)
		if err != nil {
			return nil, fmt.Errorf("entries: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("entries: %w", err)
	}
	return out, nil
}

// --- helpers ---

// BoundEntryListLimit clamps a caller-supplied limit to the same range the
// repository applies internally. Exposed so HTTP handlers can echo back the
// effective page size in pagination responses.
func BoundEntryListLimit(n int) int {
	if n <= 0 {
		return defaultListLimit
	}
	if n > maxListLimit {
		return maxListLimit
	}
	return n
}

func boundLimitOffset(limit, offset int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	return BoundEntryListLimit(limit), offset
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEntryList(s rowScanner) (model.Entry, error) {
	var e model.Entry
	var read, saved int
	err := s.Scan(
		&e.ID, &e.FeedID, &e.UserID, &e.Hash, &e.Title, &e.URL, &e.CommentsURL, &e.Author, &e.Summary,
		&e.PublishedAt, &e.ReadingTime, &read, &e.ReadAt, &saved, &e.SavedAt, &e.CreatedAt, &e.ChangedAt,
	)
	if err != nil {
		return e, err
	}
	e.Read = read != 0
	e.Saved = saved != 0
	return e, nil
}

func scanEntryFull(s rowScanner) (model.Entry, error) {
	var e model.Entry
	var read, saved int
	err := s.Scan(
		&e.ID, &e.FeedID, &e.UserID, &e.Hash, &e.Title, &e.URL, &e.CommentsURL, &e.Author, &e.Summary, &e.Content,
		&e.PublishedAt, &e.ReadingTime, &read, &e.ReadAt, &saved, &e.SavedAt, &e.CreatedAt, &e.ChangedAt,
	)
	if err != nil {
		return e, err
	}
	e.Read = read != 0
	e.Saved = saved != 0
	return e, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
