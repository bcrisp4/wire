package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/bcrisp4/wire/internal/model"
	"github.com/bcrisp4/wire/internal/store"
)

const defaultUserID int64 = 1

// registerEntryRoutes wires the entry-related REST endpoints on mux.
// Kept package-private so tests and Server share one source of truth.
func registerEntryRoutes(mux *http.ServeMux, repo store.EntriesAPI) {
	mux.Handle("GET /api/v1/entries", listEntriesHandler(repo))
	mux.Handle("GET /api/v1/entries/{id}", getEntryHandler(repo))
	mux.Handle("PUT /api/v1/entries/{id}", updateEntryHandler(repo))
	mux.Handle("PUT /api/v1/entries/read", bulkMarkReadHandler(repo))
}

func listEntriesHandler(repo store.EntriesAPI) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q, err := parseEntryQuery(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		entries, err := repo.List(r.Context(), q)
		if err != nil {
			http.Error(w, "list entries", http.StatusInternalServerError)
			return
		}
		total, err := repo.CountList(r.Context(), q)
		if err != nil {
			http.Error(w, "count entries", http.StatusInternalServerError)
			return
		}
		if entries == nil {
			entries = []model.Entry{} // JSON [] instead of null when empty
		}
		writeJSON(w, http.StatusOK, listResponse{
			Entries: entries,
			Total:   total,
			Limit:   store.BoundEntryListLimit(q.Limit),
			Offset:  q.Offset,
		})
	})
}

func getEntryHandler(repo store.EntriesAPI) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		e, err := repo.Get(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "get entry", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, e)
	})
}

type updateEntryRequest struct {
	Read  *bool `json:"read"`
	Saved *bool `json:"saved"`
}

func updateEntryHandler(repo store.EntriesAPI) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		var req updateEntryRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := repo.UpdateState(r.Context(), id, req.Read, req.Saved); err != nil {
			http.Error(w, "update entry", http.StatusInternalServerError)
			return
		}
		e, err := repo.Get(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "get entry", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, e)
	})
}

type bulkMarkReadRequest struct {
	FeedID     *int64 `json:"feed_id"`
	CategoryID *int64 `json:"category_id"`
}

func bulkMarkReadHandler(repo store.EntriesAPI) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req bulkMarkReadRequest
		// Empty body is allowed (marks all). Decode unconditionally so that
		// chunked requests (ContentLength == -1) are handled correctly, and
		// treat io.EOF as "no body provided".
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.FeedID != nil && req.CategoryID != nil {
			http.Error(w, "feed_id and category_id are mutually exclusive", http.StatusBadRequest)
			return
		}
		err := repo.BulkMarkRead(r.Context(), store.BulkReadScope{
			UserID:     defaultUserID,
			FeedID:     req.FeedID,
			CategoryID: req.CategoryID,
		})
		if err != nil {
			http.Error(w, "bulk mark read", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// --- helpers ---

type listResponse struct {
	Entries []model.Entry `json:"entries"`
	Total   int           `json:"total"`
	Limit   int           `json:"limit"`
	Offset  int           `json:"offset"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func parseEntryQuery(r *http.Request) (store.EntryQuery, error) {
	v := r.URL.Query()
	q := store.EntryQuery{
		UserID: defaultUserID,
		Status: v.Get("status"),
		Sort:   v.Get("sort"),
		Order:  v.Get("order"),
	}
	if q.Status == "" {
		q.Status = "unread"
	}
	switch q.Status {
	case "unread", "read", "all":
	default:
		return q, errors.New("invalid status (want unread|read|all)")
	}
	saved, err := parseOptionalBool(v.Get("saved"))
	if err != nil {
		return q, errors.New("invalid saved (want true|false|1|0)")
	}
	q.Saved = saved
	if q.FeedID, err = parseOptionalInt64(v.Get("feed_id")); err != nil {
		return q, errors.New("invalid feed_id")
	}
	if q.CategoryID, err = parseOptionalInt64(v.Get("category_id")); err != nil {
		return q, errors.New("invalid category_id")
	}
	if q.Limit, err = parseNonNegInt(v.Get("limit")); err != nil {
		return q, errors.New("invalid limit")
	}
	if q.Offset, err = parseNonNegInt(v.Get("offset")); err != nil {
		return q, errors.New("invalid offset")
	}
	return q, nil
}

func parseOptionalBool(s string) (*bool, error) {
	if s == "" {
		return nil, nil
	}
	switch strings.ToLower(s) {
	case "true", "1":
		t := true
		return &t, nil
	case "false", "0":
		f := false
		return &f, nil
	}
	return nil, errors.New("not a bool")
}

func parseOptionalInt64(s string) (*int64, error) {
	if s == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func parseNonNegInt(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, errors.New("not a non-negative int")
	}
	return n, nil
}
