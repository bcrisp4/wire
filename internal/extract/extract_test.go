package extract_test

import (
	"context"
	"strings"
	"testing"

	"github.com/bcrisp4/wire/internal/extract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noisyArticle wraps article HTML in a navigation/sidebar/script soup so the
// readability fallback has work to do.
const noisyArticle = `<!doctype html>
<html><head><title>Sample</title></head>
<body>
<header><nav>home about contact</nav></header>
<aside class="sidebar"><ul><li>related</li><li>links</li></ul></aside>
<script>window.tracker = true;</script>
<main>
<article>
<h1>The Headline of the Article</h1>
<p>This is the very first paragraph of substantive prose. It contains enough
words that Readability will consider it the document's main content. We are
deliberately filling it with prose so the heuristic scoring picks this body
over the navigation chrome. Readability needs at least a few hundred
characters of text to commit to a candidate.</p>
<p>This is the second paragraph, also with quite a lot of words to bring the
total length comfortably above Readability's character threshold. The library
walks the DOM, scores each candidate by text density, and then renders the
winner. We want to see this body come through cleanly with the script tag
stripped and the navigation gone.</p>
<p>A final paragraph for good measure, padding the article body with yet
more prose. The reading-time computation should round up to at least one
minute regardless of length, but with three full paragraphs we will exceed
the per-minute word budget in any case.</p>
</article>
</main>
<footer>copyright 2026</footer>
</body></html>`

func TestExtract_CustomRule(t *testing.T) {
	html := `<html><body>
<nav>nav</nav>
<div class="article-body"><p>hello <b>world</b> from custom rule</p></div>
<aside>aside content</aside>
</body></html>`
	res, err := extract.Extract(context.Background(), "https://example.com/post", html, "div.article-body")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Contains(t, res.Content, "hello")
	assert.Contains(t, res.Content, "<b>world</b>")
	// Custom rule must NOT pull in the aside or nav.
	assert.NotContains(t, res.Content, "aside content")
	assert.NotContains(t, res.Content, "nav")
}

func TestExtract_PredefinedSameSiteRule(t *testing.T) {
	// arstechnica.com → div.post-content per Miniflux rules
	html := `<html><body>
<header>nav stuff</header>
<div class="post-content"><p>article body via predefined rule</p></div>
<footer>footer</footer>
</body></html>`
	res, err := extract.Extract(context.Background(), "https://arstechnica.com/some-article", html, "")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Contains(t, res.Content, "article body via predefined rule")
	assert.NotContains(t, res.Content, "nav stuff")
}

func TestExtract_ReadabilityFallback(t *testing.T) {
	res, err := extract.Extract(context.Background(), "https://unknown.example/post", noisyArticle, "")
	require.NoError(t, err)
	require.NotNil(t, res)
	// Readability should retain the article paragraphs.
	assert.Contains(t, res.Content, "first paragraph of substantive prose")
	// And drop the script.
	assert.NotContains(t, res.Content, "window.tracker")
	// Reading time is at least 1 minute.
	assert.GreaterOrEqual(t, res.ReadingTime, 1)
}

func TestExtract_SanitizerStripsDangerousTags(t *testing.T) {
	html := `<html><body>
<div class="article-body">
<p>safe paragraph</p>
<script>alert('xss')</script>
<a href="https://example.com" onclick="alert(1)">link</a>
<iframe src="https://evil.example/embed"></iframe>
</div>
</body></html>`
	res, err := extract.Extract(context.Background(), "https://example.com/p", html, "div.article-body")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.NotContains(t, res.Content, "<script")
	assert.NotContains(t, res.Content, "alert(")
	assert.NotContains(t, res.Content, "onclick")
	// iframe pointing at non-allowlisted domain should be removed.
	assert.NotContains(t, res.Content, "evil.example")
	// The safe content should still be there.
	assert.Contains(t, res.Content, "safe paragraph")
	// Anchor stays but loses inline handler.
	assert.Contains(t, res.Content, `href="https://example.com"`)
}

func TestExtract_ReadingTimeCeilWordsOver250(t *testing.T) {
	// Build content with exactly 501 words — should be ceil(501/250) = 3 minutes.
	body := strings.Repeat("word ", 501)
	html := `<html><body><div class="article-body"><p>` + body + `</p></div></body></html>`
	res, err := extract.Extract(context.Background(), "https://example.com/long", html, "div.article-body")
	require.NoError(t, err)
	assert.Equal(t, 3, res.ReadingTime)
}

func TestExtract_ReadingTimeMinimumOne(t *testing.T) {
	html := `<html><body><div class="article-body"><p>tiny</p></div></body></html>`
	res, err := extract.Extract(context.Background(), "https://example.com/t", html, "div.article-body")
	require.NoError(t, err)
	assert.Equal(t, 1, res.ReadingTime)
}

func TestExtract_GarbageInputReturnsErrorNotPanic(t *testing.T) {
	// Empty body — readability has nothing to work with, custom/predefined paths
	// have no matching node either; expect a wrapped error rather than panic.
	_, err := extract.Extract(context.Background(), "https://example.com/empty", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "extract:")
}
