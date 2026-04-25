package store

import (
	"context"

	"github.com/bcrisp4/wire/internal/model"
)

// Store is the umbrella accessor for per-resource repositories.
// Phase 1 will add concrete sqlite-backed implementations behind these interfaces.
type Store interface {
	Users() UserRepo
	Categories() CategoryRepo
	Feeds() FeedRepo
	Entries() EntryRepo
	Icons() IconRepo
	Tombstones() TombstoneRepo
	Enclosures() EnclosureRepo
	Close() error
}

type UserRepo interface {
	Get(ctx context.Context, id int64) (*model.User, error)
	Update(ctx context.Context, u *model.User) error
}

type CategoryRepo interface {
	List(ctx context.Context, userID int64) ([]model.Category, error)
	Create(ctx context.Context, c *model.Category) error
	Rename(ctx context.Context, id int64, name string) error
	Delete(ctx context.Context, id int64) error
}

type FeedRepo interface {
	List(ctx context.Context, userID int64) ([]model.Feed, error)
	Get(ctx context.Context, id int64) (*model.Feed, error)
	Create(ctx context.Context, f *model.Feed) error
	Update(ctx context.Context, f *model.Feed) error
	Delete(ctx context.Context, id int64) error
	DueForPolling(ctx context.Context, now int64, limit int) ([]model.Feed, error)
}

type EntryRepo interface {
	List(ctx context.Context, q EntryQuery) ([]model.Entry, error)
	Get(ctx context.Context, id int64) (*model.Entry, error)
	Insert(ctx context.Context, e *model.Entry) error
	UpdateState(ctx context.Context, id int64, read, saved *bool) error
	BulkMarkRead(ctx context.Context, scope BulkReadScope) error
	Search(ctx context.Context, userID int64, query string, limit, offset int) ([]model.Entry, error)
}

type EntryQuery struct {
	UserID     int64
	Status     string // "unread" | "read" | "all"
	Saved      *bool
	FeedID     *int64
	CategoryID *int64
	Sort       string // "published_at" | "created_at"
	Order      string // "asc" | "desc"
	Limit      int
	Offset     int
}

type BulkReadScope struct {
	UserID     int64
	FeedID     *int64
	CategoryID *int64
}

type IconRepo interface {
	GetByHash(ctx context.Context, hash string) (*model.Icon, error)
	Insert(ctx context.Context, i *model.Icon) (int64, error)
}

type TombstoneRepo interface {
	Has(ctx context.Context, feedID int64, hash string) (bool, error)
	Insert(ctx context.Context, feedID int64, hash string) error
}

type EnclosureRepo interface {
	List(ctx context.Context, entryID int64) ([]model.Enclosure, error)
	Insert(ctx context.Context, e *model.Enclosure) error
}
