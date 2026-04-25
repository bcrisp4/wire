package opml

import (
	"bytes"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sortSubs(s []Subscription) {
	sort.Slice(s, func(i, j int) bool { return s[i].FeedURL < s[j].FeedURL })
}

func TestParse_FlatList(t *testing.T) {
	doc := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Test</title></head>
  <body>
    <outline type="rss"  xmlUrl="https://a.example/rss"  htmlUrl="https://a.example" title="A" />
    <outline type="atom" xmlUrl="https://b.example/atom" htmlUrl="https://b.example" title="B" />
  </body>
</opml>`
	subs, err := Parse(strings.NewReader(doc))
	require.NoError(t, err)
	require.Len(t, subs, 2)
	sortSubs(subs)
	assert.Equal(t, Subscription{FeedURL: "https://a.example/rss", SiteURL: "https://a.example", Title: "A"}, subs[0])
	assert.Equal(t, Subscription{FeedURL: "https://b.example/atom", SiteURL: "https://b.example", Title: "B"}, subs[1])
}

func TestParse_NestedCategories(t *testing.T) {
	doc := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Test</title></head>
  <body>
    <outline title="News">
      <outline type="rss"  xmlUrl="https://hn.example/rss"  title="HN" />
      <outline type="atom" xmlUrl="https://verge.example/feed" title="Verge" />
    </outline>
    <outline type="rss" xmlUrl="https://orphan.example/rss" title="Orphan" />
  </body>
</opml>`
	subs, err := Parse(strings.NewReader(doc))
	require.NoError(t, err)
	require.Len(t, subs, 3)
	sortSubs(subs)
	assert.Equal(t, "News", subs[0].Category)
	assert.Equal(t, "https://hn.example/rss", subs[0].FeedURL)
	assert.Equal(t, "News", subs[2].Category)
	assert.Equal(t, "https://verge.example/feed", subs[2].FeedURL)
	assert.Equal(t, "", subs[1].Category)
	assert.Equal(t, "https://orphan.example/rss", subs[1].FeedURL)
}

func TestParse_IgnoresOutlinesWithoutXmlUrl(t *testing.T) {
	doc := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Test</title></head>
  <body>
    <outline text="A bookmark" type="link" url="https://example.com/" />
    <outline type="rss" xmlUrl="https://feed.example/rss" title="Feed" />
  </body>
</opml>`
	subs, err := Parse(strings.NewReader(doc))
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, "https://feed.example/rss", subs[0].FeedURL)
}

func TestParse_TitleFallsBackToText(t *testing.T) {
	// OPML 1.0 used `text`; 2.0 added `title`. Honour either.
	doc := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="1.1">
  <head><title>Test</title></head>
  <body>
    <outline type="rss" xmlUrl="https://x.example/rss" text="Just Text" />
  </body>
</opml>`
	subs, err := Parse(strings.NewReader(doc))
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, "Just Text", subs[0].Title)
}

func TestParse_MalformedXMLErrors(t *testing.T) {
	_, err := Parse(strings.NewReader("<opml><body><outline></opml>"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "opml:")
}

func TestWrite_RoundTrip(t *testing.T) {
	in := []Subscription{
		{FeedURL: "https://hn.example/rss", SiteURL: "https://hn.example", Title: "HN", Category: "News"},
		{FeedURL: "https://verge.example/feed", SiteURL: "https://verge.example", Title: "Verge", Category: "News"},
		{FeedURL: "https://orphan.example/rss", SiteURL: "https://orphan.example", Title: "Orphan"},
	}
	var buf bytes.Buffer
	require.NoError(t, Write(&buf, in))

	out, err := Parse(&buf)
	require.NoError(t, err)
	require.Len(t, out, len(in))

	sortSubs(in)
	sortSubs(out)
	assert.Equal(t, in, out)
}

func TestWrite_EmitsValidOPML2(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, Write(&buf, []Subscription{
		{FeedURL: "https://a.example/rss", Title: "A"},
	}))
	body := buf.String()
	assert.Contains(t, body, `<?xml`)
	assert.Contains(t, body, `<opml version="2.0">`)
	assert.Contains(t, body, `xmlUrl="https://a.example/rss"`)
}
