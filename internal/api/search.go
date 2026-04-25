package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/bcrisp4/wire/internal/model"
	"github.com/bcrisp4/wire/internal/store"
)

const (
	searchDefaultLimit = 50
	searchMaxLimit     = 100
)

// searchHandler implements GET /api/v1/search?q=<query>&limit=<n>&offset=<n>.
//
// Response: {"entries":[...],"limit":L,"offset":O,"query":"...","has_more":bool}.
//
// `total` is intentionally omitted: against an FTS5 contentless index, counting
// all matches would require running the query a second time as
// SELECT COUNT(*) FROM (...), which is roughly as expensive as the search
// itself. `has_more` is computed locally from len(entries)==limit — approximate
// but free, and good enough for a "load more" UI.
//
// log may be nil; callers without a logger (most tests) pass nil.
func searchHandler(s store.Store, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		query := strings.TrimSpace(q.Get("q"))
		if query == "" {
			http.Error(w, "missing query parameter q", http.StatusBadRequest)
			return
		}

		limit := clampPositiveInt(q.Get("limit"), searchDefaultLimit, searchMaxLimit)
		offset := parseNonNegativeInt(q.Get("offset"), 0)

		const userID = int64(1)
		entries, err := s.Entries().Search(r.Context(), userID, query, limit, offset)
		if err != nil {
			if log != nil {
				log.Error("search", "err", fmt.Errorf("search: %w", err), "q", query)
			}
			http.Error(w, "search failed", http.StatusInternalServerError)
			return
		}
		if entries == nil {
			entries = []model.Entry{}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Entries []model.Entry `json:"entries"`
			Limit   int           `json:"limit"`
			Offset  int           `json:"offset"`
			Query   string        `json:"query"`
			HasMore bool          `json:"has_more"`
		}{
			Entries: entries,
			Limit:   limit,
			Offset:  offset,
			Query:   query,
			HasMore: len(entries) == limit,
		})
	})
}

// clampPositiveInt returns def for blank/garbage/non-positive input, and caps
// any larger value at max.
func clampPositiveInt(raw string, def, max int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func parseNonNegativeInt(raw string, def int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return def
	}
	return n
}
