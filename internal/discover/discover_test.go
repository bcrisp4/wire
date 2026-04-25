package discover

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain installs a permissive URL guard so the discover tests can talk
// to httptest servers (which bind to 127.0.0.1). The production guard
// rejects loopback addresses by design. The validation logic itself is
// covered separately in TestValidateURL_*.
func TestMain(m *testing.M) {
	restore := SetValidateURLForTest(func(string) error { return nil })
	code := m.Run()
	restore()
	os.Exit(code)
}

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
// wrapped errors with the "discover:" prefix. We start a real httptest
// server, capture its URL, then close it so the next request reliably fails
// with a connection refused — more deterministic than a hard-coded port.
func TestDiscover_NetworkErrorWrapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL + "/"
	srv.Close()

	_, err := Discover(context.Background(), http.DefaultClient, url)
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

// TestDiscover_DirectFeed_GenericXMLContentType verifies that a feed served
// as application/xml (rather than application/rss+xml) is still recognized
// when the body's root element confirms it. This is common in the wild.
func TestDiscover_DirectFeed_GenericXMLContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel></channel></rss>`))
	}))
	defer srv.Close()

	got, err := Discover(context.Background(), srv.Client(), srv.URL+"/feed")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "rss", got[0].Type)
}

// TestDiscover_DirectFeed_TextXMLAtom covers Atom served as text/xml.
func TestDiscover_DirectFeed_TextXMLAtom(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"></feed>`))
	}))
	defer srv.Close()

	got, err := Discover(context.Background(), srv.Client(), srv.URL+"/atom")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "atom", got[0].Type)
}

// TestDiscover_GenericXML_NotAFeed ensures we don't classify arbitrary XML
// as a feed when the root element isn't rss/feed/rdf.
func TestDiscover_GenericXML_NotAFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0"?><sitemap></sitemap>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	got, err := Discover(context.Background(), srv.Client(), srv.URL+"/")
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestValidateURL exercises the production URL guard directly (TestMain
// stubs it for the rest of the suite). We bypass DNS by using IP literals
// and stubbing lookupHostFn for the hostname case.
func TestValidateURL(t *testing.T) {
	t.Run("rejects non-http schemes", func(t *testing.T) {
		assert.ErrorIs(t, validateURL("file:///etc/passwd"), errBlockedScheme)
		assert.ErrorIs(t, validateURL("gopher://example.com/"), errBlockedScheme)
	})
	t.Run("rejects loopback IP literal", func(t *testing.T) {
		assert.ErrorIs(t, validateURL("http://127.0.0.1/"), errBlockedAddress)
		assert.ErrorIs(t, validateURL("http://[::1]/"), errBlockedAddress)
	})
	t.Run("rejects private IP literal", func(t *testing.T) {
		assert.ErrorIs(t, validateURL("http://10.0.0.1/"), errBlockedAddress)
		assert.ErrorIs(t, validateURL("http://192.168.1.1/"), errBlockedAddress)
		assert.ErrorIs(t, validateURL("http://172.16.0.1/"), errBlockedAddress)
	})
	t.Run("rejects link-local IP literal", func(t *testing.T) {
		assert.ErrorIs(t, validateURL("http://169.254.169.254/"), errBlockedAddress)
	})
	t.Run("rejects localhost name without DNS", func(t *testing.T) {
		assert.ErrorIs(t, validateURL("http://localhost/"), errBlockedAddress)
	})
	t.Run("accepts public IP literal", func(t *testing.T) {
		assert.NoError(t, validateURL("http://8.8.8.8/"))
	})
	t.Run("accepts hostname resolving to public IP", func(t *testing.T) {
		prev := lookupHostFn
		lookupHostFn = func(string) ([]net.IP, error) { return []net.IP{net.ParseIP("8.8.8.8")}, nil }
		defer func() { lookupHostFn = prev }()
		assert.NoError(t, validateURL("http://example.com/"))
	})
	t.Run("rejects hostname resolving to private IP", func(t *testing.T) {
		prev := lookupHostFn
		lookupHostFn = func(string) ([]net.IP, error) { return []net.IP{net.ParseIP("10.0.0.5")}, nil }
		defer func() { lookupHostFn = prev }()
		assert.ErrorIs(t, validateURL("http://internal.example/"), errBlockedAddress)
	})
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
