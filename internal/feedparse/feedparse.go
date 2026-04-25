// Package feedparse converts raw feed bytes (RSS, Atom, JSON Feed) into
// ready-to-store model.Entry values. It is a pure-function library with no
// I/O of its own — fetching is the caller's responsibility.
package feedparse

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"

	"github.com/bcrisp4/wire/internal/model"
)

// Result is what Parse returns for callers (the polling worker, OPML import,
// etc.). Entries have Hash, Title, URL, Author, Summary, PublishedAt set; the
// caller is responsible for filling FeedID, UserID, CreatedAt and inserting.
type Result struct {
	Title       string
	SiteURL     string
	Description string
	Entries     []model.Entry
}

// Parse parses RSS, Atom, or JSON Feed bytes. sourceURL is used to resolve
// relative entry links against the feed's origin.
func Parse(ctx context.Context, body []byte, sourceURL string) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("feedparse: %w", err)
	}

	fp := gofeed.NewParser()
	feed, err := fp.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("feedparse: %w", err)
	}

	// Best-effort base for resolving relative entry links. Treat parse errors
	// or non-absolute results as "no base" so relative links pass through
	// unchanged rather than being resolved against a junk base.
	base, err := url.Parse(sourceURL)
	if err != nil || !base.IsAbs() {
		base = nil
	}

	out := &Result{
		Title:       strings.TrimSpace(feed.Title),
		SiteURL:     resolveAbsolute(base, feed.Link),
		Description: strings.TrimSpace(feed.Description),
		Entries:     make([]model.Entry, 0, len(feed.Items)),
	}

	for _, item := range feed.Items {
		entry, ok := itemToEntry(item, base)
		if !ok {
			continue // skip malformed entries; don't fail the whole feed
		}
		out.Entries = append(out.Entries, entry)
	}

	return out, nil
}

// itemToEntry maps a gofeed.Item to a model.Entry. Returns false if the item
// is unusable (e.g. neither title, link, nor GUID).
func itemToEntry(item *gofeed.Item, base *url.URL) (model.Entry, bool) {
	if item == nil {
		return model.Entry{}, false
	}

	link := resolveAbsolute(base, item.Link)
	title := strings.TrimSpace(item.Title)
	guid := strings.TrimSpace(item.GUID)
	if link == "" && title == "" && guid == "" {
		return model.Entry{}, false
	}

	e := model.Entry{Title: title}

	if link != "" {
		e.URL = strPtr(link)
	}

	if author := firstAuthor(item); author != "" {
		e.Author = strPtr(author)
	}

	if summary := pickSummary(item); summary != "" {
		e.Summary = strPtr(summary)
	}

	if item.PublishedParsed != nil {
		secs := item.PublishedParsed.UTC().Unix()
		e.PublishedAt = &secs
	}

	// Hash from the most stable identifier available: prefer the link,
	// fall back to the feed-supplied GUID. Without one of these, no-link
	// items with matching title+pubdate would collide under the
	// UNIQUE(feed_id, hash) constraint.
	hashKey := link
	if hashKey == "" {
		hashKey = guid
	}
	e.Hash = EntryHash(hashKey, title, e.PublishedAt)

	return e, true
}

// pickSummary returns the best-effort summary text for an entry. gofeed maps
// RSS <description>, Atom <summary>, and JSON Feed `summary` into Description;
// JSON Feed entries with only `content_*` populate Content. We prefer
// Description when present and fall back to Content for JSON-Feed-style items
// that omit a summary.
func pickSummary(item *gofeed.Item) string {
	if d := strings.TrimSpace(item.Description); d != "" {
		return d
	}
	return strings.TrimSpace(item.Content)
}

func firstAuthor(item *gofeed.Item) string {
	for _, a := range item.Authors {
		if a == nil {
			continue
		}
		if name := strings.TrimSpace(a.Name); name != "" {
			return name
		}
	}
	if item.Author != nil {
		if name := strings.TrimSpace(item.Author.Name); name != "" {
			return name
		}
	}
	return ""
}

// resolveAbsolute resolves rel against base. If rel is already absolute it is
// returned unchanged; if base is nil and rel is relative, rel is returned as-is.
// Returns "" if rel is empty, fails to parse, or resolves to an absolute URL
// with a non-http(s) scheme (feeds are untrusted; e.g. javascript:, data:,
// file: links should never be persisted as entry URLs).
func resolveAbsolute(base *url.URL, rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return ""
	}
	ref, err := url.Parse(rel)
	if err != nil {
		return ""
	}
	var resolved *url.URL
	switch {
	case ref.IsAbs():
		resolved = ref
	case base == nil:
		// Relative URL with no base: pass through unchanged. Cannot validate
		// scheme yet; the caller (or downstream) decides what to do.
		return ref.String()
	default:
		resolved = base.ResolveReference(ref)
	}
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}
	return resolved.String()
}

// EntryHash is the dedup key used by store.EntryRepo (unique per feed_id):
//
//	sha256( normalize(url) + "\n" + title + "\n" + publishedAtRFC3339 )
//
// normalize(url) lowercases scheme+host but leaves path/query untouched, so
// that "HTTPS://Example.COM/Post" hashes the same as "https://example.com/Post"
// while "/Post" and "/POST" stay distinct. If publishedAt is nil, the time
// component is the empty string.
func EntryHash(rawURL, title string, publishedAt *int64) string {
	publishedStr := ""
	if publishedAt != nil {
		publishedStr = time.Unix(*publishedAt, 0).UTC().Format(time.RFC3339)
	}

	h := sha256.New()
	h.Write([]byte(normalizeURL(rawURL)))
	h.Write([]byte{'\n'})
	h.Write([]byte(title))
	h.Write([]byte{'\n'})
	h.Write([]byte(publishedStr))
	return hex.EncodeToString(h.Sum(nil))
}

// normalizeURL lowercases the scheme and host of u, leaving path/query/fragment
// untouched. Inputs that don't parse fall through unchanged.
func normalizeURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	return u.String()
}

func strPtr(s string) *string { return &s }
