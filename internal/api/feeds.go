package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bcrisp4/wire/internal/jobs"
	"github.com/bcrisp4/wire/internal/model"
	"github.com/bcrisp4/wire/internal/store"
)

// queueFeedPoll is the queue name for feed polling jobs. Unit 4 is expected to
// promote this to jobs.QueueFeedPoll; until then we use the literal string so
// Unit 6 can land independently.
//
// TODO(unit-4): replace with jobs.QueueFeedPoll once it lands.
const queueFeedPoll = "feed.poll"

// feedJSON is the wire shape for a feed. Field names match design.md §6.
// etag and last_modified are omitted intentionally — they are internal HTTP
// cache state, not API surface.
type feedJSON struct {
	ID                 int64   `json:"id"`
	FeedURL            string  `json:"feed_url"`
	SiteURL            *string `json:"site_url"`
	Title              string  `json:"title"`
	Description        *string `json:"description"`
	CategoryID         *int64  `json:"category_id"`
	IconID             *int64  `json:"icon_id"`
	LastPolledAt       *int64  `json:"last_polled_at"`
	NextPollAt         *int64  `json:"next_poll_at"`
	PollInterval       int     `json:"poll_interval"`
	ErrorCount         int     `json:"error_count"`
	LastError          *string `json:"last_error"`
	Crawler            bool    `json:"crawler"`
	ScraperRules       *string `json:"scraper_rules"`
	Disabled           bool    `json:"disabled"`
	IgnoreEntryUpdates bool    `json:"ignore_entry_updates"`
	CreatedAt          int64   `json:"created_at"`
	UpdatedAt          int64   `json:"updated_at"`
}

func toFeedJSON(f *model.Feed) feedJSON {
	return feedJSON{
		ID:                 f.ID,
		FeedURL:            f.FeedURL,
		SiteURL:            f.SiteURL,
		Title:              f.Title,
		Description:        f.Description,
		CategoryID:         f.CategoryID,
		IconID:             f.IconID,
		LastPolledAt:       f.LastPolledAt,
		NextPollAt:         f.NextPollAt,
		PollInterval:       f.PollInterval,
		ErrorCount:         f.ErrorCount,
		LastError:          f.LastError,
		Crawler:            f.Crawler,
		ScraperRules:       f.ScraperRules,
		Disabled:           f.Disabled,
		IgnoreEntryUpdates: f.IgnoreEntryUpdates,
		CreatedAt:          f.CreatedAt,
		UpdatedAt:          f.UpdatedAt,
	}
}

// registerFeedRoutes attaches all /api/v1/feeds endpoints to mux.
func (s *Server) registerFeedRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/feeds", s.handleFeedsList)
	mux.HandleFunc("POST /api/v1/feeds", s.handleFeedsCreate)
	mux.HandleFunc("GET /api/v1/feeds/{id}", s.handleFeedGet)
	mux.HandleFunc("PUT /api/v1/feeds/{id}", s.handleFeedUpdate)
	mux.HandleFunc("DELETE /api/v1/feeds/{id}", s.handleFeedDelete)
	mux.HandleFunc("POST /api/v1/feeds/{id}/refresh", s.handleFeedRefresh)
}

func (s *Server) handleFeedsList(w http.ResponseWriter, r *http.Request) {
	feeds, err := s.opts.Store.Feeds().List(r.Context(), defaultUserID)
	if err != nil {
		s.serverError(w, r, "feeds.list", err)
		return
	}
	out := make([]feedJSON, 0, len(feeds))
	for i := range feeds {
		out = append(out, toFeedJSON(&feeds[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

type createFeedReq struct {
	FeedURL    string `json:"feed_url"`
	CategoryID *int64 `json:"category_id"`
}

func (s *Server) handleFeedsCreate(w http.ResponseWriter, r *http.Request) {
	var req createFeedReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	req.FeedURL = strings.TrimSpace(req.FeedURL)
	if req.FeedURL == "" {
		http.Error(w, "feed_url is required", http.StatusBadRequest)
		return
	}

	now := time.Now().Unix()
	f := &model.Feed{
		UserID:       defaultUserID,
		CategoryID:   req.CategoryID,
		Title:        req.FeedURL, // placeholder until the poller fetches the real title
		FeedURL:      req.FeedURL,
		PollInterval: 3600,
		NextPollAt:   &now, // poll immediately
	}
	if err := s.opts.Store.Feeds().Create(r.Context(), f); err != nil {
		// The store layer translates SQLite UNIQUE violations to ErrConflict,
		// keeping driver-specific error parsing out of the handler.
		if errors.Is(err, store.ErrConflict) {
			http.Error(w, "feed_url already subscribed", http.StatusConflict)
			return
		}
		s.serverError(w, r, "feeds.create", err)
		return
	}

	if err := enqueuePoll(r.Context(), s.opts.Queue, f.ID); err != nil {
		s.opts.Logger.Error("enqueue feed.poll", "feed_id", f.ID, "err", err)
		// The feed exists in the DB; the scheduler's periodic poll will pick it up
		// even if this enqueue failed, so we still return 201.
	}
	writeJSON(w, http.StatusCreated, toFeedJSON(f))
}

func (s *Server) handleFeedGet(w http.ResponseWriter, r *http.Request) {
	f, ok := s.lookupFeed(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toFeedJSON(f))
}

type updateFeedReq struct {
	Title              *string `json:"title"`
	CategoryID         *int64  `json:"category_id"`
	Crawler            *bool   `json:"crawler"`
	ScraperRules       *string `json:"scraper_rules"`
	Disabled           *bool   `json:"disabled"`
	IgnoreEntryUpdates *bool   `json:"ignore_entry_updates"`
}

func (s *Server) handleFeedUpdate(w http.ResponseWriter, r *http.Request) {
	f, ok := s.lookupFeed(w, r)
	if !ok {
		return
	}
	var req updateFeedReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Title != nil {
		f.Title = *req.Title
	}
	if req.CategoryID != nil {
		f.CategoryID = req.CategoryID
	}
	if req.Crawler != nil {
		f.Crawler = *req.Crawler
	}
	if req.ScraperRules != nil {
		f.ScraperRules = req.ScraperRules
	}
	if req.Disabled != nil {
		f.Disabled = *req.Disabled
	}
	if req.IgnoreEntryUpdates != nil {
		f.IgnoreEntryUpdates = *req.IgnoreEntryUpdates
	}
	if err := s.opts.Store.Feeds().Update(r.Context(), f); err != nil {
		s.serverError(w, r, "feeds.update", err)
		return
	}
	writeJSON(w, http.StatusOK, toFeedJSON(f))
}

func (s *Server) handleFeedDelete(w http.ResponseWriter, r *http.Request) {
	f, ok := s.lookupFeed(w, r)
	if !ok {
		return
	}
	if err := s.opts.Store.Feeds().Delete(r.Context(), f.ID); err != nil {
		s.serverError(w, r, "feeds.delete", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFeedRefresh(w http.ResponseWriter, r *http.Request) {
	f, ok := s.lookupFeed(w, r)
	if !ok {
		return
	}
	if err := enqueuePoll(r.Context(), s.opts.Queue, f.ID); err != nil {
		s.serverError(w, r, "feeds.refresh", err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// lookupFeed parses the {id} path value, fetches the feed, and enforces user
// ownership. On any miss it writes 404 and returns ok=false; the 404-on-mismatch
// (rather than 403) is intentional — leaking ID existence across users is an
// info disclosure.
func (s *Server) lookupFeed(w http.ResponseWriter, r *http.Request) (*model.Feed, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return nil, false
	}
	f, err := s.opts.Store.Feeds().Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return nil, false
		}
		s.serverError(w, r, "feeds.get", err)
		return nil, false
	}
	if f.UserID != defaultUserID {
		http.NotFound(w, r)
		return nil, false
	}
	return f, true
}

func enqueuePoll(ctx context.Context, q jobs.Queue, feedID int64) error {
	payload, err := json.Marshal(map[string]int64{"feed_id": feedID})
	if err != nil {
		return err
	}
	_, err = q.Enqueue(ctx, queueFeedPoll, payload)
	return err
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	s.opts.Logger.Error(op, "path", r.URL.Path, "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}
