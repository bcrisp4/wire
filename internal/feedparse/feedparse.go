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

	base, _ := url.Parse(sourceURL) // best-effort; nil base means absolute links pass through unchanged

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
// is unusable (e.g. neither title nor link).
func itemToEntry(item *gofeed.Item, base *url.URL) (model.Entry, bool) {
	if item == nil {
		return model.Entry{}, false
	}

	link := resolveAbsolute(base, item.Link)
	title := strings.TrimSpace(item.Title)
	if link == "" && title == "" {
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

	e.Hash = EntryHash(link, title, e.PublishedAt)

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
func resolveAbsolute(base *url.URL, rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return ""
	}
	ref, err := url.Parse(rel)
	if err != nil {
		return rel
	}
	if ref.IsAbs() || base == nil {
		return ref.String()
	}
	return base.ResolveReference(ref).String()
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
