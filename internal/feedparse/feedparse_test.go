package feedparse

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const rss2Sample = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example Blog</title>
    <link>https://example.com/</link>
    <description>An example blog</description>
    <item>
      <title>First Post</title>
      <link>https://example.com/posts/first</link>
      <description>Hello, world.</description>
      <author>alice@example.com (Alice)</author>
      <pubDate>Mon, 02 Jan 2006 15:04:05 -0700</pubDate>
    </item>
    <item>
      <title>Second Post</title>
      <link>https://example.com/posts/second</link>
      <description>Another entry.</description>
      <pubDate>Tue, 03 Jan 2006 15:04:05 -0700</pubDate>
    </item>
  </channel>
</rss>`

const atomSample = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Atom Example</title>
  <link href="https://atom.example.com/"/>
  <id>https://atom.example.com/</id>
  <updated>2026-01-02T15:04:05Z</updated>
  <entry>
    <title>Atom Entry One</title>
    <id>https://atom.example.com/entries/1</id>
    <link href="https://atom.example.com/entries/1"/>
    <published>2026-01-02T15:04:05Z</published>
    <updated>2026-01-02T15:04:05Z</updated>
    <summary>Summary one.</summary>
    <author><name>Bob</name></author>
  </entry>
</feed>`

const jsonFeedSample = `{
  "version": "https://jsonfeed.org/version/1.1",
  "title": "JSON Feed Example",
  "home_page_url": "https://json.example.com/",
  "feed_url": "https://json.example.com/feed.json",
  "description": "A JSON feed",
  "items": [
    {
      "id": "https://json.example.com/items/1",
      "url": "https://json.example.com/items/1",
      "title": "JSON Item",
      "summary": "JSON summary",
      "date_published": "2026-02-03T04:05:06Z",
      "authors": [{"name": "Carol"}]
    }
  ]
}`

const rssRelativeLinks = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Relative Links</title>
    <link>/</link>
    <item>
      <title>Relative Entry</title>
      <link>/posts/relative</link>
      <description>desc</description>
    </item>
  </channel>
</rss>`

const rssNoPubDate = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>No Dates</title>
    <link>https://nodates.example.com/</link>
    <item>
      <title>Undated</title>
      <link>https://nodates.example.com/p/1</link>
      <description>no date</description>
    </item>
  </channel>
</rss>`

func TestParse_RSS2(t *testing.T) {
	ctx := context.Background()
	res, err := Parse(ctx, []byte(rss2Sample), "https://example.com/feed.xml")
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "Example Blog", res.Title)
	assert.Equal(t, "https://example.com/", res.SiteURL)
	assert.Equal(t, "An example blog", res.Description)
	require.Len(t, res.Entries, 2)

	first := res.Entries[0]
	assert.Equal(t, "First Post", first.Title)
	require.NotNil(t, first.URL)
	assert.Equal(t, "https://example.com/posts/first", *first.URL)
	require.NotNil(t, first.Summary)
	assert.Equal(t, "Hello, world.", *first.Summary)
	require.NotNil(t, first.Author)
	assert.Contains(t, *first.Author, "Alice")
	require.NotNil(t, first.PublishedAt)
	assert.NotEmpty(t, first.Hash)
	assert.Nil(t, first.Content)

	// Hash should be stable across calls.
	res2, err := Parse(ctx, []byte(rss2Sample), "https://example.com/feed.xml")
	require.NoError(t, err)
	require.Len(t, res2.Entries, 2)
	assert.Equal(t, first.Hash, res2.Entries[0].Hash)
	assert.Equal(t, res.Entries[1].Hash, res2.Entries[1].Hash)

	// Distinct entries have distinct hashes.
	assert.NotEqual(t, res.Entries[0].Hash, res.Entries[1].Hash)
}

func TestParse_Atom(t *testing.T) {
	ctx := context.Background()
	res, err := Parse(ctx, []byte(atomSample), "https://atom.example.com/feed")
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "Atom Example", res.Title)
	assert.Equal(t, "https://atom.example.com/", res.SiteURL)
	require.Len(t, res.Entries, 1)

	e := res.Entries[0]
	assert.Equal(t, "Atom Entry One", e.Title)
	require.NotNil(t, e.URL)
	assert.Equal(t, "https://atom.example.com/entries/1", *e.URL)
	require.NotNil(t, e.PublishedAt)
	require.NotNil(t, e.Author)
	assert.Equal(t, "Bob", *e.Author)
	require.NotNil(t, e.Summary)
	assert.Equal(t, "Summary one.", *e.Summary)
	assert.NotEmpty(t, e.Hash)
}

func TestParse_JSONFeed(t *testing.T) {
	ctx := context.Background()
	res, err := Parse(ctx, []byte(jsonFeedSample), "https://json.example.com/feed.json")
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "JSON Feed Example", res.Title)
	assert.Equal(t, "https://json.example.com/", res.SiteURL)
	require.Len(t, res.Entries, 1)

	e := res.Entries[0]
	assert.Equal(t, "JSON Item", e.Title)
	require.NotNil(t, e.URL)
	assert.Equal(t, "https://json.example.com/items/1", *e.URL)
	require.NotNil(t, e.PublishedAt)
	require.NotNil(t, e.Summary)
	assert.Equal(t, "JSON summary", *e.Summary)
	require.NotNil(t, e.Author)
	assert.Equal(t, "Carol", *e.Author)
}

func TestParse_RelativeLinksResolved(t *testing.T) {
	ctx := context.Background()
	res, err := Parse(ctx, []byte(rssRelativeLinks), "https://relative.example.com/feed.xml")
	require.NoError(t, err)
	require.Len(t, res.Entries, 1)

	require.NotNil(t, res.Entries[0].URL)
	assert.Equal(t, "https://relative.example.com/posts/relative", *res.Entries[0].URL)
}

func TestParse_MissingPublishedAt(t *testing.T) {
	ctx := context.Background()
	res, err := Parse(ctx, []byte(rssNoPubDate), "https://nodates.example.com/feed.xml")
	require.NoError(t, err)
	require.Len(t, res.Entries, 1)
	assert.Nil(t, res.Entries[0].PublishedAt)
}

func TestEntryHash_Deterministic(t *testing.T) {
	ts := int64(1700000000)
	h1 := EntryHash("https://example.com/post", "Hello", &ts)
	h2 := EntryHash("https://example.com/post", "Hello", &ts)
	assert.Equal(t, h1, h2)
	assert.NotEmpty(t, h1)
	assert.Len(t, h1, 64) // sha256 hex
}

func TestEntryHash_HostCaseInsensitive(t *testing.T) {
	ts := int64(1700000000)
	lower := EntryHash("https://example.com/Post", "Title", &ts)
	upper := EntryHash("HTTPS://EXAMPLE.COM/Post", "Title", &ts)
	mixed := EntryHash("Https://Example.COM/Post", "Title", &ts)
	assert.Equal(t, lower, upper)
	assert.Equal(t, lower, mixed)
}

func TestEntryHash_PathCaseSensitive(t *testing.T) {
	// Path component differences should produce different hashes.
	ts := int64(1700000000)
	a := EntryHash("https://example.com/post", "Title", &ts)
	b := EntryHash("https://example.com/POST", "Title", &ts)
	assert.NotEqual(t, a, b)
}

func TestEntryHash_NilPublishedAt(t *testing.T) {
	a := EntryHash("https://example.com/p", "Title", nil)
	b := EntryHash("https://example.com/p", "Title", nil)
	assert.Equal(t, a, b)

	ts := int64(1700000000)
	c := EntryHash("https://example.com/p", "Title", &ts)
	assert.NotEqual(t, a, c)
}

func TestParse_InvalidFeed(t *testing.T) {
	ctx := context.Background()
	_, err := Parse(ctx, []byte("not a feed"), "https://example.com/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "feedparse:")
}
