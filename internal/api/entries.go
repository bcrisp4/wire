package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/bcrisp4/wire/internal/model"
	"github.com/bcrisp4/wire/internal/store"
)

// registerEntryRoutes wires the entry-related REST endpoints on mux.
// Kept package-private so tests and Server share one source of truth.
func registerEntryRoutes(mux *http.ServeMux, repo store.EntriesAPI, logger *slog.Logger) {
	mux.Handle("GET /api/v1/entries", listEntriesHandler(repo, logger))
	mux.Handle("GET /api/v1/entries/{id}", getEntryHandler(repo, logger))
	mux.Handle("PUT /api/v1/entries/{id}", updateEntryHandler(repo, logger))
	mux.Handle("PUT /api/v1/entries/read", bulkMarkReadHandler(repo, logger))
}

func listEntriesHandler(repo store.EntriesAPI, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q, err := parseEntryQuery(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		entries, err := repo.List(r.Context(), q)
		if err != nil {
			logger.Error("entries.list", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		total, err := repo.CountList(r.Context(), q)
		if err != nil {
			logger.Error("entries.count", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
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

func getEntryHandler(repo store.EntriesAPI, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := parsePathID(w, r)
		if !ok {
			return
		}
		e, err := repo.Get(r.Context(), id)
		switch {
		case err == nil:
			writeJSON(w, http.StatusOK, e)
		case errors.Is(err, store.ErrNotFound):
			http.NotFound(w, r)
		default:
			logger.Error("entries.get", "err", err, "id", id)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	})
}

type updateEntryRequest struct {
	Read  *bool `json:"read"`
	Saved *bool `json:"saved"`
}

func updateEntryHandler(repo store.EntriesAPI, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := parsePathID(w, r)
		if !ok {
			return
		}
		var req updateEntryRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := dec.Decode(&struct{}{}); err != io.EOF {
			http.Error(w, "invalid json: trailing data", http.StatusBadRequest)
			return
		}
		err := repo.UpdateState(r.Context(), id, req.Read, req.Saved)
		switch {
		case err == nil:
			// fall through to fetch+write
		case errors.Is(err, store.ErrNotFound):
			http.NotFound(w, r)
			return
		default:
			logger.Error("entries.update", "err", err, "id", id)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		e, err := repo.Get(r.Context(), id)
		switch {
		case err == nil:
			writeJSON(w, http.StatusOK, e)
		case errors.Is(err, store.ErrNotFound):
			http.NotFound(w, r)
		default:
			logger.Error("entries.update.get", "err", err, "id", id)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	})
}

type bulkMarkReadRequest struct {
	FeedID     *int64 `json:"feed_id"`
	CategoryID *int64 `json:"category_id"`
}

func bulkMarkReadHandler(repo store.EntriesAPI, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req bulkMarkReadRequest
		// Empty body is allowed (marks all). Decode unconditionally so that
		// chunked requests (ContentLength == -1) are handled correctly, and
		// treat io.EOF as "no body provided".
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		switch err := dec.Decode(&req); {
		case err == nil:
			if err := dec.Decode(&struct{}{}); err != io.EOF {
				http.Error(w, "invalid json: trailing data", http.StatusBadRequest)
				return
			}
		case errors.Is(err, io.EOF):
			// empty body: scope = all
		default:
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
			logger.Error("entries.bulk_mark_read", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
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
