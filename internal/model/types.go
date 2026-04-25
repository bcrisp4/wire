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
	ID          int64   `json:"id"`
	FeedID      int64   `json:"feed_id"`
	UserID      int64   `json:"user_id"`
	Hash        string  `json:"hash"`
	Title       string  `json:"title"`
	URL         *string `json:"url"`
	CommentsURL *string `json:"comments_url"`
	Author      *string `json:"author"`
	Summary     *string `json:"summary"`
	Content     *string `json:"content"`
	PublishedAt *int64  `json:"published_at"`
	ReadingTime int     `json:"reading_time"`
	Read        bool    `json:"read"`
	ReadAt      *int64  `json:"read_at"`
	Saved       bool    `json:"saved"`
	SavedAt     *int64  `json:"saved_at"`
	CreatedAt   int64   `json:"created_at"`
	ChangedAt   int64   `json:"changed_at"`
}

type EntryTombstone struct {
	FeedID    int64  `json:"feed_id"`
	Hash      string `json:"hash"`
	CreatedAt int64  `json:"created_at"`
}

type Enclosure struct {
	ID       int64  `json:"id"`
	EntryID  int64  `json:"entry_id"`
	URL      string `json:"url"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
}
