// Package extract turns a raw HTML article page into sanitized, reader-ready
// content. It dispatches in three layers:
//
//  1. If the caller supplied custom CSS rules (Feed.ScraperRules), match them
//     against the parsed document and concatenate the rendered nodes.
//  2. Otherwise, look up the URL's host (sans "www.") in the vendored
//     Miniflux predefined-rules table; apply the same CSS-selector logic.
//  3. Otherwise, fall through to Readeck's go-readability.
//
// The output is run through the vendored Miniflux sanitizer with the page URL
// as the base for resolving relative links.
//
// Reading time = ceil(words / 250), with a minimum of 1 minute.
package extract

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"net/url"
	"strings"

	readability "codeberg.org/readeck/go-readability"
	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

// Result is what Extract returns.
type Result struct {
	Content     string // sanitized HTML
	ReadingTime int    // minutes (ceil(words/250), min 1)
	Image       string // optional cover URL (best effort, may be empty)
}

// Extract runs custom CSS rules first (if any), else same-site predefined
// rules from rules.go, else falls through to Readeck's go-readability. The
// result is sanitized via the vendored Miniflux sanitizer.
//
// pageURL is the source page URL (used for resolving relative links and rule
// dispatch). rawHTML is the raw page body. customRules is the per-feed CSS
// selector override (Feed.ScraperRules); empty means "no custom rule".
func Extract(ctx context.Context, pageURL, rawHTML, customRules string) (*Result, error) {
	if strings.TrimSpace(rawHTML) == "" {
		return nil, fmt.Errorf("extract: empty html body")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}

	// Layer 1+2: rule-based extraction via CSS selectors.
	rule := strings.TrimSpace(customRules)
	if rule == "" {
		rule = predefinedRule(pageURL)
	}
	if rule != "" {
		if content, ok := extractByRule(rawHTML, rule); ok {
			return finalize(pageURL, content, "")
		}
		// If the rule selector matched nothing, fall through to readability
		// rather than returning empty content.
	}

	// Layer 3: Readeck go-readability.
	parsedURL, _ := url.Parse(pageURL)
	article, err := readability.FromReader(strings.NewReader(rawHTML), parsedURL)
	if err != nil {
		return nil, fmt.Errorf("extract: readability: %w", err)
	}
	if strings.TrimSpace(article.Content) == "" {
		return nil, fmt.Errorf("extract: readability produced empty content")
	}

	return finalize(pageURL, article.Content, article.Image)
}

// extractByRule matches selector against rawHTML and returns the concatenated
// outer-HTML of the matched nodes. Returns ok=false when nothing matches.
func extractByRule(rawHTML, selector string) (string, bool) {
	sel, err := cascadia.Compile(selector)
	if err != nil {
		return "", false
	}
	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return "", false
	}
	matches := cascadia.QueryAll(doc, sel)
	if len(matches) == 0 {
		return "", false
	}
	var buf bytes.Buffer
	for _, n := range matches {
		if err := html.Render(&buf, n); err != nil {
			return "", false
		}
	}
	return buf.String(), true
}

// predefinedRule returns the CSS selector registered for pageURL's host
// (sans leading "www."), or "" if none is defined. Uses Hostname() so a port
// suffix in the URL doesn't cause the lookup to miss.
func predefinedRule(pageURL string) string {
	parsed, err := url.Parse(pageURL)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	host := strings.TrimPrefix(parsed.Hostname(), "www.")
	return predefinedRules[host]
}

// finalize sanitizes content against pageURL and computes reading time.
// Returns an error when SanitizeHTML produces empty output for non-empty
// input — that signals a sanitizer parse failure or depth-cap hit, and we
// don't want to silently overwrite a feed-provided summary with "".
func finalize(pageURL, content, image string) (*Result, error) {
	clean := SanitizeHTML(pageURL, content, &SanitizerOptions{OpenLinksInNewTab: true})
	if clean == "" && strings.TrimSpace(content) != "" {
		return nil, fmt.Errorf("extract: sanitizer produced empty output")
	}
	return &Result{
		Content:     clean,
		ReadingTime: readingTime(clean),
		Image:       image,
	}, nil
}

// readingTime returns ceil(wordCount/250), with a minimum of 1 minute.
func readingTime(htmlContent string) int {
	words := len(strings.Fields(StripTags(htmlContent)))
	if words == 0 {
		return 1
	}
	return int(math.Ceil(float64(words) / 250.0))
}
