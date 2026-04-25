package discover

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDiscover_FindsTwoLinkAlternates verifies that two <link rel="alternate">
// tags are returned in document order, with their type and title preserved.
func TestDiscover_FindsTwoLinkAlternates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html>
<html><head>
<link rel="alternate" type="application/rss+xml" title="Main RSS" href="/feed.xml">
<link rel="alternate" type="application/atom+xml" title="Atom" href="/atom.xml">
</head><body></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	got, err := Discover(context.Background(), srv.Client(), srv.URL+"/")
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, srv.URL+"/feed.xml", got[0].URL)
	assert.Equal(t, "Main RSS", got[0].Title)
	assert.Equal(t, "rss", got[0].Type)

	assert.Equal(t, srv.URL+"/atom.xml", got[1].URL)
	assert.Equal(t, "Atom", got[1].Title)
	assert.Equal(t, "atom", got[1].Type)
}

// TestDiscover_FallsBackToWellKnownPaths confirms that when the HTML has no
// link rel=alternate tags, well-known feed paths are probed (HEAD/GET) and any
// that return a feed-ish content type are returned.
func TestDiscover_FallsBackToWellKnownPaths(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><html><head></head><body>no feeds here</body></html>`))
	})
	mux.HandleFunc("/feed", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss></rss>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	got, err := Discover(context.Background(), srv.Client(), srv.URL+"/")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, srv.URL+"/feed", got[0].URL)
	assert.Equal(t, "rss", got[0].Type)
}

// TestDiscover_NoFeedsReturnsEmpty asserts that absence of feeds is not an
// error — callers receive an empty slice.
func TestDiscover_NoFeedsReturnsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<!doctype html><html><head></head><body>nothing</body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	got, err := Discover(context.Background(), srv.Client(), srv.URL+"/")
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestDiscover_AtomTypeIsCategorizedCorrectly is covered partly by the
// two-alternates test, but here we check that an atom-only page yields atom.
func TestDiscover_AtomTypeIsCategorizedCorrectly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><html><head>
<link rel="alternate" type="application/atom+xml" href="/atom.xml" title="Atom-only">
</head><body></body></html>`))
	}))
	defer srv.Close()

	got, err := Discover(context.Background(), srv.Client(), srv.URL+"/")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "atom", got[0].Type)
	assert.Equal(t, "Atom-only", got[0].Title)
}

// TestDiscover_RelativeHrefIsResolved confirms a relative href is resolved
// against the page URL — important since most feeds advertise via relative
// URLs.
func TestDiscover_RelativeHrefIsResolved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		// note: bare "feed.xml" without leading slash, and the page URL is /blog/
		_, _ = w.Write([]byte(`<!doctype html><html><head>
<link rel="alternate" type="application/rss+xml" href="feed.xml">
</head><body></body></html>`))
	}))
	defer srv.Close()

	got, err := Discover(context.Background(), srv.Client(), srv.URL+"/blog/")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, srv.URL+"/blog/feed.xml", got[0].URL)
}

// TestDiscover_NetworkErrorWrapped confirms that connection errors surface as
// wrapped errors with the "discover:" prefix.
func TestDiscover_NetworkErrorWrapped(t *testing.T) {
	// 127.0.0.1:1 is reliably unreachable for unprivileged users.
	_, err := Discover(context.Background(), http.DefaultClient, "http://127.0.0.1:1/")
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "discover:"),
		"error should be wrapped with discover: prefix, got %q", err.Error())
}

// TestDiscover_DirectFeedURL covers the polish: when the user submits an
// exact feed URL (Content-Type sniff yields RSS/Atom), we return one
// candidate that is the URL itself.
func TestDiscover_DirectFeedURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"></feed>`))
	}))
	defer srv.Close()

	got, err := Discover(context.Background(), srv.Client(), srv.URL+"/atom")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, srv.URL+"/atom", got[0].URL)
	assert.Equal(t, "atom", got[0].Type)
}

// TestDiscover_DedupesIdenticalCandidates confirms the same feed URL appearing
// in HTML alternate links and a well-known path probe is returned only once.
// (The well-known probes are skipped if HTML yielded results, so a more useful
// dedup test is repeated alternate links in HTML.)
func TestDiscover_DedupesIdenticalCandidates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><html><head>
<link rel="alternate" type="application/rss+xml" href="/feed.xml" title="A">
<link rel="alternate" type="application/rss+xml" href="/feed.xml" title="A">
</head><body></body></html>`))
	}))
	defer srv.Close()

	got, err := Discover(context.Background(), srv.Client(), srv.URL+"/")
	require.NoError(t, err)
	assert.Len(t, got, 1)
}
