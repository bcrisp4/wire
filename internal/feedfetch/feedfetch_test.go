package feedfetch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetch_200OK_PopulatesFields(t *testing.T) {
	const body = "<rss>hello</rss>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"abc123"`)
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2026 07:28:00 GMT")
		w.Header().Set("Content-Type", "application/rss+xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	f := New()
	resp, err := f.Fetch(context.Background(), Request{URL: srv.URL})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.Status)
	assert.False(t, resp.NotModified)
	assert.Equal(t, body, string(resp.Body))
	assert.Equal(t, `"abc123"`, resp.ETag)
	assert.Equal(t, "Wed, 21 Oct 2026 07:28:00 GMT", resp.LastModified)
	assert.Equal(t, "application/rss+xml", resp.ContentType)
	assert.Equal(t, srv.URL, resp.FinalURL)
}

func TestFetch_304NotModified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	f := New()
	resp, err := f.Fetch(context.Background(), Request{URL: srv.URL, PrevETag: `"abc"`})
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotModified, resp.Status)
	assert.True(t, resp.NotModified)
	assert.Empty(t, resp.Body)
}

func TestFetch_SendsConditionalHeaders(t *testing.T) {
	var gotIfNoneMatch, gotIfModifiedSince, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIfNoneMatch = r.Header.Get("If-None-Match")
		gotIfModifiedSince = r.Header.Get("If-Modified-Since")
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := New(WithUserAgent("test-ua/1.0"))
	_, err := f.Fetch(context.Background(), Request{
		URL:              srv.URL,
		PrevETag:         `"prev-etag"`,
		PrevLastModified: "Wed, 21 Oct 2026 07:28:00 GMT",
	})
	require.NoError(t, err)
	assert.Equal(t, `"prev-etag"`, gotIfNoneMatch)
	assert.Equal(t, "Wed, 21 Oct 2026 07:28:00 GMT", gotIfModifiedSince)
	assert.Equal(t, "test-ua/1.0", gotUA)
}

func TestFetch_DefaultUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := New()
	_, err := f.Fetch(context.Background(), Request{URL: srv.URL})
	require.NoError(t, err)
	assert.Equal(t, defaultUserAgent, gotUA)
}

func TestFetch_FollowsRedirects_Up_To_5(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// /0 -> /1 -> /2 -> /3 -> /4 -> /5 (final 200): exactly 5 redirects.
	for i := 0; i < 5; i++ {
		i := i
		mux.HandleFunc(fmt.Sprintf("/%d", i), func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, fmt.Sprintf("%s/%d", srv.URL, i+1), http.StatusFound)
		})
	}
	mux.HandleFunc("/5", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	f := New()
	resp, err := f.Fetch(context.Background(), Request{URL: srv.URL + "/0"})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.Status)
	assert.Equal(t, "ok", string(resp.Body))
	assert.Equal(t, srv.URL+"/5", resp.FinalURL)
}

func TestFetch_RejectsTooManyRedirects(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Endless redirect chain
	for i := 0; i < 20; i++ {
		i := i
		mux.HandleFunc(fmt.Sprintf("/%d", i), func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, fmt.Sprintf("%s/%d", srv.URL, i+1), http.StatusFound)
		})
	}

	f := New()
	_, err := f.Fetch(context.Background(), Request{URL: srv.URL + "/0"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTooManyRedirects), "expected ErrTooManyRedirects, got: %v", err)
}

func TestFetch_FinalURLAfterRedirect(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/end", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/end", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	f := New()
	resp, err := f.Fetch(context.Background(), Request{URL: srv.URL + "/start"})
	require.NoError(t, err)
	assert.Equal(t, srv.URL+"/end", resp.FinalURL)
}

func TestFetch_CacheControlMaxAge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=600")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := New()
	resp, err := f.Fetch(context.Background(), Request{URL: srv.URL})
	require.NoError(t, err)
	assert.Equal(t, 10*time.Minute, resp.MaxAge)
}

func TestFetch_CacheControlMaxAge_Absent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := New()
	resp, err := f.Fetch(context.Background(), Request{URL: srv.URL})
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), resp.MaxAge)
}

func TestFetch_RetryAfter_DeltaSeconds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	f := New()
	_, err := f.Fetch(context.Background(), Request{URL: srv.URL})
	require.Error(t, err)
	var statusErr *StatusError
	require.True(t, errors.As(err, &statusErr), "expected StatusError, got %v", err)
	assert.Equal(t, http.StatusTooManyRequests, statusErr.Status)
	assert.Equal(t, 30*time.Second, statusErr.RetryAfter)
}

func TestFetch_RetryAfter_HTTPDate(t *testing.T) {
	// 60 seconds in the future, formatted as HTTP-date.
	future := time.Now().UTC().Add(60 * time.Second).Truncate(time.Second)
	dateStr := future.Format(http.TimeFormat)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", dateStr)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	f := New()
	_, err := f.Fetch(context.Background(), Request{URL: srv.URL})
	require.Error(t, err)
	var statusErr *StatusError
	require.True(t, errors.As(err, &statusErr), "expected StatusError, got %v", err)
	// Allow some slack for time taken between formatting and parsing.
	assert.InDelta(t, float64(60*time.Second), float64(statusErr.RetryAfter), float64(5*time.Second))
}

func TestFetch_BodyCap_Exceeded(t *testing.T) {
	big := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()

	f := New(WithMaxBodyBytes(1024))
	_, err := f.Fetch(context.Background(), Request{URL: srv.URL})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBodyTooLarge), "expected ErrBodyTooLarge, got: %v", err)
}

func TestFetch_BodyCap_AtLimit_OK(t *testing.T) {
	body := strings.Repeat("x", 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	f := New(WithMaxBodyBytes(1024))
	resp, err := f.Fetch(context.Background(), Request{URL: srv.URL})
	require.NoError(t, err)
	assert.Equal(t, 1024, len(resp.Body))
}

func TestFetch_NonOK_NotModified_ReturnsStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	f := New()
	_, err := f.Fetch(context.Background(), Request{URL: srv.URL})
	require.Error(t, err)
	var statusErr *StatusError
	require.True(t, errors.As(err, &statusErr), "expected StatusError, got %v", err)
	assert.Equal(t, http.StatusInternalServerError, statusErr.Status)
	// The wrapped error message should mention the status.
	assert.Contains(t, err.Error(), "500")
}

func TestFetch_404_ReturnsStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := New()
	_, err := f.Fetch(context.Background(), Request{URL: srv.URL})
	require.Error(t, err)
	var statusErr *StatusError
	require.True(t, errors.As(err, &statusErr))
	assert.Equal(t, http.StatusNotFound, statusErr.Status)
}

func TestFetch_ContextCancellation(t *testing.T) {
	// Server that blocks until the request is cancelled.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	f := New()
	_, err := f.Fetch(ctx, Request{URL: srv.URL})
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled), "expected context.Canceled, got: %v", err)
}

func TestFetch_WithHTTPClient_Override(t *testing.T) {
	// Custom client with a sentinel transport that always errors. Confirms
	// WithHTTPClient is actually used.
	custom := &http.Client{Transport: errTransport{}}
	f := New(WithHTTPClient(custom))
	_, err := f.Fetch(context.Background(), Request{URL: "http://example.invalid/"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sentinel-transport")
}

type errTransport struct{}

func (errTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("sentinel-transport")
}
