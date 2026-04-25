package api

import (
	"bytes"
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
		// Cap the entire request body before any parsing. This guards both raw
		// and multipart paths; ParseMultipartForm only enforces an in-memory
		// limit and would otherwise spill unbounded data to disk.
		r.Body = http.MaxBytesReader(w, r.Body, maxOPMLBytes)

		body, cleanup, err := readOPMLBody(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer cleanup()
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

		// Buffer the encoded OPML so a serializer error returns a clean 500
		// instead of a half-written 200 download. Subscription lists are tiny
		// (typically KBs) so the extra allocation is harmless.
		var buf bytes.Buffer
		if err := opml.Write(&buf, subs); err != nil {
			log.Error("opml export: write", "err", err)
			http.Error(w, "export failed", http.StatusInternalServerError)
			return
		}
		// text/x-opml is the de-facto OPML MIME but "+xml" advertises the
		// underlying format, satisfying generic XML clients (and our tests).
		w.Header().Set("Content-Type", "text/x-opml+xml; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="wire-subscriptions.opml"`)
		_, _ = w.Write(buf.Bytes())
	})
}

// readOPMLBody returns the raw OPML document, sniffing both raw-body and
// multipart submissions. The cleanup function removes any temp files
// ParseMultipartForm may have spilled to disk and should be deferred by the
// caller alongside Close on the body.
func readOPMLBody(r *http.Request) (io.ReadCloser, func(), error) {
	noop := func() {}
	ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		ct = "" // fall through to default raw-body handling
	}
	if strings.HasPrefix(ct, "multipart/") {
		// maxMemory is the in-memory portion; anything larger spills to a
		// temp file. The overall body size is already capped by the
		// MaxBytesReader applied in the handler.
		if err := r.ParseMultipartForm(maxOPMLBytes); err != nil {
			return nil, noop, fmt.Errorf("opml: parse multipart: %w", err)
		}
		cleanup := func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			return nil, cleanup, fmt.Errorf("opml: missing 'file' field: %w", err)
		}
		return f, cleanup, nil
	}
	// Raw body — XML, OPML, or unspecified. Body is already wrapped in a
	// MaxBytesReader by the handler.
	return r.Body, noop, nil
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
		feedURL := strings.TrimSpace(s.FeedURL)
		if feedURL == "" {
			continue
		}
		// Per-iteration copy: every Feed gets its own *int64 so later
		// mutations through one feed's pointer can't alias another's.
		nextPoll := now
		categoryName := strings.TrimSpace(s.Category)
		var categoryID *int64
		if categoryName != "" {
			id, ok := catIDByName[categoryName]
			if !ok {
				cat := &model.Category{UserID: opmlUserID, Name: categoryName}
				if err := st.Categories().Create(ctx, cat); err != nil {
					// Another importer / a concurrent request could have
					// created this category in between our List and Create.
					// Re-list and retry the lookup so we coalesce instead of
					// failing.
					if isConflict(err) {
						refreshed, lerr := st.Categories().List(ctx, opmlUserID)
						if lerr != nil {
							return result, fmt.Errorf("opml: relist categories: %w", lerr)
						}
						for _, c := range refreshed {
							catIDByName[c.Name] = c.ID
						}
						id, ok = catIDByName[categoryName]
						if !ok {
							return result, fmt.Errorf("opml: category %q lost to race", categoryName)
						}
					} else {
						return result, fmt.Errorf("opml: create category %q: %w", categoryName, err)
					}
				} else {
					id = cat.ID
					catIDByName[categoryName] = id
					result.CategoriesCreated++
				}
			}
			categoryID = &id
		}

		title := strings.TrimSpace(s.Title)
		if title == "" {
			title = feedURL
		}
		feed := &model.Feed{
			UserID:       opmlUserID,
			CategoryID:   categoryID,
			Title:        title,
			FeedURL:      feedURL,
			SiteURL:      stringPtr(strings.TrimSpace(s.SiteURL)),
			PollInterval: 3600,
			NextPollAt:   &nextPoll,
			Crawler:      false,
		}
		if err := st.Feeds().Create(ctx, feed); err != nil {
			if isConflict(err) {
				result.SkippedDuplicates++
				continue
			}
			return result, fmt.Errorf("opml: create feed %q: %w", feedURL, err)
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

// isConflict reports whether err represents a uniqueness conflict from the
// store. We prefer the typed sentinel store.ErrConflict but fall back to
// substring matching the underlying driver/fake error text so this code keeps
// working until every repo wraps with the sentinel.
func isConflict(err error) bool {
	if errors.Is(err, store.ErrConflict) {
		return true
	}
	for e := err; e != nil; e = errors.Unwrap(e) {
		if strings.Contains(strings.ToLower(e.Error()), "unique constraint") {
			return true
		}
	}
	return false
}
