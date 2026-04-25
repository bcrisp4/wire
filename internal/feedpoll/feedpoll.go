// Package feedpoll implements Wire's adaptive feed-polling worker and cron
// dispatcher (design.md §5).
//
// The package depends on local interfaces (FeedRepo, EntryRepo, TombstoneRepo,
// Parser, Fetcher) rather than importing siblings directly. cmd/wire wires the
// concrete store/parser/fetcher implementations in by structural interface
// satisfaction. This keeps feedpoll buildable and unit-testable in isolation
// while sibling Phase 1 units land in parallel.
package feedpoll

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/bcrisp4/wire/internal/jobs"
	"github.com/bcrisp4/wire/internal/model"
)

// QueueName is the Honker queue and scheduled-task name for adaptive polling.
const QueueName = jobs.QueueFeedPoll

// MaxConsecutiveErrors after which a feed is skipped even if scheduled.
const MaxConsecutiveErrors = 10

// HostConcurrency is the per-host inflight cap for feed.poll jobs.
const HostConcurrency = 4

const (
	minPollInterval = 15 * time.Minute
	maxPollInterval = 24 * time.Hour
	weeklyWindow    = 7 * 24 * time.Hour
)

// NextInterval implements the adaptive polling formula from design.md §5:
//
//	interval = (7 days / max(weeklyEntryCount, 1)) / factor, clamped to [15min, 24h].
//
// factor defaults to 1.0; tests can pass higher to poll more aggressively, lower
// to poll less aggressively. Non-positive factor is treated as 1.0.
func NextInterval(weeklyEntryCount int, factor float64) time.Duration {
	if weeklyEntryCount < 1 {
		weeklyEntryCount = 1
	}
	if factor <= 0 {
		factor = 1.0
	}
	d := time.Duration(float64(weeklyWindow) / float64(weeklyEntryCount) / factor)
	if d < minPollInterval {
		return minPollInterval
	}
	if d > maxPollInterval {
		return maxPollInterval
	}
	return d
}

// FeedRepo is the subset of store.FeedRepo we need. Method shapes match
// store.FeedRepo so the production type satisfies this interface structurally.
type FeedRepo interface {
	List(ctx context.Context, userID int64) ([]model.Feed, error)
	Get(ctx context.Context, id int64) (*model.Feed, error)
	Update(ctx context.Context, f *model.Feed) error
	DueForPolling(ctx context.Context, now int64, limit int) ([]model.Feed, error)
}

// EntryRepo is the subset of store.EntryRepo we need.
type EntryRepo interface {
	Insert(ctx context.Context, e *model.Entry) error
}

// TombstoneRepo is the subset of store.TombstoneRepo we need.
type TombstoneRepo interface {
	Has(ctx context.Context, feedID int64, hash string) (bool, error)
}

// Parser is the local parser interface. The production parser (Unit 1) provides
// an adapter type with a matching Parse method.
type Parser interface {
	Parse(ctx context.Context, body []byte, sourceURL string) (*ParseResult, error)
}

// Fetcher is the local fetcher interface. The production fetcher (Unit 2)
// satisfies this structurally.
type Fetcher interface {
	Fetch(ctx context.Context, req FetchRequest) (*FetchResponse, error)
}

// ParseResult mirrors the shape feedparse will expose.
type ParseResult struct {
	Title   string
	SiteURL string
	Entries []ParsedEntry
}

// ParsedEntry mirrors the per-entry shape from the parser. Hash is the stable
// content hash used for dedup against entries + tombstones.
type ParsedEntry struct {
	Hash        string
	Title       string
	URL         *string
	CommentsURL *string
	Author      *string
	Summary     *string
	Content     *string
	PublishedAt *int64
}

// FetchRequest carries conditional-GET hints into the fetcher.
type FetchRequest struct {
	URL              string
	PrevETag         *string
	PrevLastModified *string
}

// FetchResponse is what the fetcher returns. NotModified=true short-circuits
// parsing on a 304.
type FetchResponse struct {
	Status       int
	Body         []byte
	ETag         *string
	LastModified *string
	NotModified  bool
}

// Deps bundles dependencies for the worker and the dispatcher. Parser, Fetcher,
// and the repos are interfaces so unit tests can provide fakes without pulling
// in sibling packages.
type Deps struct {
	Logger     *slog.Logger
	Queue      jobs.Queue
	Feeds      FeedRepo
	Entries    EntryRepo
	Tombstones TombstoneRepo
	Parser     Parser
	Fetcher    Fetcher
	// DB is the raw SQLite handle used by ratelimit.Acquire. Optional in tests.
	DB *sql.DB
	// Now lets tests inject a fixed clock. Defaults to time.Now if nil.
	Now func() time.Time
	// AcquireHost is the per-host concurrency gate; if nil, host limiting is
	// skipped (tests). Production wires this to ratelimit.Acquire.
	AcquireHost func(ctx context.Context, db *sql.DB, host string, limit, perSec int) (bool, error)
	// BackoffSec computes the retry delay (seconds) from the job's attempt
	// count. Defaults to a simple exponential when nil.
	BackoffSec func(attempts int) int
}

func (d *Deps) now() time.Time {
	if d.Now == nil {
		return time.Now()
	}
	return d.Now()
}

func (d *Deps) backoff(attempts int) int {
	if d.BackoffSec != nil {
		return d.BackoffSec(attempts)
	}
	// Default: 30s * 2^(attempts-1), capped at 1h.
	if attempts < 1 {
		attempts = 1
	}
	delay := 30
	for i := 1; i < attempts && delay < 3600; i++ {
		delay *= 2
	}
	if delay > 3600 {
		delay = 3600
	}
	return delay
}

// pollPayload is the JSON shape of a feed.poll job.
type pollPayload struct {
	FeedID int64 `json:"feed_id"`
}

// EnqueueDue enqueues a feed.poll job for every feed whose next_poll_at <= now.
// Called by the cron handler (or directly in tests).
func EnqueueDue(ctx context.Context, deps Deps, batchLimit int) error {
	if batchLimit <= 0 {
		batchLimit = 100
	}
	due, err := deps.Feeds.DueForPolling(ctx, deps.now().Unix(), batchLimit)
	if err != nil {
		return fmt.Errorf("feedpoll: list due: %w", err)
	}
	for i := range due {
		payload, err := json.Marshal(pollPayload{FeedID: due[i].ID})
		if err != nil {
			return fmt.Errorf("feedpoll: marshal payload: %w", err)
		}
		if _, err := deps.Queue.Enqueue(ctx, QueueName, payload); err != nil {
			return fmt.Errorf("feedpoll: enqueue feed %d: %w", due[i].ID, err)
		}
	}
	return nil
}

// RunWorker drains the feed.poll queue until ctx is canceled. Callers should
// add to a sync.WaitGroup before invoking. Long-running goroutine; respects
// ctx.Done() between claims.
func RunWorker(ctx context.Context, deps Deps, workerID string) {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	log := deps.Logger.With("worker", workerID)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		job, err := deps.Queue.Claim(ctx, QueueName, workerID)
		if errors.Is(err, jobs.ErrNoJob) {
			// Cron fires once a minute; the polling loop is fine. Future:
			// switch to Honker ClaimWaker for sub-2ms wake-ups.
			if !sleepOrCancel(ctx, 250*time.Millisecond) {
				return
			}
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Error("claim failed", "err", err)
			if !sleepOrCancel(ctx, time.Second) {
				return
			}
			continue
		}
		if err := processJob(ctx, deps, job); err != nil {
			log.Error("process job", "job_id", job.ID, "err", err)
		}
	}
}

func sleepOrCancel(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// processJob handles a single feed.poll job. Errors returned here are
// observability-only; per-job failure is signaled via Job.Fail / feed updates.
func processJob(ctx context.Context, deps Deps, job *jobs.Job) error {
	var p pollPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		// Malformed payload: ack and drop so it doesn't requeue forever.
		_ = job.Ack(ctx)
		return fmt.Errorf("feedpoll: unmarshal payload: %w", err)
	}

	feed, err := deps.Feeds.Get(ctx, p.FeedID)
	if err != nil {
		_ = job.Ack(ctx)
		return fmt.Errorf("feedpoll: get feed %d: %w", p.FeedID, err)
	}

	// Disabled or persistently failing: ack and skip.
	if feed.Disabled || feed.ErrorCount >= MaxConsecutiveErrors {
		return job.Ack(ctx)
	}

	// Per-host concurrency gate. Honker's TryRateLimit is fixed-window and the
	// closest built-in we have (RESEARCH-honker §2). Skip if not wired.
	if deps.AcquireHost != nil && deps.DB != nil {
		ok, err := deps.AcquireHost(ctx, deps.DB, hostOf(feed.FeedURL), HostConcurrency, 1)
		if err != nil {
			return retryWithError(ctx, deps, job, feed, fmt.Errorf("acquire host slot: %w", err))
		}
		if !ok {
			// Defer 30s and try again — another worker is using the slot.
			return job.Fail(ctx, "host concurrency", 30)
		}
	}

	resp, err := deps.Fetcher.Fetch(ctx, FetchRequest{
		URL:              feed.FeedURL,
		PrevETag:         feed.ETag,
		PrevLastModified: feed.LastModified,
	})
	if err != nil {
		return retryWithError(ctx, deps, job, feed, fmt.Errorf("fetch: %w", err))
	}

	now := deps.now()

	// 304 Not Modified: just bump next_poll_at, clear errors.
	if resp.NotModified {
		applyPollSuccess(feed, now, nil)
		if err := deps.Feeds.Update(ctx, feed); err != nil {
			return retryWithError(ctx, deps, job, feed, fmt.Errorf("update feed: %w", err))
		}
		return job.Ack(ctx)
	}

	parsed, err := deps.Parser.Parse(ctx, resp.Body, feed.FeedURL)
	if err != nil {
		return retryWithError(ctx, deps, job, feed, fmt.Errorf("parse: %w", err))
	}

	// Insert new entries, skipping tombstoned hashes.
	for i := range parsed.Entries {
		pe := &parsed.Entries[i]
		known, err := deps.Tombstones.Has(ctx, feed.ID, pe.Hash)
		if err != nil {
			return retryWithError(ctx, deps, job, feed, fmt.Errorf("tombstone lookup: %w", err))
		}
		if known {
			continue
		}
		entry := &model.Entry{
			FeedID:      feed.ID,
			UserID:      feed.UserID,
			Hash:        pe.Hash,
			Title:       pe.Title,
			URL:         pe.URL,
			CommentsURL: pe.CommentsURL,
			Author:      pe.Author,
			Summary:     pe.Summary,
			Content:     pe.Content,
			PublishedAt: pe.PublishedAt,
			CreatedAt:   now.Unix(),
			ChangedAt:   now.Unix(),
		}
		if err := deps.Entries.Insert(ctx, entry); err != nil {
			// Unique-violation on duplicate hash is non-fatal: count and continue.
			if isDuplicateEntryErr(err) {
				continue
			}
			return retryWithError(ctx, deps, job, feed, fmt.Errorf("insert entry: %w", err))
		}
	}

	applyPollSuccess(feed, now, resp)
	if err := deps.Feeds.Update(ctx, feed); err != nil {
		return retryWithError(ctx, deps, job, feed, fmt.Errorf("update feed: %w", err))
	}
	return job.Ack(ctx)
}

// applyPollSuccess updates feed bookkeeping after a successful poll. resp may
// be nil for 304 paths (in which case ETag/LastModified stay as-is).
//
// WeeklyEntryCount approximation: design.md §5 calls for a 7-day moving window;
// a precise implementation would query SUM over entries.created_at >= now-7d.
// For Phase 1 we keep the existing value (loaded from the DB) and let later
// units refine the calculation. This intentionally trades precision for
// simplicity; the polling formula is itself an approximation.
func applyPollSuccess(f *model.Feed, now time.Time, resp *FetchResponse) {
	if resp != nil {
		if resp.ETag != nil {
			f.ETag = resp.ETag
		}
		if resp.LastModified != nil {
			f.LastModified = resp.LastModified
		}
	}
	t := now.Unix()
	f.LastPolledAt = &t
	next := now.Add(NextInterval(f.WeeklyEntryCount, 1)).Unix()
	f.NextPollAt = &next
	f.ErrorCount = 0
	f.LastError = nil
}

// retryWithError increments error_count on the feed, persists last_error, and
// asks Honker to retry the job using the configured backoff. The feed Update
// failing is logged via the returned error but does not block the Job.Fail
// call — we don't want a transient DB error to also lose the retry.
func retryWithError(ctx context.Context, deps Deps, job *jobs.Job, feed *model.Feed, cause error) error {
	msg := cause.Error()
	feed.ErrorCount++
	feed.LastError = &msg
	if updErr := deps.Feeds.Update(ctx, feed); updErr != nil {
		// Log via returned error; still attempt the job retry.
		deps.Logger.Error("update feed after error", "feed_id", feed.ID, "err", updErr)
	}
	return job.Fail(ctx, msg, deps.backoff(int(job.Attempts)))
}

// isDuplicateEntryErr returns true for SQLite UNIQUE-constraint errors so the
// caller can treat duplicates as a no-op rather than a hard failure. We keep
// this string-based to avoid coupling feedpoll to the sqlite driver type.
func isDuplicateEntryErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "UNIQUE constraint failed") || strings.Contains(s, "constraint failed: UNIQUE")
}

// hostOf returns the lower-cased host of a URL, or the URL itself if unparsable.
func hostOf(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil || u.Host == "" {
		return rawurl
	}
	return strings.ToLower(u.Host)
}
