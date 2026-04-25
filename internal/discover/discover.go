// Package discover finds candidate feed URLs for a website. Discovery is a
// pure HTTP+HTML operation with no database state: callers pass a page URL,
// receive a deduped, ordered list of candidate feeds. The user (or a higher
// layer) picks one to subscribe to.
//
// Strategy:
//  1. Fetch the page (10s timeout, 1 MB cap, max 5 redirects).
//  2. If the response itself sniffs as an RSS/Atom document (Content-Type
//     plus a confirming root-element check on the body), return the URL
//     itself as the single candidate.
//  3. Otherwise parse HTML and collect every
//     <link rel="alternate" type="application/(rss|atom)+xml" href="..." title="...">
//     in document order, resolving relative hrefs against the page URL.
//  4. If HTML yielded zero candidates, probe well-known paths (/feed,
//     /feed/, /rss, /rss.xml, /atom.xml, /index.xml, /feed.xml) and keep
//     those whose body sniffs as a feed.
package discover

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
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

// errBlockedAddress is returned when a URL resolves to a loopback, private,
// or otherwise non-routable address. It is wrapped before reaching callers
// and intentionally does not include the resolved IP in its message — we
// don't want to leak internal network details back to the API caller.
var errBlockedAddress = errors.New("address not allowed")

// errBlockedScheme is returned for URLs whose scheme is not http or https.
var errBlockedScheme = errors.New("scheme not allowed")

// errInvalidURL wraps URL parse failures and missing-host cases. Callers can
// test for it with errors.Is to distinguish bad input from upstream failures.
var errInvalidURL = errors.New("invalid URL")

// IsValidationError reports whether err originated from input validation
// (bad scheme, blocked address, malformed URL) rather than a transport or
// upstream failure. The HTTP layer uses this to pick 400 vs 502.
func IsValidationError(err error) bool {
	return errors.Is(err, errBlockedScheme) ||
		errors.Is(err, errBlockedAddress) ||
		errors.Is(err, errInvalidURL)
}

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
	if err := validateURLFn(pageURL); err != nil {
		return nil, fmt.Errorf("discover: %w", err)
	}
	// Wrap the client so the redirect cap is enforced regardless of caller
	// configuration. We don't mutate the caller's client.
	c := *client
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects {
			return errTooManyRedirects
		}
		// Re-validate redirect targets so a user-controlled host can't
		// bounce us to an internal address.
		if err := validateURLFn(req.URL.String()); err != nil {
			return err
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	body, contentType, finalURL, err := fetch(ctx, &c, pageURL)
	if err != nil {
		return nil, fmt.Errorf("discover: %w", err)
	}

	// If the page itself is a feed, that's the only candidate. We confirm
	// via root-element sniff because servers commonly send "application/xml"
	// or "text/xml" for feeds.
	if t, ok := sniffFeed(contentType, body); ok {
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
// looks like a syndication feed, ("xml", true) if it is a generic XML type
// that may be a feed (caller should confirm via body), or ("", false)
// otherwise.
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
	case "application/xml", "text/xml":
		return "xml", true
	}
	return "", false
}

// sniffFeed combines a Content-Type sniff with a body root-element check.
// For "rss"/"atom" content types we trust the server. For generic XML types
// ("application/xml", "text/xml") we look at the first significant element
// in the body to decide whether it's actually a feed. Returns ("rss"|"atom",
// true) or ("", false).
func sniffFeed(contentType string, body []byte) (string, bool) {
	t, ok := sniffFeedType(contentType)
	if !ok {
		return "", false
	}
	if t == "rss" || t == "atom" {
		return t, true
	}
	// t == "xml" — confirm via body root-element scan.
	return rootElementFeedType(body)
}

// rootElementFeedType scans the leading bytes of an XML document for the
// first significant element and returns ("rss"|"atom", true) if it is a
// recognized feed root. The scan is intentionally simple: we only look at a
// small prefix to keep this cheap.
func rootElementFeedType(body []byte) (string, bool) {
	const scanLimit = 1024
	if len(body) > scanLimit {
		body = body[:scanLimit]
	}
	s := strings.ToLower(string(body))
	// Skip past XML prolog and any leading whitespace/comments by finding
	// the first '<elementName' that is not '<?xml' or '<!--'.
	for {
		i := strings.Index(s, "<")
		if i < 0 {
			return "", false
		}
		s = s[i:]
		if strings.HasPrefix(s, "<?") {
			end := strings.Index(s, "?>")
			if end < 0 {
				return "", false
			}
			s = s[end+2:]
			continue
		}
		if strings.HasPrefix(s, "<!--") {
			end := strings.Index(s, "-->")
			if end < 0 {
				return "", false
			}
			s = s[end+3:]
			continue
		}
		if strings.HasPrefix(s, "<!") {
			// DOCTYPE or similar; skip to next '>'.
			end := strings.Index(s, ">")
			if end < 0 {
				return "", false
			}
			s = s[end+1:]
			continue
		}
		// First real element. Strip leading '<' and read the element name
		// (up to whitespace or '>'). Handle namespace prefix by taking the
		// local part after a colon if present.
		s = s[1:]
		end := strings.IndexAny(s, " \t\r\n>/")
		if end < 0 {
			return "", false
		}
		name := s[:end]
		if i := strings.Index(name, ":"); i >= 0 {
			name = name[i+1:]
		}
		switch name {
		case "rss":
			return "rss", true
		case "feed":
			return "atom", true
		case "rdf":
			// RSS 1.0 (RDF Site Summary).
			return "rss", true
		}
		return "", false
	}
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
	// Generic XML types in <link type="..."> are too ambiguous to surface as
	// "xml" candidates: API consumers expect "rss" or "atom". For direct-feed
	// detection we resolve "xml" by sniffing the response body, but here we
	// only have a type attribute to go on, so reject it.
	if feedType != "rss" && feedType != "atom" {
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

// probeFeed GETs url and returns its sniffed feed type if the response is
// 2xx and the content sniffs as RSS or Atom (Content-Type plus a root-element
// check on the body). HEAD is unreliable on many shared hosts, so we use GET
// but cap the read at 8 KiB — enough for the XML prolog and root element.
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
	// Read a bounded prefix. We then drain any remainder up to a small cap
	// so the connection can be reused — http.Client only reuses the
	// underlying connection if the response body is read to completion (or
	// closed after a complete read).
	const probeReadLimit = 8 * 1024
	body, _ := io.ReadAll(io.LimitReader(resp.Body, probeReadLimit))
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 32*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", false
	}
	return sniffFeed(resp.Header.Get("Content-Type"), body)
}

// lookupHostFn resolves a hostname to a list of IP addresses. It is a
// package variable so tests can stub it; callers should not override it.
var lookupHostFn = net.LookupIP

// validateURLFn is the URL guard invoked by Discover. It is a package
// variable so tests (which use httptest servers bound to 127.0.0.1) can
// substitute a permissive implementation. Production callers should not
// override it.
var validateURLFn = validateURL

// validateURL parses rawURL and rejects schemes other than http/https and
// hosts that resolve to loopback, link-local, private, multicast, or
// unspecified addresses. This is a lightweight SSRF guard: the user is
// intentionally fetching the URL they typed, but we don't want a single-user
// reader to be coaxed into hitting internal services on behalf of an
// attacker who somehow lands a payload in the input.
func validateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: %v", errInvalidURL, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return errBlockedScheme
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: missing host", errInvalidURL)
	}
	// Fast-path literal IPs.
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return errBlockedAddress
		}
		return nil
	}
	// Reject obvious loopback names without a DNS round-trip.
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

// isPublicIP reports whether ip is a plausibly routable public address.
// Private (RFC 1918), loopback, link-local, multicast, and unspecified
// addresses are all considered non-public.
func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() ||
		ip.IsInterfaceLocalMulticast() {
		return false
	}
	return true
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
