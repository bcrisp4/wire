// Package feedfetch provides an HTTP client tailored for feed polling:
// conditional GET via ETag/Last-Modified, redirect handling with a hard cap,
// body-size limits, and parsing of Cache-Control max-age and Retry-After
// headers.
//
// The package is a pure-function library; it has no state beyond the
// per-process *Fetcher. Callers (the polling worker) are responsible for
// persisting the returned ETag/LastModified and respecting MaxAge/RetryAfter.
package feedfetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTimeout      = 30 * time.Second
	defaultMaxBodyBytes = 10 * 1024 * 1024 // 10 MB
	defaultUserAgent    = "wire-feed-reader/0.1 (+https://github.com/bcrisp4/wire)"
	maxRedirects        = 5
)

// ErrTooManyRedirects is returned when a fetch exceeds the redirect cap.
// Callers (e.g. the polling worker) may use this to disable feeds that loop.
var ErrTooManyRedirects = errors.New("feedfetch: too many redirects")

// ErrBodyTooLarge is returned when the response body exceeds MaxBodyBytes.
var ErrBodyTooLarge = errors.New("feedfetch: response body exceeds limit")

// StatusError represents an HTTP response that is neither 2xx nor 304.
// It carries the parsed Retry-After value (0 if absent or unparseable) so
// callers can apply server-mandated backoff for 429/503 responses.
type StatusError struct {
	Status     int
	RetryAfter time.Duration
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("feedfetch: unexpected status %d", e.Status)
}

// Request is a single feed-fetch request.
type Request struct {
	URL              string
	PrevETag         string // "" if none
	PrevLastModified string // "" if none
	UserAgent        string // optional; falls back to the Fetcher default
}

// Response is the result of a successful (2xx or 304) feed fetch.
type Response struct {
	Status       int
	NotModified  bool          // true iff 304
	Body         []byte        // empty on 304
	ETag         string        // response ETag (empty if absent)
	LastModified string        // response Last-Modified (empty if absent)
	MaxAge       time.Duration // parsed from Cache-Control: max-age=N (0 if none)
	RetryAfter   time.Duration // parsed from Retry-After header (0 if none)
	FinalURL     string        // after redirects
	ContentType  string
}

// Option configures a Fetcher.
type Option func(*Fetcher)

// WithTimeout sets the per-request timeout. Default 30s.
func WithTimeout(d time.Duration) Option {
	return func(f *Fetcher) { f.timeout = d }
}

// WithMaxBodyBytes caps response body size. Default 10 MB.
func WithMaxBodyBytes(n int64) Option {
	return func(f *Fetcher) { f.maxBodyBytes = n }
}

// WithUserAgent sets the User-Agent header sent on requests.
func WithUserAgent(s string) Option {
	return func(f *Fetcher) { f.userAgent = s }
}

// WithHTTPClient injects a custom *http.Client. Primarily for tests.
// Note: when supplied, the caller's CheckRedirect is replaced so the
// redirect cap is still enforced.
func WithHTTPClient(c *http.Client) Option {
	return func(f *Fetcher) { f.client = c }
}

// Fetcher executes feed requests. Construct once per process and reuse.
type Fetcher struct {
	client       *http.Client
	timeout      time.Duration
	maxBodyBytes int64
	userAgent    string
}

// New returns a Fetcher with sane defaults, overridable via Options.
func New(opts ...Option) *Fetcher {
	f := &Fetcher{
		timeout:      defaultTimeout,
		maxBodyBytes: defaultMaxBodyBytes,
		userAgent:    defaultUserAgent,
	}
	for _, opt := range opts {
		opt(f)
	}
	if f.client == nil {
		f.client = &http.Client{}
	}
	// Always enforce our redirect policy, even on caller-supplied clients.
	// CheckRedirect is consulted before each redirect; len(via) equals the
	// number of redirects already followed, so > maxRedirects allows up to
	// maxRedirects redirects.
	f.client.CheckRedirect = func(_ *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects {
			return ErrTooManyRedirects
		}
		return nil
	}
	return f
}

// Fetch performs a single feed fetch. It honours conditional GET headers
// from req.PrevETag / req.PrevLastModified, follows up to 5 redirects, and
// caps the body at MaxBodyBytes.
//
// Returns:
//   - (resp, nil) for 2xx and 304 responses
//   - (nil, *StatusError) for any other HTTP status
//   - (nil, ErrTooManyRedirects) when redirect cap is exceeded
//   - (nil, ErrBodyTooLarge) when the response body exceeds MaxBodyBytes
//   - (nil, ctx.Err()) on context cancellation
func (f *Fetcher) Fetch(ctx context.Context, req Request) (*Response, error) {
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("feedfetch: build request: %w", err)
	}

	ua := req.UserAgent
	if ua == "" {
		ua = f.userAgent
	}
	httpReq.Header.Set("User-Agent", ua)
	httpReq.Header.Set("Accept", "application/atom+xml, application/rss+xml, application/feed+json, application/json, application/xml;q=0.9, */*;q=0.8")
	if req.PrevETag != "" {
		httpReq.Header.Set("If-None-Match", req.PrevETag)
	}
	if req.PrevLastModified != "" {
		httpReq.Header.Set("If-Modified-Since", req.PrevLastModified)
	}

	httpResp, err := f.client.Do(httpReq)
	if err != nil {
		// http.Client wraps redirect errors in *url.Error; unwrap for errors.Is.
		if errors.Is(err, ErrTooManyRedirects) {
			return nil, ErrTooManyRedirects
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("feedfetch: %w", ctxErr)
		}
		return nil, fmt.Errorf("feedfetch: %w", err)
	}
	defer httpResp.Body.Close()

	resp := &Response{
		Status:       httpResp.StatusCode,
		ETag:         httpResp.Header.Get("ETag"),
		LastModified: httpResp.Header.Get("Last-Modified"),
		ContentType:  httpResp.Header.Get("Content-Type"),
		MaxAge:       parseMaxAge(httpResp.Header.Get("Cache-Control")),
		RetryAfter:   parseRetryAfter(httpResp.Header.Get("Retry-After"), time.Now()),
		FinalURL:     httpResp.Request.URL.String(),
	}

	switch {
	case resp.Status == http.StatusNotModified:
		resp.NotModified = true
		return resp, nil
	case resp.Status >= 200 && resp.Status < 300:
		body, err := readCapped(httpResp.Body, f.maxBodyBytes)
		if err != nil {
			return nil, err
		}
		resp.Body = body
		return resp, nil
	default:
		return nil, &StatusError{Status: resp.Status, RetryAfter: resp.RetryAfter}
	}
}

// readCapped reads at most max+1 bytes; if the +1 byte arrives, the body
// exceeded the cap and we return ErrBodyTooLarge.
func readCapped(r io.Reader, max int64) ([]byte, error) {
	limited := io.LimitReader(r, max+1)
	buf, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("feedfetch: read body: %w", err)
	}
	if int64(len(buf)) > max {
		return nil, ErrBodyTooLarge
	}
	return buf, nil
}

// parseMaxAge extracts the max-age directive from a Cache-Control header.
// Returns 0 if absent or unparseable.
func parseMaxAge(cc string) time.Duration {
	if cc == "" {
		return 0
	}
	for _, part := range strings.Split(cc, ",") {
		part = strings.TrimSpace(part)
		const prefix = "max-age="
		if strings.HasPrefix(strings.ToLower(part), prefix) {
			n, err := strconv.Atoi(strings.TrimSpace(part[len(prefix):]))
			if err != nil || n < 0 {
				return 0
			}
			return time.Duration(n) * time.Second
		}
	}
	return 0
}

// parseRetryAfter accepts both delta-seconds and HTTP-date forms.
// Returns 0 if absent or unparseable. Negative deltas (date in the past)
// are clamped to 0.
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil {
		if n < 0 {
			return 0
		}
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		d := t.Sub(now)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}
