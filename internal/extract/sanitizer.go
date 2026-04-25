// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
//
// Vendored from miniflux/v2 internal/reader/sanitizer/sanitizer.go (Apache-2.0).
// Source: https://github.com/miniflux/v2/blob/main/internal/reader/sanitizer/sanitizer.go
//
// Wire-local modifications:
//   - Replaced miniflux.app/v2/internal/urllib with a tiny inline helper that
//     uses stdlib net/url for absolute-URL resolution and host extraction.
//   - Dropped miniflux.app/v2/internal/config (YouTube/Invidious overrides);
//     iframes are matched against the static iframeAllowList only.
//   - Dropped miniflux.app/v2/internal/reader/urlcleaner tracking-param
//     stripping; Wire treats that as a separate, orthogonal feature.
//
// The allowlist tables and core filterAndRenderHTML algorithm are unchanged.

package extract

import (
	"errors"
	"io"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

const sanitizerMaxDepth = 512 // Match WebKit's nested-tag cap.

var (
	allowedHTMLTagsAndAttributes = map[string][]string{
		"a":          {"href", "title", "id"},
		"abbr":       {"title"},
		"acronym":    {"title"},
		"aside":      {},
		"audio":      {"src"},
		"blockquote": {},
		"b":          {},
		"br":         {},
		"caption":    {},
		"cite":       {},
		"code":       {},
		"dd":         {"id"},
		"del":        {},
		"dfn":        {},
		"dl":         {"id"},
		"dt":         {"id"},
		"em":         {},
		"figcaption": {},
		"figure":     {},
		"h1":         {"id"},
		"h2":         {"id"},
		"h3":         {"id"},
		"h4":         {"id"},
		"h5":         {"id"},
		"h6":         {"id"},
		"hr":         {},
		"i":          {},
		"iframe":     {"width", "height", "frameborder", "src", "allowfullscreen"},
		"img":        {"alt", "title", "src", "srcset", "sizes", "width", "height", "fetchpriority", "decoding"},
		"ins":        {},
		"kbd":        {},
		"li":         {"id"},
		"ol":         {"id"},
		"p":          {},
		"picture":    {},
		"pre":        {},
		"q":          {"cite"},
		"rp":         {},
		"rt":         {},
		"rtc":        {},
		"ruby":       {},
		"s":          {},
		"small":      {},
		"samp":       {},
		"source":     {"src", "type", "srcset", "sizes", "media"},
		"strong":     {},
		"sub":        {},
		"sup":        {"id"},
		"table":      {},
		"td":         {"rowspan", "colspan"},
		"tfoot":      {},
		"th":         {"rowspan", "colspan"},
		"thead":      {},
		"time":       {"datetime"},
		"tr":         {},
		"u":          {},
		"ul":         {"id"},
		"var":        {},
		"video":      {"poster", "height", "width", "src"},
		"wbr":        {},

		// MathML.
		"annotation":     {},
		"annotation-xml": {},
		"maction":        {},
		"math":           {"xmlns"},
		"merror":         {},
		"mfrac":          {},
		"mi":             {},
		"mmultiscripts":  {},
		"mn":             {},
		"mo":             {},
		"mover":          {},
		"mpadded":        {},
		"mphantom":       {},
		"mprescripts":    {},
		"mroot":          {},
		"mrow":           {},
		"ms":             {},
		"mspace":         {},
		"msqrt":          {},
		"mstyle":         {},
		"msub":           {},
		"msubsup":        {},
		"msup":           {},
		"mtable":         {},
		"mtd":            {},
		"mtext":          {},
		"mtr":            {},
		"munder":         {},
		"munderover":     {},
		"semantics":      {},
	}

	iframeAllowList = map[string]struct{}{
		"bandcamp.com":         {},
		"cdn.embedly.com":      {},
		"dailymotion.com":      {},
		"framatube.org":        {},
		"open.spotify.com":     {},
		"player.bilibili.com":  {},
		"player.twitch.tv":     {},
		"player.vimeo.com":     {},
		"soundcloud.com":       {},
		"vk.com":               {},
		"w.soundcloud.com":     {},
		"youtube-nocookie.com": {},
		"youtube.com":          {},
	}

	blockedResourceURLSubstrings = []string{
		"api.flattr.com",
		"www.facebook.com/sharer.php",
		"feeds.feedburner.com",
		"feedsportal.com",
		"linkedin.com/shareArticle",
		"pinterest.com/pin/create/button/",
		"stats.wordpress.com",
		"twitter.com/intent/tweet",
		"twitter.com/share",
		"x.com/intent/tweet",
		"x.com/share",
	}

	validURISchemes = []string{
		"https:",
		"http:",
		"apt:",
		"bitcoin:",
		"callto:",
		"dav:",
		"davs:",
		"ed2k:",
		"facetime:",
		"feed:",
		"ftp:",
		"geo:",
		"git:",
		"gopher:",
		"irc:",
		"irc6:",
		"ircs:",
		"itms-apps:",
		"itms:",
		"magnet:",
		"mailto:",
		"news:",
		"nntp:",
		"rtmp:",
		"sftp:",
		"sip:",
		"sips:",
		"shortcuts:",
		"skype:",
		"spotify:",
		"ssh:",
		"steam:",
		"svn:",
		"svn+ssh:",
		"tel:",
		"webcal:",
		"xmpp:",
		"opener:",
		"hack:",
	}

	dataAttributeAllowedPrefixes = []string{
		"data:image/avif",
		"data:image/apng",
		"data:image/png",
		"data:image/svg",
		"data:image/svg+xml",
		"data:image/jpg",
		"data:image/jpeg",
		"data:image/gif",
		"data:image/webp",
	}
)

// SanitizerOptions holds options for the HTML sanitizer.
type SanitizerOptions struct {
	OpenLinksInNewTab bool
}

// SanitizeHTML takes raw HTML input and removes any disallowed tags and
// attributes. The output is rendered as a string.
func SanitizeHTML(baseURL, rawHTML string, opts *SanitizerOptions) string {
	if opts == nil {
		opts = &SanitizerOptions{}
	}
	var buffer strings.Builder
	buffer.Grow(len(rawHTML) * 3 / 4)

	// Wrap in <body> so html.Parse treats the input as a fragment.
	doc, err := html.Parse(io.MultiReader(
		strings.NewReader("<body>"),
		strings.NewReader(rawHTML),
		strings.NewReader("</body>"),
	))
	if err != nil {
		return ""
	}

	body := doc.FirstChild.FirstChild.NextSibling
	parsedBaseURL, _ := url.Parse(baseURL)
	for c := body.FirstChild; c != nil; c = c.NextSibling {
		if err := filterAndRenderHTML(&buffer, c, parsedBaseURL, opts, sanitizerMaxDepth-2); err != nil {
			return ""
		}
	}

	return buffer.String()
}

func findAllowedIframeSourceDomain(iframeSourceURL string) (string, bool) {
	domain := domainWithoutWWW(iframeSourceURL)
	if _, ok := iframeAllowList[domain]; ok {
		return domain, true
	}
	return "", false
}

func filterAndRenderHTML(buf *strings.Builder, n *html.Node, parsedBaseURL *url.URL, opts *SanitizerOptions, depth uint) error {
	if n == nil {
		return nil
	}
	if depth == 0 {
		return errors.New("maximum nested tags limit reached")
	}

	switch n.Type {
	case html.TextNode:
		buf.WriteString(html.EscapeString(n.Data))
	case html.ElementNode:
		tag := n.Data
		if shouldIgnoreTag(n, tag) {
			return nil
		}

		if _, ok := allowedHTMLTagsAndAttributes[tag]; !ok {
			// Tag isn't allowed, but its children may still be.
			return filterAndRenderHTMLChildren(buf, n, parsedBaseURL, opts, depth-1)
		}

		htmlAttributes, hasAllRequired := sanitizeAttributes(parsedBaseURL, tag, n.Attr, opts)
		if !hasAllRequired {
			if tag == "iframe" {
				// Drop blocked iframes wholesale.
				return nil
			}
			return filterAndRenderHTMLChildren(buf, n, parsedBaseURL, opts, depth-1)
		}
		buf.WriteByte('<')
		buf.WriteString(n.Data)
		if htmlAttributes != "" {
			buf.WriteByte(' ')
			buf.WriteString(htmlAttributes)
		}
		buf.WriteByte('>')

		if isSelfContainedTag(tag) {
			return nil
		}

		if tag != "iframe" {
			filterAndRenderHTMLChildren(buf, n, parsedBaseURL, opts, depth-1)
		}

		buf.WriteString("</")
		buf.WriteString(n.Data)
		buf.WriteByte('>')
	default:
	}
	return nil
}

func filterAndRenderHTMLChildren(buf *strings.Builder, n *html.Node, parsedBaseURL *url.URL, opts *SanitizerOptions, depth uint) error {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if err := filterAndRenderHTML(buf, c, parsedBaseURL, opts, depth); err != nil {
			return err
		}
	}
	return nil
}

func hasRequiredAttributes(s *mandatoryAttributesStruct, tagName string) bool {
	switch tagName {
	case "a":
		return s.href
	case "iframe":
		return s.src
	case "source", "img":
		return s.src || s.srcset
	}
	return true
}

func hasValidURIScheme(absoluteURL string) bool {
	for _, scheme := range validURISchemes {
		if strings.HasPrefix(absoluteURL, scheme) {
			return true
		}
	}
	return false
}

func isBlockedResource(absoluteURL string) bool {
	for _, blocked := range blockedResourceURLSubstrings {
		if strings.Contains(absoluteURL, blocked) {
			return true
		}
	}
	return false
}

func isBlockedTag(tagName string) bool {
	switch tagName {
	case "noscript", "script", "style":
		return true
	}
	return false
}

func isExternalResourceAttribute(attribute string) bool {
	switch attribute {
	case "src", "href", "poster", "cite":
		return true
	}
	return false
}

func isHidden(n *html.Node) bool {
	for _, attr := range n.Attr {
		if attr.Key == "hidden" {
			return true
		}
	}
	return false
}

func isPixelTracker(tagName string, attributes []html.Attribute) bool {
	if tagName != "img" {
		return false
	}
	hasHeight := false
	hasWidth := false
	for _, attribute := range attributes {
		if attribute.Val == "1" || attribute.Val == "0" {
			switch attribute.Key {
			case "height":
				hasHeight = true
			case "width":
				hasWidth = true
			}
		}
	}
	return hasHeight && hasWidth
}

func isPositiveInteger(value string) bool {
	if value == "" {
		return false
	}
	if n, err := strconv.Atoi(value); err == nil {
		return n > 0
	}
	return false
}

func isSelfContainedTag(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input",
		"link", "meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}

func isValidDataAttribute(value string) bool {
	for _, prefix := range dataAttributeAllowedPrefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func isValidDecodingValue(value string) bool {
	switch value {
	case "sync", "async", "auto":
		return true
	}
	return false
}

func isValidFetchPriorityValue(value string) bool {
	switch value {
	case "high", "low", "auto":
		return true
	}
	return false
}

type mandatoryAttributesStruct struct {
	href   bool
	src    bool
	srcset bool
}

func trackAttributes(s *mandatoryAttributesStruct, attributeName string) {
	switch attributeName {
	case "href":
		s.href = true
	case "src":
		s.src = true
	case "srcset":
		s.srcset = true
	}
}

func sanitizeAttributes(parsedBaseURL *url.URL, tagName string, attributes []html.Attribute, opts *SanitizerOptions) (string, bool) {
	htmlAttrs := make([]string, 0, len(attributes))
	mandatoryAttributes := mandatoryAttributesStruct{}
	var isAnchorLink bool

	allowedAttributes := allowedHTMLTagsAndAttributes[tagName]

	for _, attribute := range attributes {
		if !slices.Contains(allowedAttributes, attribute.Key) {
			continue
		}
		value := attribute.Val

		switch tagName {
		case "math":
			if attribute.Key == "xmlns" && value != "http://www.w3.org/1998/Math/MathML" {
				value = "http://www.w3.org/1998/Math/MathML"
			}
		case "img":
			switch attribute.Key {
			case "fetchpriority":
				if !isValidFetchPriorityValue(value) {
					continue
				}
			case "decoding":
				if !isValidDecodingValue(value) {
					continue
				}
			case "width", "height":
				if !isPositiveInteger(value) {
					continue
				}
			case "srcset":
				value = sanitizeSrcsetAttr(parsedBaseURL, value)
				if value == "" {
					continue
				}
			}
		case "source":
			if attribute.Key == "srcset" {
				value = sanitizeSrcsetAttr(parsedBaseURL, value)
				if value == "" {
					continue
				}
			}
		}

		if isExternalResourceAttribute(attribute.Key) {
			switch {
			case tagName == "iframe":
				_, trusted := findAllowedIframeSourceDomain(attribute.Val)
				if !trusted {
					return "", false
				}
				value = attribute.Val
			case tagName == "img" && attribute.Key == "src" && isValidDataAttribute(attribute.Val):
				value = attribute.Val
			case tagName == "a" && attribute.Key == "href" && strings.HasPrefix(attribute.Val, "#"):
				value = attribute.Val
				isAnchorLink = true
			default:
				if isBlockedResource(value) {
					return "", false
				}
				abs, err := resolveToAbsoluteURL(parsedBaseURL, value)
				if err != nil {
					continue
				}
				value = abs
				if !hasValidURIScheme(value) {
					continue
				}
			}
		}

		trackAttributes(&mandatoryAttributes, attribute.Key)
		htmlAttrs = append(htmlAttrs, attribute.Key+`="`+html.EscapeString(value)+`"`)
	}

	if !hasRequiredAttributes(&mandatoryAttributes, tagName) {
		return "", false
	}

	if !isAnchorLink {
		switch tagName {
		case "a":
			htmlAttrs = append(htmlAttrs, `rel="noopener noreferrer"`, `referrerpolicy="no-referrer"`)
			if opts.OpenLinksInNewTab {
				htmlAttrs = append(htmlAttrs, `target="_blank"`)
			}
		case "video", "audio":
			htmlAttrs = append(htmlAttrs, "controls")
		case "iframe":
			htmlAttrs = append(htmlAttrs, `sandbox="allow-scripts allow-same-origin allow-popups allow-popups-to-escape-sandbox"`, `loading="lazy"`)
		case "img":
			htmlAttrs = append(htmlAttrs, `loading="lazy"`)
		}
	}

	return strings.Join(htmlAttrs, " "), true
}

func sanitizeSrcsetAttr(parsedBaseURL *url.URL, value string) string {
	candidates := parseSrcSetAttribute(value)
	if len(candidates) == 0 {
		return ""
	}
	out := make([]*imageCandidate, 0, len(candidates))
	for _, c := range candidates {
		abs, err := resolveToAbsoluteURL(parsedBaseURL, c.ImageURL)
		if err != nil {
			continue
		}
		if !hasValidURIScheme(abs) || isBlockedResource(abs) {
			continue
		}
		c.ImageURL = abs
		out = append(out, c)
	}
	return imageCandidates(out).String()
}

func shouldIgnoreTag(n *html.Node, tag string) bool {
	if isPixelTracker(tag, n.Attr) {
		return true
	}
	if isBlockedTag(tag) {
		return true
	}
	if isHidden(n) {
		return true
	}
	return false
}

// resolveToAbsoluteURL resolves a possibly-relative URL against parsedBaseURL.
// Replaces miniflux/v2 internal/urllib.ResolveToAbsoluteURLWithParsedBaseURL.
func resolveToAbsoluteURL(parsedBaseURL *url.URL, ref string) (string, error) {
	if strings.HasPrefix(ref, "//") {
		return "https:" + ref, nil
	}
	if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
		return ref, nil
	}
	parsedRef, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	if parsedRef.IsAbs() {
		return ref, nil
	}
	if parsedBaseURL == nil {
		return ref, nil
	}
	return parsedBaseURL.ResolveReference(parsedRef).String(), nil
}

// domainWithoutWWW returns the host of u (sans leading "www."). Replaces
// miniflux/v2 internal/urllib.DomainWithoutWWW.
func domainWithoutWWW(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	return strings.TrimPrefix(parsed.Host, "www.")
}
