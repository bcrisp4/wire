# Readability / Content Extraction — Pre-Implementation Research

> Captured during Phase 0. Phase 0 ships **no extraction code**. This document
> records the Phase 1 decision so the work can be picked up directly by a
> follow-up worker without re-doing the research.

## Decision

Use **`codeberg.org/readeck/go-readability/v2`** as the extraction core.
**Vendor selectively from Miniflux** — only the parts that are actually
better than reaching for a library. **Do not** lift Miniflux's
`internal/reader/readability/` wholesale.

### Use Readeck go-readability for the core extraction

`codeberg.org/readeck/go-readability/v2` is the actively maintained Go port
of Mozilla's Readability.js (tracking v0.6 parity). It is the de-facto
successor to the now-archived `go-shiori/go-readability`. License: MIT.

Entry point: `readability.New(httpClient, url, body).Parse()` returning a
struct with cleaned `Content`, `TextContent`, `Title`, `Excerpt`, etc.

### Vendor from Miniflux (Apache-2.0)

- **`internal/reader/scraper/rules.go`** — ~50 hardcoded
  `domain → CSS selector` defaults (e.g. `arstechnica.com → div.post-content`).
  Pure data, copy verbatim.
- **The same-site rules-then-readability dispatcher** (~40 lines from
  `scraper.go`) — applies custom CSS selectors first when the site domain
  matches; falls back to Readability otherwise.
- **`internal/reader/sanitizer/sanitizer.go`** — stdlib-only
  (`golang.org/x/net/html`) HTML allowlist sanitizer. No bluemonday dep.
  Useful if we want to keep the dependency surface small.

License: Apache-2.0. Preserve `LICENSE` and per-file
`// SPDX-License-Identifier: Apache-2.0` headers; add a `NOTICE` if we
redistribute.

### Skip from Miniflux

- `internal/reader/readability/readability.go` — competent but older heuristic
  port (no Readability.js v0.6 parity). The Readeck fork is better maintained
  and tracks upstream.
- `internal/reader/scraper/scraper.go` orchestration code that drags in
  Miniflux's `fetcher` and global config — not a clean lift.

## Failure handling

Miniflux's pattern: on scrape error, log a warning and **keep the
feed-provided summary as the entry's content**. Don't drop the entry. We will
follow the same pattern in Phase 1.

## Image handling

Miniflux's image-proxy logic lives separately in `internal/mediaproxy/`
(`RewriteDocumentWithRelativeProxyURL`, `RewriteDocumentWithAbsoluteProxyURL`)
and rewrites `img.src`, `img.srcset`, `audio/video src`, `video poster`.
Modes: `all` (proxy http+https), default (http only), `none`.

This is a Phase 1+ concern — Wire's design doesn't currently call out an
image proxy. Defer until we have a use case.

## Phase 1 worker shape (sketch)

```go
// internal/extract/extract.go
type Extracted struct {
    Content     string  // sanitized HTML
    ReadingTime int     // minutes
    Image       string  // optional cover image URL
}

func ExtractEntry(ctx context.Context, url, html string, customRules string) (*Extracted, error) {
    // 1. If customRules set, try CSS-selector extraction (vendored Miniflux dispatcher).
    // 2. If same-site predefined rule exists, try that.
    // 3. Else, fall through to Readeck go-readability.
    // 4. Sanitize the result via vendored sanitizer (or bluemonday).
    // 5. Compute reading time (words / 250).
    // 6. Return *Extracted; nil + error if everything failed.
}
```

`feed.poll` queues `entry.extract` jobs for each new entry on feeds where
`crawler=1`. The extract worker calls `ExtractEntry` and updates
`entries.content` / `entries.reading_time`. On failure it logs and leaves the
feed-provided summary in place.

## References

- Readeck fork: <https://codeberg.org/readeck/go-readability>
- Miniflux v2: <https://github.com/miniflux/v2>
- Miniflux scraper: <https://github.com/miniflux/v2/blob/main/internal/reader/scraper/scraper.go>
- Miniflux rules.go: <https://github.com/miniflux/v2/blob/main/internal/reader/scraper/rules.go>
- Miniflux sanitizer: <https://github.com/miniflux/v2/blob/main/internal/reader/sanitizer/sanitizer.go>
