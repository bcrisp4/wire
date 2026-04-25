// Package opml parses and emits OPML 2.0 subscription documents.
//
// Wire uses OPML solely for feed import/export. Recognised outlines have a
// non-empty xmlUrl and (optionally) a type of "rss" or "atom"; outlines with
// no xmlUrl that contain children are treated as category groups. Anything
// else (link bookmarks, free-form text outlines) is ignored.
package opml

import (
	"encoding/xml"
	"fmt"
	"io"
	"sort"
)

// Subscription is a flattened feed entry from an OPML document.
type Subscription struct {
	FeedURL  string
	SiteURL  string
	Title    string
	Category string // "" if uncategorised
}

// outline mirrors the OPML <outline> element. It self-references for nested
// category groups.
type outline struct {
	Type     string    `xml:"type,attr"`
	XMLURL   string    `xml:"xmlUrl,attr"`
	HTMLURL  string    `xml:"htmlUrl,attr"`
	Title    string    `xml:"title,attr"`
	Text     string    `xml:"text,attr"`
	Outlines []outline `xml:"outline"`
}

type opmlDoc struct {
	XMLName xml.Name `xml:"opml"`
	Body    struct {
		Outlines []outline `xml:"outline"`
	} `xml:"body"`
}

// Parse parses an OPML 2.0 document (best-effort for 1.x). Outlines without an
// xmlUrl that contain child outlines are treated as category groups; all other
// non-feed outlines are ignored.
func Parse(r io.Reader) ([]Subscription, error) {
	var doc opmlDoc
	dec := xml.NewDecoder(r)
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("opml: %w", err)
	}
	var subs []Subscription
	for _, o := range doc.Body.Outlines {
		walk(o, "", &subs)
	}
	return subs, nil
}

func walk(o outline, category string, out *[]Subscription) {
	if o.XMLURL != "" {
		title := o.Title
		if title == "" {
			title = o.Text
		}
		*out = append(*out, Subscription{
			FeedURL:  o.XMLURL,
			SiteURL:  o.HTMLURL,
			Title:    title,
			Category: category,
		})
		return
	}
	// No xmlUrl — treat as a category if it has children, else ignore.
	if len(o.Outlines) == 0 {
		return
	}
	groupName := o.Title
	if groupName == "" {
		groupName = o.Text
	}
	// Wire's data model has flat categories; deeper nested groups inherit
	// their nearest named ancestor.
	if groupName == "" {
		groupName = category
	}
	for _, c := range o.Outlines {
		walk(c, groupName, out)
	}
}

// writeOutline mirrors `outline` for marshalling. omitempty keeps empty
// htmlUrl / type attributes off category groups.
type writeOutline struct {
	XMLName  xml.Name       `xml:"outline"`
	Type     string         `xml:"type,attr,omitempty"`
	Title    string         `xml:"title,attr,omitempty"`
	Text     string         `xml:"text,attr,omitempty"`
	XMLURL   string         `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string         `xml:"htmlUrl,attr,omitempty"`
	Outlines []writeOutline `xml:"outline,omitempty"`
}

type writeDoc struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    struct {
		Title string `xml:"title"`
	} `xml:"head"`
	Body struct {
		Outlines []writeOutline `xml:"outline"`
	} `xml:"body"`
}

// Write emits a valid OPML 2.0 document. Subscriptions are grouped by
// Category; uncategorised feeds are emitted at the top of <body> (no
// surrounding group), keeping the document compact for the common case of a
// flat subscription list.
func Write(w io.Writer, subs []Subscription) error {
	var doc writeDoc
	doc.Version = "2.0"
	doc.Head.Title = "Wire subscriptions"

	groups := map[string][]Subscription{}
	for _, s := range subs {
		groups[s.Category] = append(groups[s.Category], s)
	}
	for _, s := range groups[""] {
		doc.Body.Outlines = append(doc.Body.Outlines, feedOutline(s))
	}
	names := make([]string, 0, len(groups))
	for name := range groups {
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		group := writeOutline{Title: name, Text: name}
		for _, s := range groups[name] {
			group.Outlines = append(group.Outlines, feedOutline(s))
		}
		doc.Body.Outlines = append(doc.Body.Outlines, group)
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return fmt.Errorf("opml: %w", err)
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("opml: %w", err)
	}
	return nil
}

func feedOutline(s Subscription) writeOutline {
	t := s.Title
	if t == "" {
		t = s.FeedURL
	}
	return writeOutline{
		Type:    "rss",
		Title:   t,
		Text:    t,
		XMLURL:  s.FeedURL,
		HTMLURL: s.SiteURL,
	}
}
