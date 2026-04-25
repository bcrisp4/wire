package extract

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bcrisp4/wire/internal/jobs"
)

// Default tuning for the extract worker. Article HTML can be substantially
// larger than feed XML, so the cap here is bigger than feedfetch's.
const (
	workerHTTPTimeout  = 30 * time.Second
	workerBodyCap      = 5 * 1024 * 1024
	workerUserAgent    = "wire-feed-reader/0.1 (+https://github.com/bcrisp4/wire) extract"
	workerIdleSleep    = 250 * time.Millisecond
	workerMaxRedirects = 5
)

// errBlockedAddress is returned when the article URL (or a redirect target)
// resolves to a non-public address. Entry URLs come from feeds, which are
// user-controlled; without this guard a malicious entry could coax the
// worker into hitting cloud-metadata or internal admin endpoints.
var errBlockedAddress = errors.New("address not allowed")

// errBlockedScheme is returned for non-http(s) schemes.
var errBlockedScheme = errors.New("scheme not allowed")

// errTooManyRedirects is returned when the redirect chain exceeds the cap.
var errTooManyRedirects = errors.New("too many redirects")

// validateURLFn is the SSRF guard invoked before each fetch. It is a package
// variable so tests using httptest (bound to 127.0.0.1) can substitute a
// permissive implementation. Mirrors the pattern in internal/discover.
var validateURLFn = validateURL

// lookupHostFn matches net.LookupIP; tests can override.
var lookupHostFn = net.LookupIP

// EntryFetcher returns the URL and per-feed scraper rules for a given entry ID.
// The store interface (EntryRepo.Get + FeedRepo.Get) intentionally has no
// "give me URL+rules" helper, so the worker takes a function so callers can
// compose it however they like.
type EntryFetcher func(ctx context.Context, entryID int64) (url, rules string, err error)

// EntryUpdater persists the extracted content + reading time for an entry.
// store.EntryRepo.UpdateState only handles read/saved flags by design; this
// worker writes content via a direct UPDATE supplied by the caller.
type EntryUpdater func(ctx context.Context, entryID int64, content string, readingTime int) error

// Deps is the wiring the worker needs. All fields are required except
// OnJobDone (test hook) and HTTPClient (defaults to a sane one).
type Deps struct {
	Queue        jobs.Queue
	Logger       *slog.Logger
	HTTPClient   *http.Client
	EntryFetcher EntryFetcher
	EntryUpdater EntryUpdater

	// OnJobDone is invoked after each job is acked or failed; tests use it
	// to know when work is finished without polling.
	OnJobDone func(jobID int64)
}

type extractPayload struct {
	EntryID int64 `json:"entry_id"`
}

// RunWorker drains the entry.extract queue until ctx is canceled.
//
// Each job: fetch entry URL + per-feed rules, GET the article HTML, run
// Extract, update entries.content / entries.reading_time. On any error the
// job is acked (not retried) — the feed-provided summary remains, matching
// Miniflux's "log and move on" pattern. Hard infrastructure errors (queue
// claim failed, etc.) trigger a backoff sleep before retrying the loop.
func RunWorker(ctx context.Context, deps Deps, workerID string) {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.HTTPClient == nil {
		deps.HTTPClient = &http.Client{Timeout: workerHTTPTimeout}
	}
	log := deps.Logger.With("worker", workerID, "queue", jobs.QueueEntryExtract)
	log.Info("extract worker started")
	defer log.Info("extract worker stopped")

	for {
		if err := ctx.Err(); err != nil {
			return
		}

		job, err := deps.Queue.Claim(ctx, jobs.QueueEntryExtract, workerID)
		if err != nil {
			if errors.Is(err, jobs.ErrNoJob) {
				if !sleepCtx(ctx, workerIdleSleep) {
					return
				}
				continue
			}
			if ctx.Err() != nil {
				return
			}
			log.Warn("claim failed", "err", err)
			if !sleepCtx(ctx, workerIdleSleep) {
				return
			}
			continue
		}

		processJob(ctx, deps, log, job)
		if deps.OnJobDone != nil {
			deps.OnJobDone(job.ID)
		}
	}
}

// ackJob acks job and logs any Ack failure under reason. Honker's Ack can
// fail and leave the job stuck in the claimed state, so we surface those
// failures rather than swallowing them.
func ackJob(ctx context.Context, jl *slog.Logger, job *jobs.Job, reason string) {
	if err := job.Ack(ctx); err != nil {
		jl.Error("ack failed", "reason", reason, "err", err)
	}
}

func processJob(ctx context.Context, deps Deps, log *slog.Logger, job *jobs.Job) {
	jl := log.With("job_id", job.ID)

	var payload extractPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		jl.Warn("invalid payload, dropping", "err", err)
		ackJob(ctx, jl, job, "invalid payload")
		return
	}
	jl = jl.With("entry_id", payload.EntryID)

	if payload.EntryID == 0 {
		jl.Warn("payload missing entry_id, dropping")
		ackJob(ctx, jl, job, "missing entry_id")
		return
	}

	url, rules, err := deps.EntryFetcher(ctx, payload.EntryID)
	if err != nil {
		jl.Warn("entry lookup failed", "err", err)
		ackJob(ctx, jl, job, "entry lookup failed")
		return
	}
	if strings.TrimSpace(url) == "" {
		jl.Warn("entry has no URL, skipping extraction")
		ackJob(ctx, jl, job, "empty URL")
		return
	}

	html, err := fetchArticleHTML(ctx, deps.HTTPClient, url)
	if err != nil {
		jl.Warn("fetch failed", "url", url, "err", err)
		ackJob(ctx, jl, job, "fetch failed")
		return
	}

	res, err := Extract(ctx, url, html, rules)
	if err != nil {
		jl.Warn("extract failed", "url", url, "err", err)
		ackJob(ctx, jl, job, "extract failed")
		return
	}

	if err := deps.EntryUpdater(ctx, payload.EntryID, res.Content, res.ReadingTime); err != nil {
		jl.Warn("update failed", "err", err)
		ackJob(ctx, jl, job, "update failed")
		return
	}

	jl.Info("extracted", "url", url, "bytes", len(res.Content), "reading_time", res.ReadingTime)
	ackJob(ctx, jl, job, "success")
}

// fetchArticleHTML GETs articleURL and returns the body (capped at
// workerBodyCap). The URL is validated against the SSRF guard, and redirect
// targets are re-validated so a server can't bounce us to an internal address.
func fetchArticleHTML(ctx context.Context, client *http.Client, articleURL string) (string, error) {
	if err := validateURLFn(articleURL); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, articleURL, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", workerUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	// Wrap a copy of the caller's client so we can install a redirect policy
	// without mutating their configuration.
	c := *client
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > workerMaxRedirects {
			return errTooManyRedirects
		}
		return validateURLFn(req.URL.String())
	}

	resp, err := c.Do(req)
	if err != nil {
		return "", fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, workerBodyCap+1))
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	if int64(len(body)) > workerBodyCap {
		return "", fmt.Errorf("body exceeds %d bytes", workerBodyCap)
	}
	return string(body), nil
}

// validateURL parses rawURL and rejects schemes other than http/https and
// hosts that resolve to loopback, link-local, private, multicast, or
// unspecified addresses. Mirrors internal/discover.validateURL.
func validateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return errBlockedScheme
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("missing host in %q", rawURL)
	}
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return errBlockedAddress
		}
		return nil
	}
	switch strings.ToLower(host) {
	case "localhost", "ip6-localhost", "ip6-loopback":
		return errBlockedAddress
	}
	ips, err := lookupHostFn(host)
	if err != nil {
		return fmt.Errorf("resolve host: %w", err)
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return errBlockedAddress
		}
	}
	return nil
}

// isPublicIP reports whether ip is plausibly routable.
func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() ||
		ip.IsInterfaceLocalMulticast() {
		return false
	}
	return true
}

// sleepCtx sleeps d or returns false if ctx is canceled first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// NewSQLEntryFetcher returns an EntryFetcher that reads entry URL and the
// owning feed's scraper_rules from db. It exists so callers can wire the
// worker without re-implementing the join.
func NewSQLEntryFetcher(db *sql.DB) EntryFetcher {
	const q = `
SELECT COALESCE(e.url, ''), COALESCE(f.scraper_rules, '')
FROM entries e
JOIN feeds f ON f.id = e.feed_id
WHERE e.id = ?`
	return func(ctx context.Context, entryID int64) (string, string, error) {
		var url, rules string
		if err := db.QueryRowContext(ctx, q, entryID).Scan(&url, &rules); err != nil {
			return "", "", fmt.Errorf("entry lookup: %w", err)
		}
		return url, rules, nil
	}
}

// NewSQLEntryUpdater returns an EntryUpdater that writes content and
// reading_time on the entries table. We bypass store.EntryRepo here because
// its UpdateState contract intentionally omits content updates — see
// internal/store/store.go.
//
// Returns sql.ErrNoRows if the entry was deleted between enqueue and update,
// so the worker logs and skips rather than acking as success.
func NewSQLEntryUpdater(db *sql.DB) EntryUpdater {
	const q = `UPDATE entries SET content = ?, reading_time = ?, changed_at = unixepoch() WHERE id = ?`
	return func(ctx context.Context, entryID int64, content string, readingTime int) error {
		res, err := db.ExecContext(ctx, q, content, readingTime, entryID)
		if err != nil {
			return fmt.Errorf("entry update: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("entry update rows affected: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("entry update: %w", sql.ErrNoRows)
		}
		return nil
	}
}
