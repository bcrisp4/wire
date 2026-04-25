package store

import "errors"

// ErrNotFound is returned when a Get/Update/Delete targets a row that does not
// exist. Repos wrap this with fmt.Errorf("%w: ...") so callers can errors.Is-test
// it without importing database/sql.
var ErrNotFound = errors.New("store: not found")

// ErrConflict is returned when a write would violate a UNIQUE constraint
// (e.g. duplicate category name, duplicate feed_url for a user). Handlers
// translate this to HTTP 409 Conflict.
var ErrConflict = errors.New("store: conflict")

// ErrInvalid is returned when a write supplies a value the schema rejects
// (e.g. a foreign-key reference to a row that does not exist). Handlers
// translate this to HTTP 400 Bad Request.
var ErrInvalid = errors.New("store: invalid")
