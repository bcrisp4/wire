package store

import "database/sql"

// sqliteStore is the concrete Store backed by a single *sql.DB. The DB is
// owned by Honker (jobs.HonkerBackend.RawDB()); Store.Close is a no-op so the
// connection lifetime is governed by the owner.
type sqliteStore struct {
	db *sql.DB

	users      *userRepo
	categories *categoryRepo
	feeds      *feedRepo
	entries    *entryRepo
	icons      *iconRepo
	tombstones *tombstoneRepo
	enclosures *enclosureRepo
}

// New returns a Store backed by db. db is owned by the caller (Honker in
// production); Store.Close does NOT close it.
func New(db *sql.DB) Store {
	return &sqliteStore{
		db:         db,
		users:      &userRepo{db: db},
		categories: &categoryRepo{db: db},
		feeds:      &feedRepo{db: db},
		entries:    &entryRepo{db: db},
		icons:      &iconRepo{db: db},
		tombstones: &tombstoneRepo{db: db},
		enclosures: &enclosureRepo{db: db},
	}
}

func (s *sqliteStore) Users() UserRepo           { return s.users }
func (s *sqliteStore) Categories() CategoryRepo  { return s.categories }
func (s *sqliteStore) Feeds() FeedRepo           { return s.feeds }
func (s *sqliteStore) Entries() EntryRepo        { return s.entries }
func (s *sqliteStore) Icons() IconRepo           { return s.icons }
func (s *sqliteStore) Tombstones() TombstoneRepo { return s.tombstones }
func (s *sqliteStore) Enclosures() EnclosureRepo { return s.enclosures }

// Close is a no-op; Honker owns the *sql.DB lifecycle.
func (s *sqliteStore) Close() error { return nil }
