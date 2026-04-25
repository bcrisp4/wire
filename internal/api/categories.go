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

// Single-user mode: hardcode user ID until auth lands. See PHASE1-PROMPT.md.
const defaultUserID int64 = 1

type categoryDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type categoryWriteReq struct {
	Name string `json:"name"`
}

func registerCategoryRoutes(mux *http.ServeMux, repo store.CategoryRepo, logger *slog.Logger) {
	mux.Handle("GET /api/v1/categories", categoriesList(repo, logger))
	mux.Handle("POST /api/v1/categories", categoriesCreate(repo, logger))
	mux.Handle("PUT /api/v1/categories/{id}", categoriesRename(repo, logger))
	mux.Handle("DELETE /api/v1/categories/{id}", categoriesDelete(repo, logger))
}

func categoriesList(repo store.CategoryRepo, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := defaultUserID
		cats, err := repo.List(r.Context(), userID)
		if err != nil {
			logger.Error("categories.list", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		out := make([]categoryDTO, 0, len(cats))
		for _, c := range cats {
			out = append(out, categoryDTO{ID: c.ID, Name: c.Name})
		}
		writeJSON(w, http.StatusOK, out)
	})
}

func categoriesCreate(repo store.CategoryRepo, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, ok := decodeCategoryWrite(w, r)
		if !ok {
			return
		}

		c := &model.Category{UserID: defaultUserID, Name: req.Name}
		switch err := repo.Create(r.Context(), c); {
		case err == nil:
			writeJSON(w, http.StatusCreated, categoryDTO{ID: c.ID, Name: c.Name})
		case errors.Is(err, store.ErrConflict):
			http.Error(w, "category already exists", http.StatusConflict)
		default:
			logger.Error("categories.create", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	})
}

func categoriesRename(repo store.CategoryRepo, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := parsePathID(w, r)
		if !ok {
			return
		}
		req, ok := decodeCategoryWrite(w, r)
		if !ok {
			return
		}

		switch err := repo.Rename(r.Context(), id, req.Name); {
		case err == nil:
			writeJSON(w, http.StatusOK, categoryDTO{ID: id, Name: req.Name})
		case errors.Is(err, store.ErrNotFound):
			http.Error(w, "category not found", http.StatusNotFound)
		case errors.Is(err, store.ErrConflict):
			http.Error(w, "category name already exists", http.StatusConflict)
		default:
			logger.Error("categories.rename", "err", err, "id", id)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	})
}

func categoriesDelete(repo store.CategoryRepo, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := parsePathID(w, r)
		if !ok {
			return
		}

		switch err := repo.Delete(r.Context(), id); {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, store.ErrNotFound):
			http.Error(w, "category not found", http.StatusNotFound)
		default:
			logger.Error("categories.delete", "err", err, "id", id)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	})
}

// decodeCategoryWrite parses {"name": "..."} from r.Body, trims whitespace,
// and rejects an empty result with 400. Trailing JSON after the first object
// (e.g. `{"name":"x"}{"name":"y"}`) is also rejected. Returns false if the
// response has already been written.
func decodeCategoryWrite(w http.ResponseWriter, r *http.Request) (categoryWriteReq, bool) {
	var req categoryWriteReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return req, false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "invalid JSON body: trailing data", http.StatusBadRequest)
		return req, false
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return req, false
	}
	return req, true
}

func parsePathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
