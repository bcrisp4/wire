package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/bcrisp4/wire/internal/model"
	"github.com/bcrisp4/wire/internal/opml"
	"github.com/bcrisp4/wire/internal/store"
)

// opmlUserID is the single-user placeholder until auth lands. Schema is
// multi-user ready (user_id columns exist throughout) but Phase 1 hardcodes 1.
const opmlUserID int64 = 1

// maxOPMLBytes caps the import body. Real OPML files are tiny (KBs); this
// keeps malicious uploads from exhausting memory.
const maxOPMLBytes = 5 << 20 // 5 MiB

// registerOPMLRoutes wires the OPML import/export endpoints onto mux.
func registerOPMLRoutes(mux *http.ServeMux, st store.Store, log *slog.Logger) {
	mux.Handle("POST /api/v1/opml/import", opmlImportHandler(st, log))
	mux.Handle("GET /api/v1/opml/export", opmlExportHandler(st, log))
}

func opmlImportHandler(st store.Store, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := readOPMLBody(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer body.Close()

		subs, err := opml.Parse(body)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid opml: %s", err), http.StatusBadRequest)
			return
		}

		result, err := importSubscriptions(r.Context(), st, subs)
		if err != nil {
			log.Error("opml import", "err", err)
			http.Error(w, "import failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})
}

func opmlExportHandler(st store.Store, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		feeds, err := st.Feeds().List(r.Context(), opmlUserID)
		if err != nil {
			log.Error("opml export: feeds list", "err", err)
			http.Error(w, "export failed", http.StatusInternalServerError)
			return
		}
		cats, err := st.Categories().List(r.Context(), opmlUserID)
		if err != nil {
			log.Error("opml export: categories list", "err", err)
			http.Error(w, "export failed", http.StatusInternalServerError)
			return
		}
		catNames := make(map[int64]string, len(cats))
		for _, c := range cats {
			catNames[c.ID] = c.Name
		}
		subs := make([]opml.Subscription, 0, len(feeds))
		for _, f := range feeds {
			s := opml.Subscription{FeedURL: f.FeedURL, Title: f.Title}
			if f.SiteURL != nil {
				s.SiteURL = *f.SiteURL
			}
			if f.CategoryID != nil {
				s.Category = catNames[*f.CategoryID]
			}
			subs = append(subs, s)
		}

		// text/x-opml is the de-facto OPML MIME but "+xml" advertises the
		// underlying format, satisfying generic XML clients (and our tests).
		w.Header().Set("Content-Type", "text/x-opml+xml; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="wire-subscriptions.opml"`)
		if err := opml.Write(w, subs); err != nil {
			log.Error("opml export: write", "err", err)
		}
	})
}

// readOPMLBody returns the raw OPML document, sniffing both raw-body and
// multipart submissions. The caller is responsible for closing the result.
func readOPMLBody(r *http.Request) (io.ReadCloser, error) {
	ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		ct = "" // fall through to default raw-body handling
	}
	if strings.HasPrefix(ct, "multipart/") {
		if err := r.ParseMultipartForm(maxOPMLBytes); err != nil {
			return nil, fmt.Errorf("opml: parse multipart: %w", err)
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			return nil, fmt.Errorf("opml: missing 'file' field: %w", err)
		}
		return f, nil
	}
	// Raw body — XML, OPML, or unspecified.
	return io.NopCloser(http.MaxBytesReader(nil, r.Body, maxOPMLBytes)), nil
}

type importResult struct {
	Imported          int `json:"imported"`
	SkippedDuplicates int `json:"skipped_duplicates"`
	CategoriesCreated int `json:"categories_created"`
}

func importSubscriptions(ctx context.Context, st store.Store, subs []opml.Subscription) (importResult, error) {
	var result importResult

	existing, err := st.Categories().List(ctx, opmlUserID)
	if err != nil {
		return result, fmt.Errorf("opml: list categories: %w", err)
	}
	catIDByName := make(map[string]int64, len(existing))
	for _, c := range existing {
		catIDByName[c.Name] = c.ID
	}

	now := time.Now().Unix()
	for _, s := range subs {
		if strings.TrimSpace(s.FeedURL) == "" {
			continue
		}
		var categoryID *int64
		if s.Category != "" {
			id, ok := catIDByName[s.Category]
			if !ok {
				cat := &model.Category{UserID: opmlUserID, Name: s.Category}
				if err := st.Categories().Create(ctx, cat); err != nil {
					return result, fmt.Errorf("opml: create category %q: %w", s.Category, err)
				}
				id = cat.ID
				catIDByName[s.Category] = id
				result.CategoriesCreated++
			}
			categoryID = &id
		}

		title := s.Title
		if title == "" {
			title = s.FeedURL
		}
		feed := &model.Feed{
			UserID:       opmlUserID,
			CategoryID:   categoryID,
			Title:        title,
			FeedURL:      s.FeedURL,
			SiteURL:      stringPtr(s.SiteURL),
			PollInterval: 3600,
			NextPollAt:   &now,
			Crawler:      false,
		}
		if err := st.Feeds().Create(ctx, feed); err != nil {
			if isUniqueViolation(err) {
				result.SkippedDuplicates++
				continue
			}
			return result, fmt.Errorf("opml: create feed %q: %w", s.FeedURL, err)
		}
		result.Imported++
	}
	return result, nil
}

func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// isUniqueViolation matches go-sqlite3's "UNIQUE constraint failed: ..."
// message and the equivalent string our in-memory test fakes produce. We
// match the message rather than the typed error so handlers don't depend on
// the driver package.
func isUniqueViolation(err error) bool {
	for e := err; e != nil; e = errors.Unwrap(e) {
		if strings.Contains(strings.ToLower(e.Error()), "unique constraint") {
			return true
		}
	}
	return false
}
