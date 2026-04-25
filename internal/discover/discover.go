// Package discover finds candidate feed URLs for a website. Discovery is a
// pure HTTP+HTML operation with no database state: callers pass a page URL,
// receive a deduped, ordered list of candidate feeds. The user (or a higher
// layer) picks one to subscribe to.
//
// Strategy:
//  1. Fetch the page (10s timeout, 1 MB cap, max 5 redirects).
//  2. If the response itself is an RSS/Atom document (Content-Type sniff),
//     return the URL itself as the single candidate.
//  3. Otherwise parse HTML and collect every
//     <link rel="alternate" type="application/(rss|atom)+xml" href="..." title="...">
//     in document order, resolving relative hrefs against the page URL.
//  4. If HTML yielded zero candidates, probe well-known paths (/feed,
//     /feed/, /rss, /rss.xml, /atom.xml, /index.xml, /feed.xml) and keep
//     those whose Content-Type sniffs as a feed.
package discover

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	defaultTimeout = 10 * time.Second
	maxBodyBytes   = 1 * 1024 * 1024 // 1 MB
	defaultUA      = "wire-feed-reader/0.1"
	maxRedirects   = 5
)

// wellKnownPaths is the ordered list of URLs to probe when HTML autodiscovery
// finds nothing.
var wellKnownPaths = []string{
	"/feed",
	"/feed/",
	"/rss",
	"/rss.xml",
	"/atom.xml",
	"/index.xml",
	"/feed.xml",
}

// errTooManyRedirects is returned by the redirect policy when the cap is
// exceeded. It is wrapped before reaching callers.
var errTooManyRedirects = errors.New("too many redirects")

// Candidate is a single feed URL discovered for a page.
type Candidate struct {
	URL   string
	Title string // from <link title="...">; may be empty
	Type  string // "rss" or "atom"
}

// Discover fetches the page at pageURL and returns deduped feed candidates,
// in HTML-link-order then well-known-order. An empty result is not an error.
func Discover(ctx context.Context, client *http.Client, pageURL string) ([]Candidate, error) {
	if client == nil {
		client = http.DefaultClient
	}
	// Wrap the client so the redirect cap is enforced regardless of caller
	// configuration. We don't mutate the caller's client.
	c := *client
	c.CheckRedirect = func(_ *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects {
			return errTooManyRedirects
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	body, contentType, finalURL, err := fetch(ctx, &c, pageURL)
	if err != nil {
		return nil, fmt.Errorf("discover: %w", err)
	}

	// If the page itself is a feed, that's the only candidate.
	if t, ok := sniffFeedType(contentType); ok {
		return []Candidate{{URL: finalURL, Type: t}}, nil
	}

	// Parse HTML for <link rel="alternate" ...>.
	candidates := parseHTMLLinks(body, finalURL)
	if len(candidates) > 0 {
		return dedupe(candidates), nil
	}

	// Fallback: probe well-known paths.
	probed := probeWellKnown(ctx, &c, finalURL)
	return dedupe(probed), nil
}

// fetch GETs url, returns the body (capped), content type, and final URL
// after redirects.
func fetch(ctx context.Context, client *http.Client, u string) ([]byte, string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", defaultUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,application/rss+xml,application/atom+xml,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, errTooManyRedirects) {
			return nil, "", "", errTooManyRedirects
		}
		return nil, "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, "", "", fmt.Errorf("read body: %w", err)
	}
	if int64(len(body)) > maxBodyBytes {
		return nil, "", "", fmt.Errorf("body exceeds %d bytes", maxBodyBytes)
	}

	finalURL := u
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return body, resp.Header.Get("Content-Type"), finalURL, nil
}

// sniffFeedType inspects a Content-Type and returns ("rss"|"atom", true) if it
// looks like a syndication feed, or ("", false) otherwise.
func sniffFeedType(contentType string) (string, bool) {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	// strip parameters (charset etc.)
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch ct {
	case "application/rss+xml":
		return "rss", true
	case "application/atom+xml":
		return "atom", true
	}
	return "", false
}

// parseHTMLLinks walks the HTML tree and collects <link rel="alternate"
// type="application/(rss|atom)+xml" href="...">. Hrefs are resolved against
// base. Returns links in document order.
func parseHTMLLinks(body []byte, base string) []Candidate {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return nil
	}

	var out []Candidate
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, "link") {
			if c, ok := candidateFromLink(n, baseURL); ok {
				out = append(out, c)
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return out
}

func candidateFromLink(n *html.Node, base *url.URL) (Candidate, bool) {
	var rel, typ, href, title string
	for _, a := range n.Attr {
		switch strings.ToLower(a.Key) {
		case "rel":
			rel = a.Val
		case "type":
			typ = a.Val
		case "href":
			href = a.Val
		case "title":
			title = a.Val
		}
	}
	if !relContains(rel, "alternate") {
		return Candidate{}, false
	}
	feedType, ok := sniffFeedType(typ)
	if !ok {
		return Candidate{}, false
	}
	if href == "" {
		return Candidate{}, false
	}
	resolved, err := base.Parse(href)
	if err != nil {
		return Candidate{}, false
	}
	return Candidate{URL: resolved.String(), Title: title, Type: feedType}, true
}

// relContains reports whether the rel attribute (a space-separated token list)
// contains the target token, case-insensitively.
func relContains(rel, target string) bool {
	for _, tok := range strings.Fields(rel) {
		if strings.EqualFold(tok, target) {
			return true
		}
	}
	return false
}

// probeWellKnown issues GETs to each well-known path on the page's host and
// keeps those whose Content-Type sniffs as a feed.
func probeWellKnown(ctx context.Context, client *http.Client, pageURL string) []Candidate {
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}
	var out []Candidate
	for _, p := range wellKnownPaths {
		probe, err := base.Parse(p)
		if err != nil {
			continue
		}
		t, ok := probeFeed(ctx, client, probe.String())
		if !ok {
			continue
		}
		out = append(out, Candidate{URL: probe.String(), Type: t})
	}
	return out
}

// probeFeed GETs url and returns its sniffed feed type if present.
func probeFeed(ctx context.Context, client *http.Client, u string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("User-Agent", defaultUA)
	resp, err := client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	// Drain a small amount so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", false
	}
	return sniffFeedType(resp.Header.Get("Content-Type"))
}

// dedupe returns cands with duplicate URLs removed, preserving first-seen
// order.
func dedupe(cands []Candidate) []Candidate {
	if len(cands) == 0 {
		return cands
	}
	seen := make(map[string]struct{}, len(cands))
	out := make([]Candidate, 0, len(cands))
	for _, c := range cands {
		if _, ok := seen[c.URL]; ok {
			continue
		}
		seen[c.URL] = struct{}{}
		out = append(out, c)
	}
	return out
}
