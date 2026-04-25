// Package model defines the core domain entities. Schema lives in internal/store/schema.
//
// Pointer fields represent NULL-able columns: distinguishing NULL from zero
// avoids sql.NullX clutter at the model layer. Conversions happen in the
// storage layer.
package model

type User struct {
	ID             int64
	Username       string
	Theme          string
	Font           string
	EntriesPerPage int
	DefaultSort    string
	DefaultOrder   string
	CreatedAt      int64
}

type Category struct {
	ID     int64
	UserID int64
	Name   string
}

type Icon struct {
	ID       int64
	Hash     string
	MimeType string
	Content  []byte
}

type Feed struct {
	ID                 int64
	UserID             int64
	CategoryID         *int64
	IconID             *int64
	Title              string
	FeedURL            string
	SiteURL            *string
	Description        *string
	ETag               *string
	LastModified       *string
	LastPolledAt       *int64
	NextPollAt         *int64
	PollInterval       int
	ErrorCount         int
	LastError          *string
	WeeklyEntryCount   int
	Crawler            bool
	ScraperRules       *string
	Disabled           bool
	IgnoreEntryUpdates bool
	CreatedAt          int64
	UpdatedAt          int64
}

type Entry struct {
	ID          int64
	FeedID      int64
	UserID      int64
	Hash        string
	Title       string
	URL         *string
	CommentsURL *string
	Author      *string
	Summary     *string
	Content     *string
	PublishedAt *int64
	ReadingTime int
	Read        bool
	ReadAt      *int64
	Saved       bool
	SavedAt     *int64
	CreatedAt   int64
	ChangedAt   int64
}

type EntryTombstone struct {
	FeedID    int64
	Hash      string
	CreatedAt int64
}

type Enclosure struct {
	ID       int64
	EntryID  int64
	URL      string
	MimeType string
	Size     int64
}
